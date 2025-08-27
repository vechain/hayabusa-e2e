package lifecycle

import (
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/delegations"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	utils2 "github.com/vechain/hayabusa-e2e/cmd/txsimulation/utils"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/validators"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/test/datagen"
	"github.com/vechain/thor/v2/thor"
)

type Generator interface {
	CreateValidator(startBlock uint32) (ValidatorConfig, bool)
	CreateDelegator(startBlock uint32) (DelegatorConfig, bool)
}

type Engine struct {
	stack       *stack.Stack
	validators  *validators.Service
	delegations *delegations.PositionManager
	lifecycles  map[thor.Bytes32]Lifecycle
	withdrawn   map[thor.Bytes32]Lifecycle
	workerPool  *WorkerPool
	generator   Generator
	mu          sync.Mutex
}

func NewEngine(
	stack *stack.Stack,
	validators *validators.Service,
	delegations *delegations.PositionManager,
	generator Generator,
) *Engine {
	pool := NewWorkerPool(10)
	pool.Start()
	return &Engine{
		validators:  validators,
		delegations: delegations,
		lifecycles:  make(map[thor.Bytes32]Lifecycle),
		withdrawn:   make(map[thor.Bytes32]Lifecycle),
		stack:       stack,
		generator:   generator,
		workerPool:  pool,
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
					e.workerPool.Run(func() {
						if err := lifecycle.Process(best.Number); err != nil {
							slog.Error("failed to process lifecycle", "type", lifecycle.Type(), "id", lifecycle.ID(), "error", err)
						}
					})
				} else {
					toRemove = append(toRemove, id)
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

			slog.Info("🚒  validations status",
				"pending", validationStatus[StatusPending],
				"queued", validationStatus[StatusQueued],
				"active", validationStatus[StatusActive],
				"exit signalled", validationStatus[StatusExitSignalled],
			)

			slog.Info("🚒  delegations status",
				"pending", delegationStatus[StatusPending],
				"queued", delegationStatus[StatusQueued],
				"active", delegationStatus[StatusActive],
				"exit signalled", delegationStatus[StatusExitSignalled],
			)

			slog.Info(fmt.Sprintf("👨‍💼 %s", e.delegations.Summary()))
		}
	}
}

// Flush places a lock on the engine and waits for all lifecycles to reach the given status.
func (e *Engine) Flush(status Status) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	ticker := utils.NewTicker(e.stack.Client())

	processed := false
	for !processed {
		best, err := ticker.Wait(15 * time.Second)
		if err != nil {
			slog.Error("failed to wait for best block", "error", err)
			return err
		}
		for _, lifecycle := range e.lifecycles {
			e.workerPool.Run(func(l Lifecycle, current *api.JSONExpandedBlock) Worker {
				return func() {
					if err := lifecycle.Process(best.Number); err != nil {
						slog.Error("failed to process lifecycle", "type", lifecycle.Type(), "id", lifecycle.ID(), "error", err)
					}
				}
			}(lifecycle, best))
		}

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
	mbp := int(e.stack.Config().MaxBlockProposers)
	maxQueued := mbp / 8
	desiredQueued := utils2.RandomBetween(0, maxQueued)
	desiredQueued = max(desiredQueued, 3) // Ensure at least 3 queued
	spaces := int(e.stack.Config().MaxBlockProposers) + desiredQueued - lifecycles
	amount := utils2.RandomBetween(0, spaces)

	slog.Info("🌚 generating validator cycles", "amount", amount, "lifecycles", lifecycles, "spaces", spaces)

	for range amount {
		lifecycle, ok := e.generator.CreateValidator(block.Number)
		if !ok {
			slog.Info("no more validator accounts available")
			return
		}
		cycle := NewValidatorLifecycle(lifecycle, e.validators, e.delegations, e.stack)
		e.lifecycles[datagen.RandomHash()] = cycle
	}
}

func (e *Engine) generateDelegatorCycles(block *api.JSONExpandedBlock) {
	e.mu.Lock()
	defer e.mu.Unlock()

	available := e.delegations.Available()
	totalSupply := e.delegations.TotalSupply()

	upperLimit := math.Sqrt(float64(available))
	amount := utils2.RandomBetween(int(upperLimit)/2, int(upperLimit))
	amount = min(amount, 80) // Limit to 80 to avoid full blocks

	slog.Info("🌚 generating delegator cycles", "amount", amount, "available", available, "totalSupply", totalSupply)

	for i := 0; i < amount; i++ {
		config, ok := e.generator.CreateDelegator(block.Number)
		if !ok {
			slog.Info("no more delegator accounts available")
			return
		}
		cycle := NewDelegatorLifecycle(config, e.delegations, e.validators, e.stack)
		e.lifecycles[datagen.RandomHash()] = cycle
	}
}
