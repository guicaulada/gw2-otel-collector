// Package poller fetches each endpoint family on its own interval and writes
// the result into the store. Each family runs in its own goroutine so intervals
// are independent; all stop when the context is cancelled.
package poller

import (
	"context"
	"log/slog"
	"strings"
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
	OnResets(ctx context.Context, kind string, count int)
	OnPvPGames(ctx context.Context, games []gw2.PvPGame)
}

// resetFamilies maps reset-cycle metric kinds to their account endpoints.
var resetFamilies = []struct{ kind, path string }{
	{"worldbosses", "account/worldbosses"},
	{"dungeons", "account/dungeons"},
	{"raids", "account/raids"},
	{"mapchests", "account/mapchests"},
	{"dailycrafting", "account/dailycrafting"},
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

		// Fractal augmentations (progression entries other than luck).
		prog, err := p.client.AccountProgression(ctx)
		if err != nil {
			return err
		}
		augments := map[string]int64{}
		for _, e := range prog {
			if e.ID != "luck" {
				augments[e.ID] = e.Value
			}
		}

		// Legendary armory: owned copies vs available.
		owned, err := p.client.AccountLegendaryArmory(ctx)
		if err != nil {
			return err
		}
		available, err := p.client.LegendaryArmory(ctx)
		if err != nil {
			return err
		}
		var copies int64
		for _, o := range owned {
			copies += o.Count
		}

		p.store.SetProgression(&store.Progression{
			Luck:               luck,
			MasteriesCount:     len(masteries),
			PointsByRegion:     byRegion,
			FractalAugments:    augments,
			LegendaryOwned:     len(owned),
			LegendaryCopies:    copies,
			LegendaryAvailable: len(available),
		}, time.Now())
		return nil
	})

	// Achievement points: account achievements joined to cached definitions.
	achDefs := map[int]gw2.AchievementDef{} // id -> def (lazy, process-lifetime cache)
	p.run(ctx, "achievements", p.intervals.Achievements, func(ctx context.Context) error {
		accAch, err := p.client.AccountAchievements(ctx)
		if err != nil {
			return err
		}
		var missing []int
		for _, a := range accAch {
			if _, ok := achDefs[a.ID]; !ok {
				missing = append(missing, a.ID)
			}
		}
		if len(missing) > 0 {
			defs, err := p.client.AchievementsByIDs(ctx, missing)
			if err != nil {
				return err
			}
			for _, d := range defs {
				achDefs[d.ID] = d
			}
		}
		var totalAP int64
		done := 0
		for _, a := range accAch {
			if a.Done {
				done++
			}
			totalAP += achievementPoints(achDefs[a.ID], a)
		}
		p.store.SetAchievements(&store.Achievements{TotalAP: totalAP, Done: done, Total: len(accAch)}, time.Now())
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
			for _, e := range c.Equipment {
				if e == nil {
					continue
				}
				idSet[e.ID] = struct{}{}
				for _, up := range e.Upgrades {
					idSet[up] = struct{}{}
				}
				for _, inf := range e.Infusions {
					idSet[inf] = struct{}{}
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

	p.run(ctx, "resets", p.intervals.Resets, func(ctx context.Context) error {
		counts := make(map[string]int, len(resetFamilies))
		for _, rf := range resetFamilies {
			ids, err := p.client.AccountStringList(ctx, rf.path, rf.path)
			if err != nil {
				return err
			}
			counts[rf.kind] = len(ids)
			if p.emitter != nil {
				p.emitter.OnResets(ctx, rf.kind, len(ids))
			}
		}
		p.store.SetResets(counts, time.Now())
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

	p.run(ctx, "wvw", p.intervals.WvW, func(ctx context.Context) error {
		acc := p.store.Account()
		if acc == nil {
			var err error
			if acc, err = p.client.Account(ctx); err != nil {
				return err
			}
		}
		if acc.World == 0 {
			return nil
		}
		m, err := p.client.WvWMatchByWorld(ctx, acc.World)
		if err != nil {
			return err
		}
		w := &store.WvW{
			MatchID: m.ID, Score: m.Scores, VictoryPoints: m.VictoryPoints,
			Kills: m.Kills, Deaths: m.Deaths,
			PPT:            map[string]int64{},
			ObjectivesHeld: map[string]map[string]int{},
		}
		for color, worlds := range m.AllWorlds {
			for _, wid := range worlds {
				if wid == acc.World {
					w.HomeColor = color
				}
			}
		}
		for _, mp := range m.Maps {
			w.Maps = append(w.Maps, store.WvWMapStats{
				Type: mp.Type, Scores: mp.Scores, Kills: mp.Kills, Deaths: mp.Deaths,
			})
			for _, o := range mp.Objectives {
				color := strings.ToLower(o.Owner)
				if color == "neutral" || color == "" {
					continue
				}
				w.PPT[color] += o.PointsTick
				if w.ObjectivesHeld[color] == nil {
					w.ObjectivesHeld[color] = map[string]int{}
				}
				w.ObjectivesHeld[color][o.Type]++
			}
		}
		p.store.SetWvW(w, time.Now())
		return nil
	})

	p.run(ctx, "pvp", p.intervals.PvP, func(ctx context.Context) error {
		s, err := p.client.PvPStats(ctx)
		if err != nil {
			return err
		}
		p.store.SetPvP(s, time.Now())
		standings, err := p.client.PvPStandings(ctx)
		if err != nil {
			return err
		}
		p.store.SetPvPStandings(standings, time.Now())
		if p.emitter != nil {
			games, err := p.client.PvPGames(ctx)
			if err != nil {
				return err
			}
			p.emitter.OnPvPGames(ctx, games)
		}
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

	// Wardrobe breakdown: unlocked skins/dyes bucketed by static type/rarity.
	// Definitions are cached process-lifetime; only newly-unlocked ids are fetched.
	skinMeta := map[int]gw2.Skin{} // skin id -> type/rarity
	dyeRarity := map[int]string{}  // dye id -> rarity tier
	p.run(ctx, "wardrobe", p.intervals.Wardrobe, func(ctx context.Context) error {
		skinIDs, err := p.client.AccountIntList(ctx, "account/skins")
		if err != nil {
			return err
		}
		var missingSkins []int
		for _, id := range skinIDs {
			if _, ok := skinMeta[id]; !ok {
				missingSkins = append(missingSkins, id)
			}
		}
		if len(missingSkins) > 0 {
			defs, err := p.client.SkinsByIDs(ctx, missingSkins)
			if err != nil {
				return err
			}
			for _, d := range defs {
				skinMeta[d.ID] = d
			}
		}
		skins := map[string]map[string]int{}
		for _, id := range skinIDs {
			m, ok := skinMeta[id]
			if !ok {
				continue // def not returned (e.g. hidden skin) — skip
			}
			if skins[m.Type] == nil {
				skins[m.Type] = map[string]int{}
			}
			skins[m.Type][m.Rarity]++
		}

		dyeIDs, err := p.client.AccountIntList(ctx, "account/dyes")
		if err != nil {
			return err
		}
		var missingDyes []int
		for _, id := range dyeIDs {
			if _, ok := dyeRarity[id]; !ok {
				missingDyes = append(missingDyes, id)
			}
		}
		if len(missingDyes) > 0 {
			defs, err := p.client.ColorsByIDs(ctx, missingDyes)
			if err != nil {
				return err
			}
			for _, d := range defs {
				dyeRarity[d.ID] = colorRarity(d.Categories)
			}
		}
		dyes := map[string]int{}
		for _, id := range dyeIDs {
			dyes[dyeRarity[id]]++
		}

		p.store.SetWardrobe(&store.Wardrobe{Skins: skins, Dyes: dyes}, time.Now())
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
		comm := &store.Commerce{
			CoinsPerGemBuy:  buy.CoinsPerGem,
			CoinsPerGemSell: sell.CoinsPerGem,
			DeliveryCoins:   delivery.Coins,
			DeliveryItems:   int64(len(delivery.Items)),
		}
		if curBuys, err := p.client.TransactionsCurrent(ctx, "buys"); err == nil {
			comm.OpenBuyCount = int64(len(curBuys))
			for _, t := range curBuys {
				comm.OpenBuyValue += t.Price * t.Quantity
			}
		}
		if curSells, err := p.client.TransactionsCurrent(ctx, "sells"); err == nil {
			comm.OpenSellCount = int64(len(curSells))
			for _, t := range curSells {
				comm.OpenSellValue += t.Price * t.Quantity
			}
		}
		p.store.SetCommerce(comm, time.Now())

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

// colorRarityTiers is the set of dye rarity categories the API uses; a color's
// categories array carries exactly one of these alongside hue/material tags.
var colorRarityTiers = map[string]bool{
	"Starter": true, "Common": true, "Uncommon": true, "Rare": true, "Exclusive": true,
}

// colorRarity extracts the rarity tier from a dye's categories, or "Unknown".
func colorRarity(categories []string) string {
	for _, c := range categories {
		if colorRarityTiers[c] {
			return c
		}
	}
	return "Unknown"
}

// achievementPoints computes AP earned for one achievement: tier points for
// every tier reached (all tiers when done), plus repeatable completions capped
// at point_cap. Approximates gw2efficiency's total (ignores the global daily-AP
// cap).
func achievementPoints(def gw2.AchievementDef, acc gw2.AccountAchievement) int64 {
	var reached, all int64
	for _, t := range def.Tiers {
		all += t.Points
		if acc.Done || acc.Current >= t.Count {
			reached += t.Points
		}
	}
	total := reached
	if acc.Repeated > 0 {
		total += acc.Repeated * all
		if def.PointCap > 0 && total > def.PointCap {
			total = def.PointCap
		}
	}
	return total
}

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
