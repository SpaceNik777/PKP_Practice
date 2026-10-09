// Пакет memory — адаптер хранения резервов в памяти процесса.
// Он импортирует предметную область, а не наоборот: направление
// зависимости — проверяемое свойство графа импортов.
package memory

import (
	"context"
	"fmt"
	"sync"

	"example.com/orderflow/internal/reservation"
)

// Статическая проверка соответствия порту: если интерфейс
// reservation.Repository изменится, ошибка компиляции возникнет
// прямо в файле адаптера.
var _ reservation.Repository = (*Repository)(nil)

type Repository struct {
	mu           sync.RWMutex
	reservations map[string]reservation.Reservation
}

func NewRepository() *Repository {
	return &Repository{reservations: make(map[string]reservation.Reservation)}
}

// Create вставляет новый резерв. Проверка существования и запись
// выполняются под одной блокировкой, иначе два одновременных вызова
// с одним идентификатором оба прошли бы проверку и один затёр бы
// другой. Проверки пересечения интервалов здесь нет намеренно:
// учёт занятости слотов — ответственность расписания, а не хранилища.
func (r *Repository) Create(ctx context.Context, value reservation.Reservation) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.reservations[value.ID()]; exists {
		return reservation.ErrConflict
	}
	r.reservations[value.ID()] = value
	return nil
}

func (r *Repository) ByID(ctx context.Context, id string) (reservation.Reservation, error) {
	if err := ctx.Err(); err != nil {
		return reservation.Reservation{}, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	value, exists := r.reservations[id]
	if !exists {
		return reservation.Reservation{}, fmt.Errorf("%w: %s", reservation.ErrNotFound, id)
	}
	return value, nil
}

// Save записывает новую версию уже существующего резерва после
// перехода состояния. Сохранение неизвестного идентификатора —
// ошибка «не найдено»: Save не создаёт агрегаты, для этого есть Create.
func (r *Repository) Save(ctx context.Context, value reservation.Reservation) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.reservations[value.ID()]; !exists {
		return fmt.Errorf("%w: %s", reservation.ErrNotFound, value.ID())
	}
	r.reservations[value.ID()] = value
	return nil
}
