package utils

import (
	"errors"

	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thorclient/bind"
)

func DebugRevert(method *bind.MethodBuilder, receipt *api.Receipt) error {
	_, err := method.Call().
		AtRevision(receipt.Meta.BlockID.String()).
		Caller(&receipt.Meta.TxOrigin).
		Execute()
	if err == nil {
		return errors.New("transaction reverted but no error returned from call")
	}
	return err
}
