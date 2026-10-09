package order

import (
	"context"
	"fmt"
)

type Repository interface {
	Create(ctx context.Context, value Order) error
	ByID(ctx context.Context, id string) (Order, error)
}

type CreateCommand struct {
	ID    string
	Lines []Line
}

type Created struct {
	ID         string
	Version    int64
	Status     Status
	TotalCents int64
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Create(ctx context.Context, command CreateCommand) (Created, error) {
	value, err := New(command.ID, command.Lines)
	if err != nil {
		return Created{}, err
	}
	if err := s.repository.Create(ctx, value); err != nil {
		return Created{}, fmt.Errorf("create order %s: %w", value.ID(), err)
	}
	return Created{
		ID:         value.ID(),
		Version:    value.Version(),
		Status:     value.Status(),
		TotalCents: value.TotalCents(),
	}, nil
}

func (s *Service) ByID(ctx context.Context, id string) (Order, error) {
	if id == "" {
		return Order{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}
	return s.repository.ByID(ctx, id)
}
