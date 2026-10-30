// Package config loads ad server settings from environment variables.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"time"
)

// Config holds all runtime settings. Every field maps to one OPENAD_* variable.
type Config struct {
	HTTPAddr        string        // OPENAD_HTTP_ADDR
	PostgresURL     string        // OPENAD_POSTGRES_URL
	ClickHouseAddr  string        // OPENAD_CLICKHOUSE_ADDR, native protocol host:port
	RedisAddr       string        // OPENAD_REDIS_ADDR
	ShutdownTimeout time.Duration // OPENAD_SHUTDOWN_TIMEOUT
}

// Load reads the environment and validates the result.
func Load() (Config, error) {
	return load(os.Getenv)
}

func load(getenv func(string) string) (Config, error) {
	get := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return def
	}

	c := Config{
		HTTPAddr:       get("OPENAD_HTTP_ADDR", ":8080"),
		PostgresURL:    getenv("OPENAD_POSTGRES_URL"),
		ClickHouseAddr: get("OPENAD_CLICKHOUSE_ADDR", "localhost:9000"),
		RedisAddr:      get("OPENAD_REDIS_ADDR", "localhost:6379"),
	}

	timeout, err := time.ParseDuration(get("OPENAD_SHUTDOWN_TIMEOUT", "15s"))
	if err != nil {
		return Config{}, fmt.Errorf("OPENAD_SHUTDOWN_TIMEOUT: %w", err)
	}
	c.ShutdownTimeout = timeout

	if c.PostgresURL == "" {
		return Config{}, fmt.Errorf("OPENAD_POSTGRES_URL is required")
	}
	if _, err := c.PostgresAddr(); err != nil {
		return Config{}, err
	}
	for key, addr := range map[string]string{
		"OPENAD_CLICKHOUSE_ADDR": c.ClickHouseAddr,
		"OPENAD_REDIS_ADDR":      c.RedisAddr,
	} {
		if _, _, err := net.SplitHostPort(addr); err != nil {
			return Config{}, fmt.Errorf("%s: %w", key, err)
		}
	}
	return c, nil
}

// PostgresAddr returns host:port from PostgresURL, defaulting the port to 5432.
func (c Config) PostgresAddr() (string, error) {
	u, err := url.Parse(c.PostgresURL)
	if err != nil {
		return "", fmt.Errorf("OPENAD_POSTGRES_URL: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("OPENAD_POSTGRES_URL: unsupported scheme %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("OPENAD_POSTGRES_URL: missing host")
	}
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	return net.JoinHostPort(u.Hostname(), port), nil
}
