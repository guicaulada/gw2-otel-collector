package poller

import (
	"testing"

	"github.com/guicaulada/gw2-otel-collector/internal/gw2"
)

func tier(count int, points int64) struct {
	Count  int   `json:"count"`
	Points int64 `json:"points"`
} {
	return struct {
		Count  int   `json:"count"`
		Points int64 `json:"points"`
	}{count, points}
}

func TestAchievementPoints(t *testing.T) {
	def := gw2.AchievementDef{Tiers: []struct {
		Count  int   `json:"count"`
		Points int64 `json:"points"`
	}{tier(5, 5), tier(10, 10)}} // 15 points across two tiers

	cases := []struct {
		name string
		def  gw2.AchievementDef
		acc  gw2.AccountAchievement
		want int64
	}{
		{"not started", def, gw2.AccountAchievement{Current: 0}, 0},
		{"first tier reached", def, gw2.AccountAchievement{Current: 7}, 5},
		{"all tiers via current", def, gw2.AccountAchievement{Current: 10}, 15},
		{"done flag reaches all", def, gw2.AccountAchievement{Done: true}, 15},
		{"repeatable adds full sweeps", def, gw2.AccountAchievement{Done: true, Repeated: 2}, 15 + 2*15},
		{"point cap clamps total",
			gw2.AchievementDef{PointCap: 40, Tiers: def.Tiers},
			gw2.AccountAchievement{Done: true, Repeated: 5}, 40},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := achievementPoints(tc.def, tc.acc); got != tc.want {
				t.Errorf("achievementPoints = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestIsLegendaryCategory(t *testing.T) {
	yes := []string{"Legendary Weapons", "Legendary Armor", "Precursor Weapons", "legendary trinkets"}
	no := []string{"Collections", "Festival", "Story Journal", ""}
	for _, n := range yes {
		if !isLegendaryCategory(n) {
			t.Errorf("isLegendaryCategory(%q) = false, want true", n)
		}
	}
	for _, n := range no {
		if isLegendaryCategory(n) {
			t.Errorf("isLegendaryCategory(%q) = true, want false", n)
		}
	}
}

func TestColorRarity(t *testing.T) {
	cases := []struct {
		name       string
		categories []string
		want       string
	}{
		{"rare dye", []string{"Gray", "Metal", "Rare"}, "Rare"},
		{"common dye", []string{"Blue", "Vibrant", "Common"}, "Common"},
		{"rarity not last", []string{"Exclusive", "Red"}, "Exclusive"},
		{"no rarity", []string{"Blue", "Vibrant"}, "Unknown"},
		{"empty", nil, "Unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := colorRarity(tc.categories); got != tc.want {
				t.Errorf("colorRarity(%v) = %q, want %q", tc.categories, got, tc.want)
			}
		})
	}
}
