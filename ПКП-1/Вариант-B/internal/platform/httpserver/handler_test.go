package httpserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newCapturingHandler создаёт обработчик с журналом в буфер:
// предметом ряда тестов являются сами поля записи журнала.
func newCapturingHandler(t *testing.T, maxBodyBytes int64) (*Handler, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	return NewHandler(logger, maxBodyBytes), &buf
}

// decodeJSONLine ищет в журнале запись с указанным сообщением
// и возвращает её поля.
func decodeJSONLine(t *testing.T, buf *bytes.Buffer, msg string) map[string]any {
	t.Helper()
	scanner := bufio.NewScanner(buf)
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode log line %q: %v", scanner.Text(), err)
		}
		if record["msg"] == msg {
			return record
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan log: %v", err)
	}
	t.Fatalf("log record %q not found in:\n%s", msg, buf.String())
	return nil
}

// TestReadinessCanBeDisabled — базовый тест практики: после
// SetReady(false) маршрут /readyz отвечает 503.
func TestReadinessCanBeDisabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewHandler(logger, 1048576)
	handler.SetReady(false)

	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	handler.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestLivezAndReadyzWhenReady(t *testing.T) {
	handler, _ := newCapturingHandler(t, 1048576)

	for _, tc := range []struct {
		path   string
		status int
		field  string
	}{
		{"/livez", http.StatusOK, "alive"},
		{"/readyz", http.StatusOK, "ready"},
	} {
		request := httptest.NewRequest(http.MethodGet, tc.path, nil)
		response := httptest.NewRecorder()
		handler.Routes().ServeHTTP(response, request)

		if response.Code != tc.status {
			t.Fatalf("GET %s: status = %d, want %d", tc.path, response.Code, tc.status)
		}
		var body map[string]string
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("GET %s: decode body: %v", tc.path, err)
		}
		if body["status"] != tc.field {
			t.Fatalf("GET %s: status field = %q, want %q", tc.path, body["status"], tc.field)
		}
	}
}

// Успешное чтение тела: 202 Accepted, JSON {"status":"accepted"},
// заголовок X-Request-ID и запись "job accepted" с фактическими
// request_id, body_bytes, duration_ms.
func TestJobsAccepted(t *testing.T) {
	handler, logBuf := newCapturingHandler(t, 1048576)

	request := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader("hello"))
	response := httptest.NewRecorder()
	handler.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusAccepted)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "accepted" {
		t.Fatalf("status field = %q, want %q", body["status"], "accepted")
	}

	headerID := response.Header().Get("X-Request-ID")
	if !validRequestID(headerID) {
		t.Fatalf("X-Request-ID = %q, want a generated id matching the contract", headerID)
	}

	record := decodeJSONLine(t, logBuf, "job accepted")
	if record["request_id"] != headerID {
		t.Fatalf("log request_id = %v, want header value %q", record["request_id"], headerID)
	}
	// Тело "hello" — 5 фактических байт.
	if record["body_bytes"] != float64(5) {
		t.Fatalf("log body_bytes = %v, want 5", record["body_bytes"])
	}
	if _, ok := record["duration_ms"]; !ok {
		t.Fatal("log record has no duration_ms field")
	}
}

// Валидный входящий X-Request-ID сохраняется и возвращается клиенту.
func TestJobsValidRequestIDPreserved(t *testing.T) {
	handler, _ := newCapturingHandler(t, 1048576)

	request := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader("x"))
	request.Header.Set("X-Request-ID", "orderflow-test-12345")
	response := httptest.NewRecorder()
	handler.Routes().ServeHTTP(response, request)

	if got := response.Header().Get("X-Request-ID"); got != "orderflow-test-12345" {
		t.Fatalf("X-Request-ID = %q, want incoming value preserved", got)
	}
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusAccepted)
	}
}

// Невалидный входящий X-Request-ID (недопустимые символы или длина)
// заменяется новым сгенерированным идентификатором.
func TestJobsInvalidRequestIDReplaced(t *testing.T) {
	handler, _ := newCapturingHandler(t, 1048576)

	for _, incoming := range []string{
		"BAD ID!", // пробел и восклицательный знак вне алфавита
		"short7",  // 6 символов — ниже минимума контракта
		"id/with/slash",
	} {
		request := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader("x"))
		request.Header.Set("X-Request-ID", incoming)
		response := httptest.NewRecorder()
		handler.Routes().ServeHTTP(response, request)

		got := response.Header().Get("X-Request-ID")
		if got == incoming {
			t.Fatalf("X-Request-ID = %q: invalid value must not be echoed back", got)
		}
		if !validRequestID(got) {
			t.Fatalf("X-Request-ID = %q, want a new id matching the contract", got)
		}
		if len(got) != 32 { // 16 байт crypto/rand в hex
			t.Fatalf("X-Request-ID = %q: want 32-char hex id", got)
		}
		if response.Code != http.StatusAccepted {
			t.Fatalf("incoming %q: status = %d, want %d", incoming, response.Code, http.StatusAccepted)
		}
	}
}

// body_bytes отражает ФАКТИЧЕСКИ прочитанные байты, а не заявленный
// Content-Length: заголовку доверять нельзя. Тело 20 байт,
// заявленный Content-Length — 100.
func TestJobsBodyBytesReflectsActualReadNotContentLength(t *testing.T) {
	handler, logBuf := newCapturingHandler(t, 1048576)

	payload := strings.Repeat("a", 20)
	request := httptest.NewRequest(http.MethodPost, "/jobs", io.NopCloser(strings.NewReader(payload)))
	request.ContentLength = 100 // ложное обещание
	response := httptest.NewRecorder()
	handler.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusAccepted)
	}
	record := decodeJSONLine(t, logBuf, "job accepted")
	if record["body_bytes"] != float64(len(payload)) {
		t.Fatalf("log body_bytes = %v, want actual %d, not declared Content-Length 100", record["body_bytes"], len(payload))
	}
}

// Превышение предела: 413 c JSON-ошибкой. Случай 1 — тело на один
// байт больше MAX_BODY_BYTES (предел 64, тело 65).
func TestJobsTooLargeSlightly(t *testing.T) {
	handler, logBuf := newCapturingHandler(t, 64)

	request := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader(make([]byte, 65)))
	response := httptest.NewRecorder()
	handler.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "request body exceeds MAX_BODY_BYTES" {
		t.Fatalf("error field = %v, want 413 error message", body["error"])
	}
	if body["limit_bytes"] != float64(64) {
		t.Fatalf("limit_bytes = %v, want 64", body["limit_bytes"])
	}
	record := decodeJSONLine(t, logBuf, "job body too large")
	if record["limit_bytes"] != float64(64) {
		t.Fatalf("log limit_bytes = %v, want 64", record["limit_bytes"])
	}
}

// Превышение предела: 413. Случай 2 — тело многократно больше
// MAX_BODY_BYTES (предел 64, тело 64000).
func TestJobsTooLargeMassively(t *testing.T) {
	handler, _ := newCapturingHandler(t, 64)

	request := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader(make([]byte, 64000)))
	response := httptest.NewRecorder()
	handler.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "request body exceeds MAX_BODY_BYTES" {
		t.Fatalf("error field = %v, want 413 error message", body["error"])
	}
}

// Отрицательная готовность: SetReady(false) + POST /jobs → 503,
// причём точка допуска проверяется ДО чтения тела. Доказательство
// порядка: тело БОЛЬШЕ предела (64000 > 64), но ответ 503, а не 413 —
// значит, чтение вообще не начиналось.
func TestJobsNotReadyRejectsBeforeReadingBody(t *testing.T) {
	handler, logBuf := newCapturingHandler(t, 64)
	handler.SetReady(false)

	request := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader(make([]byte, 64000)))
	response := httptest.NewRecorder()
	handler.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d (admission check before body read)", response.Code, http.StatusServiceUnavailable)
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "not_ready" {
		t.Fatalf("status field = %q, want %q", body["status"], "not_ready")
	}
	// Тело не читалось: записи "job body too large" и "job accepted" нет.
	scanner := bufio.NewScanner(logBuf)
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode log line %q: %v", scanner.Text(), err)
		}
		if record["msg"] == "job accepted" || record["msg"] == "job body too large" {
			t.Fatalf("unexpected log record %v: body must not be read when not ready", record["msg"])
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan log: %v", err)
	}
}

// Общий журнал запросов также включает request_id.
func TestRequestLogIncludesRequestID(t *testing.T) {
	handler, logBuf := newCapturingHandler(t, 1048576)

	request := httptest.NewRequest(http.MethodGet, "/livez", nil)
	request.Header.Set("X-Request-ID", "livez-check-0001")
	response := httptest.NewRecorder()
	handler.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	record := decodeJSONLine(t, logBuf, "http request completed")
	if record["request_id"] != "livez-check-0001" {
		t.Fatalf("log request_id = %v, want livez-check-0001", record["request_id"])
	}
	if _, ok := record["duration_ms"]; !ok {
		t.Fatal("log record has no duration_ms field")
	}
}
