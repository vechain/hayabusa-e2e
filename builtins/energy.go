package builtins

import (
	"crypto/ecdsa"
	_ "embed"
	"fmt"
	"github.com/vechain/thor/v2/builtin"
	"math/big"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/vechain/draupnir/contracts"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
)

//go:embed energy_abi.json
var EnergyABI []byte

type Energy struct {
	contract *contracts.GenericWrapper
	client   *thorclient.Client
	key      *ecdsa.PrivateKey
}

func NewEnergy(client *thorclient.Client, key *ecdsa.PrivateKey) *Energy {
	base, err := contracts.NewGenericWrapper(client, key, EnergyABI, builtin.Energy.Address)
	if err != nil {
		panic(fmt.Sprintf("failed to create energy contract: %v", err))
	}
	return &Energy{
		contract: base,
		client:   client,
		key:      key,
	}
}

func (e *Energy) Client() *thorclient.Client {
	return e.client
}

func (e *Energy) Address() thor.Address {
	return e.contract.Address()
}

func (e *Energy) Attach(key *ecdsa.PrivateKey) *Energy {
	return &Energy{
		contract: e.contract.Attach(key),
		client:   e.client,
		key:      key,
	}
}

func (e *Energy) Revision(blockID thor.Bytes32) *Energy {
	return &Energy{
		contract: e.contract.Revision(blockID),
		client:   e.client,
		key:      e.key,
	}
}

// Name returns the name of the token
func (e *Energy) Name() (string, error) {
	var name string
	if err := e.contract.CallInto("name", &name); err != nil {
		return "", err
	}
	return name, nil
}

// Symbol returns the symbol of the token
func (e *Energy) Symbol() (string, error) {
	var symbol string
	if err := e.contract.CallInto("symbol", &symbol); err != nil {
		return "", err
	}
	return symbol, nil
}

// Decimals returns the number of decimals the token uses
func (e *Energy) Decimals() (uint8, error) {
	var decimals uint8
	if err := e.contract.CallInto("decimals", &decimals); err != nil {
		return 0, err
	}
	return decimals, nil
}

// TotalSupply returns the total token supply
func (e *Energy) TotalSupply() (*big.Int, error) {
	out := new(big.Int)
	if err := e.contract.CallInto("totalSupply", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// TotalBurned returns the total amount of burned tokens
func (e *Energy) TotalBurned() (*big.Int, error) {
	out := new(big.Int)
	if err := e.contract.CallInto("totalBurned", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// BalanceOf returns the token balance of the specified address
func (e *Energy) BalanceOf(owner thor.Address) (*big.Int, error) {
	out := new(big.Int)
	if err := e.contract.CallInto("balanceOf", &out, owner); err != nil {
		return nil, err
	}
	return out, nil
}

// Allowance returns the amount of tokens approved by the owner to be spent by the spender
func (e *Energy) Allowance(owner, spender thor.Address) (*big.Int, error) {
	out := new(big.Int)
	if err := e.contract.CallInto("allowance", &out, owner, spender); err != nil {
		return nil, err
	}
	return out, nil
}

// Transfer transfers tokens to the specified address
func (e *Energy) Transfer(to thor.Address, amount *big.Int) *contracts.Sender {
	return e.contract.Send("transfer", to, amount)
}

// TransferFrom transfers tokens from one address to another
func (e *Energy) TransferFrom(from, to thor.Address, amount *big.Int) *contracts.Sender {
	return e.contract.Send("transferFrom", from, to, amount)
}

// Approve approves the spender to spend the specified amount of tokens
func (e *Energy) Approve(spender thor.Address, amount *big.Int) *contracts.Sender {
	return e.contract.Send("approve", spender, amount)
}

// Move transfers tokens from one address to another (alias for transferFrom)
func (e *Energy) Move(from, to thor.Address, amount *big.Int) *contracts.Sender {
	return e.contract.Send("move", from, to, amount)
}

// TransferEvent represents the Transfer event
type TransferEvent struct {
	From  thor.Address
	To    thor.Address
	Value *big.Int
}

// FilterTransfer filters Transfer events between the specified blocks
func (e *Energy) FilterTransfer(from, to uint32) ([]TransferEvent, error) {
	event, ok := e.contract.ABI().Events["Transfer"]
	if !ok {
		return nil, fmt.Errorf("event not found")
	}

	logs, err := e.contract.FilterEvents("Transfer", from, to)
	if err != nil {
		return nil, err
	}

	events := make([]TransferEvent, len(logs))
	for i, log := range logs {
		fromAddr := thor.BytesToAddress(log.Topics[1][:])
		toAddr := thor.BytesToAddress(log.Topics[2][:])

		// Non-indexed fields
		data := make([]interface{}, 1)
		data[0] = new(*big.Int)

		bytes, err := hexutil.Decode(log.Data)
		if err != nil {
			return nil, err
		}

		if err := event.Inputs.Unpack(&data, bytes); err != nil {
			return nil, err
		}

		events[i] = TransferEvent{
			From:  fromAddr,
			To:    toAddr,
			Value: *(data[0].(**big.Int)),
		}
	}

	return events, nil
}

// ApprovalEvent represents the Approval event
type ApprovalEvent struct {
	Owner   thor.Address
	Spender thor.Address
	Value   *big.Int
}

// FilterApproval filters Approval events between the specified blocks
func (e *Energy) FilterApproval(from, to uint32) ([]ApprovalEvent, error) {
	event, ok := e.contract.ABI().Events["Approval"]
	if !ok {
		return nil, fmt.Errorf("event not found")
	}

	logs, err := e.contract.FilterEvents("Approval", from, to)
	if err != nil {
		return nil, err
	}

	events := make([]ApprovalEvent, len(logs))
	for i, log := range logs {
		ownerAddr := thor.BytesToAddress(log.Topics[1][:])
		spenderAddr := thor.BytesToAddress(log.Topics[2][:])

		// Non-indexed fields
		data := make([]interface{}, 1)
		data[0] = new(*big.Int)

		bytes, err := hexutil.Decode(log.Data)
		if err != nil {
			return nil, err
		}

		if err := event.Inputs.Unpack(&data, bytes); err != nil {
			return nil, err
		}

		events[i] = ApprovalEvent{
			Owner:   ownerAddr,
			Spender: spenderAddr,
			Value:   *(data[0].(**big.Int)),
		}
	}

	return events, nil
}
