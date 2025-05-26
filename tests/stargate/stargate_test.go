package stargate

import (
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
	"github.com/vechain/draupnir/common"
	"github.com/vechain/draupnir/contracts"
	"github.com/vechain/draupnir/datagen"
	"github.com/vechain/hayabusa-e2e/builtins"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/hayabusa/stargate"
	"github.com/vechain/thor/v2/api/accounts"
	"github.com/vechain/thor/v2/api/transactions"
	thorgenesis "github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/tx"
)

func Test_Stargate_SingleDelegator(t *testing.T) {
	staker, stargate, config, validationIDs := newDelegationSetup(t)

	validationID := validationIDs[0]
	ticker := common.NewTicker(staker.Client())
	validation, err := staker.Get(validationID)
	require.NoError(t, err)

	// wait for the validator to complete 1 staking period
	block := config.ForkBlock + config.TransitionPeriod + config.MinStakingPeriod
	require.NoError(t, ticker.WaitForBlock(block))
	completed, err := staker.GetCompletedPeriods(validationID)
	require.NoError(t, err)
	assert.Equal(t, 1, int(completed))

	// add the delegation
	acc := hayabusa.AdditionalAccounts[0]
	stake := new(big.Int).Mul(builtins.MinStake, big.NewInt(10)) // very large stake
	receipt, _, err := stargate.Attach(acc.PrivateKey).AddDelegator(validationID, true, 200, stake).Receipt(false)
	require.NoError(t, err)
	delegationID := receiptToDelegationID(receipt)

	// assert correct start period
	completed, err = staker.GetCompletedPeriods(validationID)
	require.NoError(t, err)
	assert.Equal(t, 1, int(completed))
	delegation, err := staker.GetDelegation(delegationID)
	require.NoError(t, err)
	assert.Equal(t, 2, int(delegation.StartPeriod))

	// assert no claimable periods
	claimable, start, end, err := stargate.GetClaimable(acc.Address)
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
	assert.Equal(t, 2, int(completed))

	// assert delegator can claim for that period
	claimable, start, end, err = stargate.GetClaimable(acc.Address)
	assert.NoError(t, err)
	assert.Equal(t, 2, int(start))
	assert.Equal(t, 2, int(end))
	assert.Equal(t, 1, claimable.Sign())

	// assert TVL
	expected := new(big.Int).Mul(builtins.MinStake, big.NewInt(int64(len(validationIDs))))
	expected = expected.Add(expected, stake)
	lockedVET, _, err := staker.TotalStake()
	require.NoError(t, err)
	assert.Equal(t, expected, lockedVET)

	firstDelegatedBlock := block
	block += 2 * config.MinStakingPeriod
	require.NoError(t, ticker.WaitForBlock(block-1))

	blockCount := 0
	for i := firstDelegatedBlock; i < block; i++ {
		block, err := staker.Client().Block(strconv.Itoa(int(i)))
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
	delegatorReward = delegatorReward.Mul(delegatorReward, big.NewInt(int64(blockCount)))

	stargateAddr := stargate.Address()
	stargateAcc, err := staker.Client().Account(&stargateAddr)
	require.NoError(t, err)
	stargateEnergy := (*big.Int)(&stargateAcc.Energy)
	assert.Equal(t, delegatorReward, stargateEnergy, "stargate energy should be equal to the expected reward, difference: %s", new(big.Int).Sub(delegatorReward, stargateEnergy).String())

	// wait for housekeeping on first block of next staking period
	require.NoError(t, ticker.WaitForBlock(block))
	completed, err = staker.GetCompletedPeriods(validationID)
	assert.NoError(t, err)
	assert.Equal(t, 4, int(completed))

	claimable, start, end, err = stargate.GetClaimable(acc.Address)
	assert.NoError(t, err)
	assert.Equal(t, 2, int(start))
	assert.Equal(t, 4, int(end))
	assert.Equal(t, delegatorReward, claimable, "claimable should be equal to the expected reward, difference: %s", new(big.Int).Sub(delegatorReward, claimable).String())
}

func newDelegationSetup(t *testing.T) (*builtins.Staker, *stargate.Stargate, *hayabusa.Config, [3]thor.Bytes32) {
	t.Helper()
	config := &hayabusa.Config{
		Nodes:             3,
		MaxBlockProposers: 3,
		ForkBlock:         0,
		TransitionPeriod:  8,
		EpochLength:       4,
		CooldownPeriod:    4,
		MinStakingPeriod:  4,
		MidStakingPeriod:  12,
		HighStakingPeriod: 24,
	}
	client, _, cancel, err := hayabusa.StartNetwork(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cancel)

	staker := builtins.NewStaker(client, hayabusa.Stargate.PrivateKey)

	var stargate *stargate.Stargate
	wg := &sync.WaitGroup{}
	wg.Add(1)

	go func() {
		defer wg.Done()
		stargate = setStargate(t, staker)
	}()

	if err := staker.WaitForFork(config.ForkBlock); err != nil {
		t.Fatalf("failed to wait for fork: %v", err)
	}

	validationIDs := [3]thor.Bytes32{}
	senders := &contracts.Senders{}

	for i := range validationIDs {
		account := hayabusa.ValidatorAccounts[i]
		sender := staker.Attach(account.PrivateKey).AddValidator(account.Address, builtins.MinStake, config.MinStakingPeriod, true)
		senders.Add(sender)
	}

	if _, receipts, err := senders.Send(false); err != nil {
		t.Fatal(err)
	} else {
		for i := range config.MaxBlockProposers {
			validationIDs[i] = receiptToID(receipts[i])
		}
	}
	if err := staker.WaitForPOS(config.ForkBlock + config.TransitionPeriod); err != nil {
		t.Fatalf("failed to wait for PoS: %v", err)
	}

	wg.Wait()

	return staker, stargate, config, validationIDs
}

func setStargate(t *testing.T, staker *builtins.Staker) *stargate.Stargate {
	chainTag, err := staker.Client().ChainTag()
	require.NoError(t, err)

	acc := hayabusa.AdditionalAccounts[0]

	// trim Bin whitespace (new lines etc.)
	bytecode := stargate.Bin
	bytecode = strings.TrimSpace(bytecode)

	bytes, err := hexutil.Decode("0x" + bytecode)
	require.NoError(t, err)

	clause := tx.NewClause(nil).WithData(bytes)
	trx := new(tx.Builder).
		Clause(clause).
		Gas(40_000_000).
		Nonce(datagen.RandUInt64()).
		ChainTag(chainTag).
		Expiration(10000).
		GasPriceCoef(255).
		Build()

	inpsection, err := staker.Client().InspectClauses(&accounts.BatchCallData{
		Caller: &acc.Address,
		Clauses: []accounts.Clause{
			{
				Data:  "0x" + bytecode,
				Value: (*math.HexOrDecimal256)(trx.Clauses()[0].Value()),
				To:    trx.Clauses()[0].To(),
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, len(inpsection))

	trx = tx.MustSign(trx, hayabusa.Stargate.PrivateKey)
	res, err := staker.Client().SendTransaction(trx)
	require.NoError(t, err)

	var receipt *transactions.Receipt
	for range 30 {
		receipt, err = staker.Client().TransactionReceipt(res.ID)
		if err == nil && receipt != nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	require.NotNil(t, receipt)

	contractAddr := receipt.Outputs[0].ContractAddress

	stargate := stargate.NewStargate(staker.Client(), *contractAddr, acc.PrivateKey)

	executor, err := builtins.NewExecutor(staker.Client(), acc.PrivateKey)
	require.NoError(t, err)
	params, err := builtins.NewParams(staker.Client(), acc.PrivateKey)
	require.NoError(t, err)
	addrBig := new(big.Int).SetBytes(contractAddr[:])
	tx, err := params.Set(thor.BytesToBytes32([]byte("stargate-contract-address")), addrBig).Build()
	require.NoError(t, err)
	to := tx.Clauses()[0].To()
	err = executor.Update(*to, tx.Clauses()[0].Data(), []thorgenesis.DevAccount{hayabusa.Executor})
	require.NoError(t, err)

	return stargate
}

func receiptToID(receipt *transactions.Receipt) thor.Bytes32 {
	return receipt.Outputs[0].Events[0].Topics[3]
}

func receiptToDelegationID(receipt *transactions.Receipt) thor.Bytes32 {
	return receipt.Outputs[0].Events[0].Topics[2]
}
