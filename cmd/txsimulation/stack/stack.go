package stack

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/utils"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/testutil"
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
	validatorAccs map[thor.Address]bind.Signer
	stargateAcc   bind.Signer
	mu            sync.Mutex // protects the stack from concurrent access
}

func NewStack(
	ctx context.Context,
	staker *builtin.Staker,
	config *hayabusa.Config,
	validatorAccs map[thor.Address]bind.Signer,
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

func (s *Stack) NextValidator() (bind.Signer, error) {
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

func (s *Stack) SendTransaction(method *bind.MethodBuilder, signer bind.Signer) (*api.Receipt, error) {
	txCtx, cancel := context.WithTimeout(s.Context(), 2*time.Minute)
	defer cancel()
	receipt, _, err := method.Send().
		WithOptions(testutil.TxOptions()).
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
