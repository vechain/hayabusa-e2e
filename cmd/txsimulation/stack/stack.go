package stack

import (
	"context"
	"log/slog"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/utils"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
	"github.com/vechain/thor/v2/tx"
)

type Stack struct {
	ctx     context.Context
	staker  *builtin.Staker
	config  *hayabusa.Config
	mu      sync.Mutex // protects the stack from concurrent access
	best    atomic.Pointer[api.JSONCollapsedBlock]
	sentTxs map[uint32]int
}

func NewStack(
	ctx context.Context,
	staker *builtin.Staker,
	config *hayabusa.Config,
) *Stack {
	s := &Stack{
		ctx:     ctx,
		staker:  staker,
		config:  config,
		sentTxs: make(map[uint32]int),
	}
	go s.pollBlocks()

	return s
}

func (s *Stack) pollBlocks() {
	ticker := time.NewTicker(100 * time.Millisecond)
	for {
		select {
		case <-s.ctx.Done():
			slog.Info("stack: tx polling exiting due to context cancellation")
			return
		case <-ticker.C:
			best, err := s.Client().Block("best")
			if err != nil {
				slog.Error("stack: failed to fetch best block", "error", err)
				continue
			}
			prev := s.best.Load()
			if prev != nil && best.ID == prev.ID {
				continue
			}

			s.mu.Lock()
			s.best.Store(best)
			count, ok := s.sentTxs[best.Number]
			if ok && count > 0 {
				slog.Info("🧱 stack: transactions mined", "block", best.Number, "sent-txs", count, "actual-txs", len(best.Transactions))
				delete(s.sentTxs, best.Number)
			}
			s.mu.Unlock()
		}
	}
}

func (s *Stack) Context() context.Context {
	return s.ctx
}

func (s *Stack) Client() *thorclient.Client {
	return s.staker.Raw().Client()
}

func (s *Stack) Staker() *builtin.Staker {
	return s.staker
}

func (s *Stack) Config() *hayabusa.Config {
	return s.config
}

func (s *Stack) RandomStakingPeriod() uint32 {
	switch utils.RandomInt(0, 3) {
	case 0:
		return s.config.MinStakingPeriod
	case 1:
		return s.config.MidStakingPeriod
	default:
		return s.config.HighStakingPeriod
	}
}

func (s *Stack) SendTransaction(method *bind.MethodBuilder, signer bind.Signer) (*tx.Transaction, error) {
	sender, err := s.makeTx(method, signer)
	if err != nil {
		slog.Error("failed to create transaction", "error", err)
		return nil, err
	}
	return sender.Submit()
}

func (s *Stack) SendTransactionAndWait(
	method *bind.MethodBuilder,
	signer bind.Signer,
) (*api.Receipt, error) {
	txCtx, cancel := context.WithTimeout(s.ctx, time.Minute)
	defer cancel()

	sender, err := s.makeTx(method, signer)
	if err != nil {
		slog.Error("failed to create transaction", "error", err)
		return nil, err
	}

	receipt, _, err := sender.SubmitAndConfirm(txCtx)
	if err != nil {
		slog.Error("failed to send transaction", "error", err)
		return nil, err
	}
	if receipt.Reverted {
		return nil, utils.DebugRevert(method, receipt)
	}
	return receipt, nil
}

func (s *Stack) DefaultExpiration() *uint32 {
	expiration := uint32(6)
	return &expiration
}

func (s *Stack) makeTx(method *bind.MethodBuilder, signer bind.Signer) (*bind.SendBuilder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	best := s.best.Load()
	if best == nil {
		var err error
		best, err = s.Client().Block("best")
		if err != nil {
			slog.Error("stack: failed to fetch best block", "error", err)
			return nil, err
		}
		s.best.Store(best)
	}

	if best.Number > 3 {
		s.sentTxs[best.Number+1]++ // +1 should be mined at the next block, not best
	}

	gas := uint64(1_000_000)
	baseFee := (*big.Int)(best.BaseFeePerGas)
	if baseFee == nil {
		baseFee = big.NewInt(thor.InitialBaseFee)
	}
	options := &bind.TxOptions{
		MaxFeePerGas: big.NewInt(0).Mul(baseFee, big.NewInt(2)),
		Gas:          &gas,
		Expiration:   s.DefaultExpiration(),
	}

	return method.Send().WithOptions(options).WithSigner(signer), nil
}
