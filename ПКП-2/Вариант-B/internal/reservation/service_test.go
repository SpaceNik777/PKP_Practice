package reservation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"example.com/orderflow/internal/reservation"
	"example.com/orderflow/internal/reservation/memory"
)

// Служебный тест использует настоящий адаптер в памяти, а не заглушку:
// сценарий проверяется целиком, включая обнаружение повтора
// идентификатора и чтение несуществующего агрегата.
func TestServiceCreatesAndCancelsReservation(t *testing.T) {
	repository := memory.NewRepository()
	service := reservation.NewService(repository)
	command := reservation.CreateCommand{
		ID:       "res-1001",
		Resource: "room-a",
		From:     time.Date(2026, 1, 12, 10, 0, 0, 0, time.UTC),
		To:       time.Date(2026, 1, 12, 12, 0, 0, 0, time.UTC),
	}

	created, err := service.Create(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != reservation.StatusActive || created.Version != 1 {
		t.Fatalf("unexpected created result: %+v", created)
	}

	cancelled, err := service.Cancel(context.Background(), command.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != reservation.StatusCancelled || cancelled.Version != 2 {
		t.Fatalf("unexpected cancelled result: %+v", cancelled)
	}

	// Повторная отмена — конфликт по выбранному контракту; класс
	// ошибки доходит через обёртку службы, что и проверяет errors.Is.
	_, err = service.Cancel(context.Background(), command.ID)
	if !errors.Is(err, reservation.ErrConflict) {
		t.Fatalf("error = %v, want ErrConflict", err)
	}

	// Дубликат идентификатора отклоняется хранилищем как конфликт.
	_, err = service.Create(context.Background(), command)
	if !errors.Is(err, reservation.ErrConflict) {
		t.Fatalf("error = %v, want ErrConflict", err)
	}

	// Чтение несуществующего агрегата через порт хранилища.
	_, err = repository.ByID(context.Background(), "res-missing")
	if !errors.Is(err, reservation.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}

	// Отмена несуществующего резерва доходит до того же класса ошибки
	// через обёртку службы.
	_, err = service.Cancel(context.Background(), "res-missing")
	if !errors.Is(err, reservation.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestServicePropagatesCancelledContext(t *testing.T) {
	repository := memory.NewRepository()
	service := reservation.NewService(repository)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := service.Create(ctx, reservation.CreateCommand{
		ID:       "res-cancelled",
		Resource: "room-a",
		From:     time.Date(2026, 1, 12, 10, 0, 0, 0, time.UTC),
		To:       time.Date(2026, 1, 12, 11, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
