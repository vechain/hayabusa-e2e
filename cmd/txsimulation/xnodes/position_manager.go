package xnodes

import (
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"

	"github.com/vechain/thor/v2/test/datagen"
	"github.com/vechain/thor/v2/thor"
)

type ActivePosition struct {
	Position  *Position
	Validator thor.Address
}

type PositionManager struct {
	maxLeaderGroupLength uint32
	positions            map[string]*Position // the configured positions
	distributionType     DistributionType

	delegations        map[thor.Bytes32]*ActivePosition
	validatorAvailable map[thor.Address]map[string]int // current available positions per validator
	validatorOriginal  map[thor.Address]map[string]int // original allocated positions per validator (NEW)
	totalActive        map[string]int
	mu                 sync.Mutex
}

type DistributionType uint8

const (
	DistributionTypeEven DistributionType = iota
	DistributionTypeSkewed
)

func NewManager(maxLeaderGroupLength uint32, distributionType DistributionType, positions []*Position) *PositionManager {
	positionMap := make(map[string]*Position)
	for _, pos := range positions {
		positionMap[pos.Name] = pos
	}

	return &PositionManager{
		maxLeaderGroupLength: maxLeaderGroupLength,
		positions:            positionMap,
		distributionType:     distributionType,

		validatorAvailable: make(map[thor.Address]map[string]int),
		validatorOriginal:  make(map[thor.Address]map[string]int),
		totalActive:        make(map[string]int),
		delegations:        make(map[thor.Bytes32]*ActivePosition),
	}
}

func (pm *PositionManager) TotalSupply() int {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if len(pm.positions) == 0 {
		return 0
	}

	total := 0
	for _, position := range pm.positions {
		total += position.MainnetUsed
	}
	return total
}

func (pm *PositionManager) Available() int {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if len(pm.positions) == 0 {
		return 0
	}

	available := 0
	for _, position := range pm.positions {
		available += position.MainnetUsed - pm.totalActive[position.Name]
	}
	return available
}

func (pm *PositionManager) Summary() string {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if len(pm.positions) == 0 {
		return "PositionManager is empty"
	}

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

	if len(pm.positions) == 0 {
		return
	}

	if _, exists := pm.validatorAvailable[validator]; exists {
		return
	}

	allocation := pm.makeValidatorsAvailablePositions(validator)
	pm.validatorAvailable[validator] = allocation

	// Store the original allocation
	pm.validatorOriginal[validator] = make(map[string]int)
	for positionID, count := range allocation {
		pm.validatorOriginal[validator][positionID] = count
	}
}

func (pm *PositionManager) UnregisterDelegator(id thor.Bytes32) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	active, exists := pm.delegations[id]
	if !exists {
		return
	}
	validator := active.Validator
	positionID := active.Position.Name
	current, exists := pm.validatorAvailable[validator]
	if !exists {
		delete(pm.delegations, id)
		return
	}

	current[positionID]++
	pm.totalActive[positionID]--
	delete(pm.delegations, id)
}

func (pm *PositionManager) UnregisterValidator(validator thor.Address) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if len(pm.positions) == 0 {
		return
	}

	current, exists := pm.validatorAvailable[validator]
	if !exists {
		return
	}

	original, exists := pm.validatorOriginal[validator]
	if !exists {
		delete(pm.validatorAvailable, validator)
		return
	}

	for positionID, available := range current {
		// Calculate how many were actually used
		used := original[positionID] - available
		pm.totalActive[positionID] -= used
	}

	delete(pm.validatorAvailable, validator)
	delete(pm.validatorOriginal, validator)
}

func (pm *PositionManager) NewPosition() (thor.Bytes32, *ActivePosition, bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if len(pm.positions) == 0 {
		return thor.Bytes32{}, nil, false
	}

	validators := make([]thor.Address, 0, len(pm.validatorAvailable))
	for address := range pm.validatorAvailable {
		validators = append(validators, address)
	}
	rand.Shuffle(len(validators), func(i, j int) {
		validators[i], validators[j] = validators[j], validators[i]
	})

	positions := make([]string, 0, len(pm.positions))
	for positionID := range pm.positions {
		if pm.totalActive[positionID] >= pm.positions[positionID].MainnetUsed {
			continue
		}
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
				id := datagen.RandomHash()
				pm.delegations[id] = &ActivePosition{
					Position:  position,
					Validator: validator,
				}
				return id, pm.delegations[id], true
			}
		}
	}

	slog.Debug("no available positions found")
	return thor.Bytes32{}, nil, false
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
