package stack

import (
	"context"
	"errors"
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

func (s *Stack) NextValidator() (*hayabusa.NodePair, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// get any element from the map, delete it and return it
	if len(s.validatorAccs) == 0 {
		slog.Error("no validators available in the stack")
		return nil, errors.New("no validators available in the stack")
	}

	for addr, signer := range s.validatorAccs {
		delete(s.validatorAccs, addr)
		return signer, nil
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

func (s *Stack) SendTransaction(method *bind.MethodBuilder, signer bind.Signer) (*api.Receipt, error) {
	txCtx, cancel := context.WithTimeout(s.Context(), time.Minute)
	defer cancel()

	best, err := s.bestBlock()
	if err != nil {
		slog.Error("failed to get best block", "error", err)
		return nil, err
	}

	gas := uint64(1_000_000)
	options := &bind.TxOptions{
		MaxFeePerGas: big.NewInt(0).Mul((*big.Int)(best.BaseFeePerGas), big.NewInt(2)),
		Gas:          &gas,
	}

	receipt, _, err := method.Send().
		WithOptions(options).
		WithSigner(signer).
		SubmitAndConfirm(txCtx)
	if err != nil {
		return nil, err
	}
	if receipt.Reverted {
		return receipt, utils.DebugRevert(method, receipt)
	}
	return receipt, nil
}
