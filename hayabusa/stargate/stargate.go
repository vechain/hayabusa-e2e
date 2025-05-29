package stargate

import (
	_ "embed"
	"fmt"
	"github.com/vechain/thor/v2/api/events"
	"github.com/vechain/thor/v2/logdb"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/httpclient"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/vechain/thor/v2/thor"
)

//go:embed Stargate.abi
var ABI []byte

//go:embed Stargate.bin
var Bin string

// Stargate represents a wrapper to interact with the Stargate contract
type Stargate struct {
	contract *bind.Caller
}

// NewStargate creates a new instance of the Stargate contract wrapper
func NewStargate(client *httpclient.Client, addr thor.Address) *Stargate {
	base, err := bind.NewCaller(client, ABI, addr)
	if err != nil {
		panic(fmt.Sprintf("failed to create stargate contract: %v", err))
	}
	return &Stargate{
		contract: base,
	}
}

// Address returns the address of the contract
func (s *Stargate) Address() thor.Address {
	return s.contract.Address()
}

// ---- Getter Methods ----

// Claims returns the claims for a given validation ID
func (s *Stargate) Claims(validationID thor.Bytes32) (uint32, error) {
	var result uint32
	if err := s.contract.CallInto("claims", &result, validationID); err != nil {
		return 0, err
	}
	return result, nil
}

// DelegationIDs returns the delegation ID for a given address
func (s *Stargate) DelegationIDs(address thor.Address) (thor.Bytes32, error) {
	out := new(common.Hash)
	if err := s.contract.CallInto("delegationIDs", &out, address); err != nil {
		return thor.Bytes32{}, err
	}
	return thor.Bytes32(*out), nil
}

// GetClaimable returns the claimable rewards for a delegator
func (s *Stargate) GetClaimable(delegator thor.Address) (*big.Int, uint32, uint32, error) {
	var out = make([]interface{}, 3)
	out[0] = new(*big.Int)
	out[1] = new(uint32)
	out[2] = new(uint32)

	if err := s.contract.CallInto("getClaimable", &out, delegator); err != nil {
		return nil, 0, 0, err
	}

	if err := s.contract.CallInto("getClaimable", &out, delegator); err != nil {
		return nil, 0, 0, err
	}

	return *(out[0].(**big.Int)), *(out[1].(*uint32)), *(out[2].(*uint32)), nil
}

// PopulatedWeights returns the populated weights for a validation ID
func (s *Stargate) PopulatedWeights(validationID thor.Bytes32) (uint32, error) {
	var result uint32
	if err := s.contract.CallInto("populatedWeights", &result, validationID); err != nil {
		return 0, err
	}
	return result, nil
}

// Reductions returns the reductions for a validation ID and period
func (s *Stargate) Reductions(validationID thor.Bytes32, period uint32) (*big.Int, error) {
	out := new(big.Int)
	if err := s.contract.CallInto("reductions", &out, validationID, period); err != nil {
		return nil, err
	}
	return out, nil
}

// Rewards returns the rewards for a validation ID and period
func (s *Stargate) Rewards(validationID thor.Bytes32, period uint32) (*big.Int, error) {
	out := new(big.Int)
	if err := s.contract.CallInto("rewards", &out, validationID, period); err != nil {
		return nil, err
	}
	return out, nil
}

// Staker returns the staker contract address
func (s *Stargate) Staker() (thor.Address, error) {
	out := new(common.Address)
	if err := s.contract.CallInto("staker", &out); err != nil {
		return thor.Address{}, err
	}
	return thor.Address(*out), nil
}

// VTHO returns the VTHO token contract address
func (s *Stargate) VTHO() (thor.Address, error) {
	out := new(common.Address)
	if err := s.contract.CallInto("vtho", &out); err != nil {
		return thor.Address{}, err
	}
	return thor.Address(*out), nil
}

// Weights returns the weights for a validation ID and period
func (s *Stargate) Weights(validationID thor.Bytes32, period uint32) (*big.Int, error) {
	out := new(big.Int)
	if err := s.contract.CallInto("weights", &out, validationID, period); err != nil {
		return nil, err
	}
	return out, nil
}

// ---- Transaction Methods ----

// AddDelegator adds a delegator to a validation ID
func (s *Stargate) AddDelegator(signer bind.Signer, validationID thor.Bytes32, autoRenew bool, multiplier uint8, amount *big.Int) *bind.Sender {
	return s.contract.Attach(signer).SenderWithVET(amount, "addDelegator", validationID, autoRenew, multiplier)
}

// ClaimRewards claims rewards for the sender
func (s *Stargate) ClaimRewards(signer bind.Signer) *bind.Sender {
	return s.contract.Attach(signer).Sender("claimRewards")
}

// DisableAutoRenew disables auto renewal for the sender's delegation
func (s *Stargate) DisableAutoRenew(signer bind.Signer) *bind.Sender {
	return s.contract.Attach(signer).Sender("disableAutoRenew")
}

// EnableAutoRenew enables auto renewal for the sender's delegation
func (s *Stargate) EnableAutoRenew(signer bind.Signer) *bind.Sender {
	return s.contract.Attach(signer).Sender("enableAutoRenew")
}

// ---- Event Filterers ----

// ClaimedRewardsEvent represents a ClaimedRewards event
type ClaimedRewardsEvent struct {
	ValidationID         thor.Bytes32
	Delegator            thor.Address
	Amount               *big.Int
	FirstClaimablePeriod uint32
	LastClaimablePeriod  uint32
}

// FilterClaimedRewards filters ClaimedRewards events
func (s *Stargate) FilterClaimedRewards(from, to uint32) ([]ClaimedRewardsEvent, error) {
	event, ok := s.contract.ABI().Events["ClaimedRewards"]
	if !ok {
		return nil, fmt.Errorf("event not found")
	}

	from64 := uint64(from)
	to64 := uint64(to)
	rnge := &events.Range{
		From: &from64,
		To:   &to64,
	}
	raw, err := s.contract.FilterEvents("ClaimedRewards", rnge, nil, logdb.ASC)
	if err != nil {
		return nil, err
	}

	out := make([]ClaimedRewardsEvent, len(raw))
	for i, log := range raw {
		validationID := thor.Bytes32(log.Topics[1][:])     // indexed
		delegator := thor.BytesToAddress(log.Topics[2][:]) // indexed

		// non-indexed
		data := make([]interface{}, 3)
		data[0] = new(*big.Int)
		data[1] = new(uint32)
		data[2] = new(uint32)

		bytes, err := hexutil.Decode(log.Data)
		if err != nil {
			return nil, err
		}

		if err := event.Inputs.Unpack(&data, bytes); err != nil {
			return nil, err
		}

		out[i] = ClaimedRewardsEvent{
			ValidationID:         validationID,
			Delegator:            delegator,
			Amount:               *(data[0].(**big.Int)),
			FirstClaimablePeriod: *(data[1].(*uint32)),
			LastClaimablePeriod:  *(data[2].(*uint32)),
		}
	}

	return out, nil
}

// Rename ClaimingEvent to ClaimParamsEvent and update fields
type ClaimParamsEvent struct {
	DelegationID              thor.Bytes32
	Delegator                 thor.Address
	FirstClaimablePeriod      uint32
	LastClaimablePeriod       uint32
	PreviouslyPopulatedPeriod uint32
	MaxClaimablePeriod        uint32
	DelegatorWeight           *big.Int
}

// Rename FilterClaiming to FilterClaimParams
func (s *Stargate) FilterClaimParams(from, to uint32) ([]ClaimParamsEvent, error) {
	event, ok := s.contract.ABI().Events["ClaimParams"]
	if !ok {
		return nil, fmt.Errorf("event not found")
	}

	from64 := uint64(from)
	to64 := uint64(to)
	rnge := &events.Range{
		From: &from64,
		To:   &to64,
	}
	raw, err := s.contract.FilterEvents("ClaimParams", rnge, nil, logdb.ASC)
	if err != nil {
		return nil, err
	}

	out := make([]ClaimParamsEvent, len(raw))
	for i, log := range raw {
		delegationID := thor.Bytes32(log.Topics[1][:]) // indexed

		// non-indexed
		data := make([]interface{}, 6)
		data[0] = new(common.Address)
		data[1] = new(uint32)
		data[2] = new(uint32)
		data[3] = new(uint32)
		data[4] = new(uint32)
		data[5] = new(*big.Int)

		bytes, err := hexutil.Decode(log.Data)
		if err != nil {
			return nil, err
		}

		if err := event.Inputs.Unpack(&data, bytes); err != nil {
			return nil, err
		}

		out[i] = ClaimParamsEvent{
			DelegationID:              delegationID,
			Delegator:                 (thor.Address)(*(data[0].(*common.Address))),
			FirstClaimablePeriod:      *(data[1].(*uint32)),
			LastClaimablePeriod:       *(data[2].(*uint32)),
			PreviouslyPopulatedPeriod: *(data[3].(*uint32)),
			MaxClaimablePeriod:        *(data[4].(*uint32)),
			DelegatorWeight:           *(data[5].(**big.Int)),
		}
	}

	return out, nil
}

// ClaimOutputsEvent represents a ClaimOutputs event
type ClaimOutputsEvent struct {
	DelegationID thor.Bytes32
	Delegator    thor.Address
	TotalRewards *big.Int
}

// FilterClaimOutputs filters ClaimOutputs events
func (s *Stargate) FilterClaimOutputs(from, to uint32) ([]ClaimOutputsEvent, error) {
	event, ok := s.contract.ABI().Events["ClaimOutputs"]
	if !ok {
		return nil, fmt.Errorf("event not found")
	}
	from64 := uint64(from)
	to64 := uint64(to)
	rnge := &events.Range{
		From: &from64,
		To:   &to64,
	}

	raw, err := s.contract.FilterEvents("ClaimOutputs", rnge, nil, logdb.ASC)
	if err != nil {
		return nil, err
	}

	out := make([]ClaimOutputsEvent, len(raw))
	for i, log := range raw {
		delegationID := thor.Bytes32(log.Topics[1][:]) // indexed

		// non-indexed
		data := make([]interface{}, 2)
		data[0] = new(common.Address)
		data[1] = new(*big.Int)

		bytes, err := hexutil.Decode(log.Data)
		if err != nil {
			return nil, err
		}

		if err := event.Inputs.Unpack(&data, bytes); err != nil {
			return nil, err
		}

		out[i] = ClaimOutputsEvent{
			DelegationID: delegationID,
			Delegator:    (thor.Address)(*(data[0].(*common.Address))),
			TotalRewards: *(data[1].(**big.Int)),
		}
	}

	return out, nil
}

// Update WeightsPopulatedEvent to match ABI
type WeightsPopulatedEvent struct {
	ValidationID   thor.Bytes32
	StakingPeriod  uint32
	PreviousWeight *big.Int
	Increase       *big.Int
	Reduction      *big.Int
	NewWeight      *big.Int
}

// Update FilterWeightsPopulated to match ABI
func (s *Stargate) FilterWeightsPopulated(from, to uint32) ([]WeightsPopulatedEvent, error) {
	event, ok := s.contract.ABI().Events["WeightsPopulated"]
	if !ok {
		return nil, fmt.Errorf("event not found")
	}

	from64 := uint64(from)
	to64 := uint64(to)
	rnge := &events.Range{
		From: &from64,
		To:   &to64,
	}
	raw, err := s.contract.FilterEvents("WeightsPopulated", rnge, nil, logdb.ASC)
	if err != nil {
		return nil, err
	}

	out := make([]WeightsPopulatedEvent, len(raw))
	for i, log := range raw {
		validationID := thor.Bytes32(log.Topics[1][:]) // indexed

		// non-indexed
		data := make([]interface{}, 5)
		data[0] = new(uint32)
		data[1] = new(*big.Int)
		data[2] = new(*big.Int)
		data[3] = new(*big.Int)
		data[4] = new(*big.Int)

		bytes, err := hexutil.Decode(log.Data)
		if err != nil {
			return nil, err
		}

		if err := event.Inputs.Unpack(&data, bytes); err != nil {
			return nil, err
		}

		out[i] = WeightsPopulatedEvent{
			ValidationID:   validationID,
			StakingPeriod:  *(data[0].(*uint32)),
			PreviousWeight: *(data[1].(**big.Int)),
			Increase:       *(data[2].(**big.Int)),
			Reduction:      *(data[3].(**big.Int)),
			NewWeight:      *(data[4].(**big.Int)),
		}
	}

	return out, nil
}

type RewardsPopulatedEvent struct {
	ValidationID         thor.Bytes32
	StakingPeriod        uint32
	BlockRewards         *big.Int
	AllDelegatorsRewards *big.Int
	ProposerRewards      *big.Int
}

// FilterRewardsPopulated filters RewardsPopulated events
func (s *Stargate) FilterRewardsPopulated(from, to uint32) ([]RewardsPopulatedEvent, error) {
	event, ok := s.contract.ABI().Events["RewardsPopulated"]
	if !ok {
		return nil, fmt.Errorf("event not found")
	}

	from64 := uint64(from)
	to64 := uint64(to)
	rnge := &events.Range{
		From: &from64,
		To:   &to64,
	}
	raw, err := s.contract.FilterEvents("RewardsPopulated", rnge, nil, logdb.ASC)
	if err != nil {
		return nil, err
	}

	out := make([]RewardsPopulatedEvent, len(raw))
	for i, log := range raw {
		validationID := thor.Bytes32(log.Topics[1][:]) // indexed

		// non-indexed
		data := make([]interface{}, 4)
		data[0] = new(uint32)
		data[1] = new(*big.Int)
		data[2] = new(*big.Int)
		data[3] = new(*big.Int)

		bytes, err := hexutil.Decode(log.Data)
		if err != nil {
			return nil, err
		}

		if err := event.Inputs.Unpack(&data, bytes); err != nil {
			return nil, err
		}

		out[i] = RewardsPopulatedEvent{
			ValidationID:         validationID,
			StakingPeriod:        *(data[0].(*uint32)),
			BlockRewards:         *(data[1].(**big.Int)),
			AllDelegatorsRewards: *(data[2].(**big.Int)),
			ProposerRewards:      *(data[3].(**big.Int)),
		}
	}

	return out, nil
}

type RewardsCalculatedEvent struct {
	ValidationID         thor.Bytes32
	StakingPeriod        uint32
	Rewards              *big.Int
	AllDelegatorsWeight  *big.Int
	AllDelegatorsRewards *big.Int
}

// FilterRewardsCalculated filters RewardsCalculated events
func (s *Stargate) FilterRewardsCalculated(from, to uint32) ([]RewardsCalculatedEvent, error) {
	event, ok := s.contract.ABI().Events["RewardsCalculated"]
	if !ok {
		return nil, fmt.Errorf("event not found")
	}

	from64 := uint64(from)
	to64 := uint64(to)
	rnge := &events.Range{
		From: &from64,
		To:   &to64,
	}
	raw, err := s.contract.FilterEvents("RewardsCalculated", rnge, nil, logdb.ASC)
	if err != nil {
		return nil, err
	}

	out := make([]RewardsCalculatedEvent, len(raw))
	for i, log := range raw {
		validationID := thor.Bytes32(log.Topics[1][:]) // indexed

		// non-indexed
		data := make([]interface{}, 4)
		data[0] = new(uint32)
		data[1] = new(*big.Int)
		data[2] = new(*big.Int)
		data[3] = new(*big.Int)

		bytes, err := hexutil.Decode(log.Data)
		if err != nil {
			return nil, err
		}

		if err := event.Inputs.Unpack(&data, bytes); err != nil {
			return nil, err
		}

		out[i] = RewardsCalculatedEvent{
			ValidationID:         validationID,
			StakingPeriod:        *(data[0].(*uint32)),
			Rewards:              *(data[1].(**big.Int)),
			AllDelegatorsWeight:  *(data[2].(**big.Int)),
			AllDelegatorsRewards: *(data[3].(**big.Int)),
		}
	}

	return out, nil
}
