package httpclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"example.com/orderflow/internal/limits"
	"example.com/orderflow/internal/platform/resilience"
)

func testRetry() resilience.Retry {
	return resilience.Retry{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		Sleep:       func(context.Context, time.Duration) error { return nil },
		Jitter:      func(time.Duration) time.Duration { return 0 },
	}
}

func newTestClient(t *testing.T, server *httptest.Server, retry resilience.Retry, concurrent int) *Client {
	t.Helper()
	client, err := New(server.URL, server.Client(), retry, resilience.NewBulkhead(concurrent))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestRetriesTwo503ThenSucceedsWithStableIdempotencyKey(t *testing.T) {
	var calls int
	var keys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		if calls <= 2 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"allowed":true}`))
	}))
	defer server.Close()

	client := newTestClient(t, server, testRetry(), 20)
	if err := client.Check(context.Background(), limits.Check{OperationID: "submit:a-1", Subject: "ivan", Limit: "active-applications-per-author"}); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
	for i, key := range keys {
		if key != "limit:submit:a-1" {
			t.Fatalf("key[%d] = %q", i, key)
		}
	}
}

func TestDeadlineStopsRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	}))
	defer server.Close()
	client := newTestClient(t, server, testRetry(), 20)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- client.Check(ctx, limits.Check{OperationID: "deadline", Subject: "ivan", Limit: "active"})
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		close(release)
	case <-time.After(time.Second):
		t.Fatal("request did not stop after context cancellation")
	}
}

func TestBulkheadFailsFastAtTwentyConcurrentCalls(t *testing.T) {
	var active, maximum atomic.Int32
	entered := make(chan struct{}, 20)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); current > old && !maximum.CompareAndSwap(old, current); old = maximum.Load() {
		}
		entered <- struct{}{}
		<-release
		w.Write([]byte(`{"allowed":true}`))
	}))
	defer server.Close()
	client := newTestClient(t, server, testRetry(), 20)

	var wg sync.WaitGroup
	errs := make(chan error, 21)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- client.Check(context.Background(), limits.Check{OperationID: string(rune('a' + i)), Subject: "ivan", Limit: "active"})
		}(i)
	}
	for i := 0; i < 20; i++ {
		<-entered
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		errs <- client.Check(context.Background(), limits.Check{OperationID: "overflow", Subject: "ivan", Limit: "active"})
	}()
	for {
		select {
		case err := <-errs:
			if !errors.Is(err, limits.ErrBusy) {
				t.Fatalf("early error = %v, want ErrBusy", err)
			}
			close(release)
			wg.Wait()
			if maximum.Load() != 20 {
				t.Fatalf("maximum = %d, want 20", maximum.Load())
			}
			return
		case <-time.After(time.Second):
			t.Fatal("overflow call did not fail fast")
		}
	}
}

func TestBusinessDenialIsNotRetried(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"allowed":false,"reason":"author already has 5 active applications"}`))
	}))
	defer server.Close()
	client := newTestClient(t, server, testRetry(), 20)
	err := client.Check(context.Background(), limits.Check{OperationID: "denied", Subject: "ivan", Limit: "active"})
	if !errors.Is(err, limits.ErrDenied) {
		t.Fatalf("error = %v, want ErrDenied", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}
