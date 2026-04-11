package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const DefaultPath = "config/config.yaml"

type Config struct {
	App      AppConfiguration `yaml:"app"`
	Logger   LoggerConfig     `yaml:"logger"`
	Postgres PostgresConfig   `yaml:"postgres"`
	Redis    RedisConfig      `yaml:"redis"`
}

func Load(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode yaml config: %w", err)
	}
	applyEnvOverrides(&cfg)

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) validate() error {
	if err := c.App.Validate(); err != nil {
		return err
	}
	if err := c.Logger.Validate(); err != nil {
		return err
	}
	if err := c.Postgres.Validate(); err != nil {
		return err
	}
	if err := c.Redis.Validate(); err != nil {
		return err
	}
	return nil
}

func fallbackString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func applyEnvOverrides(cfg *Config) {
	if cfg == nil {
		return
	}

	if value := strings.TrimSpace(os.Getenv("APP_EVENT_RETENTION")); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			cfg.App.EventRetention = duration
		}
	}

	if value := strings.TrimSpace(os.Getenv("APP_SESSION_CHALLENGE_TTL")); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			cfg.App.SessionChallengeTTL = duration
		}
	}

	if value := strings.TrimSpace(os.Getenv("APP_EVENT_BATCH_SIZE")); value != "" {
		if size, err := strconv.Atoi(value); err == nil {
			cfg.App.EventBatchSize = size
		}
	}
}
