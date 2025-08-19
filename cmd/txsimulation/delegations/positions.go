package delegations

import (
	"math/big"
	"math/rand"
	rand2 "math/rand/v2"
)

type Position struct {
	Name       string
	Stake      *big.Int
	Used       int // the amount of used positions according to:https://vechainstats.com/vechain-nodes/#xnode-log
	Multiplier uint8
}

var Positions = []*Position{
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

func RandomPosition() *Position {
	source := rand.New(rand.NewSource(rand2.Int64()))
	index := int(big.NewInt(0).Rand(source, big.NewInt(int64(len(Positions)))).Int64())
	return Positions[index]
}
