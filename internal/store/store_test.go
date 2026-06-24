package store

import (
	"sync"
	"testing"
	"time"

	"github.com/guicaulada/gw2-otel-collector/internal/gw2"
)

func TestRoundTripAndLastSuccess(t *testing.T) {
	s := New()

	if s.Account() != nil || s.Wardrobe() != nil || s.Legendary() != nil {
		t.Fatal("fresh store should return nil snapshots")
	}

	t0 := time.Unix(1000, 0)
	s.SetAccount(&gw2.Account{Name: "Tester", Age: 42}, t0)
	if a := s.Account(); a == nil || a.Name != "Tester" || a.Age != 42 {
		t.Errorf("Account round-trip failed: %+v", a)
	}

	s.SetWardrobe(&Wardrobe{Skins: map[string]map[string]int{"Armor": {"Exotic": 3}}, Dyes: map[string]int{"Rare": 5}}, t0)
	if w := s.Wardrobe(); w == nil || w.Skins["Armor"]["Exotic"] != 3 || w.Dyes["Rare"] != 5 {
		t.Errorf("Wardrobe round-trip failed: %+v", w)
	}

	s.SetLegendary(map[string]LegendaryProgress{"Legendary Weapons": {Done: 1, Total: 104, ItemsCurrent: 41, ItemsMax: 200}}, t0)
	if l := s.Legendary()["Legendary Weapons"]; l.Done != 1 || l.Total != 104 || l.ItemsCurrent != 41 {
		t.Errorf("Legendary round-trip failed: %+v", l)
	}

	ls := s.LastSuccess()
	for _, fam := range []string{"account", "wardrobe", "legendary"} {
		if got, ok := ls[fam]; !ok || !got.Equal(t0) {
			t.Errorf("LastSuccess[%q] = (%v, %v), want (%v, true)", fam, got, ok, t0)
		}
	}
}

func TestLastSuccessReturnsCopy(t *testing.T) {
	s := New()
	s.SetAccount(&gw2.Account{}, time.Unix(1, 0))
	ls := s.LastSuccess()
	ls["account"] = time.Unix(999, 0) // mutate the returned map
	if s.LastSuccess()["account"].Equal(time.Unix(999, 0)) {
		t.Error("LastSuccess must return a copy; caller mutation leaked into the store")
	}
}

// Exercises the RWMutex under the race detector.
func TestConcurrentAccess(t *testing.T) {
	s := New()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(n int) { defer wg.Done(); s.SetAccount(&gw2.Account{Age: int64(n)}, time.Now()) }(i)
		go func() { defer wg.Done(); _ = s.Account(); _ = s.LastSuccess() }()
	}
	wg.Wait()
	if s.Account() == nil {
		t.Error("expected an account after concurrent writes")
	}
}
