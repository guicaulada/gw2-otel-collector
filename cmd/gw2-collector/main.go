// Command gw2-collector polls the Guild Wars 2 API v2 and exports the data as
// OpenTelemetry metrics over OTLP. See docs/architecture-research.md.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/guicaulada/gw2-otel-collector/internal/config"
	"github.com/guicaulada/gw2-otel-collector/internal/events"
	"github.com/guicaulada/gw2-otel-collector/internal/gw2"
	"github.com/guicaulada/gw2-otel-collector/internal/metrics"
	"github.com/guicaulada/gw2-otel-collector/internal/poller"
	"github.com/guicaulada/gw2-otel-collector/internal/reference"
	"github.com/guicaulada/gw2-otel-collector/internal/state"
	"github.com/guicaulada/gw2-otel-collector/internal/store"
	"github.com/guicaulada/gw2-otel-collector/internal/telemetry"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel()}))

	if err := run(log); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}

	// Cancel on SIGINT/SIGTERM for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdown, err := telemetry.Setup(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			log.Error("telemetry shutdown", "error", err)
		}
	}()

	client, err := gw2.NewClient(gw2.Options{
		BaseURL:         cfg.APIBaseURL,
		APIKey:          cfg.APIKey,
		SchemaVersion:   cfg.SchemaVersion,
		RateLimitPerSec: cfg.RateLimitPerSec,
		RateBurst:       cfg.RateBurst,
		MaxRetries:      cfg.MaxRetries,
		RequestTimeout:  cfg.RequestTimeout,
	})
	if err != nil {
		return err
	}

	st := store.New()

	// Persistent state for the event machinery (diff baselines + seen-set).
	stateStore, err := state.Open(cfg.StatePath)
	if err != nil {
		return err
	}
	defer func() { _ = stateStore.Close() }()
	emitter := events.New(stateStore, log)

	// Reference data (id→name) — refresh once synchronously so currency names
	// are present from the first metric export, then keep it fresh in the
	// background (gated on the game build number).
	ref := reference.New(client, log)
	refCtx, refCancel := context.WithTimeout(ctx, 30*time.Second)
	if err := ref.Refresh(refCtx); err != nil {
		log.Warn("initial reference refresh failed; names unavailable until next refresh", "error", err)
	}
	refCancel()
	ref.Start(ctx, cfg.Intervals.Reference)

	reg, err := metrics.Register(st, ref)
	if err != nil {
		return err
	}
	defer func() { _ = reg.Unregister() }()

	p := poller.New(client, st, emitter, cfg.Intervals, log)
	p.Start(ctx)

	log.Info("gw2-collector started",
		"service", cfg.ServiceName,
		"otlp_endpoint", cfg.OTLPEndpointURL,
		"export_interval", cfg.ExportInterval.String(),
	)

	<-ctx.Done()
	log.Info("shutting down")
	p.Wait()
	return nil
}

func logLevel() slog.Level {
	switch os.Getenv("GW2_LOG_LEVEL") {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
