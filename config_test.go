package verexa

import (
	"strings"
	"testing"
)

func clearEnv(t *testing.T) {
	for _, name := range []string{"VEREXA_API_KEY", "VEREXA_BASE_URL", "GUARD_API_KEY", "GUARD_API_BASE_URL"} {
		t.Setenv(name, "")
	}
}

func TestConfigReadsEnvironment(t *testing.T) {
	clearEnv(t)
	t.Setenv("VEREXA_API_KEY", "vx_test_env")
	t.Setenv("VEREXA_BASE_URL", "https://api.verexa.example")

	cfg := ResolveConfig(Config{})
	if cfg.APIKey != "vx_test_env" || cfg.BaseURL != "https://api.verexa.example" {
		t.Fatalf("unexpected %+v", cfg)
	}
}

func TestConfigFallsBackToGuardNames(t *testing.T) {
	clearEnv(t)
	t.Setenv("GUARD_API_KEY", "legacy")
	t.Setenv("GUARD_API_BASE_URL", "http://localhost:9999")

	cfg := ResolveConfig(Config{})
	if cfg.APIKey != "legacy" || cfg.BaseURL != "http://localhost:9999" {
		t.Fatalf("unexpected %+v", cfg)
	}
}

func TestConfigPrefersExplicitValues(t *testing.T) {
	clearEnv(t)
	t.Setenv("VEREXA_API_KEY", "from-env")
	t.Setenv("VEREXA_BASE_URL", "https://env.example")

	cfg := ResolveConfig(Config{APIKey: "explicit", BaseURL: "https://explicit.example"})
	if cfg.APIKey != "explicit" || cfg.BaseURL != "https://explicit.example" {
		t.Fatalf("unexpected %+v", cfg)
	}
}

func TestConfigDefaultsBaseURL(t *testing.T) {
	clearEnv(t)
	if cfg := ResolveConfig(Config{APIKey: "k"}); cfg.BaseURL != "https://api.verexa.dev" {
		t.Fatalf("base url = %q", cfg.BaseURL)
	}
}

func TestConfigWarnsOnceWhenNoKey(t *testing.T) {
	clearEnv(t)
	missingKeyWarned.Store(false)
	logger, buf := captureLogger()

	cfg := ResolveConfig(Config{Logger: logger})
	ResolveConfig(Config{Logger: logger})

	if cfg.APIKey != "" {
		t.Fatalf("expected empty key, got %q", cfg.APIKey)
	}
	if n := strings.Count(buf.String(), "no API key"); n != 1 {
		t.Fatalf("expected one warning, got %d: %s", n, buf.String())
	}
}

func TestConfigIgnoresEmptyEnvironmentVariable(t *testing.T) {
	clearEnv(t)
	t.Setenv("VEREXA_API_KEY", "")
	t.Setenv("GUARD_API_KEY", "fallback")

	if cfg := ResolveConfig(Config{}); cfg.APIKey != "fallback" {
		t.Fatalf("api key = %q", cfg.APIKey)
	}
}

func TestNewReadsEnvironment(t *testing.T) {
	clearEnv(t)
	s := newVerdictServer(t)
	t.Setenv("VEREXA_API_KEY", "env-key")
	t.Setenv("VEREXA_BASE_URL", s.URL)

	New(Config{}).CheckInput(bg, "hi", CheckOptions{})

	calls := s.calls()
	if len(calls) != 1 || calls[0].header.Get("Authorization") != "Bearer env-key" {
		t.Fatalf("expected one request with the env key, got %+v", calls)
	}
}
