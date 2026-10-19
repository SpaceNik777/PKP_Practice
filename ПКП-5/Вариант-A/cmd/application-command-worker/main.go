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

	"example.com/orderflow/internal/application"
	"example.com/orderflow/internal/application/memory"
	"example.com/orderflow/internal/messaging"
)

const topic = "application.commands.v1"

type creator struct {
	mu         sync.Mutex
	processed  map[string]struct{}
	repository *memory.Repository
}

func (c *creator) Create(ctx context.Context, commandID, applicationID string, payload messaging.CreateApplicationPayload) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, duplicate := c.processed[commandID]; duplicate {
		log.Printf("duplicate command_id=%s application_id=%s", commandID, applicationID)
		return nil
	}
	value, err := application.New(applicationID, payload.Author, payload.Topic)
	if err != nil {
		return err
	}
	if err := c.repository.Create(ctx, value); err != nil && !errors.Is(err, application.ErrConflict) {
		return err
	}
	c.processed[commandID] = struct{}{}
	log.Printf("created command_id=%s application_id=%s author=%s", commandID, applicationID, payload.Author)
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
		kgo.SeedBrokers("localhost:9092"),
		kgo.ConsumeTopics(topic),
		kgo.ConsumerGroup("application-command-v1"),
		kgo.DisableAutoCommit(),
		kgo.BlockRebalanceOnPoll(),
		kgo.ClientID("orderflow-application-command-worker"),
		kgo.OnPartitionsAssigned(func(_ context.Context, _ *kgo.Client, partitions map[string][]int32) {
			log.Printf("partitions assigned: %v", partitions)
		}),
		kgo.OnPartitionsRevoked(func(_ context.Context, _ *kgo.Client, partitions map[string][]int32) {
			log.Printf("partitions revoked: %v", partitions)
		}),
	)
	if err != nil {
		return fmt.Errorf("create Kafka consumer: %w", err)
	}
	defer client.Close()
	processor := &creator{processed: make(map[string]struct{}), repository: memory.NewRepository()}

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
			if len(record.Key) == 0 {
				processErr = errors.New("empty application aggregate key")
				return
			}
			if err := messaging.ProcessApplication(ctx, record.Value, processor); err != nil {
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
