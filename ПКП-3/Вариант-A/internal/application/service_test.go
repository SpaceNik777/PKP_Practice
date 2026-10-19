// Пакет application_test — внешний тестовый пакет: он видит только
// открытый контракт и физически не может привязаться к закрытым
// полям агрегата, закрепляя обещания, данные пользователям пакета.
package application_test

import (
	"context"
	"errors"
	"testing"

	"example.com/orderflow/internal/application"
	"example.com/orderflow/internal/application/memory"
)

// TestServiceCreatesSubmitsAndRejectsRepeatedSubmit — служебный тест
// с настоящим memory-адаптером: сценарий целиком, включая обнаружение
// повтора. Create возвращает DRAFT, Submit переводит в SUBMITTED
// и наращивает version, повторный Submit отклоняется конфликтом,
// чтение несуществующего идентификатора возвращает ErrNotFound.
func TestServiceCreatesSubmitsAndRejectsRepeatedSubmit(t *testing.T) {
	repository := memory.NewRepository()
	service := application.NewService(repository)

	created, err := service.Create(context.Background(), application.CreateCommand{
		ID:     "app-2001",
		Author: "a.petrov",
		Topic:  "Purchase of licenses",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != application.StatusDraft || created.Version != 1 {
		t.Fatalf("unexpected created result: %+v", created)
	}

	submitted, err := service.Submit(context.Background(), "app-2001")
	if err != nil {
		t.Fatal(err)
	}
	if submitted.Status != application.StatusSubmitted || submitted.Version != 2 {
		t.Fatalf("unexpected submit result: %+v", submitted)
	}

	_, err = service.Submit(context.Background(), "app-2001")
	if !errors.Is(err, application.ErrConflict) {
		t.Fatalf("error = %v, want ErrConflict", err)
	}

	_, err = service.ByID(context.Background(), "app-missing")
	if !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

// TestServiceSubmitOfMissingApplication — отправка на согласование
// несуществующей заявки возвращает ErrNotFound, а не создаёт её
// неявно: Submit не может изобрести агрегат.
func TestServiceSubmitOfMissingApplication(t *testing.T) {
	repository := memory.NewRepository()
	service := application.NewService(repository)

	_, err := service.Submit(context.Background(), "app-missing")
	if !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

// TestServicePropagatesCancelledContext — сквозная проверка отмены:
// контекст, отменённый на входе, доходит до адаптера и превращается
// в context.Canceled. Если бы служба потеряла контекст по дороге,
// тест немедленно упал бы.
func TestServicePropagatesCancelledContext(t *testing.T) {
	repository := memory.NewRepository()
	service := application.NewService(repository)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := service.Create(ctx, application.CreateCommand{
		ID:     "app-cancelled",
		Author: "a.petrov",
		Topic:  "Purchase of licenses",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}

	_, err = service.Submit(ctx, "app-cancelled")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
