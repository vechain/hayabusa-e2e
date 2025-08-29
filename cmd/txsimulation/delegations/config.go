package delegations

import (
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/lifecycle"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/xnodes"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/bind"
)

type Config struct {
	lifecycle.Config
	Account      bind.Signer
	Position     *xnodes.Position
	PositionID   thor.Bytes32
	ValidationID thor.Address
}
