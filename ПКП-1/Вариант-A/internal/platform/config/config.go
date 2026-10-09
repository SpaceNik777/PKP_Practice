// Package config читает конфигурацию процесса один раз при запуске.
//
// Конфигурация превращается в обычную структуру и дальше не перечитывается:
// поведение сервиса определяется тем, что было прочитано на старте.
package config

import (
	"fmt"
	"os"
	"time"
)

// Config описывает настройки процесса, проверенные при загрузке.
type Config struct {
	// ServiceName — обязательное имя сервиса. Читается через os.LookupEnv:
	// и отсутствие переменной SERVICE_NAME, и пустое значение приводят
	// к явной ошибке запуска, а не к молчаливой подстановке значения
	// по умолчанию.
	ServiceName string

	HTTPAddr        string
	ShutdownTimeout time.Duration
	LogLevel        string
}

// Load собирает конфигурацию из окружения.
//
// Ошибка возвращается, а не вызывает panic, потому что некорректное
// окружение — ожидаемая ошибка запуска, а не ошибка в коде.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:        envOr("HTTP_ADDR", ":8080"),
		ShutdownTimeout: 10 * time.Second,
		LogLevel:        envOr("LOG_LEVEL", "info"),
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

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
