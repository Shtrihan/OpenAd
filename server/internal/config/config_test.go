package config

import (
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	c, err := load(env(map[string]string{
		"OPENAD_POSTGRES_URL": "postgres://u:p@db/openad?sslmode=disable",
		"OPENAD_IP_HASH_KEY":  "0123456789abcdef",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPAddr != ":8080" || c.RedisAddr != "localhost:6379" || c.ShutdownTimeout != 15*time.Second {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	addr, err := c.PostgresAddr()
	if err != nil || addr != "db:5432" {
		t.Fatalf("PostgresAddr() = %q, %v", addr, err)
	}
}

func TestLoadErrors(t *testing.T) {
	const key = "0123456789abcdef"
	cases := map[string]map[string]string{
		"missing postgres": {"OPENAD_IP_HASH_KEY": key},
		"bad scheme":       {"OPENAD_IP_HASH_KEY": key, "OPENAD_POSTGRES_URL": "mysql://db/x"},
		"bad redis":        {"OPENAD_IP_HASH_KEY": key, "OPENAD_POSTGRES_URL": "postgres://db/x", "OPENAD_REDIS_ADDR": "redis"},
		"bad timeout":      {"OPENAD_IP_HASH_KEY": key, "OPENAD_POSTGRES_URL": "postgres://db/x", "OPENAD_SHUTDOWN_TIMEOUT": "soon"},
		"short ip key":     {"OPENAD_IP_HASH_KEY": "short", "OPENAD_POSTGRES_URL": "postgres://db/x"},
		"bad trust proxy":  {"OPENAD_IP_HASH_KEY": key, "OPENAD_POSTGRES_URL": "postgres://db/x", "OPENAD_TRUST_PROXY": "maybe"},
	}
	for name, m := range cases {
		if _, err := load(env(m)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
