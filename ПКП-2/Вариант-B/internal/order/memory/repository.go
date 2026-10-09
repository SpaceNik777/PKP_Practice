package memory

import (
	"context"
	"fmt"
	"sync"

	"example.com/orderflow/internal/order"
)

// Статическая проверка соответствия порту: если интерфейс
// order.Repository изменится, ошибка компиляции возникнет здесь,
// а не в глубине сборки зависимостей.
var _ order.Repository = (*Repository)(nil)

type Repository struct {
	mu     sync.RWMutex
	orders map[string]order.Order
}

func NewRepository() *Repository {
	return &Repository{orders: make(map[string]order.Order)}
}

func (r *Repository) Create(ctx context.Context, value order.Order) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.orders[value.ID()]; exists {
		return order.ErrConflict
	}
	r.orders[value.ID()] = value
	return nil
}

func (r *Repository) ByID(ctx context.Context, id string) (order.Order, error) {
	if err := ctx.Err(); err != nil {
		return order.Order{}, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	value, exists := r.orders[id]
	if !exists {
		return order.Order{}, fmt.Errorf("%w: %s", order.ErrNotFound, id)
	}
	return value, nil
}
