// Пакет order содержит предметную область заказа: агрегат Order,
// его ограничения целостности и разрешённые переходы состояний.
//
// Все поля агрегата закрыты: единственный способ получить заказ —
// конструктор New, который не возвращает объект, не прошедший
// проверки. Поэтому недопустимое состояние (например, подтверждённый
// пустой заказ) недостижимо через открытый интерфейс типа.
package order

import (
	"errors"
	"fmt"
)

// Ошибки-маркеры обозначают класс результата операции. Обёртка через
// %w сохраняет класс для errors.Is, добавляя контекст операции.
var (
	ErrInvalid  = errors.New("invalid order")
	ErrConflict = errors.New("order conflict")
	ErrNotFound = errors.New("order not found")
)

// Status — состояние заказа. Набор состояний — часть словаря
// предметной области; переходы добавляются по мере появления
// сценариев (StatusRejected объявлен, но перехода к нему пока нет).
type Status string

const (
	StatusPending   Status = "PENDING"
	StatusConfirmed Status = "CONFIRMED"
	StatusRejected  Status = "REJECTED"
)

// Line — строка заказа, объект-значение: определяется только своим
// содержимым и не имеет собственной судьбы. Деньги хранятся целыми
// числами в центах: float64 не представляет десятичные дроби точно,
// а целые числа в минимальных единицах избавляют от этого класса
// ошибок полностью.
type Line struct {
	SKU       string
	Quantity  int
	UnitCents int64
}

// Order — корень агрегата. Изменения внутри агрегата происходят
// атомарно и целиком; внешний код обращается к нему только через
// корень, который отвечает за допустимость внутреннего состояния
// после любой операции.
type Order struct {
	id      string
	version int64
	status  Status
	lines   []Line
}

// New создаёт заказ в состоянии PENDING. Строки копируются: срез в Go
// ссылается на общий массив, и сохранение переданного извне среза
// позволило бы менять содержимое агрегата, минуя все проверки.
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

// Lines возвращает копию строк: выдача внутреннего среза наружу
// открыла бы вызывающему коду возможность изменять агрегат в обход
// проверок конструктора.
func (o Order) Lines() []Line {
	copied := make([]Line, len(o.lines))
	copy(copied, o.lines)
	return copied
}

// TotalCents вычисляет итоговую сумму: значение, которое можно
// вывести из состояния, не принимается на веру от клиента.
func (o Order) TotalCents() int64 {
	var total int64
	for _, line := range o.lines {
		total += int64(line.Quantity) * line.UnitCents
	}
	return total
}

// Confirm переводит заказ в CONFIRMED. Переход объявлен на указателе,
// так как изменяет состояние; допустим только из PENDING — иначе
// конфликт. Поле version увеличивается при каждом переходе и в
// дальнейшем понадобится для оптимистичной блокировки.
func (o *Order) Confirm() error {
	if o.status != StatusPending {
		return fmt.Errorf("%w: cannot confirm from %s", ErrConflict, o.status)
	}
	o.status = StatusConfirmed
	o.version++
	return nil
}
