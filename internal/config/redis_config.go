package config

import (
	"fmt"
	"strings"
)

type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Database int    `yaml:"database"`
	Password string `yaml:"password"`
}

func (r RedisConfig) Addr() string {
	host := fallbackString(r.Host, "localhost")
	port := r.Port
	if port == 0 {
		port = 6379
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func (r RedisConfig) URL() string {
	auth := ""
	if r.Password != "" {
		auth = ":" + r.Password + "@"
	}
	return fmt.Sprintf("redis://%s%s/%d", auth, r.Addr(), r.Database)
}

func (r RedisConfig) Validate() error {
	if strings.TrimSpace(r.Host) == "" {
		return fmt.Errorf("redis.host is required")
	}
	if r.Port <= 0 {
		return fmt.Errorf("redis.port must be > 0")
	}
	if r.Database < 0 {
		return fmt.Errorf("redis.database must be >= 0")
	}
	return nil
}
