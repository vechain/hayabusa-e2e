package stargate

import (
	_ "embed"
	"fmt"
	"github.com/vechain/thor/v2/api"
	"log/slog"
	"math/big"

	"github.com/vechain/thor/v2/thorclient"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/bind"
)

//go:embed Stargate.abi
var ABI []byte

//go:embed Stargate.bin
var Bin string

// Stargate represents a wrapper to interact with the Stargate contract
type Stargate struct {
	contract bind.Contract
}

// NewStargate creates a new instance of the Stargate contract wrapper
func NewStargate(client *thorclient.Client, addr thor.Address) *Stargate {
	base, err := bind.NewContract(client, ABI, &addr)
	if err != nil {
		panic(fmt.Sprintf("failed to create stargate contract: %v", err))
	}
	return &Stargate{
		contract: base,
	}
}

// Raw returns the underlying caller for direct interactions
func (s *Stargate) Raw() bind.Contract {
	return s.contract
}

// Address returns the address of the contract
func (s *Stargate) Address() *thor.Address {
	return s.contract.Address()
}

// ---- Getter Methods ----

// Claims returns the claims for a given validation ID
func (s *Stargate) Claims(validationID thor.Bytes32) (uint32, error) {
	var result uint32
	if err := s.contract.Method("claims", validationID).Call().ExecuteInto(&result); err != nil {
		return 0, err
	}
	return result, nil
}

// DelegationIDs returns the delegation ID for a given address
func (s *Stargate) DelegationIDs(address thor.Address) (thor.Bytes32, error) {
	out := new(common.Hash)
	if err := s.contract.Method("delegationIDs", address).Call().ExecuteInto(&out); err != nil {
		return thor.Bytes32{}, err
	}
	return thor.Bytes32(*out), nil
}

// GetClaimable returns the claimable rewards for a delegator
func (s *Stargate) GetClaimable(delegator thor.Address) (*big.Int, uint32, uint32, error) {
	var out = make([]any, 3)
	out[0] = new(*big.Int)
	out[1] = new(uint32)
	out[2] = new(uint32)

	if err := s.contract.Method("getClaimable", delegator).Call().ExecuteInto(&out); err != nil {
		return nil, 0, 0, err
	}

	return *(out[0].(**big.Int)), *(out[1].(*uint32)), *(out[2].(*uint32)), nil
}

// PopulatedWeights returns the populated weights for a validation ID
func (s *Stargate) PopulatedWeights(validationID thor.Bytes32) (uint32, error) {
	var result uint32
	if err := s.contract.Method("populatedWeights", validationID).Call().ExecuteInto(&result); err != nil {
		return 0, err
	}
	return result, nil
}

// Reductions returns the reductions for a validation ID and period
func (s *Stargate) Reductions(validationID thor.Bytes32, period uint32) (*big.Int, error) {
	out := new(big.Int)
	if err := s.contract.Method("reductions", validationID, period).Call().ExecuteInto(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// Rewards returns the rewards for a validation ID and period
func (s *Stargate) Rewards(validationID thor.Bytes32, period uint32) (*big.Int, error) {
	out := new(big.Int)
	if err := s.contract.Method("rewards", validationID, period).Call().ExecuteInto(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// Staker returns the staker contract address
func (s *Stargate) Staker() (thor.Address, error) {
	out := new(common.Address)
	if err := s.contract.Method("staker").Call().ExecuteInto(&out); err != nil {
		return thor.Address{}, err
	}
	return thor.Address(*out), nil
}

// VTHO returns the VTHO token contract address
func (s *Stargate) VTHO() (thor.Address, error) {
	out := new(common.Address)
	if err := s.contract.Method("vtho").Call().ExecuteInto(&out); err != nil {
		return thor.Address{}, err
	}
	return thor.Address(*out), nil
}

// Weights returns the weights for a validation ID and period
func (s *Stargate) Weights(validationID thor.Bytes32, period uint32) (*big.Int, error) {
	out := new(big.Int)
	if err := s.contract.Method("weights", validationID, period).Call().ExecuteInto(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// ---- Transaction Methods ----

// AddDelegator adds a delegator to a validation ID
func (s *Stargate) AddDelegator(signer bind.Signer, validationID thor.Bytes32, autoRenew bool, multiplier uint8, amount *big.Int) bind.SendBuilder {
	return s.contract.Method("addDelegator", validationID, autoRenew, multiplier).WithValue(amount).Send().WithSigner(signer)
}

// ClaimRewards claims rewards for the sender
func (s *Stargate) ClaimRewards(signer bind.Signer) bind.SendBuilder {
	return s.contract.Method("claimRewards").Send().WithSigner(signer)
}

// DisableAutoRenew disables auto renewal for the sender's delegation
func (s *Stargate) DisableAutoRenew(signer bind.Signer) bind.SendBuilder {
	return s.contract.Method("disableAutoRenew").Send().WithSigner(signer)
}

// EnableAutoRenew enables auto renewal for the sender's delegation
func (s *Stargate) EnableAutoRenew(signer bind.Signer) bind.SendBuilder {
	return s.contract.Method("enableAutoRenew").Send().WithSigner(signer)
}

// ---- Event Filterers ----

func (s *Stargate) filterEvents(name string, from, to uint32) ([]api.FilteredEvent, error) {
	from64 := uint64(from)
	to64 := uint64(to)
	rnge := &api.Range{
		From: &from64,
		To:   &to64,
	}
	return s.contract.FilterEvent(name).InRange(rnge).Execute()
}

type ClaimedRewardsEvent struct {
	ValidationID         thor.Bytes32
	Delegator            thor.Address
	Amount               *big.Int
	FirstClaimablePeriod uint32
	LastClaimablePeriod  uint32
}

func (s *Stargate) ParseClaimedRewards(topics []*thor.Bytes32, data string) (*ClaimedRewardsEvent, error) {
	if len(topics) < 3 {
		return nil, fmt.Errorf("not enough topics for ClaimedRewards event")
	}

	validationID := thor.Bytes32(topics[1][:])     // indexed
	delegator := thor.BytesToAddress(topics[2][:]) // indexed
	dataFields := make([]any, 3)
	dataFields[0] = new(*big.Int)
	dataFields[1] = new(uint32)
	dataFields[2] = new(uint32)
	bytes, err := hexutil.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode ClaimedRewards event data: %w", err)
	}
	if err := s.contract.ABI().Events["ClaimedRewards"].Inputs.Unpack(&dataFields, bytes); err != nil {
		return nil, fmt.Errorf("failed to unpack ClaimedRewards event data: %w", err)
	}
	return &ClaimedRewardsEvent{
		ValidationID:         validationID,
		Delegator:            delegator,
		Amount:               *(dataFields[0].(**big.Int)),
		FirstClaimablePeriod: *(dataFields[1].(*uint32)),
		LastClaimablePeriod:  *(dataFields[2].(*uint32)),
	}, nil
}

func (s *Stargate) FilterClaimedRewards(from, to uint32) ([]*ClaimedRewardsEvent, error) {
	raw, err := s.filterEvents("ClaimedRewards", from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to filter ClaimedRewards events: %w", err)
	}

	out := make([]*ClaimedRewardsEvent, len(raw))
	for i, log := range raw {
		out[i], err = s.ParseClaimedRewards(log.Topics, log.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ClaimedRewards event: %w", err)
		}
	}

	return out, nil
}

type ClaimParamsEvent struct {
	DelegationID              thor.Bytes32
	Delegator                 thor.Address
	FirstClaimablePeriod      uint32
	LastClaimablePeriod       uint32
	PreviouslyPopulatedPeriod uint32
	MaxClaimablePeriod        uint32
	DelegatorWeight           *big.Int
}

func (s *Stargate) ParseClaimParams(topics []*thor.Bytes32, data string) (*ClaimParamsEvent, error) {
	if len(topics) < 2 {
		return nil, fmt.Errorf("not enough topics for ClaimParams event")
	}

	delegationID := thor.Bytes32(topics[1][:]) // indexed
	dataFields := make([]any, 6)
	dataFields[0] = new(common.Address)
	dataFields[1] = new(uint32)
	dataFields[2] = new(uint32)
	dataFields[3] = new(uint32)
	dataFields[4] = new(uint32)
	dataFields[5] = new(*big.Int)
	bytes, err := hexutil.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode ClaimParams event data: %w", err)
	}
	if err := s.contract.ABI().Events["ClaimParams"].Inputs.Unpack(&dataFields, bytes); err != nil {
		return nil, fmt.Errorf("failed to unpack ClaimParams event data: %w", err)
	}
	return &ClaimParamsEvent{
		DelegationID:              delegationID,
		Delegator:                 (thor.Address)(*(dataFields[0].(*common.Address))),
		FirstClaimablePeriod:      *(dataFields[1].(*uint32)),
		LastClaimablePeriod:       *(dataFields[2].(*uint32)),
		PreviouslyPopulatedPeriod: *(dataFields[3].(*uint32)),
		MaxClaimablePeriod:        *(dataFields[4].(*uint32)),
		DelegatorWeight:           *(dataFields[5].(**big.Int)),
	}, nil
}

func (s *Stargate) FilterClaimParams(from, to uint32) ([]*ClaimParamsEvent, error) {
	raw, err := s.filterEvents("ClaimParams", from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to filter ClaimParams events: %w", err)
	}

	out := make([]*ClaimParamsEvent, len(raw))
	for i, log := range raw {
		out[i], err = s.ParseClaimParams(log.Topics, log.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ClaimParams event: %w", err)
		}
	}

	return out, nil
}

type ClaimOutputsEvent struct {
	DelegationID thor.Bytes32
	Delegator    thor.Address
	TotalRewards *big.Int
}

func (s *Stargate) ParseClaimOutputs(topics []*thor.Bytes32, data string) (*ClaimOutputsEvent, error) {
	if len(topics) < 2 {
		return nil, fmt.Errorf("not enough topics for ClaimOutputs event")
	}

	delegationID := thor.Bytes32(topics[1][:]) // indexed
	dataFields := make([]any, 2)
	dataFields[0] = new(common.Address)
	dataFields[1] = new(*big.Int)
	bytes, err := hexutil.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode ClaimOutputs event data: %w", err)
	}
	if err := s.contract.ABI().Events["ClaimOutputs"].Inputs.Unpack(&dataFields, bytes); err != nil {
		return nil, fmt.Errorf("failed to unpack ClaimOutputs event data: %w", err)
	}
	return &ClaimOutputsEvent{
		DelegationID: delegationID,
		Delegator:    (thor.Address)(*(dataFields[0].(*common.Address))),
		TotalRewards: *(dataFields[1].(**big.Int)),
	}, nil
}

func (s *Stargate) FilterClaimOutputs(from, to uint32) ([]*ClaimOutputsEvent, error) {
	raw, err := s.filterEvents("ClaimOutputs", from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to filter ClaimOutputs events: %w", err)
	}

	out := make([]*ClaimOutputsEvent, len(raw))
	for i, log := range raw {
		out[i], err = s.ParseClaimOutputs(log.Topics, log.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ClaimOutputs event: %w", err)
		}
	}

	return out, nil
}

type WeightsPopulatedEvent struct {
	ValidationID   thor.Bytes32
	StakingPeriod  uint32
	PreviousWeight *big.Int
	Increase       *big.Int
	Reduction      *big.Int
	NewWeight      *big.Int
}

func (s *Stargate) ParseWeightsPopulated(topics []*thor.Bytes32, data string) (*WeightsPopulatedEvent, error) {
	if len(topics) < 2 {
		return nil, fmt.Errorf("not enough topics for WeightsPopulated event")
	}

	validationID := thor.Bytes32(topics[1][:]) // indexed
	dataFields := make([]any, 5)
	dataFields[0] = new(uint32)
	dataFields[1] = new(*big.Int)
	dataFields[2] = new(*big.Int)
	dataFields[3] = new(*big.Int)
	dataFields[4] = new(*big.Int)
	bytes, err := hexutil.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode WeightsPopulated event data: %w", err)
	}
	if err := s.contract.ABI().Events["WeightsPopulated"].Inputs.Unpack(&dataFields, bytes); err != nil {
		return nil, fmt.Errorf("failed to unpack WeightsPopulated event data: %w", err)
	}
	return &WeightsPopulatedEvent{
		ValidationID:   validationID,
		StakingPeriod:  *(dataFields[0].(*uint32)),
		PreviousWeight: *(dataFields[1].(**big.Int)),
		Increase:       *(dataFields[2].(**big.Int)),
		Reduction:      *(dataFields[3].(**big.Int)),
		NewWeight:      *(dataFields[4].(**big.Int)),
	}, nil
}

func (s *Stargate) FilterWeightsPopulated(from, to uint32) ([]*WeightsPopulatedEvent, error) {
	raw, err := s.filterEvents("WeightsPopulated", from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to filter WeightsPopulated events: %w", err)
	}
	out := make([]*WeightsPopulatedEvent, len(raw))
	for i, log := range raw {
		out[i], err = s.ParseWeightsPopulated(log.Topics, log.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse WeightsPopulated event: %w", err)
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

func (s *Stargate) ParseRewardsPopulated(topics []*thor.Bytes32, data string) (*RewardsPopulatedEvent, error) {
	if len(topics) < 2 {
		return nil, fmt.Errorf("not enough topics for RewardsPopulated event")
	}

	validationID := thor.Bytes32(topics[1][:]) // indexed
	dataFields := make([]any, 4)
	dataFields[0] = new(uint32)
	dataFields[1] = new(*big.Int)
	dataFields[2] = new(*big.Int)
	dataFields[3] = new(*big.Int)
	bytes, err := hexutil.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode RewardsPopulated event data: %w", err)
	}
	if err := s.contract.ABI().Events["RewardsPopulated"].Inputs.Unpack(&dataFields, bytes); err != nil {
		return nil, fmt.Errorf("failed to unpack RewardsPopulated event data: %w", err)
	}
	return &RewardsPopulatedEvent{
		ValidationID:         validationID,
		StakingPeriod:        *(dataFields[0].(*uint32)),
		BlockRewards:         *(dataFields[1].(**big.Int)),
		AllDelegatorsRewards: *(dataFields[2].(**big.Int)),
		ProposerRewards:      *(dataFields[3].(**big.Int)),
	}, nil
}

// FilterRewardsPopulated filters RewardsPopulated events
func (s *Stargate) FilterRewardsPopulated(from, to uint32) ([]*RewardsPopulatedEvent, error) {
	raw, err := s.filterEvents("RewardsPopulated", from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to filter RewardsPopulated events: %w", err)
	}

	out := make([]*RewardsPopulatedEvent, len(raw))
	for i, log := range raw {
		out[i], err = s.ParseRewardsPopulated(log.Topics, log.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse RewardsPopulated event: %w", err)
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

// ParseRewardsCalculated
func (s *Stargate) ParseRewardsCalculated(topics []*thor.Bytes32, data string) (*RewardsCalculatedEvent, error) {
	if len(topics) < 2 {
		return nil, fmt.Errorf("not enough topics for RewardsCalculated event")
	}

	validationID := thor.Bytes32(topics[1][:]) // indexed
	dataFields := make([]any, 4)
	dataFields[0] = new(uint32)
	dataFields[1] = new(*big.Int)
	dataFields[2] = new(*big.Int)
	dataFields[3] = new(*big.Int)
	bytes, err := hexutil.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode RewardsCalculated event data: %w", err)
	}
	if err := s.contract.ABI().Events["RewardsCalculated"].Inputs.Unpack(&dataFields, bytes); err != nil {
		return nil, fmt.Errorf("failed to unpack RewardsCalculated event data: %w", err)
	}
	return &RewardsCalculatedEvent{
		ValidationID:         validationID,
		StakingPeriod:        *(dataFields[0].(*uint32)),
		Rewards:              *(dataFields[1].(**big.Int)),
		AllDelegatorsWeight:  *(dataFields[2].(**big.Int)),
		AllDelegatorsRewards: *(dataFields[3].(**big.Int)),
	}, nil
}

// FilterRewardsCalculated filters RewardsCalculated events
func (s *Stargate) FilterRewardsCalculated(from, to uint32) ([]*RewardsCalculatedEvent, error) {
	raw, err := s.filterEvents("RewardsCalculated", from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to filter RewardsCalculated events: %w", err)
	}

	out := make([]*RewardsCalculatedEvent, len(raw))
	for i, log := range raw {
		out[i], err = s.ParseRewardsCalculated(log.Topics, log.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse RewardsCalculated event: %w", err)
		}
	}

	return out, nil
}

func (s *Stargate) LogEventValues(events []*api.Event) {
	for _, event := range events {
		name := ""
		for _, abiEvent := range s.contract.ABI().Events {
			if event.Topics[0].String() == abiEvent.Id().String() {
				name = abiEvent.Name
				break
			}
		}

		if name == "" {
			fmt.Printf("Unknown event: %s\n", event.Topics[0].String())
			continue
		}

		switch name {
		case "ClaimedRewards":
			event, err := s.ParseClaimedRewards(topicsToPointers(event.Topics), event.Data)
			if err != nil {
				fmt.Printf("Error parsing ClaimedRewards event: %v\n", err)
				continue
			}

			slog.Info("ClaimedRewards Event",
				"amount", event.Amount,
				"firstClaimablePeriod", event.FirstClaimablePeriod,
				"lastClaimablePeriod", event.LastClaimablePeriod,
			)
		case "ClaimParams":
			event, err := s.ParseClaimParams(topicsToPointers(event.Topics), event.Data)
			if err != nil {
				fmt.Printf("Error parsing ClaimParams event: %v\n", err)
				continue
			}

			slog.Info("ClaimParams Event",
				"firstClaimablePeriod", event.FirstClaimablePeriod,
				"lastClaimablePeriod", event.LastClaimablePeriod,
				"previouslyPopulatedPeriod", event.PreviouslyPopulatedPeriod,
				"maxClaimablePeriod", event.MaxClaimablePeriod,
				"delegatorWeight", event.DelegatorWeight,
			)

		case "ClaimOutputs":
			event, err := s.ParseClaimOutputs(topicsToPointers(event.Topics), event.Data)
			if err != nil {
				fmt.Printf("Error parsing ClaimOutputs event: %v\n", err)
				continue
			}
			slog.Info("ClaimOutputs Event",
				"totalRewards", event.TotalRewards,
			)
		case "WeightsPopulated":
			event, err := s.ParseWeightsPopulated(topicsToPointers(event.Topics), event.Data)
			if err != nil {
				fmt.Printf("Error parsing WeightsPopulated event: %v\n", err)
				continue
			}
			slog.Info("WeightsPopulated Event",
				"stakingPeriod", event.StakingPeriod,
				"previousWeight", event.PreviousWeight,
				"increase", event.Increase,
				"reduction", event.Reduction,
				"newWeight", event.NewWeight,
			)

		case "RewardsPopulated":
			event, err := s.ParseRewardsPopulated(topicsToPointers(event.Topics), event.Data)
			if err != nil {
				fmt.Printf("Error parsing RewardsPopulated event: %v\n", err)
				continue
			}

			slog.Info("RewardsPopulated Event",
				"stakingPeriod", event.StakingPeriod,
				"blockRewards", event.BlockRewards,
				"allDelegatorsRewards", event.AllDelegatorsRewards,
				"proposerRewards", event.ProposerRewards,
			)
		case "RewardsCalculated":
			event, err := s.ParseRewardsCalculated(topicsToPointers(event.Topics), event.Data)
			if err != nil {
				fmt.Printf("Error parsing RewardsCalculated event: %v\n", err)
				continue
			}
			slog.Info("RewardsCalculated Event",
				"stakingPeriod", event.StakingPeriod,
				"rewards", event.Rewards,
				"allDelegatorsWeight", event.AllDelegatorsWeight,
				"allDelegatorsRewards", event.AllDelegatorsRewards,
			)

		default:
			slog.Warn("Unknown Stargate event",
				"name", name,
				"topics", event.Topics)
		}
	}
}

func topicsToPointers(topics []thor.Bytes32) []*thor.Bytes32 {
	pointers := make([]*thor.Bytes32, len(topics))
	for i, topic := range topics {
		pointers[i] = &topic
	}
	return pointers
}
