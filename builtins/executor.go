package builtins

import (
	"crypto/ecdsa"
	_ "embed"
	"errors"
	"github.com/vechain/draupnir/contracts"
	"github.com/vechain/thor/v2/builtin"
	thorgenesis "github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
)

type Executor struct {
	contract *contracts.GenericWrapper
	client   *thorclient.Client
	key      *ecdsa.PrivateKey
}

//go:embed executor_abi.json
var ExecutorABI []byte

func NewExecutor(client *thorclient.Client, key *ecdsa.PrivateKey) (*Executor, error) {
	contract, err := contracts.NewGenericWrapper(client, key, ExecutorABI, builtin.Executor.Address)
	if err != nil {
		return nil, err
	}
	return &Executor{
		contract: contract,
		client:   client,
		key:      key,
	}, nil
}

func (e *Executor) Address() thor.Address {
	return e.contract.Address()
}

func (e *Executor) ABI() *contracts.GenericWrapper {
	return e.contract
}

func (e *Executor) Attach(key *ecdsa.PrivateKey) *Executor {
	return &Executor{
		contract: e.contract.Attach(key),
		client:   e.client,
		key:      key,
	}
}

func (e *Executor) Propose(target thor.Address, data []byte) *contracts.Sender {
	return e.contract.Send("propose", target, data)
}

func (e *Executor) Approve(proposalID thor.Bytes32) *contracts.Sender {
	return e.contract.Send("approve", proposalID)
}

func (e *Executor) Execute(proposalID thor.Bytes32) *contracts.Sender {
	return e.contract.Send("execute", proposalID)
}

// Update proposes, approves, and executes a proposal on the executor contract.
func (e *Executor) Update(target thor.Address, data []byte, executors []thorgenesis.DevAccount) error {
	receipt, _, err := e.Attach(executors[0].PrivateKey).Propose(target, data).Receipt(false)
	if err != nil {
		return err
	}
	if receipt.Reverted {
		return errors.New("proposal reverted")
	}
	proposalID := thor.Bytes32(receipt.Outputs[0].Events[0].Topics[1][:])

	senders := contracts.Senders{}
	for _, executor := range executors {
		sender := e.Attach(executor.PrivateKey).Approve(proposalID)
		senders.Add(sender)
	}
	_, _, err = senders.Send(false)
	if err != nil {
		return err
	}

	if _, _, err := e.Attach(executors[0].PrivateKey).Execute(proposalID).Receipt(false); err != nil {
		return err
	}
	return nil
}

type Proposal struct {
	ProposalID thor.Bytes32
	Action     thor.Bytes32
}

func (e *Executor) FilterProposals(from, to uint32) ([]Proposal, error) {
	raw, err := e.contract.FilterEvents("Proposal", from, to)
	if err != nil {
		return nil, err
	}
	out := make([]Proposal, len(raw))
	for i, v := range raw {
		proposalID := thor.Bytes32(v.Topics[1][:])
		action := thor.Bytes32(v.Topics[2][:])

		out[i] = Proposal{
			ProposalID: proposalID,
			Action:     action,
		}
	}
	return out, nil
}
