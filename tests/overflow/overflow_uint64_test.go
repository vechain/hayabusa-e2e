package overflow

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func makeOverflowWei(targetWei *big.Int) *big.Int {
	two64 := new(big.Int).Lsh(big.NewInt(1), 64)
	e18 := big.NewInt(1e18)
	two64Wei := new(big.Int).Mul(two64, e18)
	return new(big.Int).Add(two64Wei, new(big.Int).Set(targetWei))
}

func newHugeConfig(name string, maxBlockProposers uint32) *hayabusa.Config {
	return &hayabusa.Config{
		Nodes:                      6,
		MaxBlockProposers:          maxBlockProposers,
		ForkBlock:                  0,
		TransitionPeriod:           10,
		EpochLength:                2,
		CooldownPeriod:             2,
		MinStakingPeriod:           4,
		MidStakingPeriod:           12,
		HighStakingPeriod:          259200,
		Name:                       name,
		BlockInterval:              uint64(5),
		ValidatorEvictionThreshold: 10,
		EvictionCheckInterval:      20,
		HugeBalances:               true,
	}
}

func Test_AddValidation_Overflow_TruncatesToUint64Remainder(t *testing.T) {
	t.Parallel()
	cfg := newHugeConfig(t.Name(), 3)
	net, err := hayabusa.NewNetwork(cfg, t.Context())
	require.NoError(t, err)
	t.Cleanup(net.Stop)
	require.NoError(t, net.Start())

	staker, err := builtin.NewStaker(net.ThorClient())
	require.NoError(t, err)
	require.NoError(t, utils.WaitForFork(t.Context(), staker, cfg.ForkBlock))

	account := hayabusa.ValidatorAccounts[0]
	targetWei := testutil.CalculateValidatorStake()
	overflowWei := makeOverflowWei(targetWei)

	receipt := testutil.Send(t, account.Endorser, staker.AddValidation(account.Node.Address(), overflowWei, cfg.MinStakingPeriod))
	require.False(t, receipt.Reverted)

	queued, err := staker.QueuedStake()
	require.NoError(t, err)
	assert.Equal(t, targetWei, queued)
}

func Test_IncreaseStake_Overflow_TruncatesDeltaQueued(t *testing.T) {
	t.Parallel()
	cfg := newHugeConfig(t.Name(), 3)
	net, err := hayabusa.NewNetwork(cfg, t.Context())
	require.NoError(t, err)
	t.Cleanup(net.Stop)
	require.NoError(t, net.Start())

	staker, err := builtin.NewStaker(net.ThorClient())
	require.NoError(t, err)
	require.NoError(t, utils.WaitForFork(t.Context(), staker, cfg.ForkBlock))

	seq := testutil.NewTxSequence(t)
	id1 := testutil.AddValidator(seq, staker, hayabusa.ValidatorAccounts[0], cfg.MinStakingPeriod)
	_ = testutil.AddValidator(seq, staker, hayabusa.ValidatorAccounts[1], cfg.MinStakingPeriod)
	_ = testutil.AddValidator(seq, staker, hayabusa.ValidatorAccounts[2], cfg.MinStakingPeriod)

	beforeQueued, err := staker.QueuedStake()
	require.NoError(t, err)

	block := cfg.ForkBlock + cfg.TransitionPeriod
	require.NoError(t, utils.WaitForPOS(t.Context(), staker, block))
	_, firstActive, err := staker.FirstActive()
	require.NoError(t, err)
	require.Equal(t, id1, firstActive)

	incTarget := testutil.CalculateValidatorStake()
	incOverflow := makeOverflowWei(incTarget)
	r := testutil.Send(t, hayabusa.ValidatorAccounts[0].Endorser, staker.IncreaseStake(id1, incOverflow))
	require.False(t, r.Reverted)

	afterQueued, err := staker.QueuedStake()
	require.NoError(t, err)
	expected := new(big.Int).Add(beforeQueued, incTarget)
	assert.Equal(t, expected, afterQueued)
}

func Test_DecreaseStake_Overflow_TruncatesLockedDecrease(t *testing.T) {
	t.Parallel()
	cfg := newHugeConfig(t.Name(), 3)
	net, err := hayabusa.NewNetwork(cfg, t.Context())
	require.NoError(t, err)
	t.Cleanup(net.Stop)
	require.NoError(t, net.Start())

	staker, err := builtin.NewStaker(net.ThorClient())
	require.NoError(t, err)
	require.NoError(t, utils.WaitForFork(t.Context(), staker, cfg.ForkBlock))

	seq := testutil.NewTxSequence(t)
	id1 := testutil.AddValidator(seq, staker, hayabusa.ValidatorAccounts[0], cfg.MinStakingPeriod)
	_ = testutil.AddValidator(seq, staker, hayabusa.ValidatorAccounts[1], cfg.MinStakingPeriod)
	_ = testutil.AddValidator(seq, staker, hayabusa.ValidatorAccounts[2], cfg.MinStakingPeriod)

	block := cfg.ForkBlock + cfg.TransitionPeriod
	require.NoError(t, utils.NewTicker(staker.Raw().Client()).WaitForBlock(block))
	_, firstActive, err := staker.FirstActive()
	require.NoError(t, err)
	require.Equal(t, id1, firstActive)

	inc := new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1e6))
	inc.Mul(inc, big.NewInt(5))
	_ = testutil.Send(t, hayabusa.ValidatorAccounts[0].Endorser, staker.IncreaseStake(id1, inc))

	require.NoError(t, utils.NewTicker(staker.Raw().Client()).WaitForBlock(block+cfg.MinStakingPeriod))

	totalsBefore, err := staker.GetValidationTotals(id1)
	require.NoError(t, err)

	decTarget := new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1e6))
	decTarget.Mul(decTarget, big.NewInt(3))
	decOverflow := makeOverflowWei(decTarget)
	_ = testutil.Send(t, hayabusa.ValidatorAccounts[0].Endorser, staker.DecreaseStake(id1, decOverflow))

	require.NoError(t, utils.NewTicker(staker.Raw().Client()).WaitForBlock(block+cfg.MinStakingPeriod*2))

	totalsAfter, err := staker.GetValidationTotals(id1)
	require.NoError(t, err)
	expectedLocked := new(big.Int).Sub(totalsBefore.TotalLockedStake, decTarget)
	assert.Equal(t, expectedLocked, totalsAfter.TotalLockedStake)
}

func Test_AddDelegation_Overflow_TruncatesStake(t *testing.T) {
	t.Parallel()
	cfg := newHugeConfig(t.Name(), 3)
	net, err := hayabusa.NewNetwork(cfg, t.Context())
	require.NoError(t, err)
	t.Cleanup(net.Stop)
	require.NoError(t, net.Start())

	staker, err := builtin.NewStaker(net.ThorClient())
	require.NoError(t, err)
	require.NoError(t, utils.WaitForFork(t.Context(), staker, cfg.ForkBlock))

	seq := testutil.NewTxSequence(t)
	id1 := testutil.AddValidator(seq, staker, hayabusa.ValidatorAccounts[0], cfg.MinStakingPeriod)

	target := builtin.MinStake()
	overflow := makeOverflowWei(target)
	receipt := testutil.Send(t, hayabusa.Stargate, staker.AddDelegation(id1, overflow, uint8(100)))
	delegationID := new(big.Int).SetBytes(receipt.Outputs[0].Events[0].Topics[2][:])

	del, err := staker.GetDelegation(delegationID)
	require.NoError(t, err)
	assert.Equal(t, target, del.Stake)
}
