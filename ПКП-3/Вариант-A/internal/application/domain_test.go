package application

import (
	"errors"
	"testing"
)

// TestNewValidatesFields — табличный тест ограничений целостности
// конструктора: пустой идентификатор, пустой автор и пустая тема
// делают заявку недопустимой до любого сохранения.
func TestNewValidatesFields(t *testing.T) {
	tests := []struct {
		name   string
		id     string
		author string
		topic  string
	}{
		{name: "empty id", author: "a.petrov", topic: "Purchase of licenses"},
		{name: "empty author", id: "app-2001", topic: "Purchase of licenses"},
		{name: "empty topic", id: "app-2001", author: "a.petrov"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.id, tt.author, tt.topic)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
		})
	}
}

// TestNewCreatesDraft проверяет начальное состояние: DRAFT, version 1.
func TestNewCreatesDraft(t *testing.T) {
	got, err := New("app-2001", "a.petrov", "Purchase of licenses")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status() != StatusDraft {
		t.Fatalf("status = %s, want DRAFT", got.Status())
	}
	if got.Version() != 1 {
		t.Fatalf("version = %d, want 1", got.Version())
	}
}

// TestSubmitTransitionsDraftToSubmitted проверяет разрешённый переход:
// только из DRAFT в SUBMITTED, version растёт.
func TestSubmitTransitionsDraftToSubmitted(t *testing.T) {
	got, err := New("app-2001", "a.petrov", "Purchase of licenses")
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Submit(); err != nil {
		t.Fatal(err)
	}
	if got.Status() != StatusSubmitted {
		t.Fatalf("status = %s, want SUBMITTED", got.Status())
	}
	if got.Version() != 2 {
		t.Fatalf("version = %d, want 2", got.Version())
	}
}

// TestSubmitCannotBeRepeated — обязательный тест повторной отправки:
// заявка уже находится в согласовании, повторный Submit — конфликт
// класса ErrConflict, различимый через errors.Is.
func TestSubmitCannotBeRepeated(t *testing.T) {
	got, err := New("app-2001", "a.petrov", "Purchase of licenses")
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Submit(); err != nil {
		t.Fatal(err)
	}
	if err := got.Submit(); !errors.Is(err, ErrConflict) {
		t.Fatalf("error = %v, want ErrConflict", err)
	}
}
