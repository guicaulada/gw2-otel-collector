package config

import (
	"testing"
	"time"
)

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv("GW2_API_KEY", "abc")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.OTLPEndpointURL != "http://localhost:4318" {
		t.Errorf("OTLPEndpointURL = %q", cfg.OTLPEndpointURL)
	}
	if cfg.Intervals.Account != 5*time.Minute {
		t.Errorf("Account interval = %v, want 5m", cfg.Intervals.Account)
	}
	if cfg.StatePath != "state.db" {
		t.Errorf("StatePath = %q", cfg.StatePath)
	}
	if cfg.RateBurst != 300 {
		t.Errorf("RateBurst = %d, want 300", cfg.RateBurst)
	}
}

func TestFromEnvMissingKey(t *testing.T) {
	t.Setenv("GW2_API_KEY", "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected error when GW2_API_KEY is missing")
	}
}

func TestFromEnvOverrides(t *testing.T) {
	t.Setenv("GW2_API_KEY", "abc")
	t.Setenv("GW2_INTERVAL_ACCOUNT", "30s")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://alloy:4318")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.Intervals.Account != 30*time.Second {
		t.Errorf("Account interval = %v, want 30s", cfg.Intervals.Account)
	}
	if cfg.OTLPEndpointURL != "http://alloy:4318" {
		t.Errorf("OTLPEndpointURL = %q", cfg.OTLPEndpointURL)
	}
}
