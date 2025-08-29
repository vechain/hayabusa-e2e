package validators

import (
	"log/slog"

	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	"github.com/vechain/thor/v2/api"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

// EventHandler - handles event filtering and lookups
type EventHandler struct {
	stack       *stack.Stack
	validatorID thor.Address
}

func NewEventHandler(stack *stack.Stack, validatorID thor.Address) *EventHandler {
	return &EventHandler{
		stack:       stack,
		validatorID: validatorID,
	}
}

func (eh *EventHandler) FindQueuedReceipt() (*api.Receipt, error) {
	offset := uint64(0)
	limit := uint64(1000)

	for {
		events, err := eh.stack.Staker().FilterValidatorQueued(nil, &api.Options{Offset: offset, Limit: &limit}, "asc")
		if err != nil {
			return nil, err
		}

		for _, event := range events {
			if event.Node == eh.validatorID {
				return eh.stack.Client().TransactionReceipt(&event.Log.Meta.TxID)
			}
		}

		if len(events) < int(limit) {
			break
		}
		offset += limit
	}

	return nil, nil
}

func (eh *EventHandler) FindExitReceipt() (*api.Receipt, error) {
	offset := uint64(0)
	limit := uint64(1000)

	for {
		events, err := eh.stack.Staker().FilterValidationSignaledExit(nil, &api.Options{Offset: offset, Limit: &limit}, "asc")
		if err != nil {
			slog.Error("failed to filter validator exited events", "error", err, "account", eh.validatorID)
			return nil, err
		}
		for _, event := range events {
			if event.Node == eh.validatorID {
				receipt, err := eh.stack.Client().TransactionReceipt(&event.Log.Meta.TxID)
				if err != nil {
					slog.Error("failed to get receipt for validator exited event", "error", err, "account", eh.validatorID)
					return nil, err
				}
				return receipt, nil
			}
		}
		if len(events) < int(limit) {
			break
		}
		offset += limit
	}

	return nil, nil
}

func (eh *EventHandler) CheckValidatorStatus() (*builtin.Validation, error) {
	return eh.stack.Staker().GetValidation(eh.validatorID)
}
