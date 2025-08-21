package validators

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"sync"
	"time"

	_ "embed"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	lru "github.com/hashicorp/golang-lru"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/builtin/staker/validation"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
)

type Service struct {
	stack *stack.Stack

	active   map[thor.Address]*validation.Validation
	queued   map[thor.Address]*validation.Validation
	exiting  map[thor.Address]*validation.Validation
	idLookup map[thor.Address]*validation.Validation
	exited   map[thor.Address]bool

	cache *lru.Cache
	mu    sync.Mutex
}

func NewState(stack *stack.Stack) *Service {
	cache, err := lru.New(1000) // Cache up to 1000 staker information entries
	if err != nil {
		slog.Error("Failed to create LRU cache for staker information", "error", err)
		return nil
	}
	s := &Service{
		stack:    stack,
		cache:    cache,
		active:   make(map[thor.Address]*validation.Validation),
		queued:   make(map[thor.Address]*validation.Validation),
		exiting:  make(map[thor.Address]*validation.Validation),
		exited:   make(map[thor.Address]bool),
		idLookup: make(map[thor.Address]*validation.Validation),
	}
	go s.poll()
	return s
}

func (s *Service) LookupAddress(id thor.Address) (*validation.Validation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.idLookup[id]
	return v, ok
}

func (s *Service) GetActiveCount() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return uint32(len(s.active))
}

func (s *Service) poll() {
	ticker := utils.NewTicker(s.stack.Client())
	for {
		select {
		case <-s.stack.Context().Done():
			return
		default:
			block, err := ticker.Wait(30 * time.Second)
			if err != nil {
				slog.Error("Failed to wait for best block", "error", err)
				time.Sleep(5 * time.Second) // Retry after a short delay
				continue
			}
			validators, err := s.FetchAll(block.ID)
			if err != nil {
				slog.Error("Failed to fetch validatorInfo", "error", err, "block", block.ID)
				continue
			}

			active := make(map[thor.Address]*validation.Validation)
			exiting := make(map[thor.Address]*validation.Validation)
			queued := make(map[thor.Address]*validation.Validation)
			for id, v := range validators {
				if v.Status == validation.StatusActive {
					if v.ExitBlock != nil {
						exiting[id] = v
					} else {
						active[id] = v
					}
				}
				if v.Status == validation.StatusQueued {
					queued[id] = v
				}
			}

			s.mu.Lock()
			s.active = active
			s.queued = queued
			s.exiting = exiting

			// first check previous lookup to find any validators that exited
			for id := range s.idLookup {
				_, ok := validators[id] // exists in current list
				if ok {
					continue
				}
				if s.exited[id] { // already updated status
					continue
				}
				validator, err := s.checkExited(id)
				if err != nil {
					slog.Warn("failed to find exited validator", "err", err)
				}
				s.idLookup[id] = validator
			}

			for id, val := range validators {
				s.idLookup[id] = val
			}

			//s.idLookup = idLookup

			slog.Info(" 🎖️ updated staker state",
				"active", len(s.active),
				"queued", len(s.queued),
				"exiting", len(s.exiting),
				"total", len(s.idLookup))
			s.mu.Unlock()
		}
	}
}

func (s *Service) checkExited(id thor.Address) (*validation.Validation, error) {
	stake, err := s.stack.Staker().GetValidation(id)
	if err != nil {
		return nil, err
	}
	periodDetails, err := s.stack.Staker().GetValidationPeriodDetails(id)
	if err != nil {
		return nil, err
	}
	withdrawable, err := s.stack.Staker().GetWithdrawable(id)
	if err != nil {
		return nil, err
	}

	v := &validation.Validation{
		Endorser:           stake.Endorser,
		Period:             periodDetails.Period,
		CompleteIterations: periodDetails.CompletedPeriods,
		Status:             validation.Status(stake.Status),
		StartBlock:         periodDetails.StartBlock,
		LockedVET:          stake.Stake,
		PendingUnlockVET:   big.NewInt(0),
		QueuedVET:          stake.QueuedStake,
		CooldownVET:        big.NewInt(0),
		WithdrawableVET:    withdrawable,
		Weight:             stake.Weight,
	}

	if periodDetails.ExitBlock != math.MaxUint32 {
		v.ExitBlock = &periodDetails.ExitBlock
	}
	if stake.OfflineBlock != math.MaxUint32 {
		v.OfflineBlock = &stake.OfflineBlock
	}

	return v, nil
}

//go:embed compiled/GetValidators.abi
var contractABI string

//go:embed compiled/GetValidators.bin
var bytecode string

// FetchAll retrieves all queued and active validators from the staker contract.
// Using a hacky getAll in a simulation. It means the method takes 35ms
// It takes 500ms if we iterate each validator on the client side
// The validators are ordered by their position in the active and queued groups. Ie FIFO.
// See `GetValidators.sol` for more details.
func (s *Service) FetchAll(blockID thor.Bytes32) (map[thor.Address]*validation.Validation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.cache.Get(blockID)
	if ok {
		return existing.(map[thor.Address]*validation.Validation), nil
	}
	if err := s.initABI(); err != nil {
		return nil, err
	}
	rawResult, err := s.fetchStakerInfo(blockID)
	if err != nil {
		return nil, err
	}
	result, err := s.unpackValidators(rawResult[1])
	if err != nil {
		return nil, err
	}
	s.cache.Add(blockID, result)
	return result, nil
}

func (s *Service) fetchStakerInfo(blockID thor.Bytes32) ([]*api.CallResult, error) {
	to := thor.MustParseAddress("0x841a6556c524d47030762eb14dc4af897e605d9b")
	selector := hexutil.Encode(getValidatorsABI.Id())
	slog.Info("getValidators", "selector", selector, "id", getValidatorsABI.Id(), "blockID", blockID)

	res, err := s.stack.Client().InspectClauses(&api.BatchCallData{
		Clauses: api.Clauses{
			{
				Data: "0x" + bytecode,
			},
			{
				To:   &to,
				Data: selector,
			},
		},
	}, thorclient.Revision(blockID.String()))
	if err != nil {
		return nil, err
	}
	expectedResultsLength := 2
	if len(res) != expectedResultsLength {
		// expect exactly expectedResultsLength results
		return nil, err
	}

	for _, r := range res {
		if r.Reverted || r.VMError != "" {
			return nil, errors.New("staker contract call reverted or had VM error: " + r.VMError)
		}
	}
	return res, nil
}

func (s *Service) unpackValidators(result *api.CallResult) (map[thor.Address]*validation.Validation, error) {
	bytes, err := hexutil.Decode(result.Data)
	if err != nil {
		return nil, err
	}
	out, err := getValidatorsABI.Outputs.UnpackValues(bytes)
	if err != nil {
		return nil, err
	}

	validators := make(map[thor.Address]*validation.Validation)
	masters := out[0].([]common.Address)
	endorsors := out[1].([]common.Address)
	statuses := out[2].([]uint8)
	offlineBlocks := out[4].([]uint32)
	stakingPeriodLengths := out[5].([]uint32)
	startBlocks := out[6].([]uint32)
	exitBlocks := out[7].([]uint32)
	completedPeriods := out[8].([]uint32)
	validatorLockedVETs := out[9].([]*big.Int)
	validatorLockedWeights := out[10].([]*big.Int)
	validatorQueuedStakes := out[12].([]*big.Int)

	for i, id := range masters {
		v := &validation.Validation{
			Endorser:           (thor.Address)(endorsors[i]),
			Beneficiary:        nil, // Beneficiary is not used in this context
			Period:             stakingPeriodLengths[i],
			CompleteIterations: completedPeriods[i],
			Status:             statuses[i],
			StartBlock:         startBlocks[i],
			LockedVET:          validatorLockedVETs[i],
			PendingUnlockVET:   big.NewInt(0),
			CooldownVET:        big.NewInt(0),
			WithdrawableVET:    big.NewInt(0),
			QueuedVET:          validatorQueuedStakes[i],
			Weight:             validatorLockedWeights[i],
		}
		if exitBlocks[i] != uint32(math.MaxUint32) {
			v.ExitBlock = &exitBlocks[i]
		}
		if offlineBlocks[i] != uint32(math.MaxUint32) {
			v.OfflineBlock = &offlineBlocks[i]
		}

		validators[thor.Address(id)] = v
	}

	return validators, nil
}

var getValidatorsABI abi.Method
var once sync.Once

func (s *Service) initABI() error {
	var err error
	var ok bool
	once.Do(func() {
		var helperABI abi.ABI
		helperABI, err = abi.JSON(bytes.NewReader([]byte(contractABI)))
		if err != nil {
			slog.Error("Failed to parse staker contract ABI", "error", err)
			return
		}
		getValidatorsABI, ok = helperABI.Methods["getValidators"]
		if !ok {
			err = fmt.Errorf("getValidatorsABI method not found in staker contract ABI")
			slog.Error("Failed to find getValidatorsABI method", "error", err)
			return
		}
	})
	return err
}
