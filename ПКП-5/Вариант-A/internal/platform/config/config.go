// Package config читает конфигурацию процесса один раз при запуске.
//
// Конфигурация превращается в обычную структуру и дальше не перечитывается:
// поведение сервиса определяется тем, что было прочитано на старте.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config описывает настройки процесса, проверенные при загрузке.
type Config struct {
	// ServiceName — обязательное имя сервиса. Читается через os.LookupEnv:
	// и отсутствие переменной SERVICE_NAME, и пустое значение приводят
	// к явной ошибке запуска, а не к молчаливой подстановке значения
	// по умолчанию.
	ServiceName string

	HTTPAddr         string
	ShutdownTimeout  time.Duration
	LogLevel         string
	Workers          int
	QueueCapacity    int
	LimitsURL        string
	LimitsTimeout    time.Duration
	LimitsAttempts   int
	LimitsBackoff    time.Duration
	LimitsConcurrent int
}

// Load собирает конфигурацию из окружения.
//
// Ошибка возвращается, а не вызывает panic, потому что некорректное
// окружение — ожидаемая ошибка запуска, а не ошибка в коде.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:         envOr("HTTP_ADDR", ":8080"),
		ShutdownTimeout:  10 * time.Second,
		LogLevel:         envOr("LOG_LEVEL", "info"),
		Workers:          4,
		QueueCapacity:    32,
		LimitsURL:        envOr("LIMITS_URL", "http://127.0.0.1:8081"),
		LimitsTimeout:    800 * time.Millisecond,
		LimitsAttempts:   3,
		LimitsBackoff:    50 * time.Millisecond,
		LimitsConcurrent: 20,
	}

	// SERVICE_NAME: различаем отсутствие переменной и пустое значение.
	// os.Getenv для обоих случаев вернул бы одну и ту же пустую строку,
	// из-за чего пустое значение незаметно превратилось бы в значение
	// по умолчанию; здесь это различие сделано явным.
	name, ok := os.LookupEnv("SERVICE_NAME")
	switch {
	case !ok:
		return Config{}, fmt.Errorf("SERVICE_NAME is required: environment variable is not set")
	case name == "":
		return Config{}, fmt.Errorf("SERVICE_NAME is required: environment variable is set to an empty value")
	}
	cfg.ServiceName = name

	workers, err := positiveIntEnv("WORKERS", cfg.Workers)
	if err != nil {
		return Config{}, err
	}
	cfg.Workers = workers
	queueCapacity, err := nonNegativeIntEnv("QUEUE_CAPACITY", cfg.QueueCapacity)
	if err != nil {
		return Config{}, err
	}
	cfg.QueueCapacity = queueCapacity
	if cfg.LimitsTimeout, err = durationEnv("LIMITS_TIMEOUT", cfg.LimitsTimeout); err != nil {
		return Config{}, err
	}
	if cfg.LimitsBackoff, err = durationEnv("LIMITS_BACKOFF", cfg.LimitsBackoff); err != nil {
		return Config{}, err
	}
	if cfg.LimitsAttempts, err = positiveIntEnv("LIMITS_ATTEMPTS", cfg.LimitsAttempts); err != nil {
		return Config{}, err
	}
	if cfg.LimitsConcurrent, err = positiveIntEnv("LIMITS_CONCURRENT", cfg.LimitsConcurrent); err != nil {
		return Config{}, err
	}

	if raw := os.Getenv("SHUTDOWN_TIMEOUT"); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("parse SHUTDOWN_TIMEOUT: %w", err)
		}
		cfg.ShutdownTimeout = value
	}

	// Проверка на положительность охватывает и значение по умолчанию.
	// Нулевой бюджет означал бы отсутствие времени на ожидание
	// активного запроса.
	if cfg.ShutdownTimeout <= 0 {
		return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT must be positive")
	}
	return cfg, nil
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return value, nil
}

func positiveIntEnv(name string, fallback int) (int, error) {
	value, err := nonNegativeIntEnv(name, fallback)
	if err != nil {
		return 0, err
	}
	if value == 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return value, nil
}

func nonNegativeIntEnv(name string, fallback int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s must be non-negative", name)
	}
	return value, nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
