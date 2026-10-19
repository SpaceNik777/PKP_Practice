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

const topic = "application.commands.v1"

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: application-command-producer APPLICATION_ID AUTHOR TOPIC")
		os.Exit(2)
	}
	if err := run(context.Background(), os.Args[1], os.Args[2], os.Args[3]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, applicationID, author, applicationTopic string) error {
	client, err := kgo.NewClient(
		kgo.SeedBrokers("localhost:9092"),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.ClientID("orderflow-application-command-producer"),
	)
	if err != nil {
		return fmt.Errorf("create Kafka producer: %w", err)
	}
	defer client.Close()

	payload, err := json.Marshal(messaging.CreateApplicationPayload{Author: author, Topic: applicationTopic})
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}
	commandID, err := newID()
	if err != nil {
		return err
	}
	value, err := json.Marshal(messaging.Command{
		CommandID:      commandID,
		CommandType:    messaging.CreateApplicationCommand,
		CommandVersion: 1,
		AggregateID:    applicationID,
		RequestedAt:    time.Now().UTC(),
		CorrelationID:  "application-request:" + applicationID,
		Payload:        payload,
	})
	if err != nil {
		return fmt.Errorf("encode command: %w", err)
	}
	record := &kgo.Record{
		Topic: topic,
		Key:   []byte(applicationID),
		Value: value,
		Headers: []kgo.RecordHeader{
			{Key: "content-type", Value: []byte("application/json")},
			{Key: "command-type", Value: []byte(messaging.CreateApplicationCommand)},
		},
	}
	result := client.ProduceSync(ctx, record)[0]
	if result.Err != nil {
		return fmt.Errorf("produce command: %w", result.Err)
	}
	fmt.Printf("command_id=%s application_id=%s partition=%d offset=%d\n", commandID, applicationID, result.Record.Partition, result.Record.Offset)
	return nil
}

func newID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate command id: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}
