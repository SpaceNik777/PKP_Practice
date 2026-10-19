package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"example.com/orderflow/internal/messaging"
)

const topic = "order.commands.v1"

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: kafka-producer ORDER_ID")
		os.Exit(2)
	}
	if err := run(context.Background(), os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, orderID string) error {
	client, err := kgo.NewClient(kgo.SeedBrokers("localhost:9092"), kgo.RequiredAcks(kgo.AllISRAcks()), kgo.ClientID("orderflow-lab-producer"))
	if err != nil {
		return fmt.Errorf("create Kafka producer: %w", err)
	}
	defer client.Close()

	payload, err := json.Marshal(messaging.CreateOrderPayload{Lines: []messaging.Line{{SKU: "BOOK-GO", Quantity: 2, UnitCents: 2500}}})
	if err != nil {
		return err
	}
	commandID, err := newID()
	if err != nil {
		return err
	}
	value, err := json.Marshal(messaging.Command{
		CommandID: commandID, CommandType: messaging.CreateOrderCommand, CommandVersion: 1,
		AggregateID: orderID, RequestedAt: time.Now().UTC(), CorrelationID: "checkout:" + orderID, Payload: payload,
	})
	if err != nil {
		return err
	}
	record := &kgo.Record{Topic: topic, Key: []byte(orderID), Value: value, Headers: []kgo.RecordHeader{
		{Key: "content-type", Value: []byte("application/json")}, {Key: "command-type", Value: []byte(messaging.CreateOrderCommand)},
	}}
	results := client.ProduceSync(ctx, record)
	if results.FirstErr() != nil {
		return fmt.Errorf("produce command: %w", results.FirstErr())
	}
	produced := results[0].Record
	fmt.Printf("command_id=%s order_id=%s partition=%d offset=%d\n", commandID, orderID, produced.Partition, produced.Offset)
	return nil
}

func newID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate command id: %w", err)
	}
	return hex.EncodeToString(bytes[:]), nil
}
