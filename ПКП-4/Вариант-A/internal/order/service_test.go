// Пакет order_test — внешний тестовый пакет: он видит только открытый
// контракт и физически не может привязаться к закрытым полям агрегата.
// Тем самым тесты закрепляют обещания, данные пользователям пакета,
// и не мешают менять внутреннее устройство.
package order_test

import (
	"context"
	"errors"
	"testing"

	"example.com/orderflow/internal/order"
	"example.com/orderflow/internal/order/memory"
)

func TestServiceCreatesOrderAndRejectsDuplicate(t *testing.T) {
	repository := memory.NewRepository()
	service := order.NewService(repository)
	command := order.CreateCommand{
		ID: "ord-1001",
		Lines: []order.Line{{
			SKU: "BOOK-GO", Quantity: 2, UnitCents: 2500,
		}},
	}

	created, err := service.Create(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if created.TotalCents != 5000 || created.Status != order.StatusPending {
		t.Fatalf("unexpected result: %+v", created)
	}

	_, err = service.Create(context.Background(), command)
	if !errors.Is(err, order.ErrConflict) {
		t.Fatalf("error = %v, want ErrConflict", err)
	}
}

func TestServicePropagatesCancelledContext(t *testing.T) {
	repository := memory.NewRepository()
	service := order.NewService(repository)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := service.Create(ctx, order.CreateCommand{
		ID:    "ord-cancelled",
		Lines: []order.Line{{SKU: "A", Quantity: 1, UnitCents: 100}},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
