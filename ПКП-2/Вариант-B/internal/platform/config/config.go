// Package config читает окружение процесса один раз при входе в run
// и превращает его в обычную структуру. Дальше каркас не перечитывает
// окружение для каждого запроса: поведение сервиса определяется тем,
// что было прочитано на старте.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// DefaultMaxBodyBytes — предел размера тела запроса по умолчанию:
// 1 MiB = 1048576 байт. Значение выбрано явно: оно с запасом вмещает
// учебные пакетные задания и при этом ограничивает расход памяти
// на чтение одного тела.
const DefaultMaxBodyBytes int64 = 1 << 20

// Config — конфигурация процесса. Успешный результат Load гарантирует
// осмысленные значения; сама структура с экспортируемыми полями
// неизменяемой не является.
type Config struct {
	HTTPAddr        string
	ShutdownTimeout time.Duration
	LogLevel        string
	MaxBodyBytes    int64
}

// Load собирает конфигурацию из окружения. Некорректные значения —
// ожидаемая ошибка запуска, поэтому возвращается ошибка, а не вызывается
// panic: решение о завершении процесса принимает main.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:        envOr("HTTP_ADDR", ":8080"),
		ShutdownTimeout: 10 * time.Second,
		LogLevel:        envOr("LOG_LEVEL", "info"),
		MaxBodyBytes:    DefaultMaxBodyBytes,
	}

	if raw := os.Getenv("SHUTDOWN_TIMEOUT"); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("parse SHUTDOWN_TIMEOUT: %w", err)
		}
		cfg.ShutdownTimeout = value
	}

	if cfg.ShutdownTimeout <= 0 {
		return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT must be positive")
	}

	if raw := os.Getenv("MAX_BODY_BYTES"); raw != "" {
		value, err := parseMaxBodyBytes(raw)
		if err != nil {
			return Config{}, err
		}
		cfg.MaxBodyBytes = value
	}

	return cfg, nil
}

// parseMaxBodyBytes разбирает MAX_BODY_BYTES и различает три
// самостоятельных класса отказа с отдельными понятными сообщениями:
//
//  1. синтаксическая ошибка — значение вообще не является числом;
//  2. неположительное значение — число разобрано, но <= 0;
//  3. выход за диапазон числового типа — число синтаксически корректно,
//     но не помещается в int64.
//
// Это разные ветви разбора, а не одна общая ошибка: оператору нужны
// разные действия (исправить опечатку, указать положительный размер,
// выбрать значение в допустимом диапазоне).
func parseMaxBodyBytes(raw string) (int64, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		var numErr *strconv.NumError
		if errors.As(err, &numErr) && errors.Is(numErr.Err, strconv.ErrRange) {
			return 0, fmt.Errorf("parse MAX_BODY_BYTES: value %q is out of range for int64", raw)
		}
		return 0, fmt.Errorf("parse MAX_BODY_BYTES: value %q is not a number", raw)
	}
	if value <= 0 {
		return 0, fmt.Errorf("MAX_BODY_BYTES must be positive, got %d", value)
	}
	return value, nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
