package validations

import (
	"fmt"
	utils2 "github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/api"
	"log/slog"
	"math"
	"math/big"
	"math/rand"
	"sync"
	"time"

	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/utils"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

var (
	Eth        = big.NewInt(1e18)                               // 1 ETH in wei
	MillionETH = big.NewInt(0).Mul(big.NewInt(1e6), Eth)        // 1 million ETH in wei
	MaxStake   = big.NewInt(0).Mul(big.NewInt(600), MillionETH) // 600 million VET in wei
	MinStake   = big.NewInt(0).Mul(big.NewInt(25), MillionETH)  // 1 million VET in wei
)

func RandomStake() *big.Int {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	max := big.NewInt(0).Div(MaxStake, big.NewInt(2))

	// Calculate the range (max - MinStake)
	rangeStake := new(big.Int).Sub(max, MinStake)

	// Generate a random number within the range
	randomOffset := new(big.Int).Rand(rng, rangeStake)

	// Add MinStake to ensure the value is within the desired range
	return new(big.Int).Add(MinStake, randomOffset)
}

type State struct {
	stack *stack.Stack

	// the below can change based on the validator's status
	idByAddress     map[thor.Address]thor.Bytes32
	activeAutoRenew map[thor.Bytes32]*builtin.Validator
	activeOneTime   map[thor.Bytes32]*builtin.Validator
	queued          map[thor.Bytes32]*builtin.Validator

	// the below are static and do not change
	idLookup map[thor.Bytes32]*builtin.Validator
	exited   map[thor.Bytes32]*builtin.Validator

	mu sync.Mutex
}

func NewState(stack *stack.Stack) *State {
	s := &State{
		stack:           stack,
		idByAddress:     make(map[thor.Address]thor.Bytes32),
		activeAutoRenew: make(map[thor.Bytes32]*builtin.Validator),
		activeOneTime:   make(map[thor.Bytes32]*builtin.Validator),
		queued:          make(map[thor.Bytes32]*builtin.Validator),
		idLookup:        make(map[thor.Bytes32]*builtin.Validator),
		exited:          make(map[thor.Bytes32]*builtin.Validator),
	}
	go s.poll()
	return s
}

// Len returns the number of validators with a status of Queued, Active or
func (s *State) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.idByAddress)
}

func (s *State) poll() {
	ticker := utils2.NewTicker(s.stack.Client())

	for {
		select {
		case <-s.stack.Context().Done():
			return
		default:
			ticker.Wait(time.Second * 15)
			ids := make(map[thor.Bytes32]builtin.StakerStatus)
			s.mu.Lock()
			for _, id := range s.idByAddress {
				ids[id] = s.idLookup[id].Status
			}
			s.mu.Unlock()

			mu := sync.Mutex{}
			wg := sync.WaitGroup{}
			newValidations := make(map[thor.Bytes32]*builtin.Validator)

			for id, status := range ids {
				wg.Add(1)
				go func(id thor.Bytes32, status builtin.StakerStatus) {
					validation, err := s.stack.Staker().Get(id)
					defer wg.Done()
					if err != nil {
						slog.Warn("failed to get validator", "id", id, "error", err)
						return
					}
					mu.Lock()
					defer mu.Unlock()
					newValidations[id] = validation
				}(id, status)
			}
			wg.Wait()

			s.mu.Lock()
			for id, validation := range newValidations {
				s.updateStatus(id, validation)
			}
			s.mu.Unlock()
		}
	}
}

func (s *State) Withdraw(id thor.Bytes32, signer bind.Signer) (*api.Receipt, error) {
	sender := s.stack.Staker().WithdrawStake(id)
	receipt, err := s.stack.SendTransaction(sender, signer)
	if err != nil {
		return nil, fmt.Errorf("failed to withdraw from validator %s: %w", id.String(), err)
	}
	return receipt, nil
}

func (s *State) QueueValidator(acc bind.Signer, autoRenew bool) (thor.Bytes32, *api.Receipt, error) {
	sender := s.stack.Staker().AddValidator(acc.Address(), RandomStake(), s.stack.Config().MinStakingPeriod)
	receipt, err := s.stack.SendTransaction(sender, acc)
	if err != nil {
		return thor.Bytes32{}, nil, fmt.Errorf("failed to queue validator %s: %w", acc.Address(), err)
	}
	id := receipt.Outputs[0].Events[0].Topics[3]
	validation, err := s.stack.Staker().Get(id)
	if err != nil {
		return thor.Bytes32{}, nil, fmt.Errorf("failed to get validator %s: %w", id.String(), err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.idByAddress[acc.Address()] = id
	s.idLookup[id] = validation
	s.queued[id] = validation
	return id, receipt, nil
}

func (s *State) DisableAutoRenew(id thor.Bytes32, signer bind.Signer) (*api.Receipt, error) {
	sender := s.stack.Staker().SignalExit(id)
	receipt, err := s.stack.SendTransaction(sender, signer)
	if err != nil {
		return nil, fmt.Errorf("failed to disable auto-renew for validator %s: %w", id.String(), err)
	}
	validation, err := s.stack.Staker().Get(id)
	if err != nil {
		return receipt, fmt.Errorf("failed to get validator %s after disabling auto-renew: %w", id.String(), err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateStatus(id, validation)

	return receipt, nil
}

func (s *State) RandomActiveAutoRenewValidator() (*builtin.Validator, thor.Bytes32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return randomFromMap(s.activeAutoRenew)
}

func (s *State) RandomActiveOneTimeValidator() (*builtin.Validator, thor.Bytes32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return randomFromMap(s.activeOneTime)
}

func (s *State) RandomQueuedValidator() (*builtin.Validator, thor.Bytes32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return randomFromMap(s.queued)
}

func (s *State) LookupAddress(address thor.Address) (thor.Bytes32, *builtin.Validator, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, exists := s.idByAddress[address]
	if !exists {
		return thor.Bytes32{}, nil, false
	}

	validation, exists := s.idLookup[id]
	if !exists {
		return thor.Bytes32{}, nil, false
	}

	return id, validation, true
}

func randomFromMap(m map[thor.Bytes32]*builtin.Validator) (*builtin.Validator, thor.Bytes32) {
	if len(m) == 0 {
		return nil, thor.Bytes32{}
	}
	keys := make([]thor.Bytes32, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	randomIndex := rand.Intn(len(keys))
	randomID := keys[randomIndex]
	return m[randomID], randomID
}

// updateStatus of a validator. It is not thread-safe, so it should be called with the mutex locked.
func (s *State) updateStatus(id thor.Bytes32, validation *builtin.Validator) {
	prevValidation, exists := s.idLookup[id]
	hasUpdates := false
	if exists && prevValidation.Status != validation.Status {
		hasUpdates = true
		slog.Info("🐳validator status changed", "addr", validation.Master, "from", prevValidation.Status, "to", validation.Status)
	}
	if exists && prevValidation.ExitBlock != validation.ExitBlock {
		hasUpdates = true
		slog.Info("🤑validator auto-renew status changed", "addr", validation.Master, "from", prevValidation.ExitBlock, "to", validation.ExitBlock)
	}
	if exists && prevValidation.Stake.Cmp(validation.Stake) != 0 {
		hasUpdates = true
		slog.Info("🤪validator stake changed", "addr", validation.Master, "from", utils.ScaleToVET(prevValidation.Stake), "to", utils.ScaleToVET(validation.Stake))
	}
	if exists && prevValidation.Weight.Cmp(validation.Weight) != 0 {
		hasUpdates = true
		slog.Info("🚚validator weight changed", "addr", validation.Master, "from", utils.ScaleToVET(prevValidation.Weight), "to", utils.ScaleToVET(validation.Weight))
	}

	if !exists {
		hasUpdates = true
		slog.Warn("validator not found in lookup", "id", id, "validation", validation)
	}

	if !hasUpdates {
		return
	}

	delete(s.idByAddress, *validation.Master)
	delete(s.activeAutoRenew, id)
	delete(s.activeOneTime, id)
	delete(s.queued, id)

	switch validation.Status {
	case builtin.StakerStatusQueued:
		s.queued[id] = validation
	case builtin.StakerStatusActive:
		if validation.ExitBlock == math.MaxUint32 {
			s.activeAutoRenew[id] = validation
		} else {
			s.activeOneTime[id] = validation
		}
	case builtin.StakerStatusExited:
		s.exited[id] = validation
		s.idLookup[id] = validation
		delete(s.idByAddress, *validation.Master)
		return
	default:
		slog.Warn("unknown status for validator", "id", id, "status", validation.Status)
	}

	s.idLookup[id] = validation
	s.idByAddress[*validation.Master] = id
}
