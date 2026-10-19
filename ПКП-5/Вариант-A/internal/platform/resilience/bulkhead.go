package resilience

import (
	"context"
	"errors"
)

var ErrBusy = errors.New("bulkhead capacity exhausted")

type Bulkhead struct{ slots chan struct{} }

func NewBulkhead(limit int) *Bulkhead {
	if limit <= 0 {
		panic("bulkhead limit must be positive")
	}
	return &Bulkhead{slots: make(chan struct{}, limit)}
}

func (b *Bulkhead) Do(ctx context.Context, operation func(context.Context) error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case b.slots <- struct{}{}:
		defer func() { <-b.slots }()
		return operation(ctx)
	default:
		return ErrBusy
	}
}
