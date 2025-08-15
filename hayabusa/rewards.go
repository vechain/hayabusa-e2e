package hayabusa

import (
	"github.com/vechain/thor/v2/thor"
	"math/big"
)

func GetExpectedReward(totalStaked *big.Int) *big.Int {
	bigE18 := big.NewInt(1e18)
	// sqrt(totalStaked / 1e18) * 1e18, we are calculating sqrt on VET and then converting to wei
	sqrtStake := new(big.Int).Sqrt(new(big.Int).Div(totalStaked, bigE18))
	sqrtStake.Mul(sqrtStake, bigE18)

	blocksPerYear := thor.NumberOfBlocksPerYear

	curveFactor := thor.InitialCurveFactor

	// reward = 1 * curveFactor * sqrt(totalStaked / 1e18) / blocksPerYear
	reward := big.NewInt(1)
	reward.Mul(reward, curveFactor)
	reward.Mul(reward, sqrtStake)
	reward.Div(reward, blocksPerYear)
	return reward
}
