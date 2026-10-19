// Пакет application содержит предметную область варианта A —
// «Заявка на согласование»: агрегат Application, его ограничения
// целостности и разрешённые переходы состояний.
//
// Пакет чистый: он не импортирует ни net/http, ни драйверы баз
// данных, ни брокеров сообщений — зависимости направлены внутрь,
// к предметной области. Это проверяемое свойство графа импортов.
package application

import (
	"errors"
	"fmt"
)

// Ошибки-маркеры классов результатов. Обёртка через %w сохраняет
// класс для errors.Is, добавляя контекст операции.
var (
	ErrInvalid  = errors.New("invalid application")
	ErrConflict = errors.New("application conflict")
	ErrNotFound = errors.New("application not found")
)

// Status — состояние заявки. StatusRejected объявлен в словаре
// предметной области, но перехода к нему пока нет: переходы
// добавляются по мере появления сценариев.
type Status string

const (
	StatusDraft     Status = "DRAFT"
	StatusSubmitted Status = "SUBMITTED"
	StatusRejected  Status = "REJECTED"
)

// Application — корень агрегата заявки на согласование. Поля закрыты:
// единственный способ получить заявку — конструктор New, который не
// возвращает объект, не прошедший проверки, поэтому недопустимое
// состояние недостижимо через открытый интерфейс типа. Полей-срезов
// нет (только строки), так что копирования срезов не требуется —
// но и напрямую поля извне недоступны.
type Application struct {
	id      string
	author  string
	topic   string
	status  Status
	version int64
}

// New создаёт заявку в состоянии DRAFT. Инварианты: непустой
// идентификатор, непустой автор и непустая тема — заявка без автора
// или без предмета согласования не имеет смысла.
func New(id, author, topic string) (Application, error) {
	if id == "" {
		return Application{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}
	if author == "" {
		return Application{}, fmt.Errorf("%w: empty author", ErrInvalid)
	}
	if topic == "" {
		return Application{}, fmt.Errorf("%w: empty topic", ErrInvalid)
	}

	return Application{
		id:      id,
		author:  author,
		topic:   topic,
		status:  StatusDraft,
		version: 1,
	}, nil
}

func (a Application) ID() string     { return a.id }
func (a Application) Author() string { return a.author }
func (a Application) Topic() string  { return a.topic }
func (a Application) Status() Status { return a.status }

// Version увеличивается при каждом переходе состояния и в дальнейшем
// понадобится для оптимистичной блокировки при записи в базу данных.
func (a Application) Version() int64 { return a.version }

// Submit переводит заявку в SUBMITTED — отправляет её на согласование.
// Переход объявлен на указателе, так как изменяет состояние; допустим
// только из DRAFT. Повторная отправка уже отправленной заявки —
// конфликт: она не может одновременно находиться в согласовании
// дважды.
func (a *Application) Submit() error {
	if a.status != StatusDraft {
		return fmt.Errorf("%w: cannot submit from %s", ErrConflict, a.status)
	}
	a.status = StatusSubmitted
	a.version++
	return nil
}
