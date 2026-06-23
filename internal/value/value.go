// Package value computes liquid account value: item counts × trading-post prices,
// summed into bounded per-component aggregates (not per-item series). Untradable /
// account-bound items have no TP price and contribute 0 — so this is a liquid TP
// valuation, as the community tools frame it. See docs/feature-coverage.md §1.
package value

import "github.com/guicaulada/gw2-otel-collector/internal/gw2"

// Account is the computed value breakdown, in copper, by component and basis.
type Account struct {
	Buy              map[string]int64 // component -> value at best buy-order price (gross)
	Sell             map[string]int64 // component -> value at lowest sell-listing price (gross)
	MaterialCategory map[int]int64    // material category id -> sell-basis value
}

// itemCounts accumulates count per item id for one component.
type itemCounts map[int]int64

func slotCounts(slots []*gw2.Slot, dst itemCounts) {
	for _, s := range slots {
		if s != nil {
			dst[s.ID] += s.Count
		}
	}
}

// Compute builds the value breakdown from raw account data and a price table.
// walletCopper is the already-liquid wallet value (coin + gems→gold).
func Compute(
	bank []*gw2.Slot,
	materials []gw2.MaterialAmount,
	shared []*gw2.Slot,
	characters []gw2.Character,
	walletCopper int64,
	prices map[int]gw2.ItemPrice,
) Account {
	components := map[string]itemCounts{
		"bank":       {},
		"materials":  {},
		"shared":     {},
		"characters": {},
		"equipment":  {},
	}

	slotCounts(bank, components["bank"])
	slotCounts(shared, components["shared"])
	materialCategory := map[int]int64{} // category -> sell value
	for _, m := range materials {
		components["materials"][m.ID] += m.Count
		if p, ok := prices[m.ID]; ok {
			materialCategory[m.Category] += m.Count * p.Sells.UnitPrice
		}
	}
	equip := components["equipment"]
	for _, c := range characters {
		for _, bag := range c.Bags {
			if bag != nil {
				slotCounts(bag.Inventory, components["characters"])
			}
		}
		// Equipped gear: the piece itself is usually account-bound (unpriced),
		// but its upgrades (runes/sigils) and infusions are often tradable.
		for _, e := range c.Equipment {
			if e == nil {
				continue
			}
			equip[e.ID]++
			for _, up := range e.Upgrades {
				equip[up]++
			}
			for _, inf := range e.Infusions {
				equip[inf]++
			}
		}
	}

	acc := Account{Buy: map[string]int64{}, Sell: map[string]int64{}, MaterialCategory: materialCategory}
	var totalBuy, totalSell int64
	for name, counts := range components {
		var buy, sell int64
		for id, n := range counts {
			p, ok := prices[id]
			if !ok {
				continue // untradable / unpriced -> 0
			}
			buy += n * p.Buys.UnitPrice
			sell += n * p.Sells.UnitPrice
		}
		acc.Buy[name] = buy
		acc.Sell[name] = sell
		totalBuy += buy
		totalSell += sell
	}

	// Wallet is already liquid gold; same on both bases.
	acc.Buy["wallet"] = walletCopper
	acc.Sell["wallet"] = walletCopper
	totalBuy += walletCopper
	totalSell += walletCopper

	acc.Buy["total"] = totalBuy
	acc.Sell["total"] = totalSell
	return acc
}
