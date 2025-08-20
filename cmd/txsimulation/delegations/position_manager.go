package delegations

import (
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"

	"github.com/vechain/thor/v2/thor"
)

type PositionManager struct {
	maxLeaderGroupLength uint32
	positions            map[string]*Position // the configured positions
	distributionType     DistributionType

	validatorAvailable map[thor.Address]map[string]int // active positions per validator
	totalActive        map[string]int                  // total number of active positions
	mu                 sync.Mutex
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

		validatorAvailable: make(map[thor.Address]map[string]int), // active positions per validator
		totalActive:        make(map[string]int),
	}
}

func (pm *PositionManager) TotalSupply() int {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	total := 0
	for _, position := range pm.positions {
		total += position.MainnetUsed
	}
	return total
}

func (pm *PositionManager) Available() int {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	available := 0
	for _, position := range pm.positions {
		available += position.MainnetUsed - pm.totalActive[position.Name]
	}
	return available
}

func (pm *PositionManager) Summary() string {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	builder := strings.Builder{}
	builder.WriteString("PositionManager Summary:\n")
	for positionID, position := range pm.positions {
		builder.WriteString(fmt.Sprintf("  %s: active=%d, available=%d\n",
			positionID,
			pm.totalActive[positionID],
			position.MainnetUsed-pm.totalActive[positionID],
		))
	}

	return builder.String()
}

func (pm *PositionManager) RegisterValidator(validator thor.Address) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.validatorAvailable[validator] = pm.makeValidatorsAvailablePositions(validator)
}

func (pm *PositionManager) UnregisterDelegator(position *Position, validator thor.Address) {
	if position == nil {
		slog.Warn("attempted to unregister nil position for validator", "address", validator.String())
		return
	}
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.validatorAvailable[validator] == nil {
		return
	}

	count, exists := pm.validatorAvailable[validator][position.Name]
	if !exists {
		slog.Warn("attempted to unregister non-existent position for validator", "address", validator.String(), "position", position.Name)
		return
	}
	if count <= 0 {
		return
	}
	pm.validatorAvailable[validator][position.Name]++
	pm.totalActive[position.Name]--
}

func (pm *PositionManager) UnregisterValidator(validator thor.Address) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	current, exists := pm.validatorAvailable[validator]
	if !exists {
		slog.Warn("attempted to unregister non-existent validator", "address", validator.String())
		return
	}
	start := pm.makeValidatorsAvailablePositions(validator)

	for positionID, count := range current {
		// reset the totals
		used := start[positionID] - count
		pm.totalActive[positionID] -= used
	}

	delete(pm.validatorAvailable, validator)
}

func (pm *PositionManager) NewPosition() (*Position, thor.Address, bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	validators := make([]thor.Address, 0, len(pm.validatorAvailable))
	for address := range pm.validatorAvailable {
		validators = append(validators, address)
	}
	rand.Shuffle(len(validators), func(i, j int) {
		validators[i], validators[j] = validators[j], validators[i]
	})

	positions := make([]string, 0, len(pm.positions))
	for positionID := range pm.positions {
		positions = append(positions, positionID)
	}
	rand.Shuffle(len(positions), func(i, j int) {
		positions[i], positions[j] = positions[j], positions[i]
	})

	for _, validator := range validators {
		for _, positionID := range positions {
			count, exists := pm.validatorAvailable[validator][positionID]
			if !exists || count <= 0 {
				continue // No available positions for this validator
			}

			if pm.totalActive[positionID] < pm.positions[positionID].MainnetUsed {
				// Found a position that can be assigned
				pm.validatorAvailable[validator][positionID]--
				pm.totalActive[positionID]++
				position := pm.positions[positionID]
				return position, validator, true
			}
		}
	}

	slog.Debug("no available positions found")
	return nil, thor.Address{}, false
}

func (pm *PositionManager) makeValidatorsAvailablePositions(addr thor.Address) map[string]int {
	available := make(map[string]int)
	for positionID, position := range pm.positions {
		if position.MainnetUsed > 0 {
			// Calculate the maximum number of positions this validator can take
			var max int
			switch pm.distributionType {
			case DistributionTypeEven:
				max = pm.calculateEvenMax(position, addr)
			case DistributionTypeSkewed:
				max = pm.calculateSkewedMax(position, addr)
			default:
				slog.Error("unknown distribution type", "type", pm.distributionType)
				continue
			}

			if max > 0 {
				available[positionID] = max
			}
		}
	}

	return available
}

func (pm *PositionManager) calculateEvenMax(position *Position, validator thor.Address) int {
	maxPerValidator := float64(position.MainnetUsed) / float64(pm.maxLeaderGroupLength)
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
	baseMax := float64(position.MainnetUsed) / float64(pm.maxLeaderGroupLength)

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
