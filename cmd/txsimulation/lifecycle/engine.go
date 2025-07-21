package lifecycle

import (
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	utils2 "github.com/vechain/hayabusa-e2e/cmd/txsimulation/utils"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/validations"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/test/datagen"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/bind"
)

type Engine struct {
	stack       *stack.Stack
	validators  *validations.State
	lifecycles  map[thor.Bytes32]Lifecycle
	stargateAcc bind.Signer
	mu          sync.Mutex
}

func NewEngine(
	stack *stack.Stack,
	validators *validations.State,
	stargateAcc bind.Signer,
) *Engine {
	return &Engine{
		validators:  validators,
		lifecycles:  make(map[thor.Bytes32]Lifecycle),
		stargateAcc: stargateAcc,
		stack:       stack,
	}
}

func (e *Engine) Info() []*Info {
	e.mu.Lock()
	defer e.mu.Unlock()

	info := make([]*Info, 0, len(e.lifecycles))
	for _, lifecycle := range e.lifecycles {
		i := lifecycle.Info()
		if i.Status < StatusQueued {
			continue // Skip lifecycles that are not queued yet
		}
		info = append(info, lifecycle.Info())
	}

	return info
}

func (e *Engine) AddLifecycle(lifecycle Lifecycle) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.lifecycles[datagen.RandomHash()] = lifecycle
}

func (e *Engine) AddValidatorLifecycle(acc bind.Signer, startBlock uint32) {
	e.mu.Lock()
	defer e.mu.Unlock()

	cycle := &ValidatorLifecycle{
		BaseLifecycle: BaseLifecycle{
			QueueDelay:     Delay{Blocks: 0, Epochs: 0},
			Account:        acc,
			StakingPeriods: uint32(utils2.Random(6)),
			WithdrawDelay: Delay{
				Blocks: uint32(utils2.RandomBetween(0, int(e.stack.Config().EpochLength))),
				Epochs: uint32(utils2.RandomBetween(1, 3)),
			},
			StartBlock: startBlock,
		},
	}

	e.lifecycles[datagen.RandomHash()] = cycle
}

func (e *Engine) AddDelegatorLifecycle(startBlock uint32) {
	e.mu.Lock()
	defer e.mu.Unlock()

	config := e.stack.Config()

	cycle := &DelegatorLifecycle{
		BaseLifecycle: BaseLifecycle{
			QueueDelay: Delay{
				Blocks: uint32(utils2.RandomBetween(0, int(config.EpochLength))),
				Epochs: uint32(utils2.RandomBetween(0, 3)),
			},
			StakingPeriods: uint32(utils2.RandomBetween(2, 5)),
			WithdrawDelay: Delay{
				Blocks: uint32(utils2.RandomBetween(0, int(config.EpochLength))),
				Epochs: uint32(utils2.RandomBetween(1, 3)),
			},
			StartBlock: startBlock,
			Account:    hayabusa.Stargate,
		},
	}

	e.lifecycles[datagen.RandomHash()] = cycle
}

func (e *Engine) RemoveLifecycle(id thor.Bytes32) {
	e.mu.Lock()
	defer e.mu.Unlock()

	delete(e.lifecycles, id)
}

func (e *Engine) Lifecycles() map[thor.Bytes32]Lifecycle {
	e.mu.Lock()
	defer e.mu.Unlock()

	return maps.Clone(e.lifecycles)
}

func (e *Engine) Run() {
	ticker := utils.NewTicker(e.stack.Client())
	for {
		select {
		case <-e.stack.Context().Done():
			return
		default:
			best, err := ticker.Wait(25 * time.Second)
			if err != nil {
				slog.Error("failed wait for best block", "error", err)
				continue
			}
			_, id, _ := e.stack.Staker().FirstActive()
			if !id.IsZero() {
				e.generateValidatorCycles(best)
				e.generateDelegatorCycles(best)
			}

			delegationStatus := make(map[Status]int)
			validationStatus := make(map[Status]int)
			e.mu.Lock()
			for _, lifecycle := range e.lifecycles {
				if lifecycle.Type() == TypeDelegator {
					delegationStatus[lifecycle.Status()]++
				}
				if lifecycle.Type() == TypeValidator {
					validationStatus[lifecycle.Status()]++
				}
				if lifecycle.Status() != StatusWithdrawn {
					go lifecycle.Process(e, best.Number)
				}
			}
			e.mu.Unlock()

			slog.Info("validations status",
				"pending", validationStatus[StatusPending],
				"queued", validationStatus[StatusQueued],
				"active", validationStatus[StatusActive],
				"exit signalled", validationStatus[StatusExitSignalled],
				"withdrawn", validationStatus[StatusWithdrawn],
			)

			slog.Info("delegations status",
				"pending", delegationStatus[StatusPending],
				"queued", delegationStatus[StatusQueued],
				"active", delegationStatus[StatusActive],
				"exit signalled", delegationStatus[StatusExitSignalled],
				"withdrawn", delegationStatus[StatusWithdrawn],
			)
		}
	}
}

// Flush places a lock on the engine and waits for all lifecycles to reach the given status.
func (e *Engine) Flush(status Status) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	processed := false
	for !processed {
		best, err := e.stack.Client().ExpandedBlock("best")
		if err != nil {
			slog.Error("failed to wait for best block", "error", err)
			return err
		}
		wg := sync.WaitGroup{}
		for _, lifecycle := range e.lifecycles {
			wg.Add(1)
			go func(l Lifecycle, current *api.JSONExpandedBlock) {
				defer wg.Done()
				if l.Status() >= status {
					return
				}
				l.Process(e, current.Number)
			}(lifecycle, best)
		}
		wg.Wait()

		processed = true
		for _, lifecycle := range e.lifecycles {
			if lifecycle.Status() < status {
				processed = false
			}
		}
	}

	return nil
}

// generateValidatorCycles looks for accounts that are not yet registered as validators and creates a lifecycle for them.
func (e *Engine) generateValidatorCycles(block *api.JSONExpandedBlock) {
	amount := utils2.RandomBetween(0, 1)
	if e.validators.Len() < 105 {
		amount = utils2.RandomBetween(4, 8)
	}

	slog.Info("generating validator cycles", "amount", amount, "block", block.Number)

	accounts := make([]bind.Signer, 0)
	for _, account := range e.stack.ValidatorAccounts() {
		if len(accounts) >= amount {
			break
		}
		id, _, ok := e.validators.LookupAddress(account.Address())
		if !ok || id.IsZero() {
			accounts = append(accounts, account)
		}
	}

	for _, account := range accounts {
		e.AddValidatorLifecycle(account, block.Number)
	}
}

func (e *Engine) generateDelegatorCycles(block *api.JSONExpandedBlock) {
	for i := 0; i < 3; i++ {
		e.AddDelegatorLifecycle(block.Number)
	}
}
