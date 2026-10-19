package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/twmb/franz-go/pkg/kgo"

	"example.com/orderflow/internal/messaging"
)

const topic = "order.commands.v1"

type memoryCreator struct {
	mu      sync.Mutex
	created map[string]struct{}
}

func (c *memoryCreator) Create(_ context.Context, commandID, orderID string, payload messaging.CreateOrderPayload) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, duplicate := c.created[commandID]; duplicate {
		log.Printf("duplicate command_id=%s order_id=%s", commandID, orderID)
		return nil
	}
	c.created[commandID] = struct{}{}
	log.Printf("created command_id=%s order_id=%s lines=%d", commandID, orderID, len(payload.Lines))
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	client, err := kgo.NewClient(
		kgo.SeedBrokers("localhost:9092"), kgo.ConsumeTopics(topic), kgo.ConsumerGroup("order-command-v1"),
		kgo.DisableAutoCommit(), kgo.BlockRebalanceOnPoll(), kgo.ClientID("orderflow-order-command-worker"),
	)
	if err != nil {
		return fmt.Errorf("create Kafka consumer: %w", err)
	}
	defer client.Close()
	creator := &memoryCreator{created: make(map[string]struct{})}
	for {
		fetches := client.PollFetches(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			client.AllowRebalance()
			return fmt.Errorf("poll Kafka: %v", errs)
		}
		var processErr error
		fetches.EachRecord(func(record *kgo.Record) {
			if processErr != nil {
				return
			}
			if string(record.Key) == "" {
				processErr = errors.New("empty aggregate key")
				return
			}
			if err := messaging.Process(ctx, record.Value, creator); err != nil {
				processErr = fmt.Errorf("process partition=%d offset=%d: %w", record.Partition, record.Offset, err)
				return
			}
			if err := client.CommitRecords(ctx, record); err != nil {
				processErr = fmt.Errorf("commit partition=%d offset=%d: %w", record.Partition, record.Offset, err)
				return
			}
			log.Printf("committed key=%s partition=%d offset=%d", record.Key, record.Partition, record.Offset)
		})
		client.AllowRebalance()
		if processErr != nil {
			return processErr
		}
	}
}
