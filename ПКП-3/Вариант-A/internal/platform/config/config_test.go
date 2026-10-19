package config

import (
	"os"
	"testing"
	"time"
)

// TestLoadRejectsInvalidShutdownTimeout — базовый тест из практики:
// строка "tomorrow" не проходит разбор time.ParseDuration.
func TestLoadRejectsInvalidShutdownTimeout(t *testing.T) {
	t.Setenv("SERVICE_NAME", "application-command")
	t.Setenv("SHUTDOWN_TIMEOUT", "tomorrow")

	_, err := Load()
	if err == nil {
		t.Fatal("expected configuration error")
	}
}

// TestLoadRequiresServiceNameAbsent: переменная SERVICE_NAME отсутствует —
// запуск должен завершиться явной ошибкой, а не значением по умолчанию.
func TestLoadRequiresServiceNameAbsent(t *testing.T) {
	// t.Setenv гарантирует восстановление исходного состояния окружения,
	// но сама требует задать значение, поэтому сначала фиксируем
	// переменную, затем снимаем её до конца теста.
	t.Setenv("SERVICE_NAME", "")
	if err := os.Unsetenv("SERVICE_NAME"); err != nil {
		t.Fatalf("unset SERVICE_NAME: %v", err)
	}

	_, err := Load()
	if err == nil {
		t.Fatal("expected configuration error for absent SERVICE_NAME")
	}
}

// TestLoadRejectsEmptyServiceName: SERVICE_NAME задана, но пустая —
// это другое состояние, чем отсутствие, и оно тоже должно отвергаться.
func TestLoadRejectsEmptyServiceName(t *testing.T) {
	t.Setenv("SERVICE_NAME", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected configuration error for empty SERVICE_NAME")
	}
}

// TestLoadAcceptsValidServiceName: корректное значение загружается в поле.
func TestLoadAcceptsValidServiceName(t *testing.T) {
	t.Setenv("SERVICE_NAME", "application-command")
	t.Setenv("SHUTDOWN_TIMEOUT", "5s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ServiceName != "application-command" {
		t.Fatalf("ServiceName = %q, want %q", cfg.ServiceName, "application-command")
	}
	if cfg.ShutdownTimeout != 5*time.Second {
		t.Fatalf("ShutdownTimeout = %v, want %v", cfg.ShutdownTimeout, 5*time.Second)
	}
}

func TestLoadWorkersAndQueueCapacity(t *testing.T) {
	t.Setenv("SERVICE_NAME", "application-command")
	t.Setenv("WORKERS", "8")
	t.Setenv("QUEUE_CAPACITY", "64")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Workers != 8 || cfg.QueueCapacity != 64 {
		t.Fatalf("Workers = %d, QueueCapacity = %d", cfg.Workers, cfg.QueueCapacity)
	}
}

func TestLoadRejectsInvalidWorkers(t *testing.T) {
	t.Setenv("SERVICE_NAME", "application-command")
	t.Setenv("WORKERS", "0")
	if _, err := Load(); err == nil {
		t.Fatal("expected WORKERS validation error")
	}
}

func TestLoadAcceptsWorkersUpperBound(t *testing.T) {
	t.Setenv("SERVICE_NAME", "application-command")
	t.Setenv("WORKERS", "64")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Workers != 64 {
		t.Fatalf("Workers = %d, want 64", cfg.Workers)
	}
}

func TestLoadRejectsWorkersAboveUpperBound(t *testing.T) {
	t.Setenv("SERVICE_NAME", "application-command")
	t.Setenv("WORKERS", "65")
	if _, err := Load(); err == nil {
		t.Fatal("expected WORKERS upper-bound validation error")
	}
}

func TestLoadRejectsNegativeQueueCapacity(t *testing.T) {
	t.Setenv("SERVICE_NAME", "application-command")
	t.Setenv("QUEUE_CAPACITY", "-1")
	if _, err := Load(); err == nil {
		t.Fatal("expected QUEUE_CAPACITY validation error")
	}
}
