// Пакет memory — адаптер хранения заказов в памяти процесса.
// Адаптер импортирует предметную область; предметная область
// не импортирует адаптер — это и есть направление зависимости.
package memory

import (
	"context"
	"fmt"
	"sync"

	"example.com/orderflow/internal/order"
)

// Repository — хранилище заказов в памяти. Отображение в Go не
// защищено от одновременного доступа, поэтому все обращения закрыты
// мьютексом: чтения идут параллельно (RWMutex), запись исключительна.
type Repository struct {
	mu     sync.RWMutex
	orders map[string]order.Order
}

func NewRepository() *Repository {
	return &Repository{orders: make(map[string]order.Order)}
}

// Статическая проверка соответствия порту: если интерфейс изменится,
// ошибка компиляции возникнет прямо в файле адаптера, а не в глубине
// сборки зависимостей.
var _ order.Repository = (*Repository)(nil)

// Create вставляет заказ. Проверка существования и вставка выполняются
// под одной блокировкой: иначе два одновременных запроса с одним
// идентификатором могли бы оба пройти проверку и один затёр бы другой.
// Значение хранится по значению: строки внутри копируются конструктором
// и методом Lines, поэтому изменить сохранённый заказ извне не получится.
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

// ByID возвращает заказ по идентификатору. Проверка ctx.Err() в начале
// операции — уважение к отмене: если вызывающий уже не ждёт ответа,
// нет смысла занимать блокировку. Ошибка «не найдено» обёрнута
// с указанием идентификатора: класс сохранён для errors.Is.
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
