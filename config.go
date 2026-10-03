package verexa

import (
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

const defaultBaseURL = "https://api.verexa.dev"

// Config is all optional, so New(Config{}) works in an app that only sets
// VEREXA_API_KEY and VEREXA_BASE_URL.
type Config struct {
	APIKey  string
	BaseURL string
	// Timeout bounds each check. Zero means 2s. The audit profile needs far
	// more; see CheckOptions.Timeout.
	Timeout    time.Duration
	FailMode   FailMode
	HTTPClient *http.Client
	Logger     *slog.Logger
}

var missingKeyWarned atomic.Bool

func env(name string) string {
	return os.Getenv(name)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ResolveConfig fills unset fields from VEREXA_* then the older GUARD_* names.
//
// It never fails. New commonly runs at package init, and a missing key there
// must not crash an app's boot. A missing key resolves to an empty APIKey,
// which Check treats the same as an unreachable server: fail open (or closed)
// without touching the network. It is logged once so the misconfiguration is
// not silent.
func ResolveConfig(cfg Config) Config {
	cfg.APIKey = firstNonEmpty(cfg.APIKey, env("VEREXA_API_KEY"), env("GUARD_API_KEY"))
	cfg.BaseURL = firstNonEmpty(cfg.BaseURL, env("VEREXA_BASE_URL"), env("GUARD_API_BASE_URL"), defaultBaseURL)
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.APIKey == "" && missingKeyWarned.CompareAndSwap(false, true) {
		cfg.Logger.Warn("Verexa: no API key configured. Set Config.APIKey or VEREXA_API_KEY. " +
			"Every check will fail open (or closed, per FailMode) until one is set. " +
			"Create one in the dashboard under Settings > API keys.")
	}
	return cfg
}
