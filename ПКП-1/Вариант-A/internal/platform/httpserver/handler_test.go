package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakePinger — подставной адаптер для тестов готовности.
// Режим waitCtx=true имитирует медленный адаптер: Ping ждёт отмены
// контекста и возвращает её причину, то есть уважает контракт отмены,
// как и настоящая реализация из main.
type fakePinger struct {
	err     error
	waitCtx bool
	calls   atomic.Int32
}

func (p *fakePinger) Ping(ctx context.Context) error {
	p.calls.Add(1)
	if p.waitCtx {
		<-ctx.Done()
		return ctx.Err()
	}
	return p.err
}

// decodeBody разбирает JSON-тело ответа. Сравнение строк целиком
// запрещено: кодировщик дописывает перевод строки.
func decodeBody(t *testing.T, r *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return body
}

// captureLog создаёт обработчик с журналом в буфер и возвращает
// функцию чтения последней JSON-записи журнала.
func captureLog() (*Handler, func(t *testing.T) map[string]any) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	pinger := &fakePinger{}
	h := NewHandler(logger, pinger)
	return h, func(t *testing.T) map[string]any {
		t.Helper()
		line := strings.TrimSpace(buf.String())
		idx := strings.LastIndex(line, "\n")
		if idx >= 0 {
			line = line[idx+1:]
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode log record %q: %v", line, err)
		}
		return record
	}
}

func TestLivezReturnsOK(t *testing.T) {
	h, _ := captureLog()

	request := httptest.NewRequest(http.MethodGet, "/livez", nil)
	response := httptest.NewRecorder()
	h.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := decodeBody(t, response)
	if body["status"] != "alive" {
		t.Fatalf("status field = %q, want %q", body["status"], "alive")
	}
}

// Готовое состояние: флаг включён, адаптер отвечает успешно.
// Проверяются код ответа, тип содержимого и содержимое JSON.
func TestReadinessReady(t *testing.T) {
	h, _ := captureLog()

	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	h.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", got, "application/json")
	}
	body := decodeBody(t, response)
	if body["status"] != "ready" {
		t.Fatalf("status field = %q, want %q", body["status"], "ready")
	}
}

// Отказ адаптера: 503 и статус not_ready в теле.
func TestReadinessNotReadyOnPingFailure(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := NewHandler(logger, &fakePinger{err: errors.New("adapter unavailable")})

	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	h.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", got, "application/json")
	}
	body := decodeBody(t, response)
	if body["status"] != "not_ready" {
		t.Fatalf("status field = %q, want %q", body["status"], "not_ready")
	}
}

// Начало остановки: флаг выключен — 503 со статусом draining,
// причём адаптер не вызывается: успешный Ping не может вернуть
// процесс в готовность после начала остановки.
func TestReadinessDraining(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pinger := &fakePinger{}
	handler := NewHandler(logger, pinger)
	handler.SetReady(false)

	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	handler.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	body := decodeBody(t, response)
	if body["status"] != "draining" {
		t.Fatalf("status field = %q, want %q", body["status"], "draining")
	}
	if n := pinger.calls.Load(); n != 0 {
		t.Fatalf("Ping called %d times, want 0: flag check must come first", n)
	}
}

// Отдельный тест контракта отмены: адаптер, ожидающий ctx.Done(),
// возвращает ошибку контекста при истечении срока.
func TestPingerRespectsContextCancellation(t *testing.T) {
	pinger := &fakePinger{waitCtx: true}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := pinger.Ping(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
}

// Контракт идентификатора: границы длины и допустимые символы.
func TestValidRequestIDContract(t *testing.T) {
	cases := []struct {
		name string
		id   string
		want bool
	}{
		{"minimal length", "12345678", true},
		{"maximal length", strings.Repeat("a", 64), true},
		{"letters digits dash underscore", "Order-Flow_2026", true},
		{"too short", "1234567", false},
		{"too long", strings.Repeat("a", 65), false},
		{"space inside", "abcd efgh", false},
		{"cyrillic", "заявка-2026", false},
		{"special characters", "req/id;1?", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validRequestID(tc.id); got != tc.want {
				t.Fatalf("validRequestID(%q) = %v, want %v", tc.id, got, tc.want)
			}
		})
	}
}

// Валидный входящий идентификатор сохраняется, возвращается в ответе
// и попадает в журнал.
func TestRequestIDAccepted(t *testing.T) {
	h, lastRecord := captureLog()
	const incoming = "Order-Flow-2026-10-04_01"

	request := httptest.NewRequest(http.MethodGet, "/livez", nil)
	request.Header.Set("X-Request-ID", incoming)
	response := httptest.NewRecorder()
	h.Routes().ServeHTTP(response, request)

	if got := response.Header().Get("X-Request-ID"); got != incoming {
		t.Fatalf("response X-Request-ID = %q, want %q", got, incoming)
	}
	record := lastRecord(t)
	if got := record["request_id"]; got != incoming {
		t.Fatalf("log request_id = %v, want %q", got, incoming)
	}
}

// Невалидный входящий идентификатор заменяется новым корректного
// формата: старое значение не попадает ни в ответ, ни в журнал.
func TestRequestIDReplacedWhenInvalid(t *testing.T) {
	h, lastRecord := captureLog()
	const incoming = "bad id!" // пробел и восклицательный знак вне контракта

	request := httptest.NewRequest(http.MethodGet, "/livez", nil)
	request.Header.Set("X-Request-ID", incoming)
	response := httptest.NewRecorder()
	h.Routes().ServeHTTP(response, request)

	got := response.Header().Get("X-Request-ID")
	if got == "" {
		t.Fatal("response X-Request-ID header is missing")
	}
	if got == incoming {
		t.Fatalf("invalid incoming id %q leaked into response", incoming)
	}
	if !validRequestID(got) {
		t.Fatalf("generated id %q does not satisfy the contract", got)
	}
	record := lastRecord(t)
	if rid, _ := record["request_id"].(string); rid == incoming || !validRequestID(rid) {
		t.Fatalf("log request_id = %v, want a generated valid id instead of %q", record["request_id"], incoming)
	}
}

// При отсутствии заголовка создаётся новый идентификатор,
// и он возвращается клиенту.
func TestRequestIDGeneratedWhenAbsent(t *testing.T) {
	h, _ := captureLog()

	request := httptest.NewRequest(http.MethodGet, "/livez", nil)
	response := httptest.NewRecorder()
	h.Routes().ServeHTTP(response, request)

	got := response.Header().Get("X-Request-ID")
	if got == "" {
		t.Fatal("response X-Request-ID header is missing")
	}
	if !validRequestID(got) {
		t.Fatalf("generated id %q does not satisfy the contract", got)
	}
}
