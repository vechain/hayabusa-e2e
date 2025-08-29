package validators

import (
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/lifecycle"
	"github.com/vechain/hayabusa-e2e/hayabusa"
)

type Config struct {
	lifecycle.Config
	Account             *hayabusa.NodePair
	StakeChangeInterval uint32 // interval in staking periods to change stake
}
