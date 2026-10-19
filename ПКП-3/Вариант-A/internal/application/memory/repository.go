// Пакет memory — адаптер хранения заявок в памяти процесса.
// Адаптер импортирует предметную область; предметная область
// не импортирует адаптер — это и есть направление зависимости.
package memory

import (
	"context"
	"fmt"
	"sync"

	"example.com/orderflow/internal/application"
)

// Repository — безопасное при одновременном доступе хранилище
// заявок в памяти. Отображение в Go не защищено от параллельных
// обращений, поэтому доступ закрыт sync.RWMutex: чтения идут
// параллельно, запись исключительна. Значения хранятся по значению,
// а поля агрегата закрыты, — изменить сохранённую заявку извне
// нельзя, и поведение хранилища совпадает с настоящей базой данных.
type Repository struct {
	mu           sync.RWMutex
	applications map[string]application.Application
}

func NewRepository() *Repository {
	return &Repository{applications: make(map[string]application.Application)}
}

// Статическая проверка соответствия порту на этапе компиляции:
// если интерфейс изменится, ошибка возникнет прямо в файле
// адаптера с понятным сообщением.
var _ application.Repository = (*Repository)(nil)

// Create вставляет заявку. Проверка существования и вставка —
// под одной блокировкой, иначе два одновременных запроса с одним
// идентификатором оба прошли бы проверку и один затёр бы другой.
func (r *Repository) Create(ctx context.Context, value application.Application) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.applications[value.ID()]; exists {
		return application.ErrConflict
	}
	r.applications[value.ID()] = value
	return nil
}

// ByID возвращает заявку по идентификатору. Ошибка «не найдено»
// обёрнута с указанием идентификатора: класс сохранён для errors.Is,
// сообщение содержит данные для диагностики.
func (r *Repository) ByID(ctx context.Context, id string) (application.Application, error) {
	if err := ctx.Err(); err != nil {
		return application.Application{}, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	value, exists := r.applications[id]
	if !exists {
		return application.Application{}, fmt.Errorf("%w: %s", application.ErrNotFound, id)
	}
	return value, nil
}

// Save обновляет ранее сохранённую заявку после перехода состояния.
// Проверка существования и запись — под одной блокировкой; обновление
// несуществующей заявки — ошибка «не найдено», а не молчаливое
// создание: Save не может изобрести агрегат, которого не было.
func (r *Repository) Save(ctx context.Context, value application.Application) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.applications[value.ID()]; !exists {
		return fmt.Errorf("%w: %s", application.ErrNotFound, value.ID())
	}
	r.applications[value.ID()] = value
	return nil
}
