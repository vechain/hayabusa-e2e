package builtins

import (
	"crypto/ecdsa"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	common2 "github.com/vechain/draupnir/common"
	"github.com/vechain/draupnir/contracts"
	"github.com/vechain/thor/v2/abi"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
)

type Status uint8

const (
	StatusUnknown Status = iota
	StatusQueued
	StatusActive
	StatusCooldown
	StatusExited
)

//go:embed staker_abi.json
var StakerABI []byte
var MinStake = big.NewInt(0).Mul(big.NewInt(25_000_000), big.NewInt(1e18))

var StakerAddress = thor.BytesToAddress([]byte("Staker"))

type Staker struct {
	contract *contracts.GenericWrapper
	client   *thorclient.Client
	key      *ecdsa.PrivateKey
	abi      *abi.ABI
}

func NewStaker(client *thorclient.Client, key *ecdsa.PrivateKey) *Staker {
	base, err := contracts.NewGenericWrapper(client, key, StakerABI, StakerAddress)
	if err != nil {
		panic(fmt.Sprintf("failed to create staker contract: %v", err))
	}
	return &Staker{
		contract: base,
		client:   client,
		key:      key,
	}
}

func (s *Staker) WaitForFork(maxBlock uint32) error {
	ticker := common2.NewTicker(s.client)
	for {
		code, err := s.client.AccountCode(&StakerAddress)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		if len(code.Code) > 100 {
			slog.Info("staker is deployed", "addr", StakerAddress, "len", len(code.Code))
			return nil
		}
		blk, err := ticker.Wait(12 * time.Second)
		if err != nil {
			slog.Warn("failed to get block", "err", err)
			continue
		}
		if blk.Number > maxBlock {
			return fmt.Errorf("staker not deployed, reached max block %d", maxBlock)
		}
		slog.Info("checking for staker...", "block", blk.Number)
	}
}

func (s *Staker) WaitForPOS(maxBlock uint32) error {
	ticker := common2.NewTicker(s.client)
	for {
		first, _, err := s.FirstActive()
		if err == nil && first.Exists() {
			slog.Info("PoS is active", "first", first.Endorsor, "weight", first.Weight)
			return nil
		}
		block, err := ticker.Wait(12 * time.Second)
		if err != nil {
			slog.Warn("failed to get block", "err", err)
			continue
		}
		if block.Number > maxBlock {
			return fmt.Errorf("PoS not active, reached max block %d", maxBlock)
		}
		slog.Info("waiting for PoS to be active", "block", block.Number)
	}
}

func (s *Staker) Client() *thorclient.Client {
	return s.client
}

func (s *Staker) Address() thor.Address {
	return s.contract.Address()
}

func (s *Staker) Attach(key *ecdsa.PrivateKey) *Staker {
	return &Staker{
		contract: s.contract.Attach(key),
		client:   s.client,
		key:      key,
	}
}

// FirstActive returns the first active validator
func (s *Staker) FirstActive() (*Validator, thor.Bytes32, error) {
	out := new(common.Hash)
	if err := s.contract.CallInto("firstActive", &out); err != nil {
		return nil, thor.Bytes32{}, err
	}
	res := *out
	id := thor.Bytes32(res[:])
	if id.IsZero() {
		return nil, thor.Bytes32{}, errors.New("no active validator")
	}
	v, err := s.Get(id)
	return v, id, err
}

// FirstQueued returns the first queued validator
func (s *Staker) FirstQueued() (*Validator, thor.Bytes32, error) {
	out := new(common.Hash)
	if err := s.contract.CallInto("firstQueued", &out); err != nil {
		return nil, thor.Bytes32{}, err
	}
	res := *out
	id := thor.Bytes32(res[:])
	if id.IsZero() {
		return nil, thor.Bytes32{}, errors.New("no queued validator")
	}
	v, err := s.Get(id)
	return v, id, err
}

// Next returns the next validator
func (s *Staker) Next(id thor.Bytes32) (*Validator, thor.Bytes32, error) {
	out := new(common.Hash)
	if err := s.contract.CallInto("next", &out, id); err != nil {
		return nil, thor.Bytes32{}, err
	}
	res := *out
	next := thor.Bytes32(res[:])
	if next.IsZero() {
		return nil, thor.Bytes32{}, errors.New("no next validator")
	}
	v, err := s.Get(id)
	return v, next, err
}

func (s *Staker) TotalStake() (*big.Int, error) {
	out := new(big.Int)
	if err := s.contract.CallInto("totalStake", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Staker) QueuedStake() (*big.Int, error) {
	out := new(big.Int)
	if err := s.contract.CallInto("queuedStake", &out); err != nil {
		return nil, err
	}
	return out, nil
}

type Validator struct {
	Master    *thor.Address
	Endorsor  *thor.Address
	Stake     *big.Int
	Weight    *big.Int
	Status    Status
	AutoRenew bool
}

func (v *Validator) Exists() bool {
	return v.Endorsor != nil && !v.Endorsor.IsZero() && v.Status != 0
}

func (s *Staker) Get(id thor.Bytes32) (*Validator, error) {
	var out = make([]interface{}, 6)
	out[0] = new(common.Address)
	out[1] = new(common.Address)
	out[2] = new(*big.Int)
	out[3] = new(*big.Int)
	out[4] = new(uint8)
	out[5] = new(bool)
	if err := s.contract.CallInto("get", &out, id); err != nil {
		return nil, err
	}
	validator := &Validator{
		Master:    (*thor.Address)(out[0].(*common.Address)),
		Endorsor:  (*thor.Address)(out[1].(*common.Address)),
		Stake:     *(out[2].(**big.Int)),
		Weight:    *(out[3].(**big.Int)),
		Status:    Status(*(out[4].(*uint8))),
		AutoRenew: *(out[5].(*bool)),
	}

	return validator, nil
}

func (s *Staker) AddValidator(master thor.Address, stake *big.Int, period uint32, autoRenew bool) *contracts.Sender {
	return s.contract.SendWithVET(stake, "addValidator", master, period, autoRenew)
}

func (s *Staker) AddDelegation(
	validationID thor.Bytes32,
	stake *big.Int,
	autoRenew bool,
	multiplier uint8,
) *contracts.Sender {
	return s.contract.SendWithVET(stake, "addDelegation", validationID, autoRenew, multiplier)
}

func (s *Staker) UpdateDelegationAutoRenew(delegationID thor.Bytes32, autoRenew bool) *contracts.Sender {
	return s.contract.Send("updateDelegationAutoRenew", delegationID, autoRenew)
}

func (s *Staker) UpdateAutoRenew(validationID thor.Bytes32, autoRenew bool) *contracts.Sender {
	return s.contract.Send("updateAutoRenew", validationID, autoRenew)
}

func (s *Staker) WithdrawDelegation(delegationID thor.Bytes32) *contracts.Sender {
	return s.contract.Send("withdrawDelegation", delegationID)
}

func (s *Staker) Withdraw(validationID thor.Bytes32) *contracts.Sender {
	return s.contract.Send("withdraw", validationID)
}

func (s *Staker) DecreaseStake(validationID thor.Bytes32, amount *big.Int) *contracts.Sender {
	return s.contract.Send("decreaseStake", validationID, amount)
}

func (s *Staker) IncreaseStake(validationID thor.Bytes32, amount *big.Int) *contracts.Sender {
	return s.contract.SendWithVET(amount, "increaseStake", validationID)
}

func (s *Staker) GetWithdraw(validationID thor.Bytes32) (*big.Int, error) {
	out := new(big.Int)
	if err := s.contract.CallInto("getWithdraw", &out, validationID); err != nil {
		return nil, err
	}
	return out, nil
}

type Delegator struct {
	Stake      *big.Int
	Multiplier uint8
	AutoRenew  bool
}

func (s *Staker) GetDelegation(delegationID thor.Bytes32) (*Delegator, error) {
	var out = make([]interface{}, 3)
	out[0] = new(*big.Int)
	out[1] = new(uint8)
	out[2] = new(bool)
	if err := s.contract.CallInto("getDelegation", &out, delegationID); err != nil {
		return nil, err
	}
	delegatorInfo := &Delegator{
		Stake:      *(out[0].(**big.Int)),
		Multiplier: *(out[1].(*uint8)),
		AutoRenew:  *(out[2].(*bool)),
	}

	return delegatorInfo, nil
}

type ValidatorQueuedEvent struct {
	Endorsor     thor.Address
	Master       thor.Address
	ValidationID thor.Bytes32
	Stake        *big.Int
	Period       uint32
	AutoRenew    bool
}

func (s *Staker) FilterValidatorQueued(from, to uint32) ([]ValidatorQueuedEvent, error) {
	event, ok := s.contract.ABI().Events["ValidatorQueued"]
	if !ok {
		return nil, fmt.Errorf("event not found")
	}

	raw, err := s.contract.FilterEvents("ValidatorQueued", from, to)
	if err != nil {
		return nil, err
	}

	out := make([]ValidatorQueuedEvent, len(raw))
	for i, log := range raw {
		endorsor := thor.BytesToAddress(log.Topics[1][:]) // indexed
		master := thor.BytesToAddress(log.Topics[2][:])   // indexed
		validationID := thor.Bytes32(log.Topics[3][:])    // indexed

		// non-indexed
		data := make([]interface{}, 3)
		data[0] = new(uint32)
		data[1] = new(*big.Int)
		data[2] = new(bool)

		bytes, err := hexutil.Decode(log.Data)
		if err != nil {
			return nil, err
		}

		if err := event.Inputs.Unpack(&data, bytes); err != nil {
			return nil, err
		}

		out[i] = ValidatorQueuedEvent{
			Endorsor:     endorsor,
			Master:       master,
			ValidationID: validationID,
			Period:       *(data[0].(*uint32)),
			Stake:        *(data[1].(**big.Int)),
			AutoRenew:    *(data[2].(*bool)),
		}
	}

	return out, nil
}

type ValidatorUpdatedAutoRenewEvent struct {
	Endorsor     thor.Address
	ValidationID thor.Bytes32
	AutoRenew    bool
}

func (s *Staker) FilterValidatorUpdatedAutoRenew(from, to uint32) ([]ValidatorUpdatedAutoRenewEvent, error) {
	event, ok := s.contract.ABI().Events["ValidatorUpdatedAutoRenew"]
	if !ok {
		return nil, fmt.Errorf("event not found")
	}

	raw, err := s.contract.FilterEvents("ValidatorUpdatedAutoRenew", from, to)
	if err != nil {
		return nil, err
	}

	out := make([]ValidatorUpdatedAutoRenewEvent, len(raw))
	for i, log := range raw {
		endorsor := thor.BytesToAddress(log.Topics[1][:]) // indexed
		validationID := thor.Bytes32(log.Topics[2][:])    // indexed

		// non-indexed
		data := make([]interface{}, 1)
		data[0] = new(bool)

		bytes, err := hexutil.Decode(log.Data)
		if err != nil {
			return nil, err
		}

		if err := event.Inputs.Unpack(&data, bytes); err != nil {
			return nil, err
		}

		out[i] = ValidatorUpdatedAutoRenewEvent{
			Endorsor:     endorsor,
			ValidationID: validationID,
			AutoRenew:    *(data[0].(*bool)),
		}
	}

	return out, nil
}

type DelegationAddedEvent struct {
	ValidationID thor.Bytes32
	DelegationID thor.Bytes32
	Stake        *big.Int
	AutoRenew    bool
	Multiplier   uint8
}

func (s *Staker) FilterDelegationAdded(from, to uint32) ([]DelegationAddedEvent, error) {
	event, ok := s.contract.ABI().Events["DelegationAdded"]
	if !ok {
		return nil, fmt.Errorf("event not found")
	}

	raw, err := s.contract.FilterEvents("DelegationAdded", from, to)
	if err != nil {
		return nil, err
	}

	out := make([]DelegationAddedEvent, len(raw))
	for i, log := range raw {
		validationID := thor.Bytes32(log.Topics[1][:]) // indexed
		delegationID := thor.Bytes32(log.Topics[2][:]) // indexed

		// non-indexed
		data := make([]interface{}, 4)
		data[0] = new(*big.Int)
		data[1] = new(bool)
		data[2] = new(uint8)

		bytes, err := hexutil.Decode(log.Data)
		if err != nil {
			return nil, err
		}

		if err := event.Inputs.Unpack(&data, bytes); err != nil {
			return nil, err
		}

		out[i] = DelegationAddedEvent{
			ValidationID: validationID,
			DelegationID: delegationID,
			Stake:        *(data[0].(**big.Int)),
			AutoRenew:    *(data[1].(*bool)),
			Multiplier:   *(data[2].(*uint8)),
		}
	}

	return out, nil
}

type DelegationUpdatedAutoRenewEvent struct {
	DelegationID thor.Bytes32
	AutoRenew    bool
}

func (s *Staker) FilterDelegationUpdatedAutoRenew(from, to uint32) ([]DelegationUpdatedAutoRenewEvent, error) {
	event, ok := s.contract.ABI().Events["DelegationUpdatedAutoRenew"]
	if !ok {
		return nil, fmt.Errorf("event not found")
	}

	raw, err := s.contract.FilterEvents("DelegationUpdatedAutoRenew", from, to)
	if err != nil {
		return nil, err
	}

	out := make([]DelegationUpdatedAutoRenewEvent, len(raw))
	for i, log := range raw {
		delegationID := thor.Bytes32(log.Topics[1][:])

		// non-indexed
		data := make([]interface{}, 1)
		data[0] = new(bool)

		bytes, err := hexutil.Decode(log.Data)
		if err != nil {
			return nil, err
		}

		if err := event.Inputs.Unpack(&data, bytes); err != nil {
			return nil, err
		}

		out[i] = DelegationUpdatedAutoRenewEvent{
			DelegationID: delegationID,
			AutoRenew:    *(data[0].(*bool)),
		}
	}

	return out, nil
}

type DelegationWithdrawnEvent struct {
	DelegationID thor.Bytes32
	Stake        *big.Int
}

func (s *Staker) FilterDelegationWithdrawn(from, to uint32) ([]DelegationWithdrawnEvent, error) {
	event, ok := s.contract.ABI().Events["DelegationWithdrawn"]
	if !ok {
		return nil, fmt.Errorf("event not found")
	}

	raw, err := s.contract.FilterEvents("DelegationWithdrawn", from, to)
	if err != nil {
		return nil, err
	}

	out := make([]DelegationWithdrawnEvent, len(raw))
	for i, log := range raw {
		delegationID := thor.Bytes32(log.Topics[1][:]) // indexed

		// non-indexed
		data := make([]interface{}, 1)
		data[0] = new(*big.Int)

		bytes, err := hexutil.Decode(log.Data)
		if err != nil {
			return nil, err
		}

		if err := event.Inputs.Unpack(&data, bytes); err != nil {
			return nil, err
		}

		out[i] = DelegationWithdrawnEvent{
			DelegationID: delegationID,
			Stake:        *(data[0].(**big.Int)),
		}
	}

	return out, nil
}

type StakeIncreasedEvent struct {
	Endorsor     thor.Address
	ValidationID thor.Bytes32
	Added        *big.Int
}

func (s *Staker) FilterStakeIncreased(from, to uint32) ([]StakeIncreasedEvent, error) {
	event, ok := s.contract.ABI().Events["StakeIncreased"]
	if !ok {
		return nil, fmt.Errorf("event not found")
	}

	raw, err := s.contract.FilterEvents("StakeIncreased", from, to)
	if err != nil {
		return nil, err
	}

	out := make([]StakeIncreasedEvent, len(raw))
	for i, log := range raw {
		endorsor := thor.BytesToAddress(log.Topics[1][:]) // indexed
		validationID := thor.Bytes32(log.Topics[2][:])    // indexed

		// non-indexed
		data := make([]interface{}, 1)
		data[0] = new(*big.Int)

		bytes, err := hexutil.Decode(log.Data)
		if err != nil {
			return nil, err
		}

		if err := event.Inputs.Unpack(&data, bytes); err != nil {
			return nil, err
		}

		out[i] = StakeIncreasedEvent{
			Endorsor:     endorsor,
			ValidationID: validationID,
			Added:        *(data[0].(**big.Int)),
		}
	}

	return out, nil
}

type StakeDecreasedEvent struct {
	Endorsor     thor.Address
	ValidationID thor.Bytes32
	Removed      *big.Int
}

func (s *Staker) FilterStakeDecreased(from, to uint32) ([]StakeDecreasedEvent, error) {
	event, ok := s.contract.ABI().Events["StakeDecreased"]
	if !ok {
		return nil, fmt.Errorf("event not found")
	}

	raw, err := s.contract.FilterEvents("StakeDecreased", from, to)
	if err != nil {
		return nil, err
	}

	out := make([]StakeDecreasedEvent, len(raw))
	for i, log := range raw {
		endorsor := thor.BytesToAddress(log.Topics[1][:]) // indexed
		validationID := thor.Bytes32(log.Topics[2][:])    // indexed

		// non-indexed
		data := make([]interface{}, 1)
		data[0] = new(*big.Int)

		bytes, err := hexutil.Decode(log.Data)
		if err != nil {
			return nil, err
		}

		if err := event.Inputs.Unpack(&data, bytes); err != nil {
			return nil, err
		}

		out[i] = StakeDecreasedEvent{
			Endorsor:     endorsor,
			ValidationID: validationID,
			Removed:      *(data[0].(**big.Int)),
		}
	}

	return out, nil
}

type ValidatorWithdrawnEvent struct {
	Endorsor     thor.Address
	ValidationID thor.Bytes32
	Stake        *big.Int
}

func (s *Staker) FilterValidatorWithdrawn(from, to uint32) ([]ValidatorWithdrawnEvent, error) {
	event, ok := s.contract.ABI().Events["ValidatorWithdrawn"]
	if !ok {
		return nil, fmt.Errorf("event not found")
	}

	raw, err := s.contract.FilterEvents("ValidatorWithdrawn", from, to)
	if err != nil {
		return nil, err
	}

	out := make([]ValidatorWithdrawnEvent, len(raw))
	for i, log := range raw {
		endorsor := thor.BytesToAddress(log.Topics[1][:]) // indexed
		validationID := thor.Bytes32(log.Topics[2][:])    // indexed

		// non-indexed
		data := make([]interface{}, 1)
		data[0] = new(*big.Int)

		bytes, err := hexutil.Decode(log.Data)
		if err != nil {
			return nil, err
		}

		if err := event.Inputs.Unpack(&data, bytes); err != nil {
			return nil, err
		}

		out[i] = ValidatorWithdrawnEvent{
			Endorsor:     endorsor,
			ValidationID: validationID,
			Stake:        *(data[0].(**big.Int)),
		}
	}

	return out, nil
}
