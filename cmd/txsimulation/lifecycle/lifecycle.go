package lifecycle

import (
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/bind"
	"sync"
)

// Delay represents a delay in epochs and blocks before performing the next action
type Delay struct {
	Epochs uint32
	Blocks uint32
}

// Type represents the type of lifecycle, either Validator or Delegator
type Type int

func (t Type) String() string {
	switch t {
	case TypeValidator:
		return "validator"
	case TypeDelegator:
		return "delegator"
	default:
		return "invalid type"
	}
}

const (
	TypeValidator = Type(iota)
	TypeDelegator
)

// Status represents the current status of the lifecycle
type Status int

func (s Status) String() string {
	switch s {
	case StatusUnknown:
		return "unknown"
	case StatusPending:
		return "pending"
	case StatusQueued:
		return "queued"
	case StatusActive:
		return "active"
	case StatusExitSignalled:
		return "exit signalled"
	case StatusWithdrawn:
		return "withdrawn"
	case StatusError:
		return "error"
	default:
		return "invalid status"
	}
}

const (
	StatusUnknown Status = iota
	StatusPending
	StatusQueued
	StatusActive
	StatusExitSignalled
	StatusWithdrawn
	StatusError
)

type Info struct {
	ValidationID    thor.Bytes32
	Type            Type
	QueuedReceipt   *api.Receipt
	ActivatedBlock  uint32
	ExitReceipt     *api.Receipt
	WithdrawReceipt *api.Receipt
	ID              string
	Status          Status
}

type Lifecycle interface {
	Process(engine *Engine, block uint32) error
	Status() Status
	Type() Type
	Info() *Info
	ID() string
}

type LifecycleV2 interface {
	Queue(engine *Engine, block uint32) error
	SetActivatedBlock(engine *Engine, block uint32) error
	SignalExit(engine *Engine) error
	Withdraw(engine *Engine) error
	Status() Status
	Type() Type
	Info() *Info
	ID() string
}

type BaseLifecycle struct {
	Account        bind.Signer
	QueueDelay     Delay
	StartBlock     uint32
	StakingPeriods uint32
	WithdrawDelay  Delay

	status          Status
	queuedReceipt   *api.Receipt // the receipt of the queued transaction
	activatedBlock  uint32       // the block at which this lifecycle was activated
	exitReceipt     *api.Receipt // the receipt of the exit transaction
	withdrawReceipt *api.Receipt // the receipt of the withdraw transaction
	id              thor.Bytes32

	mu sync.Mutex
}
