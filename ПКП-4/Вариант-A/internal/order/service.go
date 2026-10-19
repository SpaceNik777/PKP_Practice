package order

import (
	"context"
	"fmt"
)

// Repository — порт хранения, объявленный на стороне потребителя
// (прикладного пакета). Интерфейс из двух методов легко понять,
// заменить в тесте и реализовать заново. Соответствие в Go
// структурное: адаптеру не требуется объявлять, что он реализует
// интерфейс, — компилятор проверит совместимость в точке сборки
// зависимостей.
type Repository interface {
	Create(ctx context.Context, value Order) error
	ByID(ctx context.Context, id string) (Order, error)
}

// CreateCommand — входная модель прикладного слоя. В ней нет
// заголовков HTTP и тегов JSON: внешний формат меняется по своим
// причинам (совместимость версий API), а команда сценария — по своим.
type CreateCommand struct {
	ID    string
	Lines []Line
}

// Created — результат сценария. Служба возвращает не сам агрегат,
// а отдельный результат: вызывающий получает ровно то, что нужно
// показать, и не приобретает возможности воздействовать на состояние.
type Created struct {
	ID         string
	Version    int64
	Status     Status
	TotalCents int64
}

// Service — прикладная служба сценария создания заказа. В ней нет
// проверок правил (они принадлежат конструктору New), нет ветвлений
// по типу хранилища и ни строчки про формат ответа — только порядок
// шагов: построить корректный агрегат, сохранить, вернуть результат.
type Service struct {
	repository Repository
}

// NewService принимает минимальный порт и возвращает конкретный тип.
func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

// Create выполняет сценарий создания. Ошибка хранилища оборачивается
// с контекстом операции через %w: класс ошибки сохраняется для
// errors.Is, а сообщение содержит достаточно данных для диагностики.
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

// ByID возвращает сохранённый заказ по идентификатору. Служба не
// проверяет контекст сама: проверка уместна там, где начинается
// настоящая работа, то есть в адаптере.
func (s *Service) ByID(ctx context.Context, id string) (Order, error) {
	if id == "" {
		return Order{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}
	return s.repository.ByID(ctx, id)
}
