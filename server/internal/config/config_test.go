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
	cases := map[string]map[string]string{
		"missing postgres": {},
		"bad scheme":       {"OPENAD_POSTGRES_URL": "mysql://db/x"},
		"bad redis":        {"OPENAD_POSTGRES_URL": "postgres://db/x", "OPENAD_REDIS_ADDR": "redis"},
		"bad timeout":      {"OPENAD_POSTGRES_URL": "postgres://db/x", "OPENAD_SHUTDOWN_TIMEOUT": "soon"},
	}
	for name, m := range cases {
		if _, err := load(env(m)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
