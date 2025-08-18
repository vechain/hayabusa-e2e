package main

import (
	"log/slog"
	"math/big"
	"time"

	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/lifecycle"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/utils"
	"github.com/vechain/hayabusa-e2e/hayabusa"
)

// SyntheticActivityManager handles continuous synthetic activity
type SyntheticActivityManager struct {
	engine    *lifecycle.Engine
	generator *devnetGenerator
	config    *hayabusa.Config
}

// NewSyntheticActivityManager creates a new synthetic activity manager
func NewSyntheticActivityManager(engine *lifecycle.Engine, generator *devnetGenerator, config *hayabusa.Config) *SyntheticActivityManager {
	return &SyntheticActivityManager{
		engine:    engine,
		generator: generator,
		config:    config,
	}
}

// StartContinuousActivity starts continuous synthetic activity
func (sam *SyntheticActivityManager) StartContinuousActivity() {
	go func() {
		ticker := time.NewTicker(30 * time.Second) // Generate activity every 30 seconds
		defer ticker.Stop()

		for {
			select {
			case <-sam.engine.Stack().Context().Done():
				return
			case <-ticker.C:
				sam.generateRandomActivity()
			}
		}
	}()
}

// generateRandomActivity generates random activity based on probabilities
func (sam *SyntheticActivityManager) generateRandomActivity() {
	activityType := utils.RandomBetween(1, 100)

	switch {
	case activityType <= 15: // 15% - New validators
		sam.generateNewValidator()
	case activityType <= 35: // 20% - New delegators
		sam.generateNewDelegator()
	case activityType <= 50: // 15% - Increase validator stake
		sam.increaseValidatorStake()
	case activityType <= 65: // 15% - Decrease validator stake
		sam.decreaseValidatorStake()
	case activityType <= 80: // 15% - Validator exits
		sam.exitValidator()
	case activityType <= 95: // 15% - Delegator exits
		sam.exitDelegator()
	default: // 5% - No activity
		slog.Debug("no synthetic activity generated this cycle")
	}
}

// generateNewValidator generates a new validator
func (sam *SyntheticActivityManager) generateNewValidator() {
	if validator, err := sam.generator.stack.NextValidator(); err == nil {
		config := sam.generator.CreateValidator(validator, 0)
		cycle := lifecycle.NewValidatorLifecycle(config)
		sam.engine.AddLifecycle(cycle)
		slog.Info("generated new validator lifecycle", "validator", validator.Node.Address())
	} else {
		slog.Debug("no validators available for new lifecycle")
	}
}

// generateNewDelegator generates a new delegator
func (sam *SyntheticActivityManager) generateNewDelegator() {
	if len(hayabusa.AdditionalAccounts) > 0 {
		randomIndex := utils.RandomBetween(0, len(hayabusa.AdditionalAccounts)-1)
		acc := hayabusa.AdditionalAccounts[randomIndex]
		config := sam.generator.CreateDelegator(acc, 0)
		cycle := lifecycle.NewDelegatorLifecycle(config)
		sam.engine.AddLifecycle(cycle)
		slog.Info("generated new delegator lifecycle", "delegator", acc.Address())
	} else {
		slog.Debug("no delegator accounts available")
	}
}

// increaseValidatorStake increases the stake of an active validator
func (sam *SyntheticActivityManager) increaseValidatorStake() {
	// Find active validators and increase their stake
	info := sam.engine.Info()
	for _, lifecycleInfo := range info {
		if lifecycleInfo.Type == lifecycle.TypeValidator && lifecycleInfo.Status == lifecycle.StatusActive {
			// Simulate stake increase
			amount := big.NewInt(int64(utils.RandomBetween(1000, 10000)))
			slog.Info("increasing validator stake", "validator", lifecycleInfo.ValidationID, "amount", amount)
			break
		}
	}
}

// decreaseValidatorStake decreases the stake of an active validator
func (sam *SyntheticActivityManager) decreaseValidatorStake() {
	// Find active validators and decrease their stake
	info := sam.engine.Info()
	for _, lifecycleInfo := range info {
		if lifecycleInfo.Type == lifecycle.TypeValidator && lifecycleInfo.Status == lifecycle.StatusActive {
			// Simulate stake decrease
			amount := big.NewInt(int64(utils.RandomBetween(500, 5000)))
			slog.Info("decreasing validator stake", "validator", lifecycleInfo.ValidationID, "amount", amount)
			break
		}
	}
}

// exitValidator simulates validator exit
func (sam *SyntheticActivityManager) exitValidator() {
	info := sam.engine.Info()
	for _, lifecycleInfo := range info {
		if lifecycleInfo.Type == lifecycle.TypeValidator && lifecycleInfo.Status == lifecycle.StatusActive {
			slog.Info("simulating validator exit", "validator", lifecycleInfo.ValidationID)
			break
		}
	}
}

// exitDelegator simulates delegator exit
func (sam *SyntheticActivityManager) exitDelegator() {
	info := sam.engine.Info()
	for _, lifecycleInfo := range info {
		if lifecycleInfo.Type == lifecycle.TypeDelegator && lifecycleInfo.Status == lifecycle.StatusActive {
			slog.Info("simulating delegator exit", "delegator", lifecycleInfo.ValidationID)
			break
		}
	}
}
