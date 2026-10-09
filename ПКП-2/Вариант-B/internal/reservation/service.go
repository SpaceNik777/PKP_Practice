package reservation

import (
	"context"
	"fmt"
	"time"
)

// Repository — минимальный порт хранения, объявленный на стороне
// потребителя (прикладного пакета). Save нужен для сохранения перехода
// состояния: служба читает агрегат, выполняет Cancel и записывает
// новую версию. Проверку пересечения интервалов порт сознательно не
// содержит: занятость слотов — ответственность расписания, а не
// хранилища резервов (см. docs/adr/0001-schedule-reservation-boundary.md).
type Repository interface {
	Create(ctx context.Context, value Reservation) error
	ByID(ctx context.Context, id string) (Reservation, error)
	Save(ctx context.Context, value Reservation) error
}

// CreateCommand — входная модель сценария создания: только то, что
// присылает внешний мир, без заголовков HTTP и тегов JSON.
type CreateCommand struct {
	ID       string
	Resource string
	From     time.Time
	To       time.Time
}

// Created — отдельный объект результата сценария. Вызывающий получает
// ровно то, что ему нужно показать, и не приобретает возможности
// воздействовать на внутреннее состояние агрегата.
type Created struct {
	ID       string
	Resource string
	From     time.Time
	To       time.Time
	Status   Status
	Version  int64
}

// Cancelled — отдельный объект результата сценария отмены.
type Cancelled struct {
	ID      string
	Status  Status
	Version int64
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

// Create строит корректный агрегат конструктором New, сохраняет его
// и возвращает результат. Правила принадлежат домену; служба отвечает
// только за порядок шагов. Ошибка хранилища оборачивается с контекстом
// операции через %w — класс сохраняется для errors.Is.
func (s *Service) Create(ctx context.Context, command CreateCommand) (Created, error) {
	value, err := New(command.ID, command.Resource, command.From, command.To)
	if err != nil {
		return Created{}, err
	}
	if err := s.repository.Create(ctx, value); err != nil {
		return Created{}, fmt.Errorf("create reservation %s: %w", value.ID(), err)
	}
	return Created{
		ID:       value.ID(),
		Resource: value.Resource(),
		From:     value.From(),
		To:       value.To(),
		Status:   value.Status(),
		Version:  value.Version(),
	}, nil
}

// Cancel читает резерв, выполняет переход и сохраняет новую версию.
// Повторная отмена доходит до домена и возвращается как ErrConflict:
// контракт повторного вызова зафиксирован в Reservation.Cancel.
func (s *Service) Cancel(ctx context.Context, id string) (Cancelled, error) {
	if id == "" {
		return Cancelled{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}
	value, err := s.repository.ByID(ctx, id)
	if err != nil {
		return Cancelled{}, fmt.Errorf("cancel reservation %s: %w", id, err)
	}
	if err := value.Cancel(); err != nil {
		return Cancelled{}, fmt.Errorf("cancel reservation %s: %w", value.ID(), err)
	}
	if err := s.repository.Save(ctx, value); err != nil {
		return Cancelled{}, fmt.Errorf("cancel reservation %s: %w", value.ID(), err)
	}
	return Cancelled{
		ID:      value.ID(),
		Status:  value.Status(),
		Version: value.Version(),
	}, nil
}
