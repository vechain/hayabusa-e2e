package delegations

import (
	"math/big"
	"math/rand"
	rand2 "math/rand/v2"
)

type Position struct {
	Name       string
	Stake      *big.Int
	Supply     int
	Multiplier uint8
}

var Positions = []*Position{
	{"Mjolnir X", big.NewInt(15600000), 158, 150},
	{"Thunder X", big.NewInt(5600000), 180, 150},
	{"Strength X", big.NewInt(1600000), 843, 150},
	{"VeThor X", big.NewInt(600000), 735, 150},
	{"Mjolnir", big.NewInt(15000000), 100, 100},
	{"Thunder", big.NewInt(5000000), 300, 100},
	{"Strength", big.NewInt(1000000), 2_500, 100},
	{"Flash", big.NewInt(200000), 25_000, 100},
	{"Lightning", big.NewInt(50000), 100_000, 100},
	{"Dawn", big.NewInt(10000), 500_000, 100},
}

func RandomPosition() *Position {
	source := rand.New(rand.NewSource(rand2.Int64()))
	index := int(big.NewInt(0).Rand(source, big.NewInt(int64(len(Positions)))).Int64())
	return Positions[index]
}
