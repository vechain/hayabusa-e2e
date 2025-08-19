package testutil

import (
	"log/slog"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func SetupTestNetworkWithEpochAndBlockInterval(t *testing.T, maxBlockProposers uint32, epochLength uint32, blockInterval uint64) (*hayabusa.Config, *thorclient.Client, hayabusa.Network) {
	config := &hayabusa.Config{
		Nodes:                      6,
		MaxBlockProposers:          maxBlockProposers,
		ForkBlock:                  0,
		TransitionPeriod:           10,
		EpochLength:                epochLength,
		CooldownPeriod:             2,
		MinStakingPeriod:           4,
		MidStakingPeriod:           12,
		HighStakingPeriod:          259200,
		Name:                       t.Name(),
		BlockInterval:              blockInterval,
		ValidatorEvictionThreshold: 10,
	}

	network, err := hayabusa.NewNetwork(config, t.Context())
	require.NoError(t, err)
	t.Cleanup(network.Stop)
	require.NoError(t, network.Start())
	return config, network.ThorClient(), *network
}

func SetupStakerAndWaitForFork(t *testing.T, client *thorclient.Client, config *hayabusa.Config) *builtin.Staker {
	staker, err := builtin.NewStaker(client)
	require.NoError(t, err)
	require.NoError(t, utils.WaitForFork(staker, config.ForkBlock))
	return staker
}

func AddValidatorWithStake(seq *TxSequence, staker *builtin.Staker, nodePair *hayabusa.NodePair, stake *big.Int, period uint32) thor.Address {
	seq.Send(nodePair.Endorser, staker.AddValidation(nodePair.Node.Address(), stake, period))
	amount := big.NewInt(0).Quo(stake, big.NewInt(1e18))
	slog.Info("✅ - added validator", "validator", nodePair.Node.Address().String(), "period", period, "stake", amount, "id", nodePair.Node.Address().String())

	return nodePair.Node.Address()
}

func AddValidator(seq *TxSequence, staker *builtin.Staker, nodePair *hayabusa.NodePair, period uint32) thor.Address {
	return AddValidatorWithStake(seq, staker, nodePair, CalculateValidatorStake(), period)
}

func CalculateValidatorStake() *big.Int {
	stake := big.NewInt(1e18)
	stake = new(big.Int).Mul(stake, big.NewInt(1e6))
	stake = new(big.Int).Mul(stake, big.NewInt(25))
	return stake
}
