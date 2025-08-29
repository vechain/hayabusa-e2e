package xnodes

import (
	"math/big"
)

type Position struct {
	Name        string
	Stake       *big.Int
	MainnetUsed int // the amount of used positions according to:https://vechainstats.com/vechain-nodes/#xnode-log
	Multiplier  uint8
}

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
	{"Mjolnir X", big.NewInt(15600000), 158, 150},
	{"Thunder X", big.NewInt(5600000), 188, 150},
	{"Strength X", big.NewInt(1600000), 836, 150},
	{"VeThor X", big.NewInt(600000), 719, 150},
	{"Mjolnir", big.NewInt(15000000), 96, 100},
	{"Thunder", big.NewInt(5000000), 257, 100},
	{"Strength", big.NewInt(1000000), 1644, 100},
	{"Flash", big.NewInt(200000), 1941, 100},
	{"Lightning", big.NewInt(50000), 2688, 100},
	{"Dawn", big.NewInt(10000), 4614, 100},
}
