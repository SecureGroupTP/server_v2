package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type PostgresConfig struct {
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	Database        string `yaml:"database"`
	Username        string `yaml:"username"`
	Password        string `yaml:"password"`
	SSLMode         string `yaml:"ssl_mode"`
	ConnectTimeoutS int    `yaml:"connect_timeout_seconds"`
}

func (p PostgresConfig) DSN() string {
	q := url.Values{}
	q.Set("sslmode", fallbackString(p.SSLMode, "disable"))
	if p.ConnectTimeoutS > 0 {
		q.Set("connect_timeout", strconv.Itoa(p.ConnectTimeoutS))
	}

	host := fallbackString(p.Host, "localhost")
	port := p.Port
	if port == 0 {
		port = 5432
	}

	database := fallbackString(p.Database, "postgres")

	userInfo := url.User(p.Username)
	if p.Password != "" {
		userInfo = url.UserPassword(p.Username, p.Password)
	}

	u := &url.URL{
		Scheme:   "postgres",
		User:     userInfo,
		Host:     fmt.Sprintf("%s:%d", host, port),
		Path:     database,
		RawQuery: q.Encode(),
	}

	return u.String()
}

func (p PostgresConfig) Validate() error {
	if strings.TrimSpace(p.Host) == "" {
		return fmt.Errorf("postgres.host is required")
	}
	if p.Port <= 0 {
		return fmt.Errorf("postgres.port must be > 0")
	}
	if strings.TrimSpace(p.Database) == "" {
		return fmt.Errorf("postgres.database is required")
	}
	if strings.TrimSpace(p.Username) == "" {
		return fmt.Errorf("postgres.username is required")
	}
	return nil
}
