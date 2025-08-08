package energy

import (
	"math/big"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/hayabusa-e2e/utils"
	native "github.com/vechain/thor/v2/builtin"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func TestEnergy(t *testing.T) {
	testutil.RunFlakyTest(t, func() error {
		return runEnergyTest(t)
	})
}

func runEnergyTest(t *testing.T) error {
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
		Name:              t.Name(),
	}
	growthStopTimeKey := thor.Blake2b([]byte("growth-stop-time"))

	network, err := hayabusa.NewNetwork(config, t.Context())
	require.NoError(t, err)
	t.Cleanup(network.Stop)
	require.NoError(t, network.Start())
	client := network.ThorClient()

	staker, err := builtin.NewStaker(client)
	require.NoError(t, err)
	energy, err := builtin.NewEnergy(client)
	require.NoError(t, err)

	require.NoError(t, utils.WaitForFork(staker, config.ForkBlock))

	senders := &utils.Senders{}
	validators := 3
	stake := builtin.MinStake()
	for i := range validators {
		acc := hayabusa.ValidatorAccounts[i]
		sender := staker.AddValidation(acc.Address(), stake, config.MinStakingPeriod).Send().WithSigner(acc).WithOptions(testutil.TxOptions())
		senders.Add(sender)
	}
	receipts, _, err := senders.Send(testutil.TxContext(t))
	require.NoError(t, err)
	for _, receipt := range receipts {
		assert.False(t, receipt.Reverted)
	}
	delegationStake := big.NewInt(0).Mul(builtin.MinStake(), big.NewInt(10))
	testutil.Send(t, hayabusa.Stargate, staker.AddDelegation(hayabusa.ValidatorAccounts[0].Address(), delegationStake, 200))

	genesisVET := big.NewInt(0)
	genesisVTHO := big.NewInt(0)
	for _, acc := range network.Genesis().Accounts {
		genesisVET = genesisVET.Add(genesisVET, (*big.Int)(acc.Balance))
		genesisVTHO = genesisVTHO.Add(genesisVTHO, (*big.Int)(acc.Energy))
	}

	assertSupply := func(blockNum uint32, expectedSupply *big.Int) {
		supply, err := energy.Revision(strconv.FormatUint(uint64(blockNum), 10)).TotalSupply()
		require.NoError(t, err)

		require.Equal(t, expectedSupply.Cmp(supply), 0, "block %d: expected %s, got %s", blockNum, expectedSupply.String(), supply.String())
	}

	require.NoError(t, utils.WaitForPOS(staker, config.ForkBlock+config.TransitionPeriod))

	genesisBlock, err := client.Block("0")
	require.NoError(t, err)

	// check PoA + transition period growth -> Should use legacy growth rate
	for i := uint32(1); i < config.ForkBlock+config.TransitionPeriod; i++ {
		currentBlock, err := client.Block(strconv.FormatUint(uint64(i), 10))
		require.NoError(t, err)

		growth := new(big.Int).SetUint64(currentBlock.Timestamp - genesisBlock.Timestamp)
		growth.Mul(growth, genesisVET)
		growth.Mul(growth, thor.EnergyGrowthRate)
		growth.Div(growth, big.NewInt(1e18))

		expectedSupply := new(big.Int).Add(genesisVTHO, growth)
		assertSupply(i, expectedSupply)
	}
	t.Logf("✅ - PoA & Transition Period growth is as expected")
	stopTime, err := client.AccountStorage(&native.Energy.Address, &growthStopTimeKey)
	assert.NoError(t, err)

	stopTimeParsed, _ := new(big.Int).SetString(strings.TrimPrefix(stopTime.Value, "0x"), 16)
	println("stop time =====", stopTimeParsed.Uint64(), genesisBlock.Timestamp)
	block := uint32((stopTimeParsed.Uint64() - genesisBlock.Timestamp) / 10) // last PoA block

	poaBlock := block
	lastPOASupply, err := energy.Revision(strconv.FormatUint(uint64(block), 10)).TotalSupply()
	require.NoError(t, err)

	validatorStaker := new(big.Int).Mul(stake, big.NewInt(int64(validators)))
	totalStake := new(big.Int).Add(validatorStaker, delegationStake)
	hayabusaGrowth := hayabusa.GetExpectedReward(totalStake)

	firstPoSBlock := poaBlock + 1
	block = config.ForkBlock + config.TransitionPeriod + config.MinStakingPeriod // wait for 1 staking period
	require.NoError(t, utils.NewTicker(staker.Raw().Client()).WaitForBlock(block))

	// check PoS growth -> Should use Hayabusa growth rate
	acc1Blocks := 0
	for i := firstPoSBlock; i < block; i++ {
		blockDiff := i - poaBlock
		increase := new(big.Int).Mul(hayabusaGrowth, big.NewInt(int64(blockDiff)))
		expectedSupply := new(big.Int).Add(lastPOASupply, increase)
		assertSupply(i, expectedSupply)
		best, err := client.Block(strconv.FormatUint(uint64(i), 10))
		require.NoError(t, err)
		if best.Signer == hayabusa.ValidatorAccounts[0].Address() {
			acc1Blocks++
		}
	}
	t.Logf("✅ - PoS growth is as expected")

	rewards, err := staker.GetDelegatorsRewards(hayabusa.ValidatorAccounts[0].Address(), 1)
	require.NoError(t, err)
	proposerRewardsPerBlock := big.NewInt(0).Mul(hayabusaGrowth, big.NewInt(3))
	proposerRewardsPerBlock = proposerRewardsPerBlock.Div(proposerRewardsPerBlock, big.NewInt(10))
	delegatorRewardsPerBlock := big.NewInt(0).Sub(hayabusaGrowth, proposerRewardsPerBlock)
	expectedRewards := big.NewInt(0).Mul(delegatorRewardsPerBlock, big.NewInt(int64(acc1Blocks)))

	require.Equal(t, expectedRewards, rewards)
	t.Logf("✅ - Staker rewards are as expected")

	return nil
}
