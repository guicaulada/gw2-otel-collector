// Package metrics registers OpenTelemetry observable instruments whose callbacks
// read the latest snapshot from the store at collection time.
//
// Snapshots map to async instruments: lifetime totals (playtime, deaths, account
// age) are Observable Counters fed the raw cumulative value; current-state values
// (level, balance, rank) are Observable Gauges. See docs/telemetry-design.md.
package metrics

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/guicaulada/gw2-otel-collector/internal/store"
)

// Resolver enriches metrics with reference data (id→name, collection totals).
// The reference cache implements it; a nil resolver omits the enrichment.
type Resolver interface {
	CurrencyName(id int) (string, bool)
	CollectionTotal(name string) (int, bool)
	ItemName(id int) (string, bool)
	QuestSeason(id int) (string, bool)
	SeasonTotals() map[string]int
}

// Register creates the gw2.* observable instruments and wires a single callback
// that observes them from the store. It returns the registration so the caller
// can unregister on shutdown. resolver enriches series with reference data.
func Register(st *store.Store, resolver Resolver) (metric.Registration, error) {
	meter := otel.Meter("github.com/guicaulada/gw2-otel-collector/internal/metrics")

	accountAge, err := meter.Int64ObservableCounter(
		"gw2.account.age",
		metric.WithUnit("s"),
		metric.WithDescription("Total account age in seconds"),
	)
	if err != nil {
		return nil, wrap("gw2.account.age", err)
	}
	fractalLevel, err := meter.Int64ObservableGauge(
		"gw2.account.fractal.level",
		metric.WithDescription("Current fractal level"),
	)
	if err != nil {
		return nil, wrap("gw2.account.fractal.level", err)
	}
	wvwRank, err := meter.Int64ObservableGauge(
		"gw2.account.wvw.rank",
		metric.WithDescription("Account WvW rank"),
	)
	if err != nil {
		return nil, wrap("gw2.account.wvw.rank", err)
	}
	charCount, err := meter.Int64ObservableGauge(
		"gw2.account.characters",
		metric.WithUnit("{character}"),
		metric.WithDescription("Number of characters on the account"),
	)
	if err != nil {
		return nil, wrap("gw2.account.characters", err)
	}
	walletBalance, err := meter.Int64ObservableGauge(
		"gw2.account.wallet.balance",
		metric.WithDescription("Wallet balance per currency (coin is in copper)"),
	)
	if err != nil {
		return nil, wrap("gw2.account.wallet.balance", err)
	}
	charPlaytime, err := meter.Int64ObservableCounter(
		"gw2.character.playtime",
		metric.WithUnit("s"),
		metric.WithDescription("Per-character playtime in seconds"),
	)
	if err != nil {
		return nil, wrap("gw2.character.playtime", err)
	}
	charDeaths, err := meter.Int64ObservableCounter(
		"gw2.character.deaths",
		metric.WithUnit("{death}"),
		metric.WithDescription("Per-character lifetime deaths"),
	)
	if err != nil {
		return nil, wrap("gw2.character.deaths", err)
	}
	charLevel, err := meter.Int64ObservableGauge(
		"gw2.character.level",
		metric.WithDescription("Per-character level"),
	)
	if err != nil {
		return nil, wrap("gw2.character.level", err)
	}
	exchangeRate, err := meter.Int64ObservableGauge(
		"gw2.commerce.exchange.coins_per_gem",
		metric.WithDescription("Gem/coin exchange rate in copper, by direction"),
	)
	if err != nil {
		return nil, wrap("gw2.commerce.exchange.coins_per_gem", err)
	}
	deliveryCoins, err := meter.Int64ObservableGauge(
		"gw2.commerce.delivery.coins",
		metric.WithDescription("Copper awaiting pickup in the trading-post delivery box"),
	)
	if err != nil {
		return nil, wrap("gw2.commerce.delivery.coins", err)
	}
	deliveryItems, err := meter.Int64ObservableGauge(
		"gw2.commerce.delivery.items",
		metric.WithUnit("{item}"),
		metric.WithDescription("Item stacks awaiting pickup in the delivery box"),
	)
	if err != nil {
		return nil, wrap("gw2.commerce.delivery.items", err)
	}
	accountAP, err := meter.Int64ObservableGauge(
		"gw2.account.achievement.points",
		metric.WithDescription("Achievement points, by period (daily/monthly)"),
	)
	if err != nil {
		return nil, wrap("gw2.account.achievement.points", err)
	}
	luck, err := meter.Int64ObservableCounter(
		"gw2.account.luck",
		metric.WithDescription("Total essence of luck consumed"),
	)
	if err != nil {
		return nil, wrap("gw2.account.luck", err)
	}
	masteriesUnlocked, err := meter.Int64ObservableGauge(
		"gw2.account.masteries.unlocked",
		metric.WithUnit("{mastery}"),
		metric.WithDescription("Number of trained mastery tracks"),
	)
	if err != nil {
		return nil, wrap("gw2.account.masteries.unlocked", err)
	}
	masteryEarned, err := meter.Int64ObservableCounter(
		"gw2.account.mastery.points.earned",
		metric.WithDescription("Mastery points earned, by region"),
	)
	if err != nil {
		return nil, wrap("gw2.account.mastery.points.earned", err)
	}
	masterySpent, err := meter.Int64ObservableGauge(
		"gw2.account.mastery.points.spent",
		metric.WithDescription("Mastery points spent, by region"),
	)
	if err != nil {
		return nil, wrap("gw2.account.mastery.points.spent", err)
	}
	bankUsed, err := meter.Int64ObservableGauge(
		"gw2.account.bank.slots.used",
		metric.WithUnit("{slot}"),
		metric.WithDescription("Occupied bank slots"),
	)
	if err != nil {
		return nil, wrap("gw2.account.bank.slots.used", err)
	}
	bankCapacity, err := meter.Int64ObservableGauge(
		"gw2.account.bank.slots.capacity",
		metric.WithUnit("{slot}"),
		metric.WithDescription("Total bank slots"),
	)
	if err != nil {
		return nil, wrap("gw2.account.bank.slots.capacity", err)
	}
	sharedUsed, err := meter.Int64ObservableGauge(
		"gw2.account.shared_inventory.slots.used",
		metric.WithUnit("{slot}"),
		metric.WithDescription("Occupied shared inventory slots"),
	)
	if err != nil {
		return nil, wrap("gw2.account.shared_inventory.slots.used", err)
	}
	sharedCapacity, err := meter.Int64ObservableGauge(
		"gw2.account.shared_inventory.slots.capacity",
		metric.WithUnit("{slot}"),
		metric.WithDescription("Total shared inventory slots"),
	)
	if err != nil {
		return nil, wrap("gw2.account.shared_inventory.slots.capacity", err)
	}
	materialCount, err := meter.Int64ObservableGauge(
		"gw2.account.material.count",
		metric.WithUnit("{item}"),
		metric.WithDescription("Material storage count, by category"),
	)
	if err != nil {
		return nil, wrap("gw2.account.material.count", err)
	}
	unlocksCount, err := meter.Int64ObservableGauge(
		"gw2.account.unlocks.count",
		metric.WithDescription("Unlocked items per collection"),
	)
	if err != nil {
		return nil, wrap("gw2.account.unlocks.count", err)
	}
	unlocksTotal, err := meter.Int64ObservableGauge(
		"gw2.account.unlocks.total",
		metric.WithDescription("Total unlockable items per collection (for completion %)"),
	)
	if err != nil {
		return nil, wrap("gw2.account.unlocks.total", err)
	}
	guildLevel, err := meter.Int64ObservableGauge("gw2.guild.level",
		metric.WithDescription("Guild level"))
	if err != nil {
		return nil, wrap("gw2.guild.level", err)
	}
	guildMembers, err := meter.Int64ObservableGauge("gw2.guild.members",
		metric.WithUnit("{member}"), metric.WithDescription("Guild member count"))
	if err != nil {
		return nil, wrap("gw2.guild.members", err)
	}
	guildCapacity, err := meter.Int64ObservableGauge("gw2.guild.member_capacity",
		metric.WithUnit("{member}"), metric.WithDescription("Guild member capacity"))
	if err != nil {
		return nil, wrap("gw2.guild.member_capacity", err)
	}
	guildCurrency, err := meter.Int64ObservableGauge("gw2.guild.currency",
		metric.WithDescription("Guild currency balance, by kind (influence/aetherium/resonance/favor)"))
	if err != nil {
		return nil, wrap("gw2.guild.currency", err)
	}
	guildUpgrades, err := meter.Int64ObservableGauge("gw2.guild.upgrades.completed",
		metric.WithUnit("{upgrade}"), metric.WithDescription("Completed guild upgrades"))
	if err != nil {
		return nil, wrap("gw2.guild.upgrades.completed", err)
	}
	guildTreasury, err := meter.Int64ObservableGauge("gw2.guild.treasury.items",
		metric.WithUnit("{item}"), metric.WithDescription("Distinct items in the guild treasury"))
	if err != nil {
		return nil, wrap("gw2.guild.treasury.items", err)
	}
	guildStashCoins, err := meter.Int64ObservableGauge("gw2.guild.stash.coins",
		metric.WithDescription("Copper across guild stash sections"))
	if err != nil {
		return nil, wrap("gw2.guild.stash.coins", err)
	}
	guildStashUsed, err := meter.Int64ObservableGauge("gw2.guild.stash.slots.used",
		metric.WithUnit("{slot}"), metric.WithDescription("Occupied guild stash slots"))
	if err != nil {
		return nil, wrap("gw2.guild.stash.slots.used", err)
	}
	guildStashSize, err := meter.Int64ObservableGauge("gw2.guild.stash.slots.capacity",
		metric.WithUnit("{slot}"), metric.WithDescription("Total guild stash slots"))
	if err != nil {
		return nil, wrap("gw2.guild.stash.slots.capacity", err)
	}
	guildStorage, err := meter.Int64ObservableGauge("gw2.guild.storage.items",
		metric.WithUnit("{item}"), metric.WithDescription("Distinct guild-storage consumables"))
	if err != nil {
		return nil, wrap("gw2.guild.storage.items", err)
	}
	pvpRank, err := meter.Int64ObservableGauge("gw2.pvp.rank",
		metric.WithDescription("PvP rank"))
	if err != nil {
		return nil, wrap("gw2.pvp.rank", err)
	}
	pvpRankPoints, err := meter.Int64ObservableGauge("gw2.pvp.rank.points",
		metric.WithDescription("PvP rank points toward next rank"))
	if err != nil {
		return nil, wrap("gw2.pvp.rank.points", err)
	}
	pvpMatches, err := meter.Int64ObservableCounter("gw2.pvp.matches",
		metric.WithUnit("{match}"), metric.WithDescription("Lifetime PvP match outcomes, by outcome"))
	if err != nil {
		return nil, wrap("gw2.pvp.matches", err)
	}
	itemPrice, err := meter.Int64ObservableGauge("gw2.commerce.item.price",
		metric.WithDescription("Tracked item best bid/ask in copper, by side"))
	if err != nil {
		return nil, wrap("gw2.commerce.item.price", err)
	}
	itemSpread, err := meter.Int64ObservableGauge("gw2.commerce.item.spread",
		metric.WithDescription("Tracked item sell-minus-buy spread in copper"))
	if err != nil {
		return nil, wrap("gw2.commerce.item.spread", err)
	}
	itemFlipMargin, err := meter.Int64ObservableGauge("gw2.commerce.item.flip_margin",
		metric.WithDescription("Tracked item flip margin in copper (sell*0.85 - buy)"))
	if err != nil {
		return nil, wrap("gw2.commerce.item.flip_margin", err)
	}
	wvMetaProgress, err := meter.Int64ObservableGauge("gw2.wizardsvault.meta.progress",
		metric.WithDescription("Wizard's Vault meta-reward progress, by period"))
	if err != nil {
		return nil, wrap("gw2.wizardsvault.meta.progress", err)
	}
	wvMetaTarget, err := meter.Int64ObservableGauge("gw2.wizardsvault.meta.target",
		metric.WithDescription("Wizard's Vault meta-reward target, by period"))
	if err != nil {
		return nil, wrap("gw2.wizardsvault.meta.target", err)
	}
	wvObjectives, err := meter.Int64ObservableGauge("gw2.wizardsvault.objectives",
		metric.WithUnit("{objective}"), metric.WithDescription("Wizard's Vault objectives, by period"))
	if err != nil {
		return nil, wrap("gw2.wizardsvault.objectives", err)
	}
	wvCompleted, err := meter.Int64ObservableGauge("gw2.wizardsvault.objectives.completed",
		metric.WithUnit("{objective}"), metric.WithDescription("Completed Wizard's Vault objectives, by period"))
	if err != nil {
		return nil, wrap("gw2.wizardsvault.objectives.completed", err)
	}
	wvUnclaimed, err := meter.Int64ObservableGauge("gw2.wizardsvault.acclaim.unclaimed",
		metric.WithDescription("Unclaimed Astral Acclaim from completed objectives, by period"))
	if err != nil {
		return nil, wrap("gw2.wizardsvault.acclaim.unclaimed", err)
	}
	storyCompleted, err := meter.Int64ObservableGauge("gw2.story.quests.completed",
		metric.WithUnit("{quest}"), metric.WithDescription("Completed story quests (union across characters), by season"))
	if err != nil {
		return nil, wrap("gw2.story.quests.completed", err)
	}
	storyTotal, err := meter.Int64ObservableGauge("gw2.story.quests.total",
		metric.WithUnit("{quest}"), metric.WithDescription("Total story quests, by season"))
	if err != nil {
		return nil, wrap("gw2.story.quests.total", err)
	}
	craftingRating, err := meter.Int64ObservableGauge("gw2.character.crafting.rating",
		metric.WithDescription("Per-character crafting discipline rating (0-500)"))
	if err != nil {
		return nil, wrap("gw2.character.crafting.rating", err)
	}
	achievementsTotalAP, err := meter.Int64ObservableGauge("gw2.account.achievement.points.total",
		metric.WithDescription("Computed total achievement points (tier-point sum)"))
	if err != nil {
		return nil, wrap("gw2.account.achievement.points.total", err)
	}
	achievementsDone, err := meter.Int64ObservableGauge("gw2.account.achievements.done",
		metric.WithUnit("{achievement}"), metric.WithDescription("Achievements completed"))
	if err != nil {
		return nil, wrap("gw2.account.achievements.done", err)
	}
	achievementsTracked, err := meter.Int64ObservableGauge("gw2.account.achievements.tracked",
		metric.WithUnit("{achievement}"), metric.WithDescription("Achievements with account progress"))
	if err != nil {
		return nil, wrap("gw2.account.achievements.tracked", err)
	}
	fractalAugment, err := meter.Int64ObservableGauge("gw2.account.fractal.augmentation",
		metric.WithDescription("Fractal augmentation level, by type"))
	if err != nil {
		return nil, wrap("gw2.account.fractal.augmentation", err)
	}
	legendaryOwned, err := meter.Int64ObservableGauge("gw2.account.legendary_armory.owned",
		metric.WithDescription("Distinct legendaries unlocked in the armory"))
	if err != nil {
		return nil, wrap("gw2.account.legendary_armory.owned", err)
	}
	legendaryCopies, err := meter.Int64ObservableGauge("gw2.account.legendary_armory.copies",
		metric.WithDescription("Total legendary copies owned"))
	if err != nil {
		return nil, wrap("gw2.account.legendary_armory.copies", err)
	}
	legendaryAvailable, err := meter.Int64ObservableGauge("gw2.account.legendary_armory.available",
		metric.WithDescription("Distinct legendaries in the armory (denominator)"))
	if err != nil {
		return nil, wrap("gw2.account.legendary_armory.available", err)
	}
	wvwScore, err := meter.Int64ObservableGauge("gw2.wvw.match.score",
		metric.WithDescription("WvW war score, by team color"))
	if err != nil {
		return nil, wrap("gw2.wvw.match.score", err)
	}
	wvwVP, err := meter.Int64ObservableGauge("gw2.wvw.match.victory_points",
		metric.WithDescription("WvW victory points, by team color"))
	if err != nil {
		return nil, wrap("gw2.wvw.match.victory_points", err)
	}
	wvwKills, err := meter.Int64ObservableGauge("gw2.wvw.match.kills",
		metric.WithDescription("WvW kills this matchup, by team color"))
	if err != nil {
		return nil, wrap("gw2.wvw.match.kills", err)
	}
	wvwDeaths, err := meter.Int64ObservableGauge("gw2.wvw.match.deaths",
		metric.WithDescription("WvW deaths this matchup, by team color"))
	if err != nil {
		return nil, wrap("gw2.wvw.match.deaths", err)
	}
	wvwPPT, err := meter.Int64ObservableGauge("gw2.wvw.match.ppt",
		metric.WithDescription("WvW points-per-tick, by team color (derived)"))
	if err != nil {
		return nil, wrap("gw2.wvw.match.ppt", err)
	}
	wvwObjectives, err := meter.Int64ObservableGauge("gw2.wvw.objectives.held",
		metric.WithUnit("{objective}"), metric.WithDescription("WvW objectives held, by team color and type"))
	if err != nil {
		return nil, wrap("gw2.wvw.objectives.held", err)
	}
	wvwHome, err := meter.Int64ObservableGauge("gw2.wvw.home_team",
		metric.WithDescription("Which team color is the account's home world (value 1)"))
	if err != nil {
		return nil, wrap("gw2.wvw.home_team", err)
	}
	resetCompleted, err := meter.Int64ObservableGauge("gw2.account.reset.completed",
		metric.WithDescription("Reset-cycle completions since the last reset, by kind (worldbosses/dungeons/raids/mapchests/dailycrafting)"))
	if err != nil {
		return nil, wrap("gw2.account.reset.completed", err)
	}
	accountValue, err := meter.Int64ObservableGauge("gw2.account.value",
		metric.WithUnit("{copper}"),
		metric.WithDescription("Liquid account value in copper, by component and price basis"))
	if err != nil {
		return nil, wrap("gw2.account.value", err)
	}
	lastSuccess, err := meter.Float64ObservableGauge(
		"gw2.poll.last_success.timestamp",
		metric.WithUnit("s"),
		metric.WithDescription("Unix timestamp of the last successful poll per family"),
	)
	if err != nil {
		return nil, wrap("gw2.poll.last_success.timestamp", err)
	}

	callback := func(_ context.Context, o metric.Observer) error {
		if a := st.Account(); a != nil {
			o.ObserveInt64(accountAge, a.Age)
			o.ObserveInt64(fractalLevel, int64(a.FractalLevel))
			o.ObserveInt64(wvwRank, int64(a.WvW.Rank))
			o.ObserveInt64(accountAP, int64(a.DailyAP),
				metric.WithAttributes(attribute.String("gw2.period", "daily")))
			o.ObserveInt64(accountAP, int64(a.MonthlyAP),
				metric.WithAttributes(attribute.String("gw2.period", "monthly")))
		}

		if p := st.Progression(); p != nil {
			o.ObserveInt64(luck, p.Luck)
			o.ObserveInt64(masteriesUnlocked, int64(p.MasteriesCount))
			for region, pts := range p.PointsByRegion {
				attrs := metric.WithAttributes(attribute.String("gw2.region", region))
				o.ObserveInt64(masteryEarned, pts.Earned, attrs)
				o.ObserveInt64(masterySpent, pts.Spent, attrs)
			}
			for kind, val := range p.FractalAugments {
				o.ObserveInt64(fractalAugment, val,
					metric.WithAttributes(attribute.String("gw2.augmentation", kind)))
			}
			o.ObserveInt64(legendaryOwned, int64(p.LegendaryOwned))
			o.ObserveInt64(legendaryCopies, p.LegendaryCopies)
			o.ObserveInt64(legendaryAvailable, int64(p.LegendaryAvailable))
		}

		if a := st.Achievements(); a != nil {
			o.ObserveInt64(achievementsTotalAP, a.TotalAP)
			o.ObserveInt64(achievementsDone, int64(a.Done))
			o.ObserveInt64(achievementsTracked, int64(a.Total))
		}

		for kind, n := range st.Resets() {
			o.ObserveInt64(resetCompleted, int64(n),
				metric.WithAttributes(attribute.String("gw2.kind", kind)))
		}

		if w := st.WvW(); w != nil {
			obs := func(inst metric.Int64Observable, m map[string]int64) {
				for color, v := range m {
					o.ObserveInt64(inst, v, metric.WithAttributes(attribute.String("gw2.team", color)))
				}
			}
			obs(wvwScore, w.Score)
			obs(wvwVP, w.VictoryPoints)
			obs(wvwKills, w.Kills)
			obs(wvwDeaths, w.Deaths)
			obs(wvwPPT, w.PPT)
			for color, byType := range w.ObjectivesHeld {
				for typ, n := range byType {
					o.ObserveInt64(wvwObjectives, int64(n), metric.WithAttributes(
						attribute.String("gw2.team", color), attribute.String("gw2.objective_type", typ)))
				}
			}
			if w.HomeColor != "" {
				o.ObserveInt64(wvwHome, 1, metric.WithAttributes(attribute.String("gw2.team", w.HomeColor)))
			}
		}

		if s := st.Storage(); s != nil {
			o.ObserveInt64(bankUsed, s.BankUsed)
			o.ObserveInt64(bankCapacity, s.BankCapacity)
			o.ObserveInt64(sharedUsed, s.SharedUsed)
			o.ObserveInt64(sharedCapacity, s.SharedCapacity)
			for category, count := range s.MaterialsByCategory {
				o.ObserveInt64(materialCount, count,
					metric.WithAttributes(attribute.Int("gw2.material.category", category)))
			}
		}

		if w := st.Wallet(); w != nil {
			for _, c := range w {
				attrs := []attribute.KeyValue{attribute.Int("gw2.currency.id", c.ID)}
				if resolver != nil {
					if name, ok := resolver.CurrencyName(c.ID); ok {
						attrs = append(attrs, attribute.String("gw2.currency.name", name))
					}
				}
				o.ObserveInt64(walletBalance, c.Value, metric.WithAttributes(attrs...))
			}
		}

		if chars := st.Characters(); chars != nil {
			o.ObserveInt64(charCount, int64(len(chars)))
			for _, c := range chars {
				attrs := metric.WithAttributes(
					attribute.String("gw2.character.name", c.Name),
					attribute.String("gw2.character.profession", c.Profession),
					attribute.String("gw2.character.race", c.Race),
				)
				o.ObserveInt64(charPlaytime, c.Age, attrs)
				o.ObserveInt64(charDeaths, c.Deaths, attrs)
				o.ObserveInt64(charLevel, int64(c.Level), attrs)
				for _, cr := range c.Crafting {
					o.ObserveInt64(craftingRating, int64(cr.Rating), metric.WithAttributes(
						attribute.String("gw2.character.name", c.Name),
						attribute.String("gw2.discipline", cr.Discipline)))
				}
			}
		}

		if c := st.Commerce(); c != nil {
			o.ObserveInt64(exchangeRate, c.CoinsPerGemBuy,
				metric.WithAttributes(attribute.String("gw2.direction", "buy_gems")))
			o.ObserveInt64(exchangeRate, c.CoinsPerGemSell,
				metric.WithAttributes(attribute.String("gw2.direction", "sell_gems")))
			o.ObserveInt64(deliveryCoins, c.DeliveryCoins)
			o.ObserveInt64(deliveryItems, c.DeliveryItems)
		}

		for _, gi := range st.Guilds() {
			g := gi.Guild
			attrs := metric.WithAttributes(
				attribute.String("gw2.guild.id", g.ID),
				attribute.String("gw2.guild.name", g.Name),
				attribute.String("gw2.guild.tag", g.Tag),
			)
			o.ObserveInt64(guildLevel, int64(g.Level), attrs)
			o.ObserveInt64(guildMembers, int64(g.MemberCount), attrs)
			o.ObserveInt64(guildCapacity, int64(g.MemberCapacity), attrs)
			for kind, val := range map[string]int64{
				"influence": g.Influence, "aetherium": g.Aetherium,
				"resonance": g.Resonance, "favor": g.Favor,
			} {
				o.ObserveInt64(guildCurrency, val, metric.WithAttributes(
					attribute.String("gw2.guild.id", g.ID),
					attribute.String("gw2.guild.name", g.Name),
					attribute.String("gw2.guild.tag", g.Tag),
					attribute.String("gw2.currency", kind),
				))
			}
			if gi.UpgradesCompleted >= 0 { // leader-only internals
				o.ObserveInt64(guildUpgrades, int64(gi.UpgradesCompleted), attrs)
				o.ObserveInt64(guildTreasury, int64(gi.TreasuryItems), attrs)
				o.ObserveInt64(guildStashCoins, gi.StashCoins, attrs)
				o.ObserveInt64(guildStashUsed, gi.StashSlotsUsed, attrs)
				o.ObserveInt64(guildStashSize, gi.StashSlotsSize, attrs)
				o.ObserveInt64(guildStorage, int64(gi.StorageItems), attrs)
			}
		}

		if p := st.PvP(); p != nil {
			o.ObserveInt64(pvpRank, int64(p.PvPRank))
			o.ObserveInt64(pvpRankPoints, int64(p.PvPRankPoints))
			for outcome, n := range map[string]int64{
				"win": p.Aggregate.Wins, "loss": p.Aggregate.Losses,
				"desertion": p.Aggregate.Desertions, "bye": p.Aggregate.Byes,
				"forfeit": p.Aggregate.Forfeits,
			} {
				o.ObserveInt64(pvpMatches, n,
					metric.WithAttributes(attribute.String("gw2.outcome", outcome)))
			}
		}

		for name, count := range st.Unlocks() {
			attrs := metric.WithAttributes(attribute.String("gw2.collection", name))
			o.ObserveInt64(unlocksCount, int64(count), attrs)
			if resolver != nil {
				if total, ok := resolver.CollectionTotal(name); ok {
					o.ObserveInt64(unlocksTotal, int64(total), attrs)
				}
			}
		}

		for _, wv := range st.WizardsVault() {
			attrs := metric.WithAttributes(attribute.String("gw2.period", wv.Period))
			if wv.HasMeta {
				o.ObserveInt64(wvMetaProgress, wv.MetaCurrent, attrs)
				o.ObserveInt64(wvMetaTarget, wv.MetaComplete, attrs)
			}
			o.ObserveInt64(wvObjectives, int64(wv.Objectives), attrs)
			o.ObserveInt64(wvCompleted, int64(wv.Completed), attrs)
			o.ObserveInt64(wvUnclaimed, wv.UnclaimedAcclaim, attrs)
		}

		for _, p := range st.Prices() {
			base := []attribute.KeyValue{attribute.Int("gw2.item.id", p.ID)}
			if resolver != nil {
				if name, ok := resolver.ItemName(p.ID); ok {
					base = append(base, attribute.String("gw2.item.name", name))
				}
			}
			o.ObserveInt64(itemPrice, p.Buys.UnitPrice,
				metric.WithAttributes(append(base, attribute.String("gw2.side", "buy"))...))
			o.ObserveInt64(itemPrice, p.Sells.UnitPrice,
				metric.WithAttributes(append(base, attribute.String("gw2.side", "sell"))...))
			o.ObserveInt64(itemSpread, p.Sells.UnitPrice-p.Buys.UnitPrice,
				metric.WithAttributes(base...))
			// Flip margin nets the 15% trading-post tax off the sell price.
			o.ObserveInt64(itemFlipMargin, int64(float64(p.Sells.UnitPrice)*0.85)-p.Buys.UnitPrice,
				metric.WithAttributes(base...))
		}

		if resolver != nil {
			if completed := st.StoryCompleted(); completed != nil {
				bySeason := map[string]int64{}
				for _, qid := range completed {
					if season, ok := resolver.QuestSeason(qid); ok {
						bySeason[season]++
					}
				}
				for season, n := range bySeason {
					o.ObserveInt64(storyCompleted, n,
						metric.WithAttributes(attribute.String("gw2.season", season)))
				}
			}
			for season, total := range resolver.SeasonTotals() {
				o.ObserveInt64(storyTotal, int64(total),
					metric.WithAttributes(attribute.String("gw2.season", season)))
			}
		}

		if v := st.AccountValue(); v != nil {
			for component, copper := range v.Buy {
				o.ObserveInt64(accountValue, copper, metric.WithAttributes(
					attribute.String("gw2.component", component),
					attribute.String("gw2.basis", "buy")))
			}
			for component, copper := range v.Sell {
				o.ObserveInt64(accountValue, copper, metric.WithAttributes(
					attribute.String("gw2.component", component),
					attribute.String("gw2.basis", "sell")))
			}
		}

		for family, ts := range st.LastSuccess() {
			o.ObserveFloat64(lastSuccess, float64(ts.Unix()),
				metric.WithAttributes(attribute.String("gw2.family", family)))
		}
		return nil
	}

	return meter.RegisterCallback(callback,
		accountAge, fractalLevel, wvwRank, charCount, walletBalance,
		charPlaytime, charDeaths, charLevel,
		exchangeRate, deliveryCoins, deliveryItems,
		accountAP, luck, masteriesUnlocked, masteryEarned, masterySpent,
		bankUsed, bankCapacity, sharedUsed, sharedCapacity, materialCount,
		unlocksCount, unlocksTotal,
		guildLevel, guildMembers, guildCapacity, guildCurrency, guildUpgrades,
		guildTreasury, guildStashCoins, guildStashUsed, guildStashSize, guildStorage,
		pvpRank, pvpRankPoints, pvpMatches,
		itemPrice, itemSpread, itemFlipMargin,
		wvMetaProgress, wvMetaTarget, wvObjectives, wvCompleted, wvUnclaimed,
		storyCompleted, storyTotal, accountValue,
		craftingRating, achievementsTotalAP, achievementsDone, achievementsTracked,
		fractalAugment, legendaryOwned, legendaryCopies, legendaryAvailable,
		resetCompleted,
		wvwScore, wvwVP, wvwKills, wvwDeaths, wvwPPT, wvwObjectives, wvwHome,
		lastSuccess,
	)
}

func wrap(name string, err error) error {
	return fmt.Errorf("create instrument %s: %w", name, err)
}
