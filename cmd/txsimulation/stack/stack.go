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
	ctx           context.Context
	staker        *builtin.Staker
	config        *hayabusa.Config
	validatorAccs map[thor.Address]*hayabusa.NodePair
	stargateAcc   bind.Signer
	mu            sync.Mutex // protects the stack from concurrent access
	best          atomic.Pointer[api.JSONCollapsedBlock]
}

func NewStack(
	ctx context.Context,
	staker *builtin.Staker,
	config *hayabusa.Config,
	validatorAccs map[thor.Address]*hayabusa.NodePair,
	stargateAcc bind.Signer,
) *Stack {
	s := &Stack{
		ctx:           ctx,
		staker:        staker,
		config:        config,
		validatorAccs: validatorAccs,
		stargateAcc:   stargateAcc,
	}
	return s
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

func (s *Stack) Stargate() bind.Signer {
	return s.stargateAcc
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

func (s *Stack) NextValidator() (*hayabusa.NodePair, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// get any element from the map, delete it and return it
	if len(s.validatorAccs) == 0 {
		slog.Error("no validators available in the stack")
		return nil, false
	}

	for addr, signer := range s.validatorAccs {
		delete(s.validatorAccs, addr)
		return signer, true
	}

	panic("stack: no validators available")
}

func (s *Stack) bestBlock() (*api.JSONCollapsedBlock, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var best *api.JSONCollapsedBlock
	cached := s.best.Load()
	if cached == nil || time.Since(time.Unix(int64(cached.Timestamp), 0)) > 6*time.Second {
		block, err := s.Client().Block("best")
		if err != nil {
			return nil, err
		}
		best = block
		s.best.Store(best)
	} else {
		best = cached
	}
	return best, nil
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

func (s *Stack) makeTx(method *bind.MethodBuilder, signer bind.Signer) (*bind.SendBuilder, error) {
	best, err := s.bestBlock()
	if err != nil {
		slog.Error("failed to get best block", "error", err)
		return nil, err
	}

	gas := uint64(1_000_000)
	baseFee := (*big.Int)(best.BaseFeePerGas)
	if baseFee == nil {
		baseFee = big.NewInt(thor.InitialBaseFee)
	}
	options := &bind.TxOptions{
		MaxFeePerGas: big.NewInt(0).Mul(baseFee, big.NewInt(2)),
		Gas:          &gas,
	}

	return method.Send().WithOptions(options).WithSigner(signer), nil
}
