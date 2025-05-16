package energy

import (
	"math/big"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vechain/draupnir/common"
	"github.com/vechain/draupnir/contracts"
	"github.com/vechain/hayabusa-e2e/builtins"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/thor/v2/thor"
)

func TestEnergy(t *testing.T) {
	config := &hayabusa.Config{
		Nodes:             3,
		MaxBlockProposers: 3,
		ForkBlock:         4,
		TransitionPeriod:  4,
		EpochLength:       4,
		CooldownPeriod:    4,
		MinStakingPeriod:  4,
		MidStakingPeriod:  12,
		HighStakingPeriod: 180,
	}
	genesis := hayabusa.Genesis(config)
	client, _, cancel, err := hayabusa.StartNetwork(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cancel)

	staker := builtins.NewStaker(client, hayabusa.ValidatorAccounts[0].PrivateKey)
	energy := builtins.NewEnergy(client, hayabusa.ValidatorAccounts[0].PrivateKey)

	require.NoError(t, staker.WaitForFork(config.ForkBlock))

	senders := &contracts.Senders{}
	validators := 3
	stake := builtins.MinStake
	for i := 0; i < validators; i++ {
		acc := hayabusa.ValidatorAccounts[i]
		sender := staker.Attach(acc.PrivateKey).AddValidator(acc.Address, stake, config.MinStakingPeriod, true)
		senders.Add(sender)
	}
	_, receipts, err := senders.Send(false)
	require.NoError(t, err)

	genesisVET := big.NewInt(0)
	genesisVTHO := big.NewInt(0)
	for _, acc := range genesis.Accounts {
		genesisVET = genesisVET.Add(genesisVET, (*big.Int)(acc.Balance))
		genesisVTHO = genesisVTHO.Add(genesisVTHO, (*big.Int)(acc.Energy))
	}

	growth := new(big.Int).SetUint64(10)
	growth.Mul(growth, genesisVET)
	growth.Mul(growth, thor.EnergyGrowthRate)
	growth.Div(growth, big.NewInt(1e18))

	assertSupply := func(blockNum uint32, expectedSupply *big.Int) {
		block, err := client.Block(strconv.FormatUint(uint64(blockNum), 10))
		require.NoError(t, err)

		supply, err := energy.Revision(block.ID).TotalSupply()
		require.NoError(t, err)

		require.Equal(t, expectedSupply.Cmp(supply), 0, "block %d: expected %s, got %s", blockNum, expectedSupply.String(), supply.String())
	}

	require.NoError(t, staker.WaitForPOS(config.ForkBlock+config.TransitionPeriod))

	// check PoA + transition period growth -> Should use legacy growth rate
	for i := uint32(1); i < config.ForkBlock+config.TransitionPeriod; i++ {
		increase := new(big.Int).Mul(growth, big.NewInt(int64(i)))
		expectedSupply := new(big.Int).Add(genesisVTHO, increase)
		assertSupply(i, expectedSupply)
	}
	t.Logf("✅ - PoA & Transition Period growth is as expected")
	block := config.ForkBlock + config.TransitionPeriod - 1 // last PoA block
	poaBlock, err := client.Block(strconv.FormatUint(uint64(block), 10))
	require.NoError(t, err)
	lastPOASupply, err := energy.Revision(poaBlock.ID).TotalSupply()
	require.NoError(t, err)

	hayabusaGrowth := hayabusa.GetExpectedReward(new(big.Int).Mul(stake, big.NewInt(int64(validators))))

	firstPoSBlock := poaBlock.Number + 1
	block = config.ForkBlock + config.TransitionPeriod + config.MinStakingPeriod // wait for 1 staking period
	require.NoError(t, common.NewTicker(staker.Client()).WaitForBlock(block))

	// check PoS growth -> Should use Hayabusa growth rate
	for i := firstPoSBlock; i < block; i++ {
		blockDiff := i - poaBlock.Number
		increase := new(big.Int).Mul(hayabusaGrowth, big.NewInt(int64(blockDiff)))
		expectedSupply := new(big.Int).Add(lastPOASupply, increase)
		assertSupply(i, expectedSupply)
	}
	t.Logf("✅ - PoS growth is as expected")

	actualStakerRewards := new(big.Int)
	for _, receipt := range receipts {
		validatorID := receipt.Outputs[0].Events[0].Topics[3]
		rewards, err := staker.GetRewards(validatorID, 1)
		require.NoError(t, err)
		actualStakerRewards = actualStakerRewards.Add(actualStakerRewards, rewards)
	}

	// growth per block * number of blocks in a staking period
	expectedStakerRewards := new(big.Int).Mul(hayabusaGrowth, big.NewInt(int64(config.MinStakingPeriod)))
	require.Equal(t, expectedStakerRewards, actualStakerRewards)
	t.Logf("✅ - Staker rewards are as expected")
}
