package messaging

import (
	"context"
	"encoding/json"
	"fmt"
)

type OrderCreator interface {
	Create(ctx context.Context, commandID, orderID string, payload CreateOrderPayload) error
}

type ApplicationCreator interface {
	Create(ctx context.Context, commandID, applicationID string, payload CreateApplicationPayload) error
}

func Process(ctx context.Context, data []byte, creator OrderCreator) error {
	command, err := DecodeCommand(data)
	if err != nil {
		return err
	}
	var payload CreateOrderPayload
	if err := json.Unmarshal(command.Payload, &payload); err != nil {
		return fmt.Errorf("%w: decode payload: %v", ErrInvalidCommand, err)
	}
	if len(payload.Lines) == 0 {
		return fmt.Errorf("%w: lines are required", ErrInvalidCommand)
	}
	if err := creator.Create(ctx, command.CommandID, command.AggregateID, payload); err != nil {
		return fmt.Errorf("create order %q: %w", command.AggregateID, err)
	}
	return nil
}

func ProcessApplication(ctx context.Context, data []byte, creator ApplicationCreator) error {
	command, err := DecodeCommandType(data, CreateApplicationCommand)
	if err != nil {
		return err
	}
	var payload CreateApplicationPayload
	if err := json.Unmarshal(command.Payload, &payload); err != nil {
		return fmt.Errorf("%w: decode payload: %v", ErrInvalidCommand, err)
	}
	if payload.Author == "" || payload.Topic == "" {
		return fmt.Errorf("%w: author and topic are required", ErrInvalidCommand)
	}
	if err := creator.Create(ctx, command.CommandID, command.AggregateID, payload); err != nil {
		return fmt.Errorf("create application %q: %w", command.AggregateID, err)
	}
	return nil
}
