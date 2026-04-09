package config

import (
	"fmt"
	"log/slog"
	"strings"
)

type LoggerConfig struct {
	Level         string `yaml:"level"`
	HumanReadable bool   `yaml:"human_readable"`
}

func (l LoggerConfig) SlogLevel() slog.Level {
	switch strings.ToLower(strings.TrimSpace(l.Level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func (l LoggerConfig) Validate() error {
	if strings.TrimSpace(l.Level) == "" {
		return fmt.Errorf("logger.level is required")
	}
	return nil
}
