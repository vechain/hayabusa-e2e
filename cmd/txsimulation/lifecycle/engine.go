package lifecycle

import (
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	utils2 "github.com/vechain/hayabusa-e2e/cmd/txsimulation/utils"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/validations"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/test/datagen"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/bind"
)

type Generator interface {
	CreateValidator(acc bind.Signer, startBlock uint32) Config
	CreateDelegator(acc bind.Signer, startBlock uint32) Config
}

type Engine struct {
	stack      *stack.Stack
	validators *validations.State
	lifecycles map[thor.Bytes32]Lifecycle
	withdrawn  map[thor.Bytes32]Lifecycle
	generator  Generator
	mu         sync.Mutex
}

func NewEngine(
	stack *stack.Stack,
	validators *validations.State,
	generator Generator,
) *Engine {
	return &Engine{
		validators: validators,
		lifecycles: make(map[thor.Bytes32]Lifecycle),
		withdrawn:  make(map[thor.Bytes32]Lifecycle),
		stack:      stack,
		generator:  generator,
	}
}

func (e *Engine) Info() []*Info {
	e.mu.Lock()
	defer e.mu.Unlock()

	info := make([]*Info, 0, len(e.lifecycles)+len(e.withdrawn))
	for _, lifecycle := range e.lifecycles {
		i := lifecycle.Info()
		if i.Status < StatusQueued {
			continue // Skip lifecycles that are not queued yet
		}
		info = append(info, lifecycle.Info())
	}

	for _, lifecycle := range e.withdrawn {
		info = append(info, lifecycle.Info())
	}

	return info
}

func (e *Engine) AddLifecycle(lifecycle Lifecycle) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.lifecycles[datagen.RandomHash()] = lifecycle
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
			toRemove := make([]thor.Bytes32, 0)
			e.mu.Lock()
			for id, lifecycle := range e.lifecycles {
				if lifecycle.Type() == TypeDelegator {
					delegationStatus[lifecycle.Status()]++
				}
				if lifecycle.Type() == TypeValidator {
					validationStatus[lifecycle.Status()]++
				}
				if lifecycle.Status() != StatusWithdrawn {
					go lifecycle.Process(e, best.Number)
				} else {
					toRemove = append(toRemove, id)
				}
			}

			// process withdraw lifecycles for logging
			for _, lifecycle := range e.withdrawn {
				if lifecycle.Type() == TypeDelegator {
					delegationStatus[lifecycle.Status()]++
				}
				if lifecycle.Type() == TypeValidator {
					validationStatus[lifecycle.Status()]++
				}
			}

			for _, id := range toRemove {
				slog.Debug("removing lifecycle", "id", id)
				existing, ok := e.lifecycles[id]
				if ok {
					delete(e.lifecycles, id)
					e.withdrawn[id] = existing
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
				lifecycle.Process(e, best.Number)
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
	e.mu.Lock()
	defer e.mu.Unlock()

	lifecycles := 0
	for _, lifecycle := range e.lifecycles {
		if lifecycle.Type() == TypeValidator && lifecycle.Status() < StatusExitSignalled {
			lifecycles++
		}
	}
	desiredQueued := utils2.RandomBetween(0, 15)
	spaces := 101 + desiredQueued - lifecycles
	amount := utils2.RandomBetween(0, spaces)

	slog.Info("generating validator cycles", "amount", amount, "lifecycles", lifecycles, "spaces", spaces)

	for range amount {
		account, err := e.stack.NextValidator()
		if err != nil {
			slog.Error("not generating any more validator cycles, no more validator keys")
			return
		}
		cycle := NewValidatorLifecycle(e.generator.CreateValidator(account, block.Number))
		e.lifecycles[datagen.RandomHash()] = cycle
	}
}

func (e *Engine) generateDelegatorCycles(block *api.JSONExpandedBlock) {
	e.mu.Lock()
	defer e.mu.Unlock()

	lifecycles := 0
	for _, lifecycle := range e.lifecycles {
		if lifecycle.Type() == TypeDelegator && lifecycle.Status() < StatusExitSignalled {
			lifecycles++
		}
	}
	upperLimit := math.Sqrt(1000 - float64(lifecycles))
	amount := utils2.RandomBetween(0, int(upperLimit))

	slog.Info("generating delegator cycles", "amount", amount, "lifecycles", lifecycles, "upperLimit", upperLimit)

	for i := 0; i < amount; i++ {
		cycle := NewDelegatorLifecycle(e.generator.CreateDelegator(e.stack.Stargate(), block.Number))
		e.lifecycles[datagen.RandomHash()] = cycle
	}
}
