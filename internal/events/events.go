// Package events manufactures discrete domain events from the snapshot-only GW2
// API by diffing each poll against persisted state, and emits them as OTel log
// records (Events). The GW2 API exposes no change feed, so this is where the
// "what changed" signal is produced. See docs/telemetry-design.md §Logs.
//
// Two patterns are used:
//   - value diffing (level-up, deaths, unlock counts, expansions): compare the
//     current value to the persisted previous one; emit on increase; on first
//     observation just record the baseline (no event).
//   - seen-set dedupe (trading-post transactions): emit once per id, marking it
//     seen AFTER emit so a crash re-emits (at-least-once) rather than drops.
package events

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"

	"github.com/guicaulada/gw2-otel-collector/internal/gw2"
	"github.com/guicaulada/gw2-otel-collector/internal/state"
)

// Emitter diffs snapshots and emits events.
type Emitter struct {
	logger log.Logger
	state  *state.Store
	log    *slog.Logger
}

// New returns an Emitter using the global logger provider.
func New(st *state.Store, l *slog.Logger) *Emitter {
	return &Emitter{
		logger: global.GetLoggerProvider().Logger("github.com/guicaulada/gw2-otel-collector/internal/events"),
		state:  st,
		log:    l,
	}
}

// emit sends one event-flavored log record (a record with an event name).
func (e *Emitter) emit(ctx context.Context, name, body string, attrs ...log.KeyValue) {
	var r log.Record
	r.SetEventName(name)
	r.SetSeverity(log.SeverityInfo)
	r.SetBody(log.StringValue(body))
	r.AddAttributes(attrs...)
	e.logger.Emit(ctx, r)
}

// diffUp emits an event when the value for key has increased since last seen.
// It records the new value either way; the first observation only sets a
// baseline (returns without emitting) so restarts don't flood.
func (e *Emitter) diffUp(ctx context.Context, key string, cur int64, onIncrease func(prev int64)) {
	prev, ok := e.state.PrevInt(key)
	if ok && cur > prev {
		onIncrease(prev)
	}
	if !ok || cur != prev {
		if err := e.state.SetInt(key, cur); err != nil {
			e.log.Warn("state SetInt failed", "key", key, "error", err)
		}
	}
}

// OnCharacters emits level-up and death events.
func (e *Emitter) OnCharacters(ctx context.Context, chars []gw2.Character) {
	for _, c := range chars {
		name := c.Name
		e.diffUp(ctx, "char_level:"+name, int64(c.Level), func(prev int64) {
			e.emit(ctx, "gw2.character.levelup",
				fmt.Sprintf("%s reached level %d", name, c.Level),
				log.String("gw2.character.name", name),
				log.String("gw2.character.profession", c.Profession),
				log.Int64("gw2.character.level.from", prev),
				log.Int64("gw2.character.level.to", int64(c.Level)))
		})
		e.diffUp(ctx, "char_deaths:"+name, c.Deaths, func(prev int64) {
			e.emit(ctx, "gw2.character.died",
				fmt.Sprintf("%s died %d time(s)", name, c.Deaths-prev),
				log.String("gw2.character.name", name),
				log.Int64("gw2.character.deaths.delta", c.Deaths-prev),
				log.Int64("gw2.character.deaths.total", c.Deaths))
		})
	}
}

// OnUnlocks emits an event when a collection's unlocked count grows.
func (e *Emitter) OnUnlocks(ctx context.Context, counts map[string]int) {
	for collection, count := range counts {
		coll := collection
		c := int64(count)
		e.diffUp(ctx, "unlocks:"+coll, c, func(prev int64) {
			e.emit(ctx, "gw2.collection.unlocked",
				fmt.Sprintf("unlocked %d new %s", c-prev, coll),
				log.String("gw2.collection", coll),
				log.Int64("gw2.collection.delta", c-prev),
				log.Int64("gw2.collection.total", c))
		})
	}
}

// OnResets emits a completion event when a reset-cycle count grows (e.g. a world
// boss killed). At reset the count drops to 0 and the baseline resets silently,
// so the next completion re-emits.
func (e *Emitter) OnResets(ctx context.Context, kind string, count int) {
	e.diffUp(ctx, "reset:"+kind, int64(count), func(prev int64) {
		e.emit(ctx, "gw2.daily.completed",
			fmt.Sprintf("%s: %d completed (%d total this cycle)", kind, int64(count)-prev, count),
			log.String("gw2.reset.kind", kind),
			log.Int64("gw2.reset.delta", int64(count)-prev),
			log.Int64("gw2.reset.total", int64(count)))
	})
}

// OnAccount emits an event when the owned-expansion set grows.
func (e *Emitter) OnAccount(ctx context.Context, a *gw2.Account) {
	if a == nil {
		return
	}
	e.diffUp(ctx, "account_access:count", int64(len(a.Access)), func(prev int64) {
		e.emit(ctx, "gw2.account.access_changed",
			fmt.Sprintf("account access grew to %d entitlements", len(a.Access)),
			log.Int64("gw2.account.access.count", int64(len(a.Access))))
	})
}

// OnGuildLog emits an event per guild-log entry newer than the persisted
// watermark (a monotonic log id), then advances the watermark after emitting.
func (e *Emitter) OnGuildLog(ctx context.Context, guildID string, entries []gw2.GuildLogEntry) {
	key := "guildlog:" + guildID
	wm, _ := e.state.PrevInt(key)
	var maxID int64 = wm
	for _, en := range entries {
		if en.ID <= wm {
			continue
		}
		if en.ID > maxID {
			maxID = en.ID
		}
		e.emit(ctx, "gw2.guild.log",
			fmt.Sprintf("guild %s: %s", en.Type, en.User),
			log.String("gw2.guild.id", guildID),
			log.String("gw2.guild.log.type", en.Type),
			log.String("gw2.guild.log.user", en.User),
			log.String("gw2.guild.log.operation", en.Operation),
			log.String("gw2.guild.log.action", en.Action))
	}
	if maxID > wm {
		if err := e.state.SetInt(key, maxID); err != nil {
			e.log.Warn("state SetInt failed", "key", key, "error", err)
		}
	}
}

// OnPvPGames emits one event per PvP match id not seen before. The API keeps
// only the last ~10 games, so the seen-set both backfills on first run and
// dedupes as the window rotates.
func (e *Emitter) OnPvPGames(ctx context.Context, games []gw2.PvPGame) {
	for _, g := range games {
		key := "pvpgame:" + g.ID
		if e.state.HasSeen(key) {
			continue
		}
		e.emit(ctx, "gw2.pvp.game",
			fmt.Sprintf("%s %s as %s (%+d rating)", g.RatingType, g.Result, g.Profession, g.RatingChange),
			log.String("gw2.pvp.game.id", g.ID),
			log.String("gw2.pvp.result", g.Result),
			log.String("gw2.pvp.team", g.Team),
			log.String("gw2.pvp.profession", g.Profession),
			log.String("gw2.pvp.rating_type", g.RatingType),
			log.Int64("gw2.pvp.rating_change", int64(g.RatingChange)),
			log.Int64("gw2.pvp.map_id", int64(g.MapID)),
			log.String("gw2.pvp.ended", g.Ended))
		if err := e.state.MarkSeen(key); err != nil {
			e.log.Warn("state MarkSeen failed", "key", key, "error", err)
		}
	}
}

// OnTransactions emits one event per completed transaction id not seen before.
func (e *Emitter) OnTransactions(ctx context.Context, txs []gw2.Transaction, side string) {
	for _, t := range txs {
		key := fmt.Sprintf("tx:%s:%d", side, t.ID)
		if e.state.HasSeen(key) {
			continue
		}
		e.emit(ctx, "gw2.commerce.transaction",
			fmt.Sprintf("%s %d x item %d at %d copper", side, t.Quantity, t.ItemID, t.Price),
			log.String("gw2.transaction.side", side),
			log.Int64("gw2.item.id", int64(t.ItemID)),
			log.Int64("gw2.transaction.unit_price", t.Price),
			log.Int64("gw2.transaction.quantity", t.Quantity),
			log.Int64("gw2.transaction.total", t.Price*t.Quantity))
		if err := e.state.MarkSeen(key); err != nil {
			e.log.Warn("state MarkSeen failed", "key", key, "error", err)
		}
	}
}
