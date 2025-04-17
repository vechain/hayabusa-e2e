package contracts

import (
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/vechain/draupnir/common"
	"github.com/vechain/thor/v2/api/accounts"
	"github.com/vechain/thor/v2/thor"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/vechain/draupnir/datagen"
	"github.com/vechain/thor/v2/api/transactions"
	"github.com/vechain/thor/v2/tx"
)

type Sender struct {
	contract   *GenericWrapper
	vet        *big.Int
	methodName string
	args       []interface{}
	mu         sync.Mutex
	sent       atomic.Bool
	tx         atomic.Pointer[tx.Transaction]
}

func newSender(contract *GenericWrapper, vet *big.Int, methodName string, args ...interface{}) *Sender {
	return &Sender{
		contract:   contract,
		vet:        vet,
		methodName: methodName,
		args:       args,
	}
}

func (s *Sender) Simulate() (*accounts.CallResult, error) {
	clause, err := s.contract.ClauseWithVET(s.vet, s.methodName, s.args...)
	if err != nil {
		return nil, errors.Join(err, s.errorContext())
	}
	caller := thor.Address(crypto.PubkeyToAddress(s.contract.key.PublicKey))
	inspectRequest := accounts.BatchCallData{
		Clauses: []accounts.Clause{
			{
				To:    clause.To(),
				Data:  hexutil.Encode(clause.Data()),
				Value: (*math.HexOrDecimal256)(clause.Value()),
			},
		},
		Caller: &caller,
	}
	simulation, err := s.contract.client.InspectClauses(&inspectRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to simulate transaction: %w", err)
	}
	if len(simulation) == 0 {
		return nil, errors.New("no simulation result")
	}
	if simulation[0].VMError != "" {
		slog.Warn("⚠️⚠️ SIMULATION FAILED ⚠️⚠️", "error", simulation[0].VMError, "method", s.methodName)
	}
	if simulation[0].Reverted {
		slog.Warn("⚠️⚠️ SIMULATION REVERTED ⚠️⚠️", "method", s.methodName)
	}

	return simulation[0], nil
}

// Build and sign the transaction without sending it to the network.
func (s *Sender) Build() (*tx.Transaction, error) {
	clause, err := s.contract.ClauseWithVET(s.vet, s.methodName, s.args...)
	if err != nil {
		return nil, errors.Join(err, s.errorContext())
	}
	if _, err := s.Simulate(); err != nil {
		return nil, errors.Join(err, s.errorContext())
	}

	best, err := s.contract.client.Block("best")
	if err != nil {
		return nil, errors.Join(err, s.errorContext())
	}
	chainTag, err := s.contract.client.ChainTag()
	if err != nil {
		return nil, errors.Join(err, s.errorContext())
	}

	//txType := tx.TypeLegacy
	//if best.BaseFeePerGas != nil {
	//	txType = tx.TypeDynamicFee
	//}

	builder := new(tx.Builder).
		Clause(clause).
		Gas(10_000_000).
		ChainTag(chainTag).Expiration(100_000_000).
		BlockRef(tx.NewBlockRef(best.Number)).
		Nonce(datagen.RandUInt64())

	// TODO: Uncomment when dynamic fee is supported
	//switch txType {
	//case tx.TypeLegacy:
	builder.GasPriceCoef(255)
	//case tx.TypeDynamicFee:
	//	priority, err := s.contract.client.FeesPriority()
	//	if err != nil {
	//		return nil, errors.Join(err, s.errorContext())
	//	}
	//	fees, err := s.contract.client.FeesHistory(1, "next", []float64{})
	//	if err != nil {
	//		return nil, errors.Join(err, s.errorContext())
	//	}
	//	builder.MaxPriorityFeePerGas(priority.MaxPriorityFeePerGas.ToInt())
	//	builder.MaxFeePerGas(fees.BaseFeePerGas[0].ToInt())
	//}

	transaction := builder.Build()
	transaction, err = tx.Sign(transaction, s.contract.key)
	if err != nil {
		return nil, errors.Join(err, s.errorContext())
	}

	return transaction, nil
}

// Send sends the transaction to the network. Does not wait for the receipt.
func (s *Sender) Send() (*tx.Transaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sent.Load() {
		return s.tx.Load(), nil
	}

	transaction, err := s.Build()
	if err != nil {
		return nil, errors.Join(err, s.errorContext())
	}

	if _, err = s.contract.client.SendTransaction(transaction); err != nil {
		return nil, errors.Join(err, s.errorContext())
	}

	s.sent.Store(true)
	s.tx.Store(transaction)

	return transaction, nil
}

// Receipt sends the transaction if it hasn't been sent already and polls for the receipt.
func (s *Sender) Receipt(allowRevert bool) (*transactions.Receipt, *tx.Transaction, error) {
	transaction, err := s.Send()
	if err != nil {
		return nil, nil, errors.Join(err, s.errorContext())
	}

	id := transaction.ID()

	var receipt *transactions.Receipt
	err = common.Retry(func() error {
		receipt, err = s.contract.client.TransactionReceipt(&id)
		if err == nil && receipt != nil {
			if receipt.Reverted && !allowRevert {
				return errors.Join(errors.New("transaction reverted"), s.errorContext())
			}
		}
		return err
	}, 100*time.Millisecond, 30*time.Second)

	return receipt, transaction, err
}

func (s *Sender) errorContext() error {
	method, ok := s.contract.abi.Methods[s.methodName]
	if !ok {
		return errors.New("method not found: " + s.methodName)
	}

	errBuilder := strings.Builder{}
	errBuilder.WriteString("transaction failed")
	errBuilder.WriteString("\nmethod=")
	errBuilder.WriteString(s.methodName)
	errBuilder.WriteString("\nsender=")
	errBuilder.WriteString(crypto.PubkeyToAddress(s.contract.key.PublicKey).String())
	errBuilder.WriteString("\nvet=")
	errBuilder.WriteString(s.vet.String())
	for i, arg := range s.args {
		errBuilder.WriteString(fmt.Sprintf("\n%s=", method.Inputs[i].Name))
		errBuilder.WriteString(fmt.Sprintf("%v", arg))
	}

	return errors.New(errBuilder.String())
}

type Senders struct {
	senders []*Sender
	mu      sync.Mutex
}

func (s *Senders) Add(sender *Sender) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.senders == nil {
		s.senders = make([]*Sender, 0)
	}
	s.senders = append(s.senders, sender)
}

// Send all transactions in parallel and returns the transactions and receipts.
// allowRevert will return an error if any transaction reverted.
func (s *Senders) Send(allowRevert bool) ([]*tx.Transaction, []*transactions.Receipt, error) {
	txs := make([]*tx.Transaction, len(s.senders))
	receipts := make([]*transactions.Receipt, len(s.senders))
	errs := make([]error, len(s.senders))

	var wg sync.WaitGroup
	for i, sender := range s.senders {
		wg.Add(1)
		go func(i int, sender *Sender) {
			defer wg.Done()
			tx, err := sender.Send()
			if err != nil {
				errs[i] = err
				return
			}
			txs[i] = tx
			receipt, _, err := sender.Receipt(allowRevert)
			if err != nil {
				errs[i] = err
				return
			}
			receipts[i] = receipt
		}(i, sender)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return txs, receipts, errors.Join(errs...)
		}
	}

	return txs, receipts, nil
}
