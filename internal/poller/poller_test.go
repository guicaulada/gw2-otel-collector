package poller

import "testing"

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
