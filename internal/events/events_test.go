package events

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"

	"github.com/guicaulada/gw2-otel-collector/internal/gw2"
	"github.com/guicaulada/gw2-otel-collector/internal/state"
)

// captured is the subset of an emitted log record the tests assert on. We copy
// it out during Export rather than retaining the SDK Record (which may be reused).
type captured struct {
	name  string
	body  string
	attrs map[string]otellog.Value
}

type recExporter struct{ records []captured }

func (e *recExporter) Export(_ context.Context, recs []sdklog.Record) error {
	for i := range recs {
		r := &recs[i]
		c := captured{name: r.EventName(), body: r.Body().AsString(), attrs: map[string]otellog.Value{}}
		r.WalkAttributes(func(kv otellog.KeyValue) bool {
			c.attrs[kv.Key] = kv.Value
			return true
		})
		e.records = append(e.records, c)
	}
	return nil
}
func (e *recExporter) Shutdown(context.Context) error   { return nil }
func (e *recExporter) ForceFlush(context.Context) error { return nil }

func (e *recExporter) count(name string) int {
	n := 0
	for _, r := range e.records {
		if r.name == name {
			n++
		}
	}
	return n
}

func (e *recExporter) first(name string) (captured, bool) {
	for _, r := range e.records {
		if r.name == name {
			return r, true
		}
	}
	return captured{}, false
}

func newEmitter(t *testing.T) (*Emitter, *recExporter) {
	t.Helper()
	exp := &recExporter{}
	// Set the global provider BEFORE New, which captures the logger at construction.
	global.SetLoggerProvider(sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exp))))
	st, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(st, slog.New(slog.NewTextHandler(io.Discard, nil))), exp
}

func TestOnCharactersBaselineThenLevelUpAndDeaths(t *testing.T) {
	e, exp := newEmitter(t)
	ctx := context.Background()
	chars := []gw2.Character{{Name: "Alvyn", Profession: "Guardian", Level: 80, Deaths: 5}}

	// First observation only records a baseline — no events.
	e.OnCharacters(ctx, chars)
	if len(exp.records) != 0 {
		t.Fatalf("baseline emitted %d events, want 0", len(exp.records))
	}

	// Level up + 2 more deaths → one event each.
	chars[0].Level, chars[0].Deaths = 81, 7
	e.OnCharacters(ctx, chars)

	if got := exp.count("gw2.character.levelup"); got != 1 {
		t.Errorf("levelup events = %d, want 1", got)
	}
	if got := exp.count("gw2.character.died"); got != 1 {
		t.Errorf("died events = %d, want 1", got)
	}
	if r, ok := exp.first("gw2.character.levelup"); ok {
		if v := r.attrs["gw2.character.level.to"].AsInt64(); v != 81 {
			t.Errorf("level.to = %d, want 81", v)
		}
	}
	if r, ok := exp.first("gw2.character.died"); ok {
		if v := r.attrs["gw2.character.deaths.delta"].AsInt64(); v != 2 {
			t.Errorf("deaths.delta = %d, want 2", v)
		}
	}
}

func TestOnUnlocksEmitsOnGrowthOnly(t *testing.T) {
	e, exp := newEmitter(t)
	ctx := context.Background()

	e.OnUnlocks(ctx, map[string]int{"skins": 100}) // baseline
	if len(exp.records) != 0 {
		t.Fatalf("baseline emitted %d, want 0", len(exp.records))
	}
	e.OnUnlocks(ctx, map[string]int{"skins": 100}) // unchanged → no event
	if len(exp.records) != 0 {
		t.Fatalf("unchanged emitted %d, want 0", len(exp.records))
	}
	e.OnUnlocks(ctx, map[string]int{"skins": 103}) // +3 → one event
	if got := exp.count("gw2.collection.unlocked"); got != 1 {
		t.Errorf("unlocked events = %d, want 1", got)
	}
	if r, ok := exp.first("gw2.collection.unlocked"); ok {
		if v := r.attrs["gw2.collection.delta"].AsInt64(); v != 3 {
			t.Errorf("delta = %d, want 3", v)
		}
	}
}

func TestOnResetsResetsBaselineSilently(t *testing.T) {
	e, exp := newEmitter(t)
	ctx := context.Background()

	e.OnResets(ctx, "worldbosses", 0) // baseline at 0
	e.OnResets(ctx, "worldbosses", 2) // +2 completed → event
	if got := exp.count("gw2.daily.completed"); got != 1 {
		t.Fatalf("completed events = %d, want 1", got)
	}
	// At reset the count drops to 0; that must NOT emit, just re-baseline.
	e.OnResets(ctx, "worldbosses", 0)
	if got := exp.count("gw2.daily.completed"); got != 1 {
		t.Errorf("after reset, completed events = %d, want still 1", got)
	}
	// Next completion emits again.
	e.OnResets(ctx, "worldbosses", 1)
	if got := exp.count("gw2.daily.completed"); got != 2 {
		t.Errorf("completed events = %d, want 2", got)
	}
}

func TestOnTransactionsSeenSetDedupes(t *testing.T) {
	e, exp := newEmitter(t)
	ctx := context.Background()
	txs := []gw2.Transaction{{ID: 1, ItemID: 19684, Price: 100, Quantity: 5}}

	e.OnTransactions(ctx, txs, "sells")
	e.OnTransactions(ctx, txs, "sells") // same id again → must not re-emit
	if got := exp.count("gw2.commerce.transaction"); got != 1 {
		t.Errorf("transaction events = %d, want 1 (deduped)", got)
	}
	if r, ok := exp.first("gw2.commerce.transaction"); ok {
		if v := r.attrs["gw2.transaction.total"].AsInt64(); v != 500 {
			t.Errorf("total = %d, want 500", v)
		}
	}
}

func TestOnPvPGamesSeenSetDedupes(t *testing.T) {
	e, exp := newEmitter(t)
	ctx := context.Background()
	games := []gw2.PvPGame{{ID: "abc", Result: "Victory", Profession: "Warrior", RatingChange: 12}}

	e.OnPvPGames(ctx, games)
	e.OnPvPGames(ctx, games) // same id → no re-emit
	if got := exp.count("gw2.pvp.game"); got != 1 {
		t.Errorf("pvp.game events = %d, want 1 (deduped)", got)
	}
	if r, ok := exp.first("gw2.pvp.game"); ok {
		if v := r.attrs["gw2.pvp.result"].AsString(); v != "Victory" {
			t.Errorf("result = %q, want Victory", v)
		}
	}
}
