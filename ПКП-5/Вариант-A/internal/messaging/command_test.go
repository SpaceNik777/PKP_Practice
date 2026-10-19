package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type creatorStub struct {
	err       error
	commandID string
	orderID   string
	payload   CreateOrderPayload
}

type applicationCreatorStub struct {
	err           error
	commandID     string
	applicationID string
	payload       CreateApplicationPayload
}

func (s *applicationCreatorStub) Create(_ context.Context, commandID, applicationID string, payload CreateApplicationPayload) error {
	s.commandID, s.applicationID, s.payload = commandID, applicationID, payload
	return s.err
}

func (s *creatorStub) Create(_ context.Context, commandID, orderID string, payload CreateOrderPayload) error {
	s.commandID, s.orderID, s.payload = commandID, orderID, payload
	return s.err
}

func validCommand(t *testing.T) []byte {
	t.Helper()
	payload, err := json.Marshal(CreateOrderPayload{Lines: []Line{{SKU: "BOOK-GO", Quantity: 2, UnitCents: 2500}}})
	if err != nil {
		t.Fatal(err)
	}
	value, err := json.Marshal(Command{
		CommandID: "cmd-1", CommandType: CreateOrderCommand, CommandVersion: 1,
		AggregateID: "ord-1", RequestedAt: time.Now().UTC(), CorrelationID: "checkout:ord-1", Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestProcessPassesDecodedOrderToCreator(t *testing.T) {
	creator := &creatorStub{}
	if err := Process(context.Background(), validCommand(t), creator); err != nil {
		t.Fatal(err)
	}
	if creator.commandID != "cmd-1" || creator.orderID != "ord-1" || len(creator.payload.Lines) != 1 {
		t.Fatalf("unexpected call: %+v", creator)
	}
}

func TestProcessRejectsUnsupportedVersionWithoutCallingCreator(t *testing.T) {
	var command Command
	if err := json.Unmarshal(validCommand(t), &command); err != nil {
		t.Fatal(err)
	}
	command.CommandVersion = 2
	value, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	creator := &creatorStub{}
	err = Process(context.Background(), value, creator)
	if !errors.Is(err, ErrInvalidCommand) || creator.orderID != "" {
		t.Fatalf("expected validation error before creator, got %v", err)
	}
}

func TestProcessReturnsCreatorError(t *testing.T) {
	want := errors.New("storage unavailable")
	err := Process(context.Background(), validCommand(t), &creatorStub{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("expected wrapped creator error, got %v", err)
	}
}

func TestProcessApplicationPassesDecodedApplicationToCreator(t *testing.T) {
	payload, err := json.Marshal(CreateApplicationPayload{Author: "Ivan", Topic: "Budget"})
	if err != nil {
		t.Fatal(err)
	}
	value, err := json.Marshal(Command{
		CommandID: "cmd-app-1", CommandType: CreateApplicationCommand, CommandVersion: 1,
		AggregateID: "app-1", RequestedAt: time.Now().UTC(), CorrelationID: "request:app-1", Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	creator := &applicationCreatorStub{}
	if err := ProcessApplication(context.Background(), value, creator); err != nil {
		t.Fatal(err)
	}
	if creator.commandID != "cmd-app-1" || creator.applicationID != "app-1" || creator.payload.Author != "Ivan" || creator.payload.Topic != "Budget" {
		t.Fatalf("unexpected call: %+v", creator)
	}
}

func TestProcessApplicationRejectsOrderCommand(t *testing.T) {
	creator := &applicationCreatorStub{}
	err := ProcessApplication(context.Background(), validCommand(t), creator)
	if !errors.Is(err, ErrInvalidCommand) || creator.applicationID != "" {
		t.Fatalf("expected command type error before creator, got %v", err)
	}
}

func TestProcessApplicationRejectsEmptyPayloadField(t *testing.T) {
	payload, err := json.Marshal(CreateApplicationPayload{Author: "Ivan"})
	if err != nil {
		t.Fatal(err)
	}
	value, err := json.Marshal(Command{
		CommandID: "cmd-app-2", CommandType: CreateApplicationCommand, CommandVersion: 1,
		AggregateID: "app-2", RequestedAt: time.Now().UTC(), Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	creator := &applicationCreatorStub{}
	err = ProcessApplication(context.Background(), value, creator)
	if !errors.Is(err, ErrInvalidCommand) || creator.applicationID != "" {
		t.Fatalf("expected payload error before creator, got %v", err)
	}
}
