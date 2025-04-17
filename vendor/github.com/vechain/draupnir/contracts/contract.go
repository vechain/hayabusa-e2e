package contracts

import (
	"bytes"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/vechain/thor/v2/api/accounts"
	"github.com/vechain/thor/v2/api/events"
	"github.com/vechain/thor/v2/api/transactions"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/tx"
)

type GenericWrapper struct {
	client *thorclient.Client
	key    *ecdsa.PrivateKey
	abi    *abi.ABI
	addr   thor.Address
}

func NewGenericWrapper(client *thorclient.Client, key *ecdsa.PrivateKey, abiData []byte, address thor.Address) (*GenericWrapper, error) {
	contractABI, err := abi.JSON(bytes.NewReader(abiData))
	if err != nil {
		return nil, err
	}
	return &GenericWrapper{
		client: client,
		key:    key,
		abi:    &contractABI,
		addr:   address,
	}, nil
}

func (g *GenericWrapper) Address() thor.Address {
	return g.addr
}

func (g *GenericWrapper) ABI() *abi.ABI {
	return g.abi
}

func (g *GenericWrapper) Attach(key *ecdsa.PrivateKey) *GenericWrapper {
	return &GenericWrapper{
		client: g.client,
		key:    key,
		abi:    g.abi,
		addr:   g.addr,
	}
}

func (g *GenericWrapper) Call(methodName string, args ...interface{}) (*accounts.CallResult, error) {
	clause, err := g.Clause(methodName, args...)
	if err != nil {
		return nil, err
	}
	caller := thor.Address(crypto.PubkeyToAddress(g.key.PublicKey))
	res, err := g.client.InspectClauses(&accounts.BatchCallData{
		Caller: &caller,
		Clauses: []accounts.Clause{
			{
				To:    &g.addr,
				Data:  hexutil.Encode(clause.Data()),
				Value: (*math.HexOrDecimal256)(big.NewInt(0)),
			},
		},
	})
	if err != nil {
		return nil, err
	}
	return res[0], nil
}

func (g *GenericWrapper) CallInto(methodName string, results interface{}, args ...interface{}) error {
	method, ok := g.abi.Methods[methodName]
	if !ok {
		return errors.New("method not found: " + methodName)
	}
	res, err := g.Call(methodName, args...)
	if err != nil {
		return err
	}
	bytes, err := hexutil.Decode(res.Data)
	if err != nil {
		return err
	}

	return method.Outputs.Unpack(results, bytes)
}

type ReceiptWaiter func() (*transactions.Receipt, error)

func (g *GenericWrapper) Send(methodName string, args ...interface{}) *Sender {
	return g.SendWithVET(big.NewInt(0), methodName, args...)
}

func (g *GenericWrapper) SendWithVET(vet *big.Int, methodName string, args ...interface{}) *Sender {
	return newSender(g, vet, methodName, args...)
}

func (g *GenericWrapper) Clause(methodName string, args ...interface{}) (*tx.Clause, error) {
	return g.ClauseWithVET(big.NewInt(0), methodName, args...)
}

func (g *GenericWrapper) ClauseWithVET(vet *big.Int, methodName string, args ...interface{}) (*tx.Clause, error) {
	method, ok := g.abi.Methods[methodName]
	if !ok {
		return nil, errors.New("method not found: " + methodName)
	}
	data, err := method.Inputs.Pack(args...)
	if err != nil {
		return nil, fmt.Errorf("failed to pack method (%s): %w", methodName, err)
	}
	data = append(method.Id()[:], data...)
	clause := tx.NewClause(&g.addr).WithData(data)
	clause = clause.WithValue(vet)

	return clause, nil
}

func (g *GenericWrapper) FilterEvents(eventName string, fromBlock, toBlock uint32) ([]events.FilteredEvent, error) {
	event, ok := g.abi.Events[eventName]
	if !ok {
		return nil, errors.New("event not found: " + eventName)
	}
	id := thor.Bytes32(event.Id())
	from := uint64(fromBlock)
	to := uint64(toBlock)
	rnge := events.Range{
		From: &from,
		To:   &to,
		Unit: "block",
	}

	req := &events.EventFilter{
		Range: &rnge,
		CriteriaSet: []*events.EventCriteria{
			{
				Address: &g.addr,
				TopicSet: events.TopicSet{
					Topic0: &id,
				},
			},
		},
	}

	return g.client.FilterEvents(req)
}
