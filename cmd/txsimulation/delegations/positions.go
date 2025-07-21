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

// TODO: Supply is correct, but multiplier is not.
var Positions = []*Position{
	{"Mjolnir X", big.NewInt(15600000), 158, 150},
	{"Thunder X", big.NewInt(5600000), 180, 145},
	{"Strength X", big.NewInt(1600000), 843, 140},
	{"VeThor X", big.NewInt(600000), 735, 135},
	{"Mjolnir", big.NewInt(15000000), 100, 130},
	{"Thunder", big.NewInt(5000000), 300, 125},
	{"Strength", big.NewInt(1000000), 2_500, 120},
	{"Flash", big.NewInt(200000), 25_000, 115},
	{"Lightning", big.NewInt(50000), 100_000, 110},
	{"Dawn", big.NewInt(10000), 500_000, 100},
}

func RandomPosition() *Position {
	source := rand.New(rand.NewSource(rand2.Int64()))
	index := int(big.NewInt(0).Rand(source, big.NewInt(int64(len(Positions)))).Int64())
	return Positions[index]
}
