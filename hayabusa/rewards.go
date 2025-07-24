package hayabusa

import (
	"math/big"
	"time"

	"github.com/vechain/thor/v2/thor"
)

func GetExpectedReward(totalStaked *big.Int) *big.Int {
	bigE18 := big.NewInt(1e18)
	// sqrt(totalStaked / 1e18) * 1e18, we are calculating sqrt on VET and then converting to wei
	sqrtStake := new(big.Int).Sqrt(new(big.Int).Div(totalStaked, bigE18))
	sqrtStake.Mul(sqrtStake, bigE18)

	isLeap := isLeapYear(time.Now().Year())
	blocksPerYear := thor.NumberOfBlocksPerYear
	if isLeap {
		blocksPerYear = new(big.Int).Add(thor.NumberOfBlocksPerYear, big.NewInt(thor.SeederInterval))
	}

	// reward = 1 * curveFactor * sqrt(totalStaked / 1e18) / blocksPerYear
	reward := big.NewInt(1)
	reward.Mul(reward, thor.InitialCurveFactor)
	reward.Mul(reward, sqrtStake)
	reward.Div(reward, blocksPerYear)
	return reward
}

func isLeapYear(year int) bool {
	if year%4 == 0 {
		if year%100 == 0 {
			return year%400 == 0
		}
		return true
	}
	return false
}
