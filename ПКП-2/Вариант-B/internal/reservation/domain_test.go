package reservation

import (
	"errors"
	"testing"
	"time"
)

func TestNewValidatesInput(t *testing.T) {
	base := time.Date(2026, 1, 12, 10, 0, 0, 0, time.UTC)
	hour := time.Hour

	tests := []struct {
		name     string
		id       string
		resource string
		from     time.Time
		to       time.Time
	}{
		{name: "empty id", resource: "room-a", from: base, to: base.Add(hour)},
		{name: "empty resource", id: "res-1", from: base, to: base.Add(hour)},
		{name: "zero length to equal from", id: "res-1", resource: "room-a", from: base, to: base},
		{name: "negative length to before from", id: "res-1", resource: "room-a", from: base, to: base.Add(-hour)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.id, tt.resource, tt.from, tt.to)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestNewCreatesActiveReservation(t *testing.T) {
	from := time.Date(2026, 1, 12, 10, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)

	got, err := New("res-1001", "room-a", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status() != StatusActive {
		t.Fatalf("status = %s, want ACTIVE", got.Status())
	}
	if got.Version() != 1 {
		t.Fatalf("version = %d, want 1", got.Version())
	}
	if got.Resource() != "room-a" || !got.From().Equal(from) || !got.To().Equal(to) {
		t.Fatalf("unexpected aggregate: %+v", got)
	}
}

func TestOverlapsHonoursHalfOpenIntervals(t *testing.T) {
	base := time.Date(2026, 1, 12, 10, 0, 0, 0, time.UTC)
	first, err := New("res-1", "room-a", base, base.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		other Reservation
		want  bool
	}{
		{
			name:  "overlapping middle",
			other: mustNew(t, "res-2", base.Add(time.Hour), base.Add(3*time.Hour)),
			want:  true,
		},
		{
			// Полуоткрытость [from, to): соседние интервалы
			// [10,12) и [12,14) стыкуются в точке 12, но не
			// пересекаются — правая граница уже свободна.
			name:  "adjacent intervals do not overlap",
			other: mustNew(t, "res-3", base.Add(2*time.Hour), base.Add(4*time.Hour)),
			want:  false,
		},
		{
			name:  "same boundaries overlap",
			other: mustNew(t, "res-4", base, base.Add(2*time.Hour)),
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := first.Overlaps(tt.other); got != tt.want {
				t.Fatalf("Overlaps = %v, want %v", got, tt.want)
			}
			// Симметрия отношения — проверяем и обратное направление.
			if got := tt.other.Overlaps(first); got != tt.want {
				t.Fatalf("reverse Overlaps = %v, want %v", got, tt.want)
			}
		})
	}
}

func mustNew(t *testing.T, id string, from, to time.Time) Reservation {
	t.Helper()
	value, err := New(id, "room-a", from, to)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestCancelTransitionsActiveToCancelled(t *testing.T) {
	base := time.Date(2026, 1, 12, 10, 0, 0, 0, time.UTC)
	got, err := New("res-1001", "room-a", base, base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	before := got.Version()
	if err := got.Cancel(); err != nil {
		t.Fatal(err)
	}
	if got.Status() != StatusCancelled {
		t.Fatalf("status = %s, want CANCELLED", got.Status())
	}
	if got.Version() != before+1 {
		t.Fatalf("version = %d, want %d", got.Version(), before+1)
	}
}

func TestCancelCannotBeCalledTwice(t *testing.T) {
	base := time.Date(2026, 1, 12, 10, 0, 0, 0, time.UTC)
	got, err := New("res-1001", "room-a", base, base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Cancel(); err != nil {
		t.Fatal(err)
	}

	// Контракт повторного вызова выбран конфликтным: повторная
	// отмена уже отменённого резерва — ошибка состояния.
	if err := got.Cancel(); !errors.Is(err, ErrConflict) {
		t.Fatalf("error = %v, want ErrConflict", err)
	}
}
