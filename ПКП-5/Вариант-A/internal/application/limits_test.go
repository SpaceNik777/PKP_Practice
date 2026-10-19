package application_test

import (
	"context"
	"errors"
	"testing"

	"example.com/orderflow/internal/application"
	"example.com/orderflow/internal/application/memory"
	"example.com/orderflow/internal/limits"
)

type checkerFunc func(context.Context, limits.Check) error

func (f checkerFunc) Check(ctx context.Context, check limits.Check) error { return f(ctx, check) }

func TestSubmitChecksActiveApplicationsLimitForAuthor(t *testing.T) {
	repository := memory.NewRepository()
	var got limits.Check
	service := application.NewServiceWithLimits(repository, checkerFunc(func(_ context.Context, check limits.Check) error { got = check; return nil }))
	if _, err := service.Create(context.Background(), application.CreateCommand{ID: "a-1", Author: "ivan", Topic: "Budget"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Submit(context.Background(), "a-1"); err != nil {
		t.Fatal(err)
	}
	if got.OperationID != "submit:a-1" || got.Subject != "ivan" || got.Limit != "active-applications-per-author" {
		t.Fatalf("check = %+v", got)
	}
}

func TestSubmitDoesNotChangeApplicationWhenLimitDenied(t *testing.T) {
	repository := memory.NewRepository()
	service := application.NewServiceWithLimits(repository, checkerFunc(func(context.Context, limits.Check) error { return limits.ErrDenied }))
	if _, err := service.Create(context.Background(), application.CreateCommand{ID: "a-1", Author: "ivan", Topic: "Budget"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Submit(context.Background(), "a-1"); !errors.Is(err, limits.ErrDenied) {
		t.Fatalf("error = %v", err)
	}
	value, err := service.ByID(context.Background(), "a-1")
	if err != nil {
		t.Fatal(err)
	}
	if value.Status() != application.StatusDraft {
		t.Fatalf("status = %s, want DRAFT", value.Status())
	}
}
