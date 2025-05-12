package stargate

import (
	"log/slog"
	"math/big"
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

func Test_Stargate(t *testing.T) {
	// Setup
	_, stargate, _, validationIDs := newDelegationSetup(t)

	account := hayabusa.Stargate
	stargate = stargate.Attach(account.PrivateKey)

	stake := new(big.Int).Mul(builtins.MinStake, big.NewInt(10))
	receipt, _, err := stargate.AddDelegator(validationIDs[0], true, 250, stake).Receipt(true)
	require.NoError(t, err)
	assert.False(t, receipt.Reverted)

	block := receipt.Meta.BlockNumber + 8
	assert.NoError(t, common.NewTicker(stargate.Client()).WaitForBlock(block))

	stargateAddr := stargate.Address()
	energy, err := stargate.Client().Account(&stargateAddr)
	require.NoError(t, err)
	bal := (big.Int)(energy.Energy)
	t.Logf("stargate energy: %s", (&bal).String())

	claimable, startPeriod, endPeriod, err := stargate.GetClaimable(account.Address)
	require.NoError(t, err)
	diff := new(big.Int).Sub(&bal, claimable)
	t.Logf("claimable: %s, startPeriod: %d, endPeriod: %d, diff: %s", claimable.String(), startPeriod, endPeriod, diff.String())

	receipt, _, err = stargate.ClaimRewards().Receipt(true)
	assert.NoError(t, err)
	assert.False(t, receipt.Reverted)

	populated, err := stargate.FilterWeightsPopulated(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
	require.NoError(t, err)
	for _, event := range populated {
		slog.Info("got weight populated event", "validationID", event.ValidationID.String(), "weight", event.Weight.String(), "period", event.StakingPeriod)
	}

	claiming, err := stargate.FilterClaiming(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
	require.NoError(t, err)
	for _, event := range claiming {
		slog.Info("got claiming event", "delegationID", event.DelegationID.String(), "delegator", event.Delegator.String(), "amount", event.Amount.String(), "firstClaimablePeriod", event.FirstClaimablePeriod, "lastClaimablePeriod", event.LastClaimablePeriod, "previouslyPopulatedPeriod", event.PreviouslyPopulatedPeriod, "maxClaimablePeriod", event.MaxClaimablePeriod)
	}

	claims, err := stargate.FilterClaimedRewards(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
	require.NoError(t, err)
	for _, event := range claims {
		slog.Info("got claimed rewards event", "delegator", event.Delegator.String(), "amount", event.Amount.String(), "first", event.FirstClaimablePeriod, "last", event.LastClaimablePeriod, "validationID", event.ValidationID.String())
	}

	t.Logf("✅ - performed first claim")

	assert.NoError(t, common.NewTicker(stargate.Client()).WaitForBlock(receipt.Meta.BlockNumber+2))

	claimable, startPeriod, endPeriod, err = stargate.GetClaimable(account.Address)
	require.NoError(t, err)
	diff = new(big.Int).Sub(&bal, claimable)
	t.Logf("claimable: %s, startPeriod: %d, endPeriod: %d, diff: %s", claimable.String(), startPeriod, endPeriod, diff.String())

	receipt, _, err = stargate.ClaimRewards().Receipt(true)
	assert.NoError(t, err)
	assert.False(t, receipt.Reverted)

	energy, err = stargate.Client().Account(&stargateAddr)
	require.NoError(t, err)
	bal = (big.Int)(energy.Energy)
	t.Logf("stargate energy: %s", (&bal).String())

	populated, err = stargate.FilterWeightsPopulated(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
	require.NoError(t, err)
	for _, event := range populated {
		slog.Info("got weight populated event", "validationID", event.ValidationID.String(), "weight", event.Weight.String(), "period", event.StakingPeriod)
	}

	claiming, err = stargate.FilterClaiming(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
	require.NoError(t, err)
	for _, event := range claiming {
		slog.Info("got claiming event", "delegationID", event.DelegationID.String(), "delegator", event.Delegator.String(), "amount", event.Amount.String(), "firstClaimablePeriod", event.FirstClaimablePeriod, "lastClaimablePeriod", event.LastClaimablePeriod, "previouslyPopulatedPeriod", event.PreviouslyPopulatedPeriod, "maxClaimablePeriod", event.MaxClaimablePeriod)
	}

	claims, err = stargate.FilterClaimedRewards(receipt.Meta.BlockNumber, receipt.Meta.BlockNumber)
	require.NoError(t, err)
	for _, event := range claims {
		slog.Info("got claimed rewards event", "delegator", event.Delegator.String(), "amount", event.Amount.String(), "first", event.FirstClaimablePeriod, "last", event.LastClaimablePeriod, "validationID", event.ValidationID.String())
	}

	t.Logf("✅ - performed secod claim")
}

func newDelegationSetup(t *testing.T) (*builtins.Staker, *stargate.Stargate, *hayabusa.Config, [6]thor.Bytes32) {
	t.Helper()
	config := &hayabusa.Config{
		Nodes:             6,
		MaxBlockProposers: 6,
		ForkBlock:         0,
		TransitionPeriod:  4,
		EpochLength:       2,
		CooldownPeriod:    2,
		MinStakingPeriod:  2,
		MidStakingPeriod:  12,
		HighStakingPeriod: 259200,
		Verbosity:         1,
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

	validationIDs := [6]thor.Bytes32{}
	senders := &contracts.Senders{}

	for i := range validationIDs {
		account := hayabusa.ValidatorAccounts[i]
		sender := staker.Attach(account.PrivateKey).AddValidator(account.Address, builtins.MinStake, config.MinStakingPeriod, true)
		senders.Add(sender)
	}

	if _, _, err := senders.Send(false); err != nil {
		t.Fatal(err)
	}
	if err := staker.WaitForPOS(config.ForkBlock + config.TransitionPeriod); err != nil {
		t.Fatalf("failed to wait for PoS: %v", err)
	}
	events, err := staker.FilterValidatorQueued(0, 1000)
	if err != nil {
		t.Fatalf("failed to filter validator queued: %v", err)
	}
	for i, event := range events {
		validationIDs[i] = event.ValidationID
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
	// 0 is the event, 1 is the validation ID
	return receipt.Outputs[0].Events[0].Topics[2]
}
