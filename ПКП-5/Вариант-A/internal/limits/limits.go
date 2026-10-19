package limits

import (
	"context"
	"errors"
)

var (
	ErrDenied    = errors.New("limit exceeded")
	ErrBusy      = errors.New("limits client is busy")
	ErrTransient = errors.New("transient limits failure")
)

type Check struct {
	OperationID string
	Subject     string
	Limit       string
}

type Checker interface {
	Check(ctx context.Context, check Check) error
}
