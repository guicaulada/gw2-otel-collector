package value

import "testing"

import "github.com/guicaulada/gw2-otel-collector/internal/gw2"

func price(id int, buy, sell int64) gw2.ItemPrice {
	var p gw2.ItemPrice
	p.ID = id
	p.Buys.UnitPrice = buy
	p.Sells.UnitPrice = sell
	return p
}

func TestComputeValuesComponentsAndTotal(t *testing.T) {
	bank := []*gw2.Slot{{ID: 1, Count: 10}, nil, {ID: 2, Count: 5}}
	materials := []gw2.MaterialAmount{{ID: 1, Count: 100}}
	shared := []*gw2.Slot{{ID: 2, Count: 1}}
	chars := []gw2.Character{{Bags: []*gw2.CharacterBag{{Inventory: []*gw2.Slot{{ID: 1, Count: 2}, nil}}}}}
	prices := map[int]gw2.ItemPrice{
		1: price(1, 10, 12),
		2: price(2, 100, 150),
		// id 3 (none) untradable -> 0
	}

	acc := Compute(bank, materials, shared, chars, 50_000, prices)

	// bank: 10×id1 + 5×id2 -> buy 10*10+5*100=600, sell 10*12+5*150=870
	if acc.Buy["bank"] != 600 || acc.Sell["bank"] != 870 {
		t.Errorf("bank = buy %d sell %d, want 600/870", acc.Buy["bank"], acc.Sell["bank"])
	}
	// materials: 100×id1 -> buy 1000 sell 1200
	if acc.Buy["materials"] != 1000 || acc.Sell["materials"] != 1200 {
		t.Errorf("materials = buy %d sell %d, want 1000/1200", acc.Buy["materials"], acc.Sell["materials"])
	}
	// shared: 1×id2 -> buy 100 sell 150
	if acc.Buy["shared"] != 100 {
		t.Errorf("shared buy = %d, want 100", acc.Buy["shared"])
	}
	// characters: 2×id1 -> buy 20 sell 24
	if acc.Buy["characters"] != 20 || acc.Sell["characters"] != 24 {
		t.Errorf("characters = buy %d sell %d, want 20/24", acc.Buy["characters"], acc.Sell["characters"])
	}
	// wallet liquid on both bases
	if acc.Buy["wallet"] != 50_000 || acc.Sell["wallet"] != 50_000 {
		t.Errorf("wallet = %d/%d, want 50000 both", acc.Buy["wallet"], acc.Sell["wallet"])
	}
	// total buy = 600+1000+100+20+50000 = 51720
	if acc.Buy["total"] != 51_720 {
		t.Errorf("total buy = %d, want 51720", acc.Buy["total"])
	}
}
