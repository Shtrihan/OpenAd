// Package config loads ad server settings from environment variables.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"
)

// Config holds all runtime settings. Every field maps to one OPENAD_* variable.
type Config struct {
	HTTPAddr        string        // OPENAD_HTTP_ADDR
	PostgresURL     string        // OPENAD_POSTGRES_URL
	ClickHouseAddr  string        // OPENAD_CLICKHOUSE_ADDR, native protocol host:port
	ClickHouseDB    string        // OPENAD_CLICKHOUSE_DB
	ClickHouseUser  string        // OPENAD_CLICKHOUSE_USER
	ClickHousePass  string        // OPENAD_CLICKHOUSE_PASSWORD
	RedisAddr       string        // OPENAD_REDIS_ADDR
	RedisPassword   string        // OPENAD_REDIS_PASSWORD
	ShutdownTimeout time.Duration // OPENAD_SHUTDOWN_TIMEOUT

	IPHashKey     string // OPENAD_IP_HASH_KEY, secret for hashing client IPs
	TrustProxy    bool   // OPENAD_TRUST_PROXY, read client IP from X-Forwarded-For
	CountryHeader string // OPENAD_GEO_COUNTRY_HEADER, e.g. CF-IPCountry
	GeoIPDB       string // OPENAD_GEOIP_DB, path to a MaxMind-format country database
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
		ClickHouseDB:   get("OPENAD_CLICKHOUSE_DB", "openad"),
		ClickHouseUser: get("OPENAD_CLICKHOUSE_USER", "default"),
		ClickHousePass: getenv("OPENAD_CLICKHOUSE_PASSWORD"),
		RedisAddr:      get("OPENAD_REDIS_ADDR", "localhost:6379"),
		RedisPassword:  getenv("OPENAD_REDIS_PASSWORD"),
		IPHashKey:      getenv("OPENAD_IP_HASH_KEY"),
		CountryHeader:  getenv("OPENAD_GEO_COUNTRY_HEADER"),
		GeoIPDB:        getenv("OPENAD_GEOIP_DB"),
	}

	if v := getenv("OPENAD_TRUST_PROXY"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("OPENAD_TRUST_PROXY: %w", err)
		}
		c.TrustProxy = b
	}
	if len(c.IPHashKey) < 16 {
		return Config{}, fmt.Errorf("OPENAD_IP_HASH_KEY must be at least 16 characters")
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
