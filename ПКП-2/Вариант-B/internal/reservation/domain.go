// Пакет reservation — предметная область резервирования ресурса.
// Агрегат Reservation владеет своим жизненным циклом (создание, отмена)
// и интервалом занятия ресурса, но не состоянием занятости слотов
// расписания в целом — см. docs/adr/0001-schedule-reservation-boundary.md.
package reservation

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalid  = errors.New("invalid reservation")
	ErrConflict = errors.New("reservation conflict")
	ErrNotFound = errors.New("reservation not found")
)

type Status string

const (
	StatusActive    Status = "ACTIVE"
	StatusCancelled Status = "CANCELLED"
)

// Reservation — корень агрегата. Резерв занимает ресурс на полуоткрытом
// интервале [From, To): момент From включён, момент To уже свободен.
// Полуоткрытость снимает пограничный конфликт: резерв до 12:00 и резерв
// с 12:00 не пересекаются (см. Overlaps), поэтому стыкующиеся слоты
// расписания не требуют искусственного сдвига границ.
//
// Поля закрыты: единственный способ получить агрегат — конструктор New,
// который не пропустит недопустимое состояние дальше в систему.
type Reservation struct {
	id       string
	resource string
	from     time.Time
	to       time.Time
	status   Status
	version  int64
}

// New создаёт активный резерв и отклоняет команды, нарушающие
// ограничения целостности: пустой идентификатор, пустой ресурс
// и неверный интервал (to <= from для полуоткрытого [from, to)).
func New(id string, resource string, from time.Time, to time.Time) (Reservation, error) {
	if id == "" {
		return Reservation{}, fmt.Errorf("%w: empty id", ErrInvalid)
	}
	if resource == "" {
		return Reservation{}, fmt.Errorf("%w: empty resource", ErrInvalid)
	}
	if !to.After(from) {
		return Reservation{}, fmt.Errorf("%w: interval [%s, %s) must have to > from", ErrInvalid, from.Format(time.RFC3339), to.Format(time.RFC3339))
	}

	return Reservation{
		id:       id,
		resource: resource,
		from:     from,
		to:       to,
		status:   StatusActive,
		version:  1,
	}, nil
}

func (r Reservation) ID() string       { return r.id }
func (r Reservation) Resource() string { return r.resource }
func (r Reservation) From() time.Time  { return r.from }
func (r Reservation) To() time.Time    { return r.to }
func (r Reservation) Status() Status   { return r.status }
func (r Reservation) Version() int64   { return r.version }

// Overlaps сообщает, пересекаются ли два полуоткрытых интервала.
// Для [from, to) пересечение есть, когда from < other.to и
// other.from < to: соседние резервы [10,12) и [12,14) стыкуются
// в точке 12, но не пересекаются. Метод-запрос: он не меняет агрегат
// и пригодится службе расписания для обоснования границы с занятостью.
func (r Reservation) Overlaps(other Reservation) bool {
	return r.from.Before(other.to) && other.from.Before(r.to)
}

// Cancel отменяет резерв. Переход допустим только из ACTIVE.
//
// Контракт повторного вызова выбран КОНФЛИКТНЫМ: отмена уже
// отменённого резерва возвращает ErrConflict, а не «успех без
// изменений». Решение симметрично Confirm из практического разбора:
// повторный вызов — ошибка состояния, а не новый результат. Жёсткая
// семантика честнее выявляет потерянные обновления вызывающей стороны
// (клиент, не знающий о первой отмене, получает ErrConflict, а не
// молчаливое подтверждение своей картины мира). Идемпотентность, если
// она понадобится транспортному адаптеру (например, повторная доставка
// сообщения), реализуется на его стороне — предварительным чтением
// статуса или подавлением конкретного класса ошибки.
func (r *Reservation) Cancel() error {
	if r.status != StatusActive {
		return fmt.Errorf("%w: cannot cancel from %s", ErrConflict, r.status)
	}
	r.status = StatusCancelled
	r.version++
	return nil
}
