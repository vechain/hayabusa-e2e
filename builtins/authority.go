package builtins

import (
	"crypto/ecdsa"
	_ "embed"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vechain/draupnir/contracts"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
)

//go:embed authority_abi.json
var AuthorityABI []byte

var AuthorityAddress = thor.BytesToAddress([]byte("Authority"))

type Authority struct {
	contract *contracts.GenericWrapper
	client   *thorclient.Client
	key      *ecdsa.PrivateKey
}

func NewAuthority(client *thorclient.Client, key *ecdsa.PrivateKey) *Authority {
	base, err := contracts.NewGenericWrapper(client, key, AuthorityABI, AuthorityAddress)
	if err != nil {
		panic(fmt.Sprintf("failed to create authority contract: %v", err))
	}
	return &Authority{
		contract: base,
		client:   client,
		key:      key,
	}
}

func (a *Authority) Client() *thorclient.Client {
	return a.client
}

func (a *Authority) Address() thor.Address {
	return a.contract.Address()
}

func (a *Authority) Attach(key *ecdsa.PrivateKey) *Authority {
	return &Authority{
		contract: a.contract.Attach(key),
		client:   a.client,
		key:      key,
	}
}

// First returns the first authority node
func (a *Authority) First() (thor.Address, error) {
	out := new(common.Address)
	if err := a.contract.CallInto("first", &out); err != nil {
		return thor.Address{}, err
	}
	return thor.Address(*out), nil
}

// Next returns the next authority node after the given node master
func (a *Authority) Next(nodeMaster thor.Address) (thor.Address, error) {
	out := new(common.Address)
	if err := a.contract.CallInto("next", &out, common.Address(nodeMaster)); err != nil {
		return thor.Address{}, err
	}
	return thor.Address(*out), nil
}

// Executor returns the executor address
func (a *Authority) Executor() (thor.Address, error) {
	out := new(common.Address)
	if err := a.contract.CallInto("executor", &out); err != nil {
		return thor.Address{}, err
	}
	return thor.Address(*out), nil
}

type AuthorityNode struct {
	Listed   bool
	Endorsor thor.Address
	Identity thor.Bytes32
	Active   bool
}

// Get returns the authority node information for the given node master
func (a *Authority) Get(nodeMaster thor.Address) (*AuthorityNode, error) {
	var out = [4]interface{}{}
	out[0] = new(bool)
	out[1] = new(common.Address)
	out[2] = new(common.Hash)
	out[3] = new(bool)

	if err := a.contract.CallInto("get", &out, common.Address(nodeMaster)); err != nil {
		return nil, err
	}

	node := &AuthorityNode{
		Listed:   *(out[0].(*bool)),
		Endorsor: thor.Address(*(out[1].(*common.Address))),
		Identity: thor.Bytes32(*(out[2].(*common.Hash))),
		Active:   *(out[3].(*bool)),
	}

	return node, nil
}

// Add adds a new authority node
func (a *Authority) Add(nodeMaster, endorsor thor.Address, identity thor.Bytes32) *contracts.Sender {
	return a.contract.Send("add", common.Address(nodeMaster), common.Address(endorsor), common.Hash(identity))
}

// Revoke revokes an authority node
func (a *Authority) Revoke(nodeMaster thor.Address) *contracts.Sender {
	return a.contract.Send("revoke", common.Address(nodeMaster))
}

type CandidateEvent struct {
	NodeMaster thor.Address
	Action     thor.Bytes32
}

// FilterCandidate filters Candidate events within the given block range
func (a *Authority) FilterCandidate(from, to uint32) ([]CandidateEvent, error) {
	event, ok := a.contract.ABI().Events["Candidate"]
	if !ok {
		return nil, fmt.Errorf("event not found")
	}

	raw, err := a.contract.FilterEvents("Candidate", from, to)
	if err != nil {
		return nil, err
	}

	out := make([]CandidateEvent, len(raw))
	for i, log := range raw {
		nodeMaster := thor.BytesToAddress(log.Topics[1][:]) // indexed

		// non-indexed data
		data := make([]interface{}, 1)
		data[0] = new(common.Hash)

		bytes := common.FromHex(log.Data)
		if err := event.Inputs.Unpack(&data, bytes); err != nil {
			return nil, err
		}

		out[i] = CandidateEvent{
			NodeMaster: nodeMaster,
			Action:     thor.Bytes32(*(data[0].(*common.Hash))),
		}
	}

	return out, nil
}
