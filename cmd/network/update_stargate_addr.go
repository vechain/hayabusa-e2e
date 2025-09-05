package main

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func setStargateAddr(client *thorclient.Client, stargate thor.Address) error {
	params, err := builtin.NewParams(client)
	if err != nil {
		return fmt.Errorf("failed to create params: %w", err)
	}

	value := new(big.Int).SetBytes(hayabusa.ParamsStargateKey.Bytes())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	receipt, _, err := params.Set(hayabusa.ParamsStargateKey, value).Send().WithSigner(hayabusa.Executor).WithOptions(testutil.TxOptions()).SubmitAndConfirm(ctx)
	if err != nil {
		return fmt.Errorf("failed to set stargate address: %w", err)
	}
	if receipt.Reverted {
		return fmt.Errorf("transaction reverted: %s", receipt.Meta.TxID)
	}

	return nil
}
