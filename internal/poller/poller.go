// Package poller fetches each endpoint family on its own interval and writes
// the result into the store. Each family runs in its own goroutine so intervals
// are independent; all stop when the context is cancelled.
package poller

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"

	"github.com/guicaulada/gw2-otel-collector/internal/config"
	"github.com/guicaulada/gw2-otel-collector/internal/gw2"
	"github.com/guicaulada/gw2-otel-collector/internal/store"
	"github.com/guicaulada/gw2-otel-collector/internal/value"
)

// Fixed quantities used to price the gem/coin exchange, so the rate is
// comparable over time (small inputs return degenerate rates).
const (
	exchangeCoinsQuantity = 100000 // 10 gold -> price of a gem in coins
	exchangeGemsQuantity  = 100    // 100 gems -> coins received per gem
)

// Emitter receives fresh snapshots so it can diff them and emit domain events.
// It is implemented by internal/events.Emitter.
type Emitter interface {
	OnAccount(ctx context.Context, a *gw2.Account)
	OnCharacters(ctx context.Context, chars []gw2.Character)
	OnUnlocks(ctx context.Context, counts map[string]int)
	OnTransactions(ctx context.Context, txs []gw2.Transaction, side string)
	OnGuildLog(ctx context.Context, guildID string, entries []gw2.GuildLogEntry)
}

// Poller drives scheduled polling of the GW2 API.
type Poller struct {
	client     *gw2.Client
	store      *store.Store
	emitter    Emitter
	intervals  config.Intervals
	trackItems []int
	log        *slog.Logger
	wg         sync.WaitGroup
}

// New returns a Poller. emitter may be nil to disable event emission.
func New(client *gw2.Client, st *store.Store, emitter Emitter, intervals config.Intervals, trackItems []int, log *slog.Logger) *Poller {
	return &Poller{client: client, store: st, emitter: emitter, intervals: intervals, trackItems: trackItems, log: log}
}

// Start launches one goroutine per family. It returns immediately; call Wait to
// block until all goroutines have stopped after the context is cancelled.
func (p *Poller) Start(ctx context.Context) {
	p.run(ctx, "account", p.intervals.Account, func(ctx context.Context) error {
		a, err := p.client.Account(ctx)
		if err != nil {
			return err
		}
		p.store.SetAccount(a, time.Now())
		if p.emitter != nil {
			p.emitter.OnAccount(ctx, a)
		}
		return nil
	})

	p.run(ctx, "wallet", p.intervals.Wallet, func(ctx context.Context) error {
		w, err := p.client.Wallet(ctx)
		if err != nil {
			return err
		}
		p.store.SetWallet(w, time.Now())
		return nil
	})

	p.run(ctx, "characters", p.intervals.Characters, func(ctx context.Context) error {
		ch, err := p.client.Characters(ctx)
		if err != nil {
			return err
		}
		p.store.SetCharacters(ch, time.Now())
		if p.emitter != nil {
			p.emitter.OnCharacters(ctx, ch)
		}
		return nil
	})

	p.run(ctx, "progression", p.intervals.Progression, func(ctx context.Context) error {
		masteries, err := p.client.Masteries(ctx)
		if err != nil {
			return err
		}
		points, err := p.client.MasteryPoints(ctx)
		if err != nil {
			return err
		}
		luck, err := p.client.Luck(ctx)
		if err != nil {
			return err
		}
		byRegion := make(map[string]store.MasteryRegionPoints, len(points.Totals))
		for _, t := range points.Totals {
			byRegion[t.Region] = store.MasteryRegionPoints{Earned: t.Earned, Spent: t.Spent}
		}
		p.store.SetProgression(&store.Progression{
			Luck:           luck,
			MasteriesCount: len(masteries),
			PointsByRegion: byRegion,
		}, time.Now())
		return nil
	})

	p.run(ctx, "storage", p.intervals.Storage, func(ctx context.Context) error {
		bank, err := p.client.Bank(ctx)
		if err != nil {
			return err
		}
		shared, err := p.client.SharedInventory(ctx)
		if err != nil {
			return err
		}
		materials, err := p.client.Materials(ctx)
		if err != nil {
			return err
		}
		byCategory := make(map[int]int64)
		for _, m := range materials {
			byCategory[m.Category] += m.Count
		}
		p.store.SetStorage(&store.Storage{
			BankUsed:            countFilled(bank),
			BankCapacity:        int64(len(bank)),
			SharedUsed:          countFilled(shared),
			SharedCapacity:      int64(len(shared)),
			MaterialsByCategory: byCategory,
		}, time.Now())
		return nil
	})

	p.run(ctx, "wizardsvault", p.intervals.WizardsVault, func(ctx context.Context) error {
		var periods []store.WizardsVaultPeriod
		for _, period := range []string{"daily", "weekly", "special"} {
			wv, err := p.client.WizardsVault(ctx, period)
			if err != nil {
				return err
			}
			var completed int
			var unclaimed int64
			for _, o := range wv.Objectives {
				if o.ProgressComplete > 0 && o.ProgressCurrent >= o.ProgressComplete {
					completed++
					if !o.Claimed {
						unclaimed += o.Acclaim
					}
				}
			}
			periods = append(periods, store.WizardsVaultPeriod{
				Period:           period,
				HasMeta:          period != "special",
				MetaCurrent:      wv.MetaProgressCurrent,
				MetaComplete:     wv.MetaProgressComplete,
				Objectives:       len(wv.Objectives),
				Completed:        completed,
				UnclaimedAcclaim: unclaimed,
			})
		}
		p.store.SetWizardsVault(periods, time.Now())
		return nil
	})

	p.run(ctx, "guild", p.intervals.Guild, func(ctx context.Context) error {
		acc := p.store.Account()
		if acc == nil { // store not populated yet (startup race) — fetch directly
			var err error
			if acc, err = p.client.Account(ctx); err != nil {
				return err
			}
		}
		if len(acc.Guilds) == 0 {
			return nil // account is in no guild
		}
		infos := make([]store.GuildInfo, 0, len(acc.Guilds))
		for _, gid := range acc.Guilds {
			g, err := p.client.Guild(ctx, gid)
			if err != nil {
				return err
			}
			info := store.GuildInfo{Guild: *g, UpgradesCompleted: -1}
			// Treasury/stash/storage/log are leader-only.
			if contains(acc.GuildLeader, gid) {
				if n, err := p.client.GuildUpgradesCompleted(ctx, gid); err == nil {
					info.UpgradesCompleted = n
				}
				if treasury, err := p.client.GuildTreasury(ctx, gid); err == nil {
					info.TreasuryItems = len(treasury)
				}
				if stash, err := p.client.GuildStash(ctx, gid); err == nil {
					for _, sec := range stash {
						info.StashCoins += sec.Coins
						info.StashSlotsSize += sec.Size
						info.StashSlotsUsed += countFilled(sec.Inventory)
					}
				}
				if storage, err := p.client.GuildStorage(ctx, gid); err == nil {
					info.StorageItems = len(storage)
				}
				if entries, err := p.client.GuildLog(ctx, gid); err == nil && p.emitter != nil {
					p.emitter.OnGuildLog(ctx, gid, entries)
				}
			}
			infos = append(infos, info)
		}
		p.store.SetGuilds(infos, time.Now())
		return nil
	})

	p.run(ctx, "value", p.intervals.Value, func(ctx context.Context) error {
		bank, err := p.client.Bank(ctx)
		if err != nil {
			return err
		}
		materials, err := p.client.Materials(ctx)
		if err != nil {
			return err
		}
		shared, err := p.client.SharedInventory(ctx)
		if err != nil {
			return err
		}
		chars, err := p.client.Characters(ctx)
		if err != nil {
			return err
		}
		wallet, err := p.client.Wallet(ctx)
		if err != nil {
			return err
		}
		gemRate, err := p.client.ExchangeGems(ctx, exchangeGemsQuantity)
		if err != nil {
			return err
		}

		// Distinct tradable item ids across all owned-item sources.
		idSet := map[int]struct{}{}
		addSlots := func(slots []*gw2.Slot) {
			for _, s := range slots {
				if s != nil {
					idSet[s.ID] = struct{}{}
				}
			}
		}
		addSlots(bank)
		addSlots(shared)
		for _, m := range materials {
			idSet[m.ID] = struct{}{}
		}
		for _, c := range chars {
			for _, bag := range c.Bags {
				if bag != nil {
					addSlots(bag.Inventory)
				}
			}
		}
		ids := make([]int, 0, len(idSet))
		for id := range idSet {
			ids = append(ids, id)
		}
		prices, err := p.client.PricesBatched(ctx, ids)
		if err != nil {
			return err
		}

		var coin, gems int64
		for _, c := range wallet {
			switch c.ID {
			case 1:
				coin = c.Value
			case 4:
				gems = c.Value
			}
		}
		walletCopper := coin + gems*gemRate.CoinsPerGem

		acc := value.Compute(bank, materials, shared, chars, walletCopper, prices)
		p.store.SetAccountValue(&acc, time.Now())
		return nil
	})

	p.run(ctx, "story", p.intervals.Story, func(ctx context.Context) error {
		chars := p.store.Characters()
		if len(chars) == 0 { // startup race — fetch the roster directly
			var err error
			if chars, err = p.client.Characters(ctx); err != nil {
				return err
			}
		}
		seen := map[int]bool{}
		for _, c := range chars {
			qids, err := p.client.CharacterQuests(ctx, c.Name)
			if err != nil {
				return err
			}
			for _, id := range qids {
				seen[id] = true
			}
		}
		completed := make([]int, 0, len(seen))
		for id := range seen {
			completed = append(completed, id)
		}
		p.store.SetStoryCompleted(completed, time.Now())
		return nil
	})

	p.run(ctx, "pvp", p.intervals.PvP, func(ctx context.Context) error {
		s, err := p.client.PvPStats(ctx)
		if err != nil {
			return err
		}
		p.store.SetPvP(s, time.Now())
		return nil
	})

	p.run(ctx, "unlocks", p.intervals.Unlocks, func(ctx context.Context) error {
		counts := make(map[string]int, len(gw2.Collections))
		for _, col := range gw2.Collections {
			n, err := p.client.CountIDs(ctx, col.AccountPath, col.AccountPath)
			if err != nil {
				return err
			}
			counts[col.Name] = n
		}
		p.store.SetUnlocks(counts, time.Now())
		if p.emitter != nil {
			p.emitter.OnUnlocks(ctx, counts)
		}
		return nil
	})

	p.run(ctx, "transactions", p.intervals.Transactions, func(ctx context.Context) error {
		if p.emitter == nil {
			return nil
		}
		for _, side := range []string{"buys", "sells"} {
			txs, err := p.client.TransactionHistory(ctx, side)
			if err != nil {
				return err
			}
			p.emitter.OnTransactions(ctx, txs, side)
		}
		return nil
	})

	p.run(ctx, "commerce", p.intervals.Commerce, func(ctx context.Context) error {
		buy, err := p.client.ExchangeCoins(ctx, exchangeCoinsQuantity)
		if err != nil {
			return err
		}
		sell, err := p.client.ExchangeGems(ctx, exchangeGemsQuantity)
		if err != nil {
			return err
		}
		delivery, err := p.client.Delivery(ctx)
		if err != nil {
			return err
		}
		p.store.SetCommerce(&store.Commerce{
			CoinsPerGemBuy:  buy.CoinsPerGem,
			CoinsPerGemSell: sell.CoinsPerGem,
			DeliveryCoins:   delivery.Coins,
			DeliveryItems:   int64(len(delivery.Items)),
		}, time.Now())

		if len(p.trackItems) > 0 {
			prices, err := p.client.Prices(ctx, p.trackItems)
			if err != nil {
				return err
			}
			p.store.SetPrices(prices, time.Now())
		}
		return nil
	})
}

// Wait blocks until all polling goroutines have exited.
func (p *Poller) Wait() { p.wg.Wait() }

// contains reports whether s is in the slice.
func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// countFilled counts non-nil (occupied) slots in a positional bank/inventory.
func countFilled(slots []*gw2.Slot) int64 {
	var n int64
	for _, s := range slots {
		if s != nil {
			n++
		}
	}
	return n
}

// run polls once immediately, then on every tick until the context is cancelled.
func (p *Poller) run(ctx context.Context, family string, interval time.Duration, fetch func(context.Context) error) {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		log := p.log.With("family", family, "interval", interval.String())

		tracer := otel.Tracer("github.com/guicaulada/gw2-otel-collector/internal/poller")
		poll := func() {
			pollCtx, span := tracer.Start(ctx, "poll "+family)
			err := fetch(pollCtx)
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			span.End()
			if err != nil {
				if ctx.Err() == nil {
					log.Error("poll failed", "error", err)
				}
				return
			}
			log.Debug("poll ok")
		}

		poll() // initial fetch so metrics are populated quickly

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Debug("poller stopping")
				return
			case <-ticker.C:
				poll()
			}
		}
	}()
}
