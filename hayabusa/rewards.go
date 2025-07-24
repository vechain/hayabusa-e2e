package hayabusa

import (
	"math/big"
	"time"

	"github.com/vechain/thor/v2/thor"
)

func GetExpectedReward(totalStaked *big.Int) *big.Int {
	currentYear := time.Now().Year()
	isLeap := false
	if currentYear%4 == 0 {
		if currentYear%100 == 0 {
			isLeap = currentYear%400 == 0
		} else {
			isLeap = true
		}
	}

	bigE18 := big.NewInt(1e18)
	sqrtStake := new(big.Int).Sqrt(new(big.Int).Div(totalStaked, bigE18))
	sqrtStake.Mul(sqrtStake, bigE18)

	blocksPerYear := big.NewInt(3153600)
	scalingFactor := big.NewInt(64)
	targetFactor := big.NewInt(1200)

	if isLeap {
		blocksPerYear = new(big.Int).Sub(blocksPerYear, big.NewInt(thor.SeederInterval))
	}

	reward := big.NewInt(1)
	reward.Mul(reward, targetFactor)
	reward.Mul(reward, scalingFactor)
	reward.Mul(reward, sqrtStake)
	reward.Div(reward, blocksPerYear)

	return reward
}
