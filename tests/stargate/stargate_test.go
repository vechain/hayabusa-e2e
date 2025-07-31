package stargate

import (
	"log/slog"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/hayabusa/stargate"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/test/datagen"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/builtin"
	"github.com/vechain/thor/v2/tx"
)

func Test_Stargate_SingleDelegator(t *testing.T) {
	t.Parallel()
	testutil.RunFlakyTest(t, func() error {
		return runTestStargateSingleDelegator(t)
	})
}

func waitForCompletedPeriods(ticker *utils.Ticker, staker *builtin.Staker, validationID thor.Address, expectedPeriods uint32) error {
	err := ticker.WaitForCondition(time.Minute*1, func() (bool, error) {
		completed, err := staker.GetCompletedPeriods(validationID)
		slog.Info("⚠️ - completed periods, waiting for greater or equal than expected", "completed", int(*completed), "expected", expectedPeriods)
		if err != nil {
			return false, err
		}
		return *completed >= expectedPeriods, nil
	})
	if err != nil {
		return testutil.StakerStatusUnknownError{ValidationID: validationID.String()}
	}
	return nil
}

func runTestStargateSingleDelegator(t *testing.T) error {
	staker, stargate, config, validationIDs := newDelegationSetup(t)

	validationID := validationIDs[0]
	ticker := utils.NewTicker(staker.Raw().Client())
	validation, err := staker.Get(validationID)
	require.NoError(t, err)

	// wait for the validator to complete 1 staking period
	block := config.ForkBlock + config.TransitionPeriod + config.MinStakingPeriod
	require.NoError(t, ticker.WaitForBlock(block))
	expectedCompletedPeriods := uint32(1)
	err = waitForCompletedPeriods(ticker, staker, validationID, expectedCompletedPeriods)
	if err != nil {
		return err
	}
	completed, err := staker.GetCompletedPeriods(validationID)
	require.NoError(t, err)
	assert.Equal(t, expectedCompletedPeriods, *completed)

	// add the delegation
	acc := hayabusa.AdditionalAccounts[0]
	stake := new(big.Int).Mul(builtin.MinStake(), big.NewInt(10)) // very large stake
	receipt := testutil.Send(t, acc, stargate.AddDelegator(validationID, 200, stake))
	require.NoError(t, err)
	delegationID := receiptToDelegationID(t, receipt)

	// assert correct start period
	completed, err = staker.GetCompletedPeriods(validationID)
	require.NoError(t, err)
	assert.Equal(t, expectedCompletedPeriods, *completed)
	delegation, err := staker.GetDelegation(delegationID)
	require.NoError(t, err)
	assert.Equal(t, 3, int(delegation.StartPeriod))

	// assert no claimable periods
	claimable, start, end, err := stargate.GetClaimable(acc.Address())
	require.NoError(t, err)
	assert.Equal(t, 0, claimable.Sign())
	// start is after end, so no claimable periods
	assert.Equal(t, 3, int(start))
	assert.Equal(t, 1, int(end))

	// wait for 1 staking period
	block += config.MinStakingPeriod
	require.NoError(t, ticker.WaitForBlock(block))

	// assert validator completed 1 more period
	expectedCompletedPeriods = 2
	err = waitForCompletedPeriods(ticker, staker, validationID, expectedCompletedPeriods)
	if err != nil {
		return err
	}
	completed, err = staker.GetCompletedPeriods(validationID)
	assert.NoError(t, err)
	assert.Equal(t, expectedCompletedPeriods, *completed)

	// assert delegator can claim for that period
	_, start, end, err = stargate.GetClaimable(acc.Address())
	assert.NoError(t, err)
	assert.Equal(t, 3, int(start))
	assert.Equal(t, 2, int(end))

	// assert TVL
	expected := new(big.Int).Mul(builtin.MinStake(), big.NewInt(int64(len(validationIDs))))
	expected = expected.Add(expected, stake)
	lockedVET, _, err := staker.TotalStake()
	require.NoError(t, err)
	assert.Equal(t, expected, lockedVET)

	firstDelegatedBlock := config.ForkBlock + config.TransitionPeriod + 2*config.MinStakingPeriod
	block += 2 * config.MinStakingPeriod
	require.NoError(t, ticker.WaitForBlock(block-1))

	t.Logf("✅ - checking delegated blocks (from %d to %d)", firstDelegatedBlock, block-1)
	blockCount := 0
	for i := firstDelegatedBlock; i < block; i++ {
		block, err := staker.Raw().Client().Block(strconv.Itoa(int(i)))
		require.NoError(t, err)
		if block.Signer == validation.Address {
			blockCount++
		}
	}

	// these rewards are the expected rewards
	lockedVET, _, err = staker.TotalStake()
	require.NoError(t, err)
	blockReward := hayabusa.GetExpectedReward(lockedVET)
	allRewards := new(big.Int).Mul(blockReward, big.NewInt(int64(blockCount)))

	proposerReward := new(big.Int).Set(blockReward)
	proposerReward = proposerReward.Mul(proposerReward, big.NewInt(3))
	proposerReward = proposerReward.Div(proposerReward, big.NewInt(10))

	delegatorReward := new(big.Int).Sub(blockReward, proposerReward)
	t.Logf("✅ rewards per block: proposer=%s, delegator=%s, blockCount=%d, total=%s", proposerReward.String(), delegatorReward.String(), blockCount, allRewards.String())
	delegatorReward = delegatorReward.Mul(delegatorReward, big.NewInt(int64(blockCount)))
	proposerReward = proposerReward.Mul(proposerReward, big.NewInt(int64(blockCount)))
	t.Logf("✅ total rewards: proposer=%s, delegator=%s, combined=%s", proposerReward.String(), delegatorReward.String(), new(big.Int).Add(proposerReward, delegatorReward).String())

	stargateAcc, err := staker.Raw().Client().Account(stargate.Address())
	require.NoError(t, err)
	stargateEnergy := (*big.Int)(stargateAcc.Energy)
	assert.Equal(t, delegatorReward, stargateEnergy, "stargate energy should be equal to the expected reward, difference: %s", new(big.Int).Sub(delegatorReward, stargateEnergy).String())

	// wait for housekeeping on first block of next staking period
	require.NoError(t, ticker.WaitForBlock(block))
	expectedCompletedPeriods = 4
	err = waitForCompletedPeriods(ticker, staker, validationID, expectedCompletedPeriods)
	if err != nil {
		return err
	}
	completed, err = staker.GetCompletedPeriods(validationID)
	assert.NoError(t, err)
	assert.Equal(t, expectedCompletedPeriods, *completed)

	t.Logf("PER Block reward: %s", blockReward.String())
	res, err := stargate.Raw().Method("getClaimable", acc.Address()).Call().Execute()
	require.NoError(t, err)
	for range res.Events {
		stargate.LogEventValues(res.Events)
	}
	t.Log(res.Data)

	claimable, start, end, err = stargate.GetClaimable(acc.Address())
	assert.NoError(t, err)
	assert.Equal(t, 3, int(start))
	assert.Equal(t, 4, int(end))
	assert.Equal(t, delegatorReward, claimable, "claimable should be equal to the expected reward, difference: %s", new(big.Int).Sub(delegatorReward, claimable).String())

	// these are the actual total rewards for the 4 periods
	totalPeriodRewards := big.NewInt(0)
	for i := start; i <= end; i++ {
		reward, err := staker.GetDelegatorsRewards(validationID, i)
		require.NoError(t, err)
		totalPeriodRewards = totalPeriodRewards.Add(totalPeriodRewards, reward)
	}

	t.Logf("✅ total rewards for: %s", totalPeriodRewards.String())
	assert.Equal(t, delegatorReward, totalPeriodRewards)

	return nil
}

func Test_Stargate_DelegatorFlow_Stake_And_Claim_Auto_Renew_Off(t *testing.T) {
	t.Parallel()
	testutil.RunFlakyTest(t, func() error {
		return runTestStargateDelegatorFlowStakeAndClaimAutoRenewOff(t)
	})
}

func runTestStargateDelegatorFlowStakeAndClaimAutoRenewOff(t *testing.T) error {
	staker, stargate, config, validationIDs := newDelegationSetup(t)

	validationID := validationIDs[0]
	ticker := utils.NewTicker(staker.Raw().Client())
	_, err := staker.Get(validationID)
	require.NoError(t, err)

	// wait for the validator to complete 1 staking period
	block := config.ForkBlock + config.TransitionPeriod + config.MinStakingPeriod
	require.NoError(t, ticker.WaitForBlock(block))
	expectedCompletedPeriods := uint32(1)
	err = waitForCompletedPeriods(ticker, staker, validationID, expectedCompletedPeriods)
	if err != nil {
		return err
	}
	completed, err := staker.GetCompletedPeriods(validationID)
	require.NoError(t, err)
	assert.Equal(t, expectedCompletedPeriods, *completed)

	// add the delegation
	acc := hayabusa.AdditionalAccounts[0]
	stake := new(big.Int).Mul(builtin.MinStake(), big.NewInt(3)) // very large stake
	receipt := testutil.Send(t, acc, stargate.AddDelegator(validationID, 200, stake))
	require.NoError(t, err)
	delegationID := receiptToDelegationID(t, receipt)

	// assert correct start period
	completed, err = staker.GetCompletedPeriods(validationID)
	require.NoError(t, err)
	assert.Equal(t, expectedCompletedPeriods, *completed)
	delegation, err := staker.GetDelegation(delegationID)
	require.NoError(t, err)
	assert.Equal(t, 3, int(delegation.StartPeriod))

	// assert no claimable periods
	claimable, start, end, err := stargate.GetClaimable(acc.Address())
	require.NoError(t, err)
	assert.Equal(t, 0, claimable.Sign())
	// start is after end, so no claimable periods
	assert.Equal(t, 3, int(start))
	assert.Equal(t, 1, int(end))

	// wait for 2 staking periods
	block += config.MinStakingPeriod * 2
	require.NoError(t, ticker.WaitForBlock(block))

	// assert validator completed 2 more period
	expectedCompletedPeriods = 3
	err = waitForCompletedPeriods(ticker, staker, validationID, expectedCompletedPeriods)
	if err != nil {
		return err
	}
	completed, err = staker.GetCompletedPeriods(validationID)
	assert.NoError(t, err)
	assert.Equal(t, expectedCompletedPeriods, *completed)

	// assert delegator can claim for that period
	claimableAmount, start, end, err := stargate.GetClaimable(acc.Address())
	assert.NoError(t, err)
	assert.Equal(t, 3, int(start))
	assert.Equal(t, 3, int(end))

	delegation, err = staker.GetDelegation(delegationID)
	assert.NoError(t, err)
	assert.True(t, delegation.Locked)

	stakerRewards, err := staker.GetDelegatorsRewards(validationID, 3)
	assert.NoError(t, err)
	assert.Equal(t, stakerRewards, claimableAmount)

	accAddress := acc.Address()
	blck, err := staker.Raw().Client().Block("best")
	assert.NoError(t, err)
	fetchedAcc, err := staker.Raw().Client().Account(&accAddress)
	assert.NoError(t, err)
	amountBefore := (*big.Int)(fetchedAcc.Energy)

	receipt = testutil.Send(t, acc, stargate.ClaimRewards())
	claimedAmount := receiptToClaimedAmount(t, receipt)
	assert.Equal(t, claimableAmount, claimedAmount)

	ticker.WaitForBlock(blck.Number + 1)
	fetchedAcc, err = staker.Raw().Client().Account(&accAddress)
	assert.NoError(t, err)
	amountAfter := (*big.Int)(fetchedAcc.Energy)

	gasUsed := big.NewInt(0).Mul(big.NewInt(0).SetUint64(receipt.GasUsed), big.NewInt(1e15))
	diff := big.NewInt(0).Sub(amountAfter, amountBefore)
	diff = diff.Add(diff, gasUsed)
	assert.Equal(t, claimedAmount, diff)

	claimable, _, _, err = stargate.GetClaimable(acc.Address())
	require.NoError(t, err)
	assert.Equal(t, 0, claimable.Sign())

	return nil
}

func Test_Stargate_DelegatorFlow_Stake_And_Claim_Auto_Renew_On_And_Off(t *testing.T) {
	t.Parallel()
	testutil.RunFlakyTest(t, func() error {
		return runTestStargateDelegatorFlowStakeAndClaimAutoRenewOnAndOff(t)
	})
}

func runTestStargateDelegatorFlowStakeAndClaimAutoRenewOnAndOff(t *testing.T) error {
	staker, stargate, config, validationIDs := newDelegationSetup(t)

	validationID := validationIDs[0]
	ticker := utils.NewTicker(staker.Raw().Client())
	_, err := staker.Get(validationID)
	require.NoError(t, err)

	// wait for the validator to complete 1 staking period
	block := config.ForkBlock + config.TransitionPeriod + config.MinStakingPeriod
	require.NoError(t, ticker.WaitForBlock(block))
	expectedCompletedPeriods := uint32(1)
	err = waitForCompletedPeriods(ticker, staker, validationID, expectedCompletedPeriods)
	if err != nil {
		return err
	}
	completed, err := staker.GetCompletedPeriods(validationID)
	require.NoError(t, err)
	assert.Equal(t, expectedCompletedPeriods, *completed)

	// add the delegation
	acc := hayabusa.AdditionalAccounts[0]
	stake := new(big.Int).Mul(builtin.MinStake(), big.NewInt(10))
	receipt := testutil.Send(t, acc, stargate.AddDelegator(validationID, 255, stake))
	require.NoError(t, err)
	delegationID := receiptToDelegationID(t, receipt)

	// assert correct start period
	completed, err = staker.GetCompletedPeriods(validationID)
	require.NoError(t, err)
	assert.Equal(t, expectedCompletedPeriods, *completed)
	delegation, err := staker.GetDelegation(delegationID)
	require.NoError(t, err)
	assert.Equal(t, 3, int(delegation.StartPeriod))

	// assert no claimable periods
	claimable, start, end, err := stargate.GetClaimable(acc.Address())
	require.NoError(t, err)
	assert.Equal(t, 0, claimable.Sign())
	// start is after end, so no claimable periods
	assert.Equal(t, 3, int(start))
	assert.Equal(t, 1, int(end))

	// wait for 2 staking periods
	block += config.MinStakingPeriod * 2
	require.NoError(t, ticker.WaitForBlock(block))

	// assert validator completed 2 more period
	expectedCompletedPeriods = 3
	err = waitForCompletedPeriods(ticker, staker, validationID, expectedCompletedPeriods)
	if err != nil {
		return err
	}
	completed, err = staker.GetCompletedPeriods(validationID)
	assert.NoError(t, err)
	assert.Equal(t, expectedCompletedPeriods, *completed)

	// assert delegator can claim for that period
	claimableAmount, start, end, err := stargate.GetClaimable(acc.Address())
	assert.NoError(t, err)
	assert.Equal(t, 3, int(start))
	assert.Equal(t, 3, int(end))

	delegation, err = staker.GetDelegation(delegationID)
	assert.NoError(t, err)
	assert.True(t, delegation.Locked)

	totalRewards, err := staker.GetDelegatorsRewards(validationID, 3)
	assert.NoError(t, err)
	assert.Equal(t, totalRewards, claimableAmount)

	accAddress := acc.Address()
	blck, err := staker.Raw().Client().Block("best")
	assert.NoError(t, err)
	fetchedAcc, err := staker.Raw().Client().Account(&accAddress)
	assert.NoError(t, err)
	amountBefore := (*big.Int)(fetchedAcc.Energy)

	receipt = testutil.Send(t, acc, stargate.ClaimRewards())
	claimedAmount := receiptToClaimedAmount(t, receipt)
	assert.Equal(t, claimableAmount, claimedAmount)

	ticker.WaitForBlock(blck.Number + 1)
	fetchedAcc, err = staker.Raw().Client().Account(&accAddress)
	assert.NoError(t, err)
	amountAfter := (*big.Int)(fetchedAcc.Energy)

	gasUsed := big.NewInt(0).Mul(big.NewInt(0).SetUint64(receipt.GasUsed), big.NewInt(1e15))
	diff := big.NewInt(0).Sub(amountAfter, amountBefore)
	diff = diff.Add(diff, gasUsed)
	assert.Equal(t, claimedAmount, diff)

	claimable, _, _, err = stargate.GetClaimable(acc.Address())
	require.NoError(t, err)
	assert.Equal(t, 0, claimable.Sign())

	receipt = testutil.Send(t, acc, stargate.DisableAutoRenew())
	stargate.LogEventValues(receipt.Outputs[0].Events)

	block = receipt.Meta.BlockNumber + config.MinStakingPeriod
	require.NoError(t, ticker.WaitForBlock(block))

	expectedCompletedPeriods = 5
	err = waitForCompletedPeriods(ticker, staker, validationID, expectedCompletedPeriods)
	if err != nil {
		return err
	}
	completed, err = staker.GetCompletedPeriods(validationID)
	assert.NoError(t, err)
	assert.Equal(t, expectedCompletedPeriods, *completed)

	delegation, err = staker.GetDelegation(delegationID)
	assert.NoError(t, err)
	assert.False(t, delegation.Locked)

	claimableAmount, start, end, err = stargate.GetClaimable(acc.Address())
	assert.NoError(t, err)
	assert.Equal(t, 4, int(start))
	assert.Equal(t, 4, int(end))

	blck, err = staker.Raw().Client().Block("best")
	assert.NoError(t, err)
	fetchedAcc, err = staker.Raw().Client().Account(&accAddress)
	assert.NoError(t, err)
	amountBefore = (*big.Int)(fetchedAcc.Energy)

	receipt = testutil.Send(t, acc, stargate.ClaimRewards())
	ticker.WaitForBlock(blck.Number + 1)
	fetchedAcc, err = staker.Raw().Client().Account(&accAddress)
	assert.NoError(t, err)
	amountAfter = (*big.Int)(fetchedAcc.Energy)

	gasUsed = big.NewInt(0).Mul(big.NewInt(0).SetUint64(receipt.GasUsed), big.NewInt(1e15))
	diff = big.NewInt(0).Sub(claimableAmount, gasUsed)
	assert.Equal(t, diff, big.NewInt(0).Sub(amountAfter, amountBefore))

	return nil
}

func newDelegationSetup(t *testing.T) (*builtin.Staker, *stargate.Stargate, *hayabusa.Config, [3]thor.Address) {
	t.Helper()
	config := &hayabusa.Config{
		Nodes:             3,
		MaxBlockProposers: 3,
		ForkBlock:         0,
		TransitionPeriod:  4,
		EpochLength:       4,
		CooldownPeriod:    4,
		MinStakingPeriod:  4,
		MidStakingPeriod:  12,
		HighStakingPeriod: 24,
		Name:              t.Name(),
	}
	network, err := hayabusa.NewNetwork(config, t.Context())
	require.NoError(t, err)
	t.Cleanup(network.Stop)
	require.NoError(t, network.Start())
	client := network.ThorClient()

	staker, err := builtin.NewStaker(client)
	require.NoError(t, err)

	var stargate *stargate.Stargate
	wg := &sync.WaitGroup{}
	wg.Add(1)

	go func() {
		defer wg.Done()
		stargate = setStargate(t, staker)
	}()

	if err := utils.WaitForFork(staker, config.ForkBlock); err != nil {
		t.Fatalf("failed to wait for fork: %v", err)
	}

	validationIDs := [3]thor.Address{}
	senders := &utils.Senders{}

	for i := range validationIDs {
		account := hayabusa.ValidatorAccounts[i]
		sender := staker.AddValidation(account.Address(), builtin.MinStake(), config.MinStakingPeriod).Send().WithSigner(account).WithOptions(testutil.TxOptions())
		senders.Add(sender)
	}

	if receipts, _, err := senders.Send(testutil.TxContext(t)); err != nil {
		t.Fatal(err)
	} else {
		for i := range config.MaxBlockProposers {
			validationIDs[i] = receiptToID(receipts[i])
		}
	}

	posBlock := config.ForkBlock + config.TransitionPeriod
	if err := utils.WaitForPOS(staker, posBlock); err != nil {
		t.Logf("⚠️ WaitForPOS failed: %v, continuing to wait for PoS activation...", err)

		timeout := time.After(10 * time.Minute)
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()

		for {
			select {
			case <-timeout:
				t.Fatalf("timed out waiting for PoS activation")
			case <-tick.C:
				_, id, err := staker.FirstActive()
				if err == nil && !id.IsZero() {
					t.Logf("✅ PoS is now active with delay")
					goto posActive
				}
			}
		}
	}

posActive:
	wg.Wait()

	return staker, stargate, config, validationIDs
}

func setStargate(t *testing.T, staker *builtin.Staker) *stargate.Stargate {
	genesis, err := staker.Raw().Client().Block("0")
	require.NoError(t, err)
	chainTag := genesis.ID[31]

	acc := hayabusa.AdditionalAccounts[0]

	bytecode := stargate.Bin
	bytecode = strings.TrimSpace(bytecode)
	bytes, err := hexutil.Decode("0x" + bytecode)
	require.NoError(t, err)

	clause := tx.NewClause(nil).WithData(bytes)
	trx := new(tx.Builder).
		Clause(clause).
		Gas(40_000_000).
		Nonce(datagen.RandUint64()).
		ChainTag(chainTag).
		Expiration(10000).
		GasPriceCoef(255).
		Build()

	caller := acc.Address()
	inspection, err := staker.Raw().Client().InspectClauses(&api.BatchCallData{
		Caller: &caller,
		Clauses: api.Clauses{
			{
				Data:  "0x" + bytecode,
				Value: (*math.HexOrDecimal256)(trx.Clauses()[0].Value()),
				To:    trx.Clauses()[0].To(),
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, len(inspection))

	trx, err = hayabusa.Stargate.SignTransaction(trx)
	require.NoError(t, err)
	res, err := staker.Raw().Client().SendTransaction(trx)
	require.NoError(t, err)

	var receipt *api.Receipt
	for range 30 {
		receipt, err = staker.Raw().Client().TransactionReceipt(res.ID)
		if err == nil && receipt != nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if receipt == nil {
		t.Fatalf("failed to get transaction receipt: %v", err)
	}

	contractAddr := receipt.Outputs[0].ContractAddress

	stargate := stargate.NewStargate(staker.Raw().Client(), *contractAddr)

	params, err := builtin.NewParams(staker.Raw().Client())
	require.NoError(t, err)
	key := thor.BytesToBytes32([]byte("stargate-contract-address"))
	value := new(big.Int).SetBytes(contractAddr[:])
	testutil.Send(t, hayabusa.Executor, params.Set(key, value))

	return stargate
}

func receiptToID(receipt *api.Receipt) thor.Address {
	id := receipt.Outputs[0].Events[0].Topics[2]
	return thor.BytesToAddress(id.Bytes())
}

func receiptToDelegationID(t *testing.T, receipt *api.Receipt) thor.Bytes32 {
	require.False(t, receipt.Reverted)
	return receipt.Outputs[0].Events[0].Topics[2]
}

func receiptToClaimedAmount(t *testing.T, receipt *api.Receipt) *big.Int {
	amountBytes, err := thor.ParseBytes32(receipt.Outputs[0].Events[5].Data[2:66])
	require.NoError(t, err)
	return big.NewInt(0).SetBytes(amountBytes.Bytes())
}
