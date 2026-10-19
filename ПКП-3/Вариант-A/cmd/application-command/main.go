// Команда application-command — точка входа процесса обработки заявок
// учебной системы OrderFlow. Заказ пока не создаётся: каркас задаёт
// жизненный цикл — явную конфигурацию, проверки состояния,
// структурированный журнал и ограниченное ожидание активных запросов
// при остановке.
//
// Разделение main/run определяет границу завершения процесса:
// вся содержательная работа находится в run, отложенные вызовы
// которой выполняются до решения main вызвать os.Exit.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"example.com/orderflow/internal/application"
	"example.com/orderflow/internal/application/memory"
	"example.com/orderflow/internal/platform/config"
	"example.com/orderflow/internal/platform/httpserver"
	"example.com/orderflow/internal/platform/workqueue"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	pingDelay, err := pingDelayFromEnv()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(cfg.LogLevel),
	}))

	rootCtx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	// Единая точка сборки: конкретная реализация Pinger создаётся
	// здесь и передаётся в конструктор обработчика явно.
	repository := memory.NewRepository()
	applications := application.NewService(repository)
	queue, err := workqueue.New(context.Background(), cfg.Workers, cfg.QueueCapacity, func(err error) {
		logger.Error("queued application submission failed", "error", err)
	})
	if err != nil {
		return fmt.Errorf("create work queue: %w", err)
	}
	handler := httpserver.NewHandler(logger, delayPinger{delay: pingDelay}, queue, applications)
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 2 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Канал с буфером 1: сервер публикует единственный результат,
	// даже если координатор уже выбрал ветвь сигнала и не читает.
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server started", "service", cfg.ServiceName, "address", cfg.HTTPAddr)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case err = <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-rootCtx.Done():
		logger.Info("shutdown signal received", "service", cfg.ServiceName, "cause", context.Cause(rootCtx))
	}

	// Сначала прекращаем заявлять готовность, затем закрываем слушатель.
	// Контекст остановки создаётся от context.Background(): отменённый
	// rootCtx сообщил, что остановку пора начинать, и не может
	// одновременно предоставлять время на неё.
	handler.SetReady(false)
	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		cfg.ShutdownTimeout,
	)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	if err := queue.Close(shutdownCtx); err != nil {
		return fmt.Errorf("close work queue: %w", err)
	}
	logger.Info("service stopped", "service", cfg.ServiceName)
	return nil
}

// delayPinger — техническая реализация Pinger для этого каркаса:
// адаптер с настраиваемой задержкой PING_DELAY_MS (0 — мгновенный
// успех). Реализация уважает отмену контекста: select ждёт либо
// истечения задержки, либо ctx.Done(), поэтому производный контекст
// /readyz с пределом 300 мс действительно ограничивает вызов.
type delayPinger struct {
	delay time.Duration
}

func (p delayPinger) Ping(ctx context.Context) error {
	if p.delay <= 0 {
		return nil
	}
	timer := time.NewTimer(p.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// pingDelayFromEnv читает PING_DELAY_MS. Отсутствие и пустое значение
// означают нулевую задержку; некорректное или отрицательное число —
// ошибка запуска.
func pingDelayFromEnv() (time.Duration, error) {
	raw, ok := os.LookupEnv("PING_DELAY_MS")
	if !ok || raw == "" {
		return 0, nil
	}
	ms, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse PING_DELAY_MS: %w", err)
	}
	if ms < 0 {
		return 0, fmt.Errorf("PING_DELAY_MS must be non-negative")
	}
	return time.Duration(ms) * time.Millisecond, nil
}

func parseLevel(value string) slog.Level {
	switch value {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
