// Package reference caches static GW2 reference data (id→name lookups) and
// refreshes it only when the game build number changes.
//
// Reads are lock-free via an atomic pointer swap: a refresh builds a fresh map
// off to the side and publishes it with a single Store; readers never block and
// never see a partial map. See docs/architecture-research.md §5.3.
package reference

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/guicaulada/gw2-otel-collector/internal/gw2"
)

// data is an immutable snapshot of reference tables. Never mutate after publish.
type data struct {
	currencies map[int]string
}

// Cache holds the latest reference data and the build number it was built for.
type Cache struct {
	client *gw2.Client
	log    *slog.Logger
	d      atomic.Pointer[data]
	build  atomic.Int64
}

// New returns an empty Cache.
func New(client *gw2.Client, log *slog.Logger) *Cache {
	return &Cache{client: client, log: log}
}

// CurrencyName resolves a currency id to its name, if loaded.
func (c *Cache) CurrencyName(id int) (string, bool) {
	d := c.d.Load()
	if d == nil {
		return "", false
	}
	name, ok := d.currencies[id]
	return name, ok
}

// Refresh checks the game build number and, if it changed (or nothing is loaded
// yet), rebuilds the reference tables. Fail-soft: on error the current good data
// is kept. Returns the error so callers can log it.
func (c *Cache) Refresh(ctx context.Context) error {
	build, err := c.client.Build(ctx)
	if err != nil {
		return err
	}
	if c.d.Load() != nil && int64(build) == c.build.Load() {
		return nil // unchanged — keep resident tables
	}

	currencies, err := c.client.Currencies(ctx)
	if err != nil {
		return err
	}
	m := make(map[int]string, len(currencies))
	for _, cur := range currencies {
		m[cur.ID] = cur.Name
	}

	c.d.Store(&data{currencies: m})
	c.build.Store(int64(build))
	c.log.Info("reference data refreshed", "build", build, "currencies", len(m))
	return nil
}

// Start runs Refresh on an interval until the context is cancelled. Call Refresh
// once synchronously first if you want tables populated before serving metrics.
func (c *Cache) Start(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := c.Refresh(ctx); err != nil && ctx.Err() == nil {
					c.log.Warn("reference refresh failed", "error", err)
				}
			}
		}
	}()
}
