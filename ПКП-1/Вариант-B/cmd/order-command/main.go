// Команда order-command — точка входа процесса приёма пакетных задач
// учебной системы OrderFlow. Очередь задач пока не создаётся: каркас
// задаёт жизненный цикл — явную конфигурацию, проверки состояния,
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
	"syscall"
	"time"

	"example.com/orderflow/internal/platform/config"
	"example.com/orderflow/internal/platform/httpserver"
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

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(cfg.LogLevel),
	}))

	rootCtx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	// Единая точка сборки: конфигурация прочитана в run и передана
	// явными параметрами — предел размера тела виден в конструкторе
	// и заменяется в тесте.
	handler := httpserver.NewHandler(logger, cfg.MaxBodyBytes)
	server := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: handler.Routes(),
		// ReadHeaderTimeout ограничивает получение заголовков,
		// IdleTimeout — ожидание следующего запроса на простаивающем
		// соединении. Общего ограничения времени чтения тела
		// (ReadTimeout) в этом каркасе нет — см. раздел 2.6 практики.
		ReadHeaderTimeout: 2 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Канал с буфером 1: сервер публикует единственный результат,
	// даже если координатор уже выбрал ветвь сигнала и не читает.
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server started",
			"address", cfg.HTTPAddr,
			"max_body_bytes", cfg.MaxBodyBytes,
		)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case err = <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-rootCtx.Done():
		logger.Info("shutdown signal received", "cause", context.Cause(rootCtx))
	}

	// Сначала прекращаем заявлять готовность (допущенные запросы
	// продолжают работу: их статус решён в точке принятия операции),
	// затем закрываем слушатель. Контекст остановки создаётся от
	// context.Background(): отменённый rootCtx сообщил, что остановку
	// пора начинать, и не может одновременно предоставлять время на неё.
	handler.SetReady(false)
	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		cfg.ShutdownTimeout,
	)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		// Ошибка ожидания: активное чтение продолжается. В реальном
		// развёртывании именно выход процесса после этой ошибки
		// завершает незавершённую медленную загрузку тела.
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	logger.Info("service stopped")
	return nil
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
