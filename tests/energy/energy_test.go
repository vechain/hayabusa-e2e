package energy

import (
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/thor/v2/thorclient/bind"
	"math/big"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/builtin"
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

	staker, err := builtin.NewStaker(client)
	require.NoError(t, err)
	energy, err := builtin.NewEnergy(client)
	require.NoError(t, err)

	require.NoError(t, utils.WaitForFork(staker, config.ForkBlock))

	senders := &bind.Senders{}
	validators := 3
	stake := builtin.MinStake()
	for i := 0; i < validators; i++ {
		acc := hayabusa.ValidatorAccounts[i]
		sender := staker.AddValidator(acc, acc.Address(), stake, config.MinStakingPeriod, true)
		senders.Add(sender)
	}
	receipts, _, err := senders.Send(testutil.TxContext(t), &bind.TxOptions{})
	require.NoError(t, err)

	genesisVET := big.NewInt(0)
	genesisVTHO := big.NewInt(0)
	for _, acc := range genesis.Accounts {
		genesisVET = genesisVET.Add(genesisVET, (*big.Int)(acc.Balance))
		genesisVTHO = genesisVTHO.Add(genesisVTHO, (*big.Int)(acc.Energy))
	}

	assertSupply := func(blockNum uint32, expectedSupply *big.Int) {
		supply, err := energy.Revision(strconv.FormatUint(uint64(blockNum), 10)).TotalSupply()
		require.NoError(t, err)

		require.Equal(t, expectedSupply.Cmp(supply), 0, "block %d: expected %s, got %s", blockNum, expectedSupply.String(), supply.String())
	}

	require.NoError(t, utils.WaitForPOS(staker, config.ForkBlock+config.TransitionPeriod))

	genesisBlock, err := client.GetBlock("0")
	require.NoError(t, err)

	// check PoA + transition period growth -> Should use legacy growth rate
	for i := uint32(1); i < config.ForkBlock+config.TransitionPeriod; i++ {
		currentBlock, err := client.GetBlock(strconv.FormatUint(uint64(i), 10))
		require.NoError(t, err)

		growth := new(big.Int).SetUint64(currentBlock.Timestamp - genesisBlock.Timestamp)
		growth.Mul(growth, genesisVET)
		growth.Mul(growth, thor.EnergyGrowthRate)
		growth.Div(growth, big.NewInt(1e18))

		expectedSupply := new(big.Int).Add(genesisVTHO, growth)
		assertSupply(i, expectedSupply)
	}
	t.Logf("✅ - PoA & Transition Period growth is as expected")
	block := config.ForkBlock + config.TransitionPeriod - 1 // last PoA block
	poaBlock := block
	lastPOASupply, err := energy.Revision(strconv.FormatUint(uint64(block), 10)).TotalSupply()
	require.NoError(t, err)

	hayabusaGrowth := hayabusa.GetExpectedReward(new(big.Int).Mul(stake, big.NewInt(int64(validators))))

	firstPoSBlock := poaBlock + 1
	block = config.ForkBlock + config.TransitionPeriod + config.MinStakingPeriod // wait for 1 staking period
	require.NoError(t, utils.NewTicker(staker.Raw().Client()).WaitForBlock(block))

	// check PoS growth -> Should use Hayabusa growth rate
	for i := firstPoSBlock; i < block; i++ {
		blockDiff := i - poaBlock
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
