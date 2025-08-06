package hayabusa

import (
	"errors"
	"math/big"
	"slices"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/vechain/networkhub/network/node/genesis"
	"github.com/vechain/thor/v2/builtin"
	devgenesis "github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/runtime"
	"github.com/vechain/thor/v2/test/datagen"
	"github.com/vechain/thor/v2/thor"
)

type Config struct {
	Nodes             int          // The number of nodes to create
	MaxBlockProposers uint32       // The number of max block proposers
	ForkBlock         uint32       // ForkConfig.HAYABUSA
	TransitionPeriod  uint32       // ForkConfig.HAYABUSA_TP
	EpochLength       uint32       // epoch-length
	CooldownPeriod    uint32       // cooldown-period
	MinStakingPeriod  uint32       // staker-low-staking-period
	MidStakingPeriod  uint32       // staker-medium-staking-period
	HighStakingPeriod uint32       // staker-high-staking-period
	StargateAddress   thor.Address // Stargate contract address
	Verbosity         int          // Verbosity level for the nodes
	StakerVerbosity   int          // Staker verbosity level
	Debug             bool         // Debug mode for the nodes
	Name              string       // Name of the network
}

// Apply the configuration to the genesis file.
func (c *Config) Apply(genesis *genesis.CustomGenesis) {
	genesis.LaunchTime = uint64(time.Now().Unix())
	genesis.ForkConfig.AddField("HAYABUSA", c.ForkBlock)
	genesis.ForkConfig.AddField("HAYABUSA_TP", c.TransitionPeriod)
	genesis.ExtraData = datagen.RandomHash().String()

	// staker config - set all values
	stakerIndex := slices.IndexFunc(genesis.Accounts, func(acc devgenesis.Account) bool {
		return acc.Address == builtin.Staker.Address
	})
	if stakerIndex == -1 {
		genesis.Accounts = append(genesis.Accounts, devgenesis.Account{
			Address: builtin.Staker.Address,
			Storage: map[string]thor.Bytes32{},
			Code:    hexutil.Encode(runtime.EmptyRuntimeBytecode),
			Balance: (*devgenesis.HexOrDecimal256)(big.NewInt(0)),
			Energy:  (*devgenesis.HexOrDecimal256)(big.NewInt(0)),
		})
		stakerIndex = len(genesis.Accounts) - 1
	}
	genesis.Accounts[stakerIndex].Storage[nameToBytes32("epoch-length")] = uint32ToBytes32(c.EpochLength, 6)
	genesis.Accounts[stakerIndex].Storage[nameToBytes32("cooldown-period")] = uint32ToBytes32(c.CooldownPeriod, 6)
	genesis.Accounts[stakerIndex].Storage[nameToBytes32("staker-low-staking-period")] = uint32ToBytes32(c.MinStakingPeriod, 6)
	genesis.Accounts[stakerIndex].Storage[nameToBytes32("staker-medium-staking-period")] = uint32ToBytes32(c.MidStakingPeriod, 30)
	genesis.Accounts[stakerIndex].Storage[nameToBytes32("staker-high-staking-period")] = uint32ToBytes32(c.HighStakingPeriod, 180)

	// params config - set max-block-proposers
	paramsIndex := slices.IndexFunc(genesis.Accounts, func(acc devgenesis.Account) bool {
		return acc.Address == builtin.Params.Address
	})
	if paramsIndex == -1 {
		genesis.Accounts = append(genesis.Accounts, devgenesis.Account{
			Address: builtin.Params.Address,
			Storage: map[string]thor.Bytes32{},
			Code:    "",
			Balance: (*devgenesis.HexOrDecimal256)(big.NewInt(0)),
			Energy:  (*devgenesis.HexOrDecimal256)(big.NewInt(0)),
		})
		paramsIndex = len(genesis.Accounts) - 1
	}
	genesis.Accounts[paramsIndex].Storage[nameToBytes32("max-block-proposers")] = uint32ToBytes32(c.MaxBlockProposers, 3)

	addr := Stargate.Address()
	if !c.StargateAddress.IsZero() {
		addr = c.StargateAddress
	}
	genesis.Accounts[paramsIndex].Storage[ParamsStargateKey] = thor.BytesToBytes32(addr.Bytes())
}

func (c *Config) Validate() error {
	if c.MinStakingPeriod%c.EpochLength != 0 {
		return errors.New("staker-low-staking-period must be a multiple of epoch-length")
	}
	if c.MidStakingPeriod%c.EpochLength != 0 {
		return errors.New("staker-medium-staking-period must be a multiple of epoch-length")
	}
	if c.HighStakingPeriod%c.EpochLength != 0 {
		return errors.New("staker-high-staking-period must be a multiple of epoch-length")
	}
	if c.TransitionPeriod%c.EpochLength != 0 {
		return errors.New("hayabusa-transition-period must be a multiple of epoch-length")
	}
	return nil
}
