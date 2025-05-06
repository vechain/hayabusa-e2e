package builtins

import (
	"crypto/ecdsa"
	_ "embed"
	"github.com/vechain/draupnir/contracts"
	"github.com/vechain/thor/v2/builtin"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"math/big"
)

type Params struct {
	contract *contracts.GenericWrapper
	client   *thorclient.Client
	key      *ecdsa.PrivateKey
}

//go:embed params_abi.json
var ParamsABI []byte

func NewParams(client *thorclient.Client, key *ecdsa.PrivateKey) (*Params, error) {
	contract, err := contracts.NewGenericWrapper(client, key, ParamsABI, builtin.Params.Address)
	if err != nil {
		return nil, err
	}
	return &Params{
		contract: contract,
		client:   client,
		key:      key,
	}, nil
}

func (p *Params) Address() thor.Address {
	return p.contract.Address()
}

func (p *Params) ABI() *contracts.GenericWrapper {
	return p.contract
}

func (p *Params) Attach(key *ecdsa.PrivateKey) *Params {
	return &Params{
		contract: p.contract.Attach(key),
		client:   p.client,
		key:      key,
	}
}

func (p *Params) Set(key thor.Bytes32, value *big.Int) *contracts.Sender {
	return p.contract.Send("set", key, value)
}

func (p *Params) Get(key thor.Bytes32) (*big.Int, error) {
	out := new(big.Int)
	if err := p.contract.CallInto("get", &out, key); err != nil {
		return nil, err
	}
	return out, nil
}
