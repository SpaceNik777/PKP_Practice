package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/orderflow/internal/application"
	"example.com/orderflow/internal/application/memory"
	"example.com/orderflow/internal/platform/workqueue"
)

func TestSubmitApplicationAccepted(t *testing.T) {
	repository := memory.NewRepository()
	service := application.NewService(repository)
	if _, err := service.Create(context.Background(), application.CreateCommand{ID: "a-1", Author: "Ivan", Topic: "Budget"}); err != nil {
		t.Fatal(err)
	}
	q, _ := workqueue.New(context.Background(), 1, 1, nil)
	h := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), &fakePinger{}, q, service)
	r := httptest.NewRequest(http.MethodPost, "/applications/a-1/submit", nil)
	w := httptest.NewRecorder()
	h.Routes().ServeHTTP(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	if err := q.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	value, err := service.ByID(context.Background(), "a-1")
	if err != nil || value.Status() != application.StatusSubmitted {
		t.Fatalf("application = %+v, error = %v", value, err)
	}
}

func TestSubmitApplicationReturns429WithRetryAfter(t *testing.T) {
	repository := memory.NewRepository()
	service := application.NewService(repository)
	q, _ := workqueue.New(context.Background(), 1, 1, nil)
	started, release := make(chan struct{}), make(chan struct{})
	if err := q.Submit(context.Background(), func(context.Context) error { close(started); <-release; return nil }); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := q.Submit(context.Background(), func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), &fakePinger{}, q, service)
	r := httptest.NewRequest(http.MethodPost, "/applications/a-2/submit", nil)
	w := httptest.NewRecorder()
	h.Routes().ServeHTTP(w, r)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want 1", got)
	}
	close(release)
	if err := q.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
