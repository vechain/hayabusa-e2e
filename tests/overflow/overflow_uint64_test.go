package overflow

import (
	"math/big"
	"testing"

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

func Test_AddValidation_Overflow_StakerAboveMaxSupply(t *testing.T) {
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

	sender := staker.AddValidation(account.Node.Address(), overflowWei, cfg.MinStakingPeriod)
	receipt, _, err := sender.Send().
		WithOptions(testutil.TxOptions()).
		WithSigner(account.Endorser).
		SubmitAndConfirm(testutil.TxContext(t))
	require.NoError(t, err)
	require.True(t, receipt.Reverted)
	_, err = sender.Call().
		AtRevision(receipt.Meta.BlockID.String()).
		Caller(&receipt.Meta.TxOrigin).
		Execute()
	require.Error(t, err)
	require.Equal(t, "contract call reverted (contract=0x00000000000000000000000000005374616b6572, method=addValidation, value=18446744073734551616000000000000000000, args=[0xc2c76defc505fc15bf6a768a8c8e760bb4844124, 4]): staker: stake is above max supply | VM error: execution reverted", err.Error())

	// We should not be able to ever send a negative stake since the types in Solidity + RLP do not allow it
	sender = staker.AddValidation(account.Node.Address(), big.NewInt(-1), cfg.MinStakingPeriod)
	_, _, err = sender.Send().
		WithOptions(testutil.TxOptions()).
		WithSigner(account.Endorser).
		SubmitAndConfirm(testutil.TxContext(t))
	require.Error(t, err)
	require.Equal(t, "failed to send transaction (contract=0x00000000000000000000000000005374616b6572, method=addValidation, value=-1, args=[0xc2c76defc505fc15bf6a768a8c8e760bb4844124, 4]): unable to encode transaction - rlp: cannot encode negative *big.Int", err.Error())
}

func Test_IncreaseStake_Overflow_StakerAboveMaxSupply(t *testing.T) {
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
	require.NoError(t, utils.WaitForPOS(t.Context(), staker, block))
	_, firstActive, err := staker.FirstActive()
	require.NoError(t, err)
	require.Equal(t, id1, firstActive)

	incTarget := testutil.CalculateValidatorStake()
	incOverflow := makeOverflowWei(incTarget)
	sender := staker.IncreaseStake(id1, incOverflow)

	receipt, _, _ := sender.Send().
		WithOptions(testutil.TxOptions()).
		WithSigner(hayabusa.ValidatorAccounts[0].Endorser).
		SubmitAndConfirm(testutil.TxContext(t))
	require.True(t, receipt.Reverted)
	_, err = sender.Call().
		AtRevision(receipt.Meta.BlockID.String()).
		Caller(&receipt.Meta.TxOrigin).
		Execute()
	require.Error(t, err)
	require.Equal(t, "contract call reverted (contract=0x00000000000000000000000000005374616b6572, method=increaseStake, value=18446744073734551616000000000000000000, args=[0xc2c76defc505fc15bf6a768a8c8e760bb4844124]): staker: stake is above max supply | VM error: execution reverted", err.Error())
}

func Test_DecreaseStake_Overflow_StakerAboveMaxSupply(t *testing.T) {
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

	decTarget := new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1e6))
	decTarget.Mul(decTarget, big.NewInt(3))
	decOverflow := makeOverflowWei(decTarget)

	sender := staker.DecreaseStake(id1, decOverflow)
	receipt, _, _ := sender.Send().
		WithOptions(testutil.TxOptions()).
		WithSigner(hayabusa.ValidatorAccounts[0].Endorser).
		SubmitAndConfirm(testutil.TxContext(t))
	require.True(t, receipt.Reverted)
	_, err = sender.Call().
		AtRevision(receipt.Meta.BlockID.String()).
		Caller(&receipt.Meta.TxOrigin).
		Execute()
	require.Error(t, err)
	require.Equal(t, "contract call reverted (contract=0x00000000000000000000000000005374616b6572, method=decreaseStake, args=[0xc2c76defc505fc15bf6a768a8c8e760bb4844124, 18446744073712551616000000000000000000]): staker: stake is above max supply | VM error: execution reverted", err.Error())
}

func Test_AddDelegation_Overflow_StakerAboveMaxSupply(t *testing.T) {
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

	sender := staker.AddDelegation(id1, overflow, uint8(100))
	receipt, _, _ := sender.Send().
		WithOptions(testutil.TxOptions()).
		WithSigner(hayabusa.Stargate).
		SubmitAndConfirm(testutil.TxContext(t))
	require.True(t, receipt.Reverted)
	_, err = sender.Call().
		AtRevision(receipt.Meta.BlockID.String()).
		Caller(&receipt.Meta.TxOrigin).
		Execute()
	require.Error(t, err)
	require.Equal(t, "contract call reverted (contract=0x00000000000000000000000000005374616b6572, method=addDelegation, value=18446744073734551616000000000000000000, args=[0xc2c76defc505fc15bf6a768a8c8e760bb4844124, 100]): staker: stake is above max supply | VM error: execution reverted", err.Error())
}
