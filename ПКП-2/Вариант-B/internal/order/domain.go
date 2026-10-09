package order

import (
	"errors"
	"fmt"
)

var (
	ErrInvalid  = errors.New("invalid order")
	ErrConflict = errors.New("order conflict")
	ErrNotFound = errors.New("order not found")
)

type Status string

const (
	StatusPending   Status = "PENDING"
	StatusConfirmed Status = "CONFIRMED"
	StatusRejected  Status = "REJECTED"
)

type Line struct {
	SKU       string
	Quantity  int
	UnitCents int64
}

type Order struct {
	id      string
	version int64
	status  Status
	lines   []Line
}

func New(id string, lines []Line) (Order, error) {
	if id == "" {
		return Order{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}
	if len(lines) == 0 {
		return Order{}, fmt.Errorf("%w: at least one line is required", ErrInvalid)
	}

	copied := make([]Line, len(lines))
	copy(copied, lines)
	for i, line := range copied {
		if line.SKU == "" {
			return Order{}, fmt.Errorf("%w: line %d has empty SKU", ErrInvalid, i)
		}
		if line.Quantity <= 0 {
			return Order{}, fmt.Errorf("%w: line %d quantity must be positive", ErrInvalid, i)
		}
		if line.UnitCents <= 0 {
			return Order{}, fmt.Errorf("%w: line %d price must be positive", ErrInvalid, i)
		}
	}

	return Order{
		id:      id,
		version: 1,
		status:  StatusPending,
		lines:   copied,
	}, nil
}

func (o Order) ID() string     { return o.id }
func (o Order) Version() int64 { return o.version }
func (o Order) Status() Status { return o.status }

func (o Order) Lines() []Line {
	copied := make([]Line, len(o.lines))
	copy(copied, o.lines)
	return copied
}

func (o Order) TotalCents() int64 {
	var total int64
	for _, line := range o.lines {
		total += int64(line.Quantity) * line.UnitCents
	}
	return total
}

func (o *Order) Confirm() error {
	if o.status != StatusPending {
		return fmt.Errorf("%w: cannot confirm from %s", ErrConflict, o.status)
	}
	o.status = StatusConfirmed
	o.version++
	return nil
}
