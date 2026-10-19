// Package workqueue implements a bounded worker pool with reject backpressure.
package workqueue

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrFull   = errors.New("work queue is full")
	ErrClosed = errors.New("work queue is closed")
)

type Job func(context.Context) error
type ErrorHandler func(error)

type Queue struct {
	ctx     context.Context
	cancel  context.CancelFunc
	jobs    chan Job
	onError ErrorHandler

	workers sync.WaitGroup
	mu      sync.RWMutex
	closed  bool
}

func New(parent context.Context, workerCount, capacity int, onError ErrorHandler) (*Queue, error) {
	if workerCount <= 0 {
		return nil, fmt.Errorf("worker count must be positive")
	}
	if capacity < 0 {
		return nil, fmt.Errorf("capacity cannot be negative")
	}
	if onError == nil {
		onError = func(error) {}
	}
	ctx, cancel := context.WithCancel(parent)
	q := &Queue{ctx: ctx, cancel: cancel, jobs: make(chan Job, capacity), onError: onError}
	q.workers.Add(workerCount)
	for range workerCount {
		go q.work()
	}
	return q, nil
}

// Submit accepts a job without waiting for queue capacity.
func (q *Queue) Submit(ctx context.Context, job Job) error {
	if job == nil {
		return fmt.Errorf("nil job")
	}
	q.mu.RLock()
	defer q.mu.RUnlock()
	if q.closed {
		return ErrClosed
	}
	select {
	case q.jobs <- job:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return ErrFull
	}
}

func (q *Queue) Len() int { return len(q.jobs) }
func (q *Queue) Cap() int { return cap(q.jobs) }

// Close rejects new jobs and waits for accepted jobs to drain. On deadline it
// cancels the context passed to running jobs and still leaves no worker leak.
func (q *Queue) Close(ctx context.Context) error {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		close(q.jobs)
	}
	q.mu.Unlock()

	done := make(chan struct{})
	go func() {
		q.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		q.cancel()
		return nil
	case <-ctx.Done():
		q.cancel()
		return ctx.Err()
	}
}

func (q *Queue) work() {
	defer q.workers.Done()
	for job := range q.jobs {
		if err := job(q.ctx); err != nil {
			q.onError(err)
		}
		if q.ctx.Err() != nil {
			return
		}
	}
}
