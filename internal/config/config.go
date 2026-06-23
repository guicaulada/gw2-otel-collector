// Package config loads collector configuration from the environment.
//
// OTLP export is configured through the standard OpenTelemetry environment
// variables (OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_EXPORTER_OTLP_HEADERS, ...), so
// switching between a local LGTM stack, a local Alloy hop, and Grafana Cloud is
// configuration-only — no code change. See docs/architecture-research.md.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration for the collector.
type Config struct {
	// GW2 API
	APIKey        string
	APIBaseURL    string
	SchemaVersion string // value for the ?v= query parameter

	// Rate limiting (per-IP token bucket; the API allows ~300 req/min).
	RateLimitPerSec float64
	RateBurst       int
	MaxRetries      int
	RequestTimeout  time.Duration

	// OTLP / telemetry
	OTLPEndpointURL string // OTEL_EXPORTER_OTLP_ENDPOINT, e.g. http://localhost:4318
	ServiceName     string
	ServiceVersion  string
	ServiceInstance string
	ExportInterval  time.Duration

	// StatePath is the bbolt file for event watermarks and diff state.
	StatePath string

	// TrackItems are trading-post item ids to track prices for (GW2_TRACK_ITEMS).
	TrackItems []int

	// Per-family poll intervals (kept >= the server's documented cache TTL).
	Intervals Intervals
}

// Intervals controls how often each endpoint family is polled.
type Intervals struct {
	Account      time.Duration
	Wallet       time.Duration
	Characters   time.Duration
	Commerce     time.Duration
	Progression  time.Duration
	Storage      time.Duration
	Unlocks      time.Duration
	Guild        time.Duration
	PvP          time.Duration
	Transactions time.Duration
	// Reference is how often the game build number is checked to invalidate
	// static reference data (id→name tables). Reference data changes only on a
	// game patch, so this can be infrequent.
	Reference time.Duration
}

// FromEnv builds a Config from environment variables, applying defaults.
// It returns an error only when a required value (the API key) is missing.
func FromEnv() (*Config, error) {
	apiKey := os.Getenv("GW2_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GW2_API_KEY is required")
	}

	host, _ := os.Hostname()
	if host == "" {
		host = "unknown-host"
	}

	return &Config{
		APIKey:        apiKey,
		APIBaseURL:    env("GW2_API_BASE_URL", "https://api.guildwars2.com/v2"),
		SchemaVersion: env("GW2_SCHEMA_VERSION", "latest"),

		RateLimitPerSec: envFloat("GW2_RATE_LIMIT_PER_SEC", 5),
		RateBurst:       envInt("GW2_RATE_BURST", 300),
		MaxRetries:      envInt("GW2_MAX_RETRIES", 4),
		RequestTimeout:  envDuration("GW2_REQUEST_TIMEOUT", 30*time.Second),

		OTLPEndpointURL: env("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318"),
		ServiceName:     env("OTEL_SERVICE_NAME", "gw2-otel-collector"),
		ServiceVersion:  env("GW2_COLLECTOR_VERSION", "0.1.0"),
		ServiceInstance: env("OTEL_SERVICE_INSTANCE_ID", fmt.Sprintf("%s-%d", host, os.Getpid())),
		ExportInterval:  envDuration("GW2_EXPORT_INTERVAL", 30*time.Second),
		StatePath:       env("GW2_STATE_PATH", "state.db"),
		TrackItems:      envInts("GW2_TRACK_ITEMS"),

		Intervals: Intervals{
			Account:      envDuration("GW2_INTERVAL_ACCOUNT", 5*time.Minute),
			Wallet:       envDuration("GW2_INTERVAL_WALLET", 5*time.Minute),
			Characters:   envDuration("GW2_INTERVAL_CHARACTERS", 5*time.Minute),
			Commerce:     envDuration("GW2_INTERVAL_COMMERCE", 5*time.Minute),
			Progression:  envDuration("GW2_INTERVAL_PROGRESSION", 10*time.Minute),
			Storage:      envDuration("GW2_INTERVAL_STORAGE", 15*time.Minute),
			Unlocks:      envDuration("GW2_INTERVAL_UNLOCKS", 15*time.Minute),
			Guild:        envDuration("GW2_INTERVAL_GUILD", 10*time.Minute),
			PvP:          envDuration("GW2_INTERVAL_PVP", 10*time.Minute),
			Transactions: envDuration("GW2_INTERVAL_TRANSACTIONS", 5*time.Minute),
			Reference:    envDuration("GW2_INTERVAL_REFERENCE", time.Hour),
		},
	}, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

// envInts parses a comma-separated list of ints (ignoring blanks/invalid).
func envInts(key string) []int {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	var out []int
	for _, p := range strings.Split(v, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
