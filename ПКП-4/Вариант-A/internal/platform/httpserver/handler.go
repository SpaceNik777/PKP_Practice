// Package httpserver содержит транспортные механизмы процесса:
// маршруты проверок состояния, промежуточные обработчики
// и зависимость проверки готовности от адаптера.
package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"example.com/orderflow/internal/application"
	"example.com/orderflow/internal/platform/workqueue"
)

// Pinger — контракт обязательного адаптера для проверки готовности.
//
// Реализация обязана уважать отмену контекста: Shutdown-подобные
// ограничения работают только тогда, когда вызов адаптера кооперативно
// завершается по ctx.Done(). Запуск игнорирующего контекст вызова
// в отдельной горутине со сменой ожидания по таймеру сам вызов
// не останавливает и создаёт утечку работы.
type Pinger interface {
	Ping(ctx context.Context) error
}

// pingTimeout — бюджет проверки адаптера в /readyz.
const pingTimeout = 300 * time.Millisecond

// Handler — корневой обработчик HTTP с локальным флагом готовности.
//
// Поле ready имеет тип atomic.Bool: обработчики читают его, координатор
// остановки изменяет. Такой объект нельзя копировать, поэтому Handler
// создаётся и передаётся по указателю.
type Handler struct {
	logger *slog.Logger
	ready  atomic.Bool
	pinger Pinger
	queue  *workqueue.Queue
	apps   *application.Service
}

// NewHandler создаёт обработчик. Реализация Pinger передаётся явно:
// в production это адаптер внешней системы, в тестах — подставной объект.
//
// Конструктор сразу выполняет Store(true) — учебное упрощение,
// оправданное отсутствием в этом каркасе внешних зависимостей,
// кроме передаваемого Pinger.
func NewHandler(logger *slog.Logger, pinger Pinger, dependencies ...any) *Handler {
	h := &Handler{
		logger: logger,
		pinger: pinger,
	}
	for _, dependency := range dependencies {
		switch value := dependency.(type) {
		case *workqueue.Queue:
			h.queue = value
		case *application.Service:
			h.apps = value
		}
	}
	h.ready.Store(true)
	return h
}

// SetReady изменяет локальный флаг готовности.
func (h *Handler) SetReady(value bool) {
	h.ready.Store(value)
}

// Routes собирает мультиплексор и оборачивает его промежуточными
// обработчиками. Порядок важен: requestID — внешний, поэтому журнал,
// находящийся внутри, уже видит идентификатор в контексте запроса.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", h.livez)
	mux.HandleFunc("GET /readyz", h.readyz)
	mux.HandleFunc("GET /slow", h.slow)
	mux.HandleFunc("POST /applications/{id}/submit", h.submitApplication)
	return h.requestID(h.requestLog(mux))
}

func (h *Handler) submitApplication(w http.ResponseWriter, r *http.Request) {
	if h.queue == nil || h.apps == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "submission unavailable"})
		return
	}
	id := r.PathValue("id")
	err := h.queue.Submit(r.Context(), func(ctx context.Context) error {
		_, err := h.apps.Submit(ctx, id)
		return err
	})
	switch {
	case err == nil:
		writeJSON(w, http.StatusAccepted, map[string]string{"id": id, "status": "accepted"})
	case errors.Is(err, workqueue.ErrFull):
		w.Header().Set("Retry-After", "1")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "queue full"})
	case errors.Is(err, workqueue.ErrClosed):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "shutting down"})
	default:
		writeJSON(w, http.StatusRequestTimeout, map[string]string{"error": err.Error()})
	}
}

// livez — проверка живости: успешный ответ доказывает лишь то,
// что процесс не заблокирован полностью и способен обслужить
// простейший запрос. Внешние зависимости здесь не проверяются.
func (h *Handler) livez(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
}

// readyz — проверка готовности: следует ли направлять в этот процесс
// новую работу. Отрицательный ответ имеет несколько причин, поэтому
// различаются статусы в теле:
//
//   - "draining" — локальный флаг выключен (начало остановки).
//     Проверка флага выполняется первой: успешный Ping не может
//     вернуть в готовность процесс, начавший остановку;
//   - "not_ready" — локальный флаг включён, но адаптер не ответил
//     успешно в пределах 300 мс.
//
// Общее имя "draining" для всех причин неготовности не подходило бы:
// оно описывает только остановку процесса и вводило бы в заблуждение
// при временном отказе адаптера.
func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	if !h.ready.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "draining"})
		return
	}

	pingCtx, cancel := context.WithTimeout(r.Context(), pingTimeout)
	defer cancel()

	if err := h.pinger.Ping(pingCtx); err != nil {
		requestID, _ := RequestIDFromContext(r.Context())
		h.logger.WarnContext(r.Context(), "readiness ping failed",
			"request_id", requestID,
			"error", err,
		)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// slow моделирует длительную операцию: одна ветвь select завершает
// его примерно через три секунды, другая реагирует на отмену
// контекста запроса. Сигнал остановки процесса этот контекст
// напрямую не отменяет, что и позволяет /slow закончить работу
// во время Shutdown.
func (h *Handler) slow(w http.ResponseWriter, r *http.Request) {
	select {
	case <-time.After(3 * time.Second):
		writeJSON(w, http.StatusOK, map[string]string{"status": "completed"})
	case <-r.Context().Done():
		requestID, _ := RequestIDFromContext(r.Context())
		h.logger.InfoContext(r.Context(), "slow request cancelled",
			"request_id", requestID,
			"error", context.Cause(r.Context()),
		)
	}
}

// requestIDKey — непубличный тип для ключа контекста, чтобы значения
// этого пакета не могли быть прочитаны или перезаписаны извне.
type requestIDKey struct{}

// RequestIDFromContext возвращает идентификатор запроса, помещённый
// промежуточным обработчиком requestID. Второе значение сообщает,
// был ли идентификатор установлен.
func RequestIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(requestIDKey{}).(string)
	return id, ok
}

// requestID — промежуточный обработчик идентификатора запроса.
//
// Входящий заголовок X-Request-ID принимается только при соответствии
// контракту приложения: длина от 8 до 64 символов, символы A–Z, a–z,
// 0–9, дефис и подчёркивание. Неверное или неоднозначное значение
// не переносится в ответ и журнал — вместо него создаётся новый
// идентификатор (crypto/rand, 16 байт, hex — 32 символа).
// Итоговое значение возвращается клиенту заголовком X-Request-ID
// и передаётся далее через context.WithValue.
func (h *Handler) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !validRequestID(id) {
			generated, err := newRequestID()
			if err != nil {
				// Отказ crypto/rand — исключительное состояние;
				// продолжать без идентификатора нельзя, поэтому
				// запрос завершается ошибкой.
				h.logger.ErrorContext(r.Context(), "generate request id", "error", err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			id = generated
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// validRequestID проверяет соответствие идентификатора контракту.
// Проверка идёт по байтам: все допустимые символы — ASCII,
// многобайтовые последовательности отвергаются.
func validRequestID(id string) bool {
	if len(id) < 8 || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}

// newRequestID генерирует идентификатор из 16 случайных байт
// в hex-кодировке (32 символа, соответствует контракту).
func newRequestID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// requestLog записывает событие завершения вызова следующего
// обработчика. Имя события читается буквально: оно не доказывает,
// что клиент получил весь ответ. Идентификатор запроса попадает
// в запись из контекста, поэтому requestLog должен находиться
// внутри requestID.
func (h *Handler) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		id, _ := RequestIDFromContext(r.Context())
		h.logger.InfoContext(r.Context(), "http request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"request_id", id,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	})
}

// writeJSON устанавливает Content-Type до записи статуса: после
// WriteHeader заголовки менять поздно. Кодировщик дописывает
// перевод строки, что учитывается в тестах разбором JSON,
// а не сравнением строк.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
