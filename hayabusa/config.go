package hayabusa

import (
	"math/big"
	"slices"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/vechain/networkhub/network/node/genesis"
	"github.com/vechain/thor/v2/builtin"
	devgenesis "github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/runtime"
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
	Debug             bool         // Debug mode for the nodes
}

// Apply the configuration to the genesis file.
func (h Config) Apply(genesis *genesis.CustomGenesis) {
	genesis.LaunchTime = uint64(time.Now().Unix())
	genesis.ForkConfig.AddField("HAYABUSA", h.ForkBlock)
	genesis.ForkConfig.AddField("HAYABUSA_TP", h.TransitionPeriod)

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
	genesis.Accounts[stakerIndex].Storage[nameToBytes32("epoch-length")] = uint32ToBytes32(h.EpochLength, 6)
	genesis.Accounts[stakerIndex].Storage[nameToBytes32("cooldown-period")] = uint32ToBytes32(h.CooldownPeriod, 6)
	genesis.Accounts[stakerIndex].Storage[nameToBytes32("staker-low-staking-period")] = uint32ToBytes32(h.MinStakingPeriod, 6)
	genesis.Accounts[stakerIndex].Storage[nameToBytes32("staker-medium-staking-period")] = uint32ToBytes32(h.MidStakingPeriod, 30)
	genesis.Accounts[stakerIndex].Storage[nameToBytes32("staker-high-staking-period")] = uint32ToBytes32(h.HighStakingPeriod, 180)

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
	genesis.Accounts[paramsIndex].Storage[nameToBytes32("max-block-proposers")] = uint32ToBytes32(h.MaxBlockProposers, 3)

	addr := Stargate.Address()
	if !h.StargateAddress.IsZero() {
		addr = h.StargateAddress
	}
	genesis.Accounts[paramsIndex].Storage[ParamsStargateKey] = thor.BytesToBytes32(addr.Bytes())
}
