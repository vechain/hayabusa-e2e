package validations

import (
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"math/rand"
	"sync"
	"time"

	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/utils"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	utils2 "github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

var (
	Eth     = big.NewInt(1e18) // 1 ETH in wei
	Million = big.NewInt(1e6)  // 1 million VET
)

func RandomStake() *big.Int {
	return RandomStakeBetween(25, 40)
}

// RandomStakeBetween generates a random number between min and max.
// It will be scaled to millions of VET.
func RandomStakeBetween(min, max uint8) *big.Int {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	maxStake := big.NewInt(0).Mul(big.NewInt(int64(max)), Million)

	minStake := big.NewInt(0).Mul(big.NewInt(int64(min)), Million)

	rangeStake := new(big.Int).Sub(maxStake, minStake)
	randomOffset := new(big.Int).Rand(rng, rangeStake)
	stake := new(big.Int).Add(minStake, randomOffset)
	stake = stake.Mul(stake, Eth) // Convert to wei
	return stake
}

type State struct {
	stack *stack.Stack

	// the below can change based on the validator's status
	active  map[thor.Address]*builtin.ValidatorStake
	queued  map[thor.Address]*builtin.ValidatorStake
	exiting map[thor.Address]*builtin.ValidatorStake

	// the below are static and do not change
	idLookup map[thor.Address]*builtin.ValidatorStatus
	exited   map[thor.Address]*builtin.ValidatorStake

	mu sync.Mutex
}

func NewState(stack *stack.Stack) *State {
	s := &State{
		stack:    stack,
		active:   make(map[thor.Address]*builtin.ValidatorStake),
		queued:   make(map[thor.Address]*builtin.ValidatorStake),
		idLookup: make(map[thor.Address]*builtin.ValidatorStatus),
		exited:   make(map[thor.Address]*builtin.ValidatorStake),
		exiting:  make(map[thor.Address]*builtin.ValidatorStake),
	}
	go s.poll()
	return s
}

// Len returns the number of validators with a status of Queued, Active or Exiting.
// It does not include validators that have exited.
func (s *State) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.active) + len(s.queued) + len(s.exiting)
}

func (s *State) poll() {
	ticker := utils2.NewTicker(s.stack.Client())

	for {
		select {
		case <-s.stack.Context().Done():
			return
		default:
			ticker.Wait(time.Second * 15)
			ids := make(map[thor.Address]builtin.StakerStatus)
			s.mu.Lock()
			for addr, _ := range s.active {
				ids[addr] = s.idLookup[addr].Status
			}
			for addr, _ := range s.queued {
				ids[addr] = s.idLookup[addr].Status
			}
			for addr, _ := range s.exiting {
				ids[addr] = s.idLookup[addr].Status
			}
			s.mu.Unlock()

			mu := sync.Mutex{}
			wg := sync.WaitGroup{}
			newValidations := make(map[thor.Address]*builtin.ValidatorStake)

			for id, status := range ids {
				wg.Add(1)
				go func(id thor.Address, status builtin.StakerStatus) {
					validation, err := s.stack.Staker().GetValidatorStake(id)
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

func (s *State) Withdraw(acc *hayabusa.NodePair) (*api.Receipt, error) {
	sender := s.stack.Staker().WithdrawStake(acc.Node.Address())
	receipt, err := s.stack.SendTransaction(sender, acc.Endorser)
	if err != nil {
		return nil, fmt.Errorf("failed to withdraw from validator %s: %w", acc.Node.Address().String(), err)
	}
	return receipt, nil
}

func (s *State) QueueValidator(acc *hayabusa.NodePair) (thor.Address, *api.Receipt, error) {
	sender := s.stack.Staker().AddValidation(acc.Node.Address(), RandomStake(), s.stack.Config().MinStakingPeriod)
	receipt, err := s.stack.SendTransaction(sender, acc.Endorser)
	id := acc.Node.Address()
	if err != nil {
		return thor.Address{}, nil, fmt.Errorf("failed to queue validator %s: %w", acc.Node.Address(), err)
	}
	validation, err := s.stack.Staker().GetValidatorStatus(id)
	if err != nil {
		return thor.Address{}, nil, fmt.Errorf("failed to get validator status %s: %w", id.String(), err)
	}

	validationStake, err := s.stack.Staker().GetValidatorStake(id)
	if err != nil {
		return thor.Address{}, nil, fmt.Errorf("failed to get validator stake %s: %w", id.String(), err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.idLookup[id] = validation
	s.queued[id] = validationStake
	return id, receipt, nil
}

func (s *State) DisableAutoRenew(acc *hayabusa.NodePair) (*api.Receipt, error) {
	sender := s.stack.Staker().SignalExit(acc.Node.Address())
	receipt, err := s.stack.SendTransaction(sender, acc.Endorser)
	if err != nil {
		return nil, fmt.Errorf("failed to disable auto-renew for validator %s: %w", acc.Node.Address().String(), err)
	}
	validation, err := s.stack.Staker().GetValidatorStake(acc.Node.Address())
	if err != nil {
		return receipt, fmt.Errorf("failed to get validator %s after disabling auto-renew: %w", acc.Node.Address().String(), err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateStatus(acc.Node.Address(), validation)

	return receipt, nil
}

func (s *State) RandomActiveValidator() (*builtin.ValidatorStake, thor.Address) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return randomFromMap(s.active)
}

func (s *State) RandomQueuedValidator() (*builtin.ValidatorStake, thor.Address) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return randomFromMap(s.queued)
}

func (s *State) LookupAddress(address thor.Address) (*builtin.ValidatorStatus, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	validation, exists := s.idLookup[address]
	if !exists {
		return nil, false
	}

	return validation, true
}

func randomFromMap(m map[thor.Address]*builtin.ValidatorStake) (*builtin.ValidatorStake, thor.Address) {
	if len(m) == 0 {
		return nil, thor.Address{}
	}
	keys := make([]thor.Address, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	randomIndex := rand.Intn(len(keys))
	randomID := keys[randomIndex]
	return m[randomID], randomID
}

// updateStatus of a validator. It is not thread-safe, so it should be called with the mutex locked.
func (s *State) updateStatus(id thor.Address, validation *builtin.ValidatorStake) error {
	prevValidation, exists := s.idLookup[id]
	hasUpdates := false
	validationStatus, err := s.stack.Staker().GetValidatorStatus(validation.Address)
	if err != nil {
		return fmt.Errorf("failed to get validator %s status: %w", id.String(), err)
	}
	if exists && prevValidation.Status != validationStatus.Status {
		hasUpdates = true
		slog.Info("🐳 validator status changed", "addr", validation.Address, "from", prevValidation.Status, "to", validationStatus.Status)
	}
	validationDetails, err := s.stack.Staker().GetValidatorPeriodDetails(validation.Address)
	if err != nil {
		return fmt.Errorf("failed to get validator %s period details: %w", id.String(), err)
	}
	prevValidationDetails, err := s.stack.Staker().GetValidatorPeriodDetails(prevValidation.Address)
	if err != nil {
		return fmt.Errorf("failed to get validator %s period details: %w", id.String(), err)
	}
	if exists && prevValidationDetails.ExitBlock != validationDetails.ExitBlock {
		hasUpdates = true
		slog.Info("🤑validator auto-renew status changed", "addr", validation.Address, "from", prevValidationDetails.ExitBlock, "to", validationDetails.ExitBlock)
	}
	prevValidationStake, err := s.stack.Staker().GetValidatorStake(prevValidation.Address)
	if err != nil {
		return fmt.Errorf("failed to get validator %s stake: %w", id.String(), err)
	}
	if exists && prevValidationStake.Stake.Cmp(validation.Stake) != 0 {
		hasUpdates = true
		slog.Info("🤪validator stake changed", "addr", validation.Address, "from", utils.ScaleToVET(prevValidationStake.Stake), "to", utils.ScaleToVET(validation.Stake))
	}
	if exists && prevValidationStake.Weight.Cmp(validation.Weight) != 0 {
		hasUpdates = true
		slog.Info("🚚validator weight changed", "addr", validation.Address, "from", utils.ScaleToVET(prevValidationStake.Weight), "to", utils.ScaleToVET(validation.Weight))
	}

	if !exists {
		hasUpdates = true
		slog.Warn("validator not found in lookup", "id", id, "validation", validation)
	}

	if !hasUpdates {
		return nil
	}

	delete(s.queued, id)
	delete(s.active, id)
	delete(s.exiting, id)
	s.idLookup[id] = validationStatus

	switch validationStatus.Status {
	case builtin.StakerStatusQueued:
		s.queued[id] = validation
	case builtin.StakerStatusActive:
		if validationDetails.ExitBlock == math.MaxUint32 {
			s.active[id] = validation
		} else {
			s.exiting[id] = validation
		}
	case builtin.StakerStatusExited:
		s.exited[id] = validation
		return nil
	default:
		slog.Warn("unknown status for validator", "id", id, "status", validationStatus.Status)
	}
	return nil
}
