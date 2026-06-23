package reference

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guicaulada/gw2-otel-collector/internal/gw2"
)

func TestRefreshIsBuildGated(t *testing.T) {
	var buildCalls, currencyCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/build":
			buildCalls.Add(1)
			_, _ = io.WriteString(w, `{"id":1000}`)
		case "/currencies":
			currencyCalls.Add(1)
			_, _ = io.WriteString(w, `[{"id":1,"name":"Coin"},{"id":2,"name":"Karma"}]`)
		default:
			// Collection index endpoints (skins, colors, ...) -> small arrays.
			_, _ = io.WriteString(w, `[1,2,3]`)
		}
	}))
	defer srv.Close()

	client, err := gw2.NewClient(gw2.Options{
		BaseURL: srv.URL, APIKey: "k", SchemaVersion: "latest",
		RateLimitPerSec: 1000, RateBurst: 1000, MaxRetries: 1, RequestTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	c := New(client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()

	if err := c.Refresh(ctx); err != nil {
		t.Fatalf("Refresh 1: %v", err)
	}
	if name, ok := c.CurrencyName(1); !ok || name != "Coin" {
		t.Errorf("CurrencyName(1) = (%q, %v)", name, ok)
	}
	if total, ok := c.CollectionTotal("skins"); !ok || total != 3 {
		t.Errorf("CollectionTotal(skins) = (%d, %v), want (3, true)", total, ok)
	}

	// Second refresh at the same build must NOT refetch currencies/collections.
	if err := c.Refresh(ctx); err != nil {
		t.Fatalf("Refresh 2: %v", err)
	}
	if n := currencyCalls.Load(); n != 1 {
		t.Errorf("currencies fetched %d times, want 1 (build-gated)", n)
	}
	if n := buildCalls.Load(); n != 2 {
		t.Errorf("build checked %d times, want 2", n)
	}
}
