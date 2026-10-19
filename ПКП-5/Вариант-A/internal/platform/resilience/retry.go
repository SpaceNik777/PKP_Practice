package resilience

import (
	"context"
	"math/rand/v2"
	"time"
)

type Retry struct {
	MaxAttempts int
	BaseDelay   time.Duration
	Sleep       func(context.Context, time.Duration) error
	Jitter      func(time.Duration) time.Duration
}

func NewRetry(maxAttempts int, baseDelay time.Duration) Retry {
	return Retry{MaxAttempts: maxAttempts, BaseDelay: baseDelay, Sleep: sleep, Jitter: func(delay time.Duration) time.Duration {
		return time.Duration(rand.Int64N(int64(delay) + 1))
	}}
}

func (r Retry) Do(ctx context.Context, retryable func(error) bool, operation func(context.Context) error) error {
	attempts := r.MaxAttempts
	if attempts < 1 {
		attempts = 1
	}
	sleepFn := r.Sleep
	if sleepFn == nil {
		sleepFn = sleep
	}
	jitterFn := r.Jitter
	if jitterFn == nil {
		jitterFn = func(time.Duration) time.Duration { return 0 }
	}
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		if err = operation(ctx); err == nil || !retryable(err) || attempt == attempts-1 {
			return err
		}
		delay := r.BaseDelay << attempt
		if err = sleepFn(ctx, delay+jitterFn(delay)); err != nil {
			return err
		}
	}
	return err
}

func sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
