package messaging

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	CreateOrderCommand       = "order.create"
	CreateApplicationCommand = "application.create"
)

var ErrInvalidCommand = errors.New("invalid command")

type Command struct {
	CommandID      string          `json:"command_id"`
	CommandType    string          `json:"command_type"`
	CommandVersion int             `json:"command_version"`
	AggregateID    string          `json:"aggregate_id"`
	RequestedAt    time.Time       `json:"requested_at"`
	CorrelationID  string          `json:"correlation_id"`
	Payload        json.RawMessage `json:"payload"`
}

type Line struct {
	SKU       string `json:"sku"`
	Quantity  int    `json:"quantity"`
	UnitCents int64  `json:"unit_cents"`
}

type CreateOrderPayload struct {
	Lines []Line `json:"lines"`
}

// CreateApplicationPayload documents how the same envelope fits variant A.
// The mandatory executable walkthrough intentionally continues to use Order.
type CreateApplicationPayload struct {
	Author string `json:"author"`
	Topic  string `json:"topic"`
}

func DecodeCommand(data []byte) (Command, error) {
	return DecodeCommandType(data, CreateOrderCommand)
}

func DecodeCommandType(data []byte, expectedType string) (Command, error) {
	var command Command
	if err := json.Unmarshal(data, &command); err != nil {
		return Command{}, fmt.Errorf("%w: decode envelope: %v", ErrInvalidCommand, err)
	}
	if strings.TrimSpace(command.CommandID) == "" || strings.TrimSpace(command.AggregateID) == "" {
		return Command{}, fmt.Errorf("%w: command_id and aggregate_id are required", ErrInvalidCommand)
	}
	if command.CommandVersion != 1 {
		return Command{}, fmt.Errorf("%w: unsupported command_version %d", ErrInvalidCommand, command.CommandVersion)
	}
	if command.CommandType != expectedType {
		return Command{}, fmt.Errorf("%w: unsupported command_type %q", ErrInvalidCommand, command.CommandType)
	}
	if command.RequestedAt.IsZero() || len(command.Payload) == 0 {
		return Command{}, fmt.Errorf("%w: requested_at and payload are required", ErrInvalidCommand)
	}
	return command, nil
}
