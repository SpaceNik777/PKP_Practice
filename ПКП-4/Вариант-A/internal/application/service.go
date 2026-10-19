package application

import (
	"context"
	"fmt"

	"example.com/orderflow/internal/limits"
)

// Repository — порт хранения, объявленный на стороне потребителя
// (прикладного пакета), а не исполнителя. Появление метода Save —
// нормальная эволюция порта: сценарий Submit потребовал сохранения
// изменённого агрегата, и порт вырос вместе с потребителем, оставаясь
// минимально достаточным.
type Repository interface {
	Create(ctx context.Context, value Application) error
	ByID(ctx context.Context, id string) (Application, error)
	Save(ctx context.Context, value Application) error
}

// CreateCommand — входная модель прикладного слоя. В ней нет
// заголовков HTTP и тегов JSON: внешний формат меняется по своим
// причинам, а команда сценария — по своим.
type CreateCommand struct {
	ID     string
	Author string
	Topic  string
}

// Created — результат сценария создания: ровно то, что нужно
// показать вызывающему, без возможности воздействовать на состояние.
type Created struct {
	ID      string
	Status  Status
	Version int64
}

// SubmitResult — отдельный объект результата отправки на согласование.
// Агрегат не раскрывается: ID, Status и Version исчерпывают то,
// что требуется знать внешнему слою после операции.
type SubmitResult struct {
	ID      string
	Status  Status
	Version int64
}

// Service — прикладная служба. В ней нет проверок правил заявки
// (они принадлежат конструктору New и переходу Submit), нет ветвлений
// по типу хранилища и ничего про формат ответа — только порядок
// шагов сценария и оборачивание ошибок контекстом операции.
type Service struct {
	repository Repository
	limits     limits.Checker
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func NewServiceWithLimits(repository Repository, checker limits.Checker) *Service {
	return &Service{repository: repository, limits: checker}
}

// Create строит корректный агрегат, сохраняет его и возвращает
// результат. Ошибка хранилища оборачивается через %w: класс
// сохраняется для errors.Is, контекст добавляется для диагностики.
func (s *Service) Create(ctx context.Context, command CreateCommand) (Created, error) {
	value, err := New(command.ID, command.Author, command.Topic)
	if err != nil {
		return Created{}, err
	}
	if err := s.repository.Create(ctx, value); err != nil {
		return Created{}, fmt.Errorf("create application %s: %w", value.ID(), err)
	}
	return Created{
		ID:      value.ID(),
		Status:  value.Status(),
		Version: value.Version(),
	}, nil
}

// Submit выполняет сценарий отправки на согласование: прочитать
// агрегат, выполнить переход, сохранить обновление, вернуть результат.
// Каждая ошибка оборачивается с контекстом операции через %w,
// поэтому класс ошибки сохраняется для errors.Is.
func (s *Service) Submit(ctx context.Context, id string) (SubmitResult, error) {
	if id == "" {
		return SubmitResult{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}

	value, err := s.repository.ByID(ctx, id)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("submit application %s: %w", id, err)
	}
	if s.limits != nil {
		if err := s.limits.Check(ctx, limits.Check{
			OperationID: "submit:" + value.ID(),
			Subject:     value.Author(),
			Limit:       "active-applications-per-author",
		}); err != nil {
			return SubmitResult{}, fmt.Errorf("check active application limit: %w", err)
		}
	}

	if err := value.Submit(); err != nil {
		return SubmitResult{}, fmt.Errorf("submit application %s: %w", id, err)
	}

	if err := s.repository.Save(ctx, value); err != nil {
		return SubmitResult{}, fmt.Errorf("submit application %s: %w", id, err)
	}

	return SubmitResult{
		ID:      value.ID(),
		Status:  value.Status(),
		Version: value.Version(),
	}, nil
}

// ByID возвращает сохранённую заявку по идентификатору. Служба
// не проверяет контекст сама: проверка уместна там, где начинается
// настоящая работа, то есть в адаптере.
func (s *Service) ByID(ctx context.Context, id string) (Application, error) {
	if id == "" {
		return Application{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}
	return s.repository.ByID(ctx, id)
}
