package xnodes

import (
	"math/big"
)

type Type string

type Position struct {
	Name             Type
	Stake            *big.Int
	MainnetUsed      int // the amount of used positions according to:https://vechainstats.com/vechain-nodes/#xnode-log
	WeightMultiplier uint8
	RewardMultiplier uint16
}

const (
	MjolnirX  Type = "Mjolnir X"
	ThunderX  Type = "Thunder X"
	StrengthX Type = "Strength X"
	VeThorX   Type = "VeThor X"
	Mjolnir   Type = "Mjolnir"
	Thunder   Type = "Thunder"
	Strength  Type = "Strength"
	Flash     Type = "Flash"
	Lightning Type = "Lightning"
	Dawn      Type = "Dawn"
)

var NoPositions []*Position

// DevnetPositions scales down the MainnetPositions according to the mbp (max block proposers) for devnet testing.
func DevnetPositions(mbp uint32) []*Position {
	positions := make([]*Position, len(MainnetPositions))

	for i, pos := range MainnetPositions {
		pos.MainnetUsed = int(float64(pos.MainnetUsed) * float64(mbp) / 101)
		positions[i] = pos
	}
	return positions
}

var MainnetPositions = []*Position{
	{MjolnirX, big.NewInt(15600000), 158, 150, 500},
	{ThunderX, big.NewInt(5600000), 188, 150, 400},
	{StrengthX, big.NewInt(1600000), 836, 150, 300},
	{VeThorX, big.NewInt(600000), 719, 150, 200},
	{Mjolnir, big.NewInt(15000000), 96, 100, 350},
	{Thunder, big.NewInt(5000000), 257, 100, 250},
	{Strength, big.NewInt(1000000), 1644, 100, 150},
	{Flash, big.NewInt(200000), 1941, 100, 130},
	{Lightning, big.NewInt(50000), 2688, 100, 115},
	{Dawn, big.NewInt(10000), 4614, 100, 100},
}
