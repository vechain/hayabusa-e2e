package delegations

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/utils"
	"github.com/vechain/thor/v2/thor"
)

type PositionManager struct {
	maxLeaderGroupLength uint32
	positions            map[string]*Position // the configured positions
	distributionType     DistributionType

	validatorActive map[thor.Address]map[string]int // active positions per validator
	totalActive     map[string]int                  // total number of active positions
	mu              sync.Mutex
}

type DistributionType uint8

const (
	DistributionTypeEven DistributionType = iota
	DistributionTypeSkewed
)

func NewManager(maxLeaderGroupLength uint32, distributionType DistributionType) *PositionManager {
	positions := make(map[string]*Position)
	for _, pos := range Positions {
		positions[pos.Name] = pos
	}

	return &PositionManager{
		maxLeaderGroupLength: maxLeaderGroupLength,
		positions:            positions,
		distributionType:     distributionType,

		validatorActive: make(map[thor.Address]map[string]int), // active positions per validator
		totalActive:     make(map[string]int),
	}
}

func (pm *PositionManager) TotalSupply() int {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	total := 0
	for _, position := range pm.positions {
		total += position.Used
	}
	return total
}

func (pm *PositionManager) UnregisterDelegator(position *Position, validator thor.Address) {
	if position == nil {
		slog.Warn("attempted to unregister nil position for validator", "address", validator.String())
		return
	}
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.validatorActive[validator] == nil {
		return
	}

	if count, exists := pm.validatorActive[validator][position.Name]; exists && count > 0 {
		pm.validatorActive[validator][position.Name]--
		pm.totalActive[position.Name]--
		if pm.totalActive[position.Name] < 0 {
			pm.totalActive[position.Name] = 0
		}
	}
}

func (pm *PositionManager) UnregisterValidator(validator thor.Address) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.validatorActive[validator] == nil {
		return
	}

	for positionID, count := range pm.validatorActive[validator] {
		if count > 0 {
			pm.totalActive[positionID] -= count
			if pm.totalActive[positionID] < 0 {
				pm.totalActive[positionID] = 0
			}
		}
	}
	delete(pm.validatorActive, validator)
	slog.Info("unregistered validator", "address", validator.String())
}

func (pm *PositionManager) NewPosition(validator thor.Address) (*Position, bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.validatorActive[validator] == nil {
		pm.validatorActive[validator] = make(map[string]int)
	}

	// get all position keys
	positionIDs := make([]string, 0)
	for positionID := range pm.positions {
		position := pm.positions[positionID]

		var max int
		switch pm.distributionType {
		case DistributionTypeEven:
			max = pm.calculateEvenMax(position, validator)
		case DistributionTypeSkewed:
			max = pm.calculateSkewedMax(position, validator)
		default:
			max = pm.calculateEvenMax(position, validator)
		}

		if pm.totalActive[positionID] < position.Used && pm.validatorActive[validator][positionID] <= max {
			positionIDs = append(positionIDs, positionID)
		}
	}

	// select a random key
	if len(positionIDs) == 0 {
		slog.Warn("no available positions for validator", "address", validator.String())
		return nil, false
	}

	positionID := positionIDs[utils.RandomInt(0, len(positionIDs)-1)]
	pm.totalActive[positionID]++
	pm.validatorActive[validator][positionID]++
	return pm.positions[positionID], true
}

func (pm *PositionManager) calculateEvenMax(position *Position, validator thor.Address) int {
	maxPerValidator := float64(position.Used) / float64(pm.maxLeaderGroupLength)
	max := int(maxPerValidator)

	// if the last byte (as %) of the address is less than the remainder, increment max by 1,
	// so we can get closer to the actual token amount
	remainder := maxPerValidator - float64(max)
	addressLastByte := float64(validator[19]) / 255
	if addressLastByte < remainder {
		max++
	}
	return max
}

func (pm *PositionManager) calculateSkewedMax(position *Position, validator thor.Address) int {
	// Create a skewed distribution where some validators can get significantly more positions
	// We'll use a power function based on the validator's address to create the skew

	// Convert address to a score between 0 and 1
	addressScore := pm.addressToScore(validator)

	// Apply power function to create skew (lower address scores get exponentially more positions)
	// Using power of 3 to create significant skew
	skewFactor := (1.0 - addressScore) * (1.0 - addressScore) * (1.0 - addressScore)

	// Base allocation (minimum everyone can get)
	baseMax := float64(position.Used) / float64(pm.maxLeaderGroupLength)

	// Skewed allocation: validators with lower addresses can get up to 3x the base
	skewedMax := baseMax * (1.0 + 2.0*skewFactor)

	return int(skewedMax)
}

func (pm *PositionManager) addressToScore(validator thor.Address) float64 {
	// Convert first 8 bytes of address to a uint64, then normalize to 0-1
	var score uint64
	for i := 0; i < 8; i++ {
		score = (score << 8) | uint64(validator[i])
	}
	// Normalize to 0-1 range
	return float64(score) / float64(^uint64(0))
}

func (pm *PositionManager) Summary() string {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	builder := strings.Builder{}
	builder.WriteString("PositionManager Summary:\n")
	for positionID, position := range pm.positions {
		builder.WriteString(fmt.Sprintf("  %s: %d active positions, %d available\n",
			positionID,
			pm.totalActive[positionID],
			position.Used-pm.totalActive[positionID],
		))
	}

	return builder.String()
}
