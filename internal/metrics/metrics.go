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

// CurrencyNamer resolves a currency id to a human-readable name. The reference
// cache implements it; a nil namer simply omits the name attribute.
type CurrencyNamer interface {
	CurrencyName(id int) (string, bool)
}

// Register creates the gw2.* observable instruments and wires a single callback
// that observes them from the store. It returns the registration so the caller
// can unregister on shutdown. namer enriches wallet series with currency names.
func Register(st *store.Store, namer CurrencyNamer) (metric.Registration, error) {
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
				if namer != nil {
					if name, ok := namer.CurrencyName(c.ID); ok {
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
		lastSuccess,
	)
}

func wrap(name string, err error) error {
	return fmt.Errorf("create instrument %s: %w", name, err)
}
