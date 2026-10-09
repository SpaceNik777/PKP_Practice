package config

import (
	"strings"
	"testing"
)

// TestLoadRejectsInvalidShutdownTimeout — базовый тест практики:
// SHUTDOWN_TIMEOUT=tomorrow не проходит разбор и отвергается.
func TestLoadRejectsInvalidShutdownTimeout(t *testing.T) {
	t.Setenv("SHUTDOWN_TIMEOUT", "tomorrow")

	_, err := Load()
	if err == nil {
		t.Fatal("expected configuration error")
	}
}

// TestLoadDefaults: без переменных используется значение MAX_BODY_BYTES
// по умолчанию — 1 MiB = 1048576 байт.
func TestLoadDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MaxBodyBytes != DefaultMaxBodyBytes {
		t.Fatalf("MaxBodyBytes = %d, want default %d", cfg.MaxBodyBytes, DefaultMaxBodyBytes)
	}
	if cfg.MaxBodyBytes != 1048576 {
		t.Fatalf("default MaxBodyBytes = %d, want 1 MiB = 1048576", cfg.MaxBodyBytes)
	}
}

// Успешный случай: корректное положительное число разбирается.
func TestLoadParsesMaxBodyBytes(t *testing.T) {
	t.Setenv("MAX_BODY_BYTES", "2048")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MaxBodyBytes != 2048 {
		t.Fatalf("MaxBodyBytes = %d, want 2048", cfg.MaxBodyBytes)
	}
}

// Класс 1: синтаксическая ошибка — значение не является числом.
func TestMaxBodyBytesSyntaxError(t *testing.T) {
	_, err := parseMaxBodyBytes("not-a-number")
	if err == nil {
		t.Fatal("expected syntax error")
	}
	if !strings.Contains(err.Error(), "is not a number") {
		t.Fatalf("error = %v, want syntax error message", err)
	}
}

// Класс 2: неположительное значение — разбирается, но <= 0.
func TestMaxBodyBytesNonPositive(t *testing.T) {
	for _, raw := range []string{"0", "-1"} {
		_, err := parseMaxBodyBytes(raw)
		if err == nil {
			t.Fatalf("MAX_BODY_BYTES=%q: expected error", raw)
		}
		if !strings.Contains(err.Error(), "must be positive") {
			t.Fatalf("MAX_BODY_BYTES=%q: error = %v, want positive-value message", raw, err)
		}
	}
}

// Класс 3: выход за диапазон int64 (max int64 + 1).
func TestMaxBodyBytesOutOfRange(t *testing.T) {
	_, err := parseMaxBodyBytes("9223372036854775808")
	if err == nil {
		t.Fatal("expected out-of-range error")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("error = %v, want out-of-range message", err)
	}
}

// Полный путь Load также отвергает некорректное MAX_BODY_BYTES
// (а не только внутренний разбор).
func TestLoadRejectsInvalidMaxBodyBytes(t *testing.T) {
	t.Setenv("MAX_BODY_BYTES", "12abc")

	_, err := Load()
	if err == nil {
		t.Fatal("expected configuration error")
	}
}
