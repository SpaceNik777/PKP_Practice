package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

// lifecycleServer — тестовый стенд: настоящий net.Listener на
// 127.0.0.1:0 и сервер в горутине. Обёртка slowProbe сигнализирует
// о входе запроса /slow до передачи управления маршрутизатору,
// позволяя тесту узнать, что обработка началась.
type lifecycleServer struct {
	handler     *Handler
	server      *http.Server
	baseURL     string
	slowStarted chan struct{}
}

func newLifecycleServer(t *testing.T) *lifecycleServer {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pinger := &fakePinger{}
	h := NewHandler(logger, pinger)

	slowStarted := make(chan struct{}, 1)
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			slowStarted <- struct{}{}
		}
		h.Routes().ServeHTTP(w, r)
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{
		Handler:           wrapped,
		ReadHeaderTimeout: 2 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	return &lifecycleServer{
		handler:     h,
		server:      srv,
		baseURL:     "http://" + ln.Addr().String(),
		slowStarted: slowStarted,
	}
}

// startSlow начинает запрос /slow настоящим http.Client и возвращает
// канал с единственным результатом.
func (s *lifecycleServer) startSlow() <-chan *http.Response {
	type outcome struct {
		resp *http.Response
		err  error
	}
	out := make(chan outcome, 1)
	go func() {
		resp, err := http.Get(s.baseURL + "/slow")
		out <- outcome{resp: resp, err: err}
	}()

	adapted := make(chan *http.Response, 1)
	go func() {
		o := <-out
		if o.err != nil {
			adapted <- nil
			return
		}
		adapted <- o.resp
	}()
	return adapted
}

// waitForSlowStart дожидается сигнала о начале обработки /slow.
func (s *lifecycleServer) waitForSlowStart(t *testing.T) {
	t.Helper()
	select {
	case <-s.slowStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("slow request did not start within 5s")
	}
}

// Сценарий 1: бюджета (5 s) достаточно для остатка работы /slow (~3 s).
// Shutdown завершается без ошибки, активный запрос получает
// 200 и {"status":"completed"}.
func TestShutdownWaitsForActiveSlowRequest(t *testing.T) {
	s := newLifecycleServer(t)
	respCh := s.startSlow()
	s.waitForSlowStart(t)

	s.handler.SetReady(false)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.server.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown error = %v, want nil (budget must cover the active request)", err)
	}

	resp := <-respCh
	if resp == nil {
		t.Fatal("client did not receive a response")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "completed" {
		t.Fatalf("status field = %q, want %q", body["status"], "completed")
	}
}

// Сценарий 2: бюджет (1 s) меньше остатка работы /slow (~3 s).
// Shutdown возвращает context.DeadlineExceeded; клиент завершённого
// ответа не получает. После неудачного ожидания тест сам управляет
// уборкой ресурсов через Close — это обязанность теста, исходный
// run() явного Close не выполняет.
func TestShutdownTimesOutWhenBudgetInsufficient(t *testing.T) {
	s := newLifecycleServer(t)
	respCh := s.startSlow()
	s.waitForSlowStart(t)

	s.handler.SetReady(false)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := s.server.Shutdown(shutdownCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want context.DeadlineExceeded", err)
	}

	if err := s.server.Close(); err != nil {
		t.Fatalf("force close after failed shutdown: %v", err)
	}

	resp := <-respCh
	if resp == nil {
		// Клиент получил сетевую ошибку — ожидаемый исход обрыва.
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		var body map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&body); err == nil && body["status"] == "completed" {
			t.Fatal("client received a completed response despite insufficient budget")
		}
	}
}
