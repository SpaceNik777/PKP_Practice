package httpserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

// slowBodyReader отдаёт данные порциями с паузами: модель клиента,
// передающего тело медленно. Размер порции и пауза задают фактическую
// скорость передачи независимо от TCP.
type slowBodyReader struct {
	data   []byte
	chunk  int
	pause  time.Duration
	offset int
}

func (r *slowBodyReader) Read(p []byte) (int, error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	time.Sleep(r.pause)
	end := r.offset + r.chunk
	if end > len(r.data) {
		end = len(r.data)
	}
	n := copy(p, r.data[r.offset:end])
	r.offset = end
	return n, nil
}

// slowUploadServer — тестовый стенд: настоящий net.Listener на
// 127.0.0.1:0 и сервер в горутине. Обёртка сигнализирует о входе
// запроса /jobs до передачи управления маршрутизатору, позволяя
// тесту узнать, что запрос принят соединением.
type slowUploadServer struct {
	handler     *Handler
	server      *http.Server
	baseURL     string
	jobsStarted chan struct{}
}

func newSlowUploadServer(t *testing.T, maxBodyBytes int64) *slowUploadServer {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewHandler(logger, maxBodyBytes)

	jobsStarted := make(chan struct{}, 1)
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/jobs" {
			jobsStarted <- struct{}{}
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

	return &slowUploadServer{
		handler:     h,
		server:      srv,
		baseURL:     "http://" + ln.Addr().String(),
		jobsStarted: jobsStarted,
	}
}

type clientOutcome struct {
	resp *http.Response
	err  error
}

// startSlowJobsClient отправляет POST /jobs настоящим http.Client
// с медленным телом. Возврат управление немедленный; единственный
// результат публикуется в канал с буфером 1.
func startSlowJobsClient(baseURL string, ctx context.Context, payload []byte, chunk int, pause time.Duration) <-chan clientOutcome {
	out := make(chan clientOutcome, 1)
	go func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/jobs",
			io.NopCloser(&slowBodyReader{data: payload, chunk: chunk, pause: pause}))
		if err != nil {
			out <- clientOutcome{err: err}
			return
		}
		req.ContentLength = int64(len(payload))
		// POST с телом без GetBody не повторяется транспортом.
		resp, err := (&http.Client{}).Do(req)
		out <- clientOutcome{resp: resp, err: err}
	}()
	return out
}

// waitForJobsStart дожидается сигнала о приёме запроса /jobs
// соединением сервера.
func (s *slowUploadServer) waitForJobsStart(t *testing.T) {
	t.Helper()
	select {
	case <-s.jobsStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("jobs request did not start within 5s")
	}
}

// waitUntilAdmitted даёт обработчику время дойти до точки принятия
// операции и заблокироваться в чтении тела, и убеждается, что быстрого
// отказа (503/413) не произошло: пустой канал результата подтверждает,
// запрос допущен и продолжает читаться.
func waitUntilAdmitted(t *testing.T, respCh <-chan clientOutcome) {
	t.Helper()
	time.Sleep(250 * time.Millisecond)
	select {
	case o := <-respCh:
		t.Fatalf("request finished prematurely (resp=%v err=%v): admission or limit must not reject it", o.resp, o.err)
	default:
	}
}

// Сценарий 1 (опыт остановки во время медленной загрузки).
//
// Тело 2048 байт НЕ превышает MAX_BODY_BYTES=4096, но передаётся
// порциями по 16 байт с паузой 100 мс: полная передача заняла бы
// ~12.8 с — много больше бюджета остановки 1 с.
//
// Ожидание: Shutdown возвращает context.DeadlineExceeded — ожидание
// не закончилось, пока клиент не завершил передачу. Клиент, не
// превышающий размер и не завершающий передачу, MaxBytesReader
// сам не останавливает: предел объёма не является сроком чтения.
//
// Какой механизм завершил чтение: в тесте — отмена клиентского
// контекста самим тестом; в реальном развёртывании — выход процесса
// после ошибки Shutdown (run возвращает ошибку, main вызывает os.Exit).
func TestShutdownTimesOutDuringSlowBodyUpload(t *testing.T) {
	s := newSlowUploadServer(t, 4096)

	payload := bytes.Repeat([]byte("x"), 2048)
	clientCtx, cancelClient := context.WithCancel(context.Background())
	defer cancelClient()
	respCh := startSlowJobsClient(s.baseURL, clientCtx, payload, 16, 100*time.Millisecond)

	s.waitForJobsStart(t)
	waitUntilAdmitted(t, respCh)

	// Точка принятия операции уже пройдена при готовом флаге:
	// смена флага позже статус допущенного запроса не меняет.
	s.handler.SetReady(false)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := s.server.Shutdown(shutdownCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want context.DeadlineExceeded", err)
	}

	// Бюджет исчерпан, а запрос всё ещё читается: канал результата
	// пуст, MaxBytesReader чтение не прекратил (размер не превышен).
	select {
	case o := <-respCh:
		t.Fatalf("client finished during shutdown budget (resp=%v err=%v): slow upload must still be in progress", o.resp, o.err)
	case <-time.After(300 * time.Millisecond):
	}

	// Зафиксировано: чтение завершает отмена клиентского контекста
	// тестом (модель выхода процесса после ошибки Shutdown в реальном
	// развёртывании — Shutdown соединения сам не рвёт).
	t.Log("reading was terminated by test-side client cancellation; in a real deployment — by process exit after the Shutdown error")
	cancelClient()

	// Уборка стенда — обязанность теста: Shutdown после ошибки
	// активные соединения не закрывает.
	if err := s.server.Close(); err != nil {
		t.Fatalf("force close after failed shutdown: %v", err)
	}
}

// Сценарий 2 (сопоставление размера, скорости и бюджета).
//
// Тело 80 байт передаётся 10 порциями по 8 байт с паузой 150 мс:
// полная передача ~1.5 с. Бюджет остановки 5 с покрывает её с запасом,
// поэтому Shutdown завершается без ошибки, а допущенный запрос
// успевает получить 202 Accepted уже во время остановки.
func TestShutdownWaitsForSlowBodyUploadCompletion(t *testing.T) {
	s := newSlowUploadServer(t, 4096)

	payload := bytes.Repeat([]byte("y"), 80)
	respCh := startSlowJobsClient(s.baseURL, context.Background(), payload, 8, 150*time.Millisecond)

	s.waitForJobsStart(t)
	waitUntilAdmitted(t, respCh)

	s.handler.SetReady(false)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.server.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown error = %v, want nil (budget covers the slow upload)", err)
	}

	outcome := <-respCh
	if outcome.err != nil {
		t.Fatalf("client error: %v", outcome.err)
	}
	defer outcome.resp.Body.Close()
	if outcome.resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", outcome.resp.StatusCode, http.StatusAccepted)
	}
}
