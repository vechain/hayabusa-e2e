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
	"github.com/vechain/thor/v2/api/accounts"
	"github.com/vechain/thor/v2/api/transactions"
	"github.com/vechain/thor/v2/test/datagen"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
	"github.com/vechain/thor/v2/tx"
)

func Test_Stargate_SingleDelegator(t *testing.T) {
	staker, stargate, config, validationIDs := newDelegationSetup(t)

	validationID := validationIDs[0]
	ticker := utils.NewTicker(staker.Raw().Client())
	validation, err := staker.Get(validationID)
	require.NoError(t, err)

	// wait for the validator to complete 1 staking period
	block := config.ForkBlock + config.TransitionPeriod + config.MinStakingPeriod
	require.NoError(t, ticker.WaitForBlock(block))
	completed, err := staker.GetCompletedPeriods(validationID)
	require.NoError(t, err)
	assert.Equal(t, 1, int(*completed))

	// add the delegation
	acc := hayabusa.AdditionalAccounts[0]
	stake := new(big.Int).Mul(builtin.MinStake(), big.NewInt(10)) // very large stake
	receipt, _, err := stargate.AddDelegator(acc, validationID, true, 200, stake).Receipt(testutil.TxContext(t), testutil.TxOptions())
	require.NoError(t, err)
	delegationID := receiptToDelegationID(receipt)

	// assert correct start period
	completed, err = staker.GetCompletedPeriods(validationID)
	require.NoError(t, err)
	assert.Equal(t, 1, int(*completed))
	delegation, err := staker.GetDelegation(delegationID)
	require.NoError(t, err)
	assert.Equal(t, 2, int(delegation.StartPeriod))

	// assert no claimable periods
	claimable, start, end, err := stargate.GetClaimable(acc.Address())
	require.NoError(t, err)
	assert.Equal(t, 0, claimable.Sign())
	// start is after end, so no claimable periods
	assert.Equal(t, 2, int(start))
	assert.Equal(t, 1, int(end))

	// wait for 1 staking period
	block += config.MinStakingPeriod
	require.NoError(t, ticker.WaitForBlock(block))

	// assert validator completed 1 more period
	completed, err = staker.GetCompletedPeriods(validationID)
	assert.NoError(t, err)
	assert.Equal(t, 2, int(*completed))

	// assert delegator can claim for that period
	claimable, start, end, err = stargate.GetClaimable(acc.Address())
	assert.NoError(t, err)
	assert.Equal(t, 2, int(start))
	assert.Equal(t, 2, int(end))

	// assert TVL
	expected := new(big.Int).Mul(builtin.MinStake(), big.NewInt(int64(len(validationIDs))))
	expected = expected.Add(expected, stake)
	lockedVET, _, err := staker.TotalStake()
	require.NoError(t, err)
	assert.Equal(t, expected, lockedVET)

	firstDelegatedBlock := block
	block += 2 * config.MinStakingPeriod
	require.NoError(t, ticker.WaitForBlock(block-1))

	blockCount := 0
	for i := firstDelegatedBlock; i < block; i++ {
		block, err := staker.Raw().Client().GetBlock(strconv.Itoa(int(i)))
		require.NoError(t, err)
		if block.Signer == *validation.Master {
			blockCount++
		}
	}
	blockReward := hayabusa.GetExpectedReward(lockedVET)

	proposerReward := new(big.Int).Set(blockReward)
	proposerReward = proposerReward.Mul(proposerReward, big.NewInt(3))
	proposerReward = proposerReward.Div(proposerReward, big.NewInt(10))

	delegatorReward := new(big.Int).Sub(blockReward, proposerReward)

	slog.Info("rewards per block", "proposer", proposerReward.String(), "delegator", delegatorReward.String(), "blockCount", blockCount)

	delegatorReward = delegatorReward.Mul(delegatorReward, big.NewInt(int64(blockCount)))

	stargateAddr := stargate.Address()
	stargateAcc, err := staker.Raw().Client().GetAccount(&stargateAddr, "best")
	require.NoError(t, err)
	stargateEnergy := (*big.Int)(&stargateAcc.Energy)
	assert.Equal(t, delegatorReward, stargateEnergy, "stargate energy should be equal to the expected reward, difference: %s", new(big.Int).Sub(delegatorReward, stargateEnergy).String())

	// wait for housekeeping on first block of next staking period
	require.NoError(t, ticker.WaitForBlock(block))
	completed, err = staker.GetCompletedPeriods(validationID)
	assert.NoError(t, err)
	assert.Equal(t, 4, int(*completed))

	claimable, start, end, err = stargate.GetClaimable(acc.Address())
	assert.NoError(t, err)
	assert.Equal(t, 2, int(start))
	assert.Equal(t, 4, int(end))
	assert.Equal(t, delegatorReward, claimable, "claimable should be equal to the expected reward, difference: %s", new(big.Int).Sub(delegatorReward, claimable).String())
	simulation, err := stargate.Raw().Simulate(big.NewInt(0), acc.Address(), "getClaimable", acc.Address())
	require.NoError(t, err)
	stargate.LogEventValues(simulation.Events)
}

func newDelegationSetup(t *testing.T) (*builtin.Staker, *stargate.Stargate, *hayabusa.Config, [3]thor.Bytes32) {
	t.Helper()
	config := &hayabusa.Config{
		Nodes:             3,
		MaxBlockProposers: 3,
		ForkBlock:         0,
		TransitionPeriod:  4,
		EpochLength:       2,
		CooldownPeriod:    2,
		MinStakingPeriod:  2,
		MidStakingPeriod:  12,
		HighStakingPeriod: 24,
	}
	client, _, cancel, err := hayabusa.StartNetwork(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cancel)

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

	validationIDs := [3]thor.Bytes32{}
	senders := &bind.Senders{}

	for i := range validationIDs {
		account := hayabusa.ValidatorAccounts[i]
		sender := staker.AddValidator(account, account.Address(), builtin.MinStake(), config.MinStakingPeriod, true)
		senders.Add(sender)
	}

	if receipts, _, err := senders.Send(testutil.TxContext(t), testutil.TxOptions()); err != nil {
		t.Fatal(err)
	} else {
		for i := range config.MaxBlockProposers {
			validationIDs[i] = receiptToID(receipts[i])
		}
	}
	if err := utils.WaitForPOS(staker, config.ForkBlock+config.TransitionPeriod); err != nil {
		t.Fatalf("failed to wait for PoS: %v", err)
	}

	wg.Wait()

	return staker, stargate, config, validationIDs
}

func setStargate(t *testing.T, staker *builtin.Staker) *stargate.Stargate {
	genesis, err := staker.Raw().Client().GetBlock("0")
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
	inpsection, err := staker.Raw().Client().InspectClauses(&accounts.BatchCallData{
		Caller: &caller,
		Clauses: []accounts.Clause{
			{
				Data:  "0x" + bytecode,
				Value: (*math.HexOrDecimal256)(trx.Clauses()[0].Value()),
				To:    trx.Clauses()[0].To(),
			},
		},
	}, "best")
	require.NoError(t, err)
	require.Equal(t, 1, len(inpsection))

	trx, err = hayabusa.Stargate.SignTransaction(trx)
	require.NoError(t, err)
	rlpTx, err := trx.MarshalBinary()
	if err != nil {
		t.Fatalf("unable to encode transaction - %v", err)
	}
	res, err := staker.Raw().Client().SendTransaction(&transactions.RawTx{Raw: hexutil.Encode(rlpTx)})
	require.NoError(t, err)

	var receipt *transactions.Receipt
	for range 30 {
		receipt, err = staker.Raw().Client().GetTransactionReceipt(res.ID, "")
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
	receipt, _, err = params.Set(hayabusa.Executor, key, value).Receipt(testutil.TxContext(t), testutil.TxOptions())
	require.NoError(t, err)
	require.False(t, receipt.Reverted, "receipt should not be reverted")

	return stargate
}

func receiptToID(receipt *transactions.Receipt) thor.Bytes32 {
	return receipt.Outputs[0].Events[0].Topics[3]
}

func receiptToDelegationID(receipt *transactions.Receipt) thor.Bytes32 {
	return receipt.Outputs[0].Events[0].Topics[2]
}
