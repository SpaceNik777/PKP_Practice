package workqueue

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
)

func TestQueueRejectsWhenCapacityIsExhausted(t *testing.T) {
	q, err := New(context.Background(), 1, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	blocking := func(context.Context) error { close(started); <-release; return nil }
	if err := q.Submit(context.Background(), blocking); err != nil {
		t.Fatal(err)
	}
	<-started
	for range 2 {
		if err := q.Submit(context.Background(), func(context.Context) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.Submit(context.Background(), func(context.Context) error { return nil }); !errors.Is(err, ErrFull) {
		t.Fatalf("Submit() error = %v, want ErrFull", err)
	}
	close(release)
	if err := q.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCloseDrainsAcceptedJobsAndRejectsNewOnes(t *testing.T) {
	q, _ := New(context.Background(), 2, 4, nil)
	var completed atomic.Int32
	for range 4 {
		if err := q.Submit(context.Background(), func(context.Context) error { completed.Add(1); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := completed.Load(); got != 4 {
		t.Fatalf("completed = %d, want 4", got)
	}
	if err := q.Submit(context.Background(), func(context.Context) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("Submit after Close = %v, want ErrClosed", err)
	}
}

func TestCloseDeadlineCancelsRunningJob(t *testing.T) {
	q, _ := New(context.Background(), 1, 1, nil)
	started := make(chan struct{})
	finished := make(chan struct{})
	if err := q.Submit(context.Background(), func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(finished)
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := q.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close() = %v", err)
	}
	<-finished
}

func BenchmarkQueueContention(b *testing.B) {
	for _, workers := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			old := runtime.GOMAXPROCS(workers)
			defer runtime.GOMAXPROCS(old)
			q, err := New(context.Background(), workers, b.N, nil)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				seed := uint64(i + 1)
				if err := q.Submit(context.Background(), func(context.Context) error {
					// CPU-bound deterministic document digest simulation.
					x := seed
					for range 2048 {
						x = x*6364136223846793005 + 1442695040888963407
					}
					if x == 0 {
						b.Fatal("unreachable digest")
					}
					return nil
				}); err != nil {
					b.Fatal(err)
				}
			}
			if err := q.Close(context.Background()); err != nil {
				b.Fatal(err)
			}
		})
	}
}
