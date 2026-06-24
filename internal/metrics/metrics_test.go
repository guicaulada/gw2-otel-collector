package metrics

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/guicaulada/gw2-otel-collector/internal/store"
	"github.com/guicaulada/gw2-otel-collector/internal/value"
)

// fakeResolver resolves only material category 49, to verify name enrichment.
type fakeResolver struct{}

func (fakeResolver) CurrencyName(int) (string, bool) { return "", false }
func (fakeResolver) MaterialCategoryName(id int) (string, bool) {
	if id == 49 {
		return "Cooking Ingredients", true
	}
	return "", false
}
func (fakeResolver) CollectionTotal(string) (int, bool) { return 0, false }
func (fakeResolver) ItemName(int) (string, bool)        { return "", false }
func (fakeResolver) QuestSeason(int) (string, bool)     { return "", false }
func (fakeResolver) SeasonTotals() map[string]int       { return nil }

func collect(t *testing.T, st *store.Store) metricdata.ResourceMetrics {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	reg, err := Register(st, fakeResolver{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { _ = reg.Unregister() })
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return rm
}

// intPoints returns the int64 gauge data points for the named metric.
func intPoints(rm metricdata.ResourceMetrics, name string) []metricdata.DataPoint[int64] {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				if g, ok := m.Data.(metricdata.Gauge[int64]); ok {
					return g.DataPoints
				}
			}
		}
	}
	return nil
}

func TestMaterialCountCarriesResolvedCategoryName(t *testing.T) {
	st := store.New()
	st.SetStorage(&store.Storage{MaterialsByCategory: map[int]int64{49: 12, 5: 3}}, time.Now())

	pts := intPoints(collect(t, st), "gw2.account.material.count")
	if len(pts) != 2 {
		t.Fatalf("material.count points = %d, want 2", len(pts))
	}
	var sawNamed bool
	for _, p := range pts {
		id, _ := p.Attributes.Value("gw2.material.category")
		name, hasName := p.Attributes.Value("gw2.material.category.name")
		if id.AsInt64() == 49 {
			sawNamed = true
			if p.Value != 12 {
				t.Errorf("category 49 count = %d, want 12", p.Value)
			}
			if !hasName || name.AsString() != "Cooking Ingredients" {
				t.Errorf("category 49 name = (%q, %v), want Cooking Ingredients", name.AsString(), hasName)
			}
		}
		if id.AsInt64() == 5 { // category 5 unresolved by the fake resolver
			if _, hasName := p.Attributes.Value("gw2.material.category.name"); hasName {
				t.Error("category 5 should have no resolved name")
			}
		}
	}
	if !sawNamed {
		t.Error("expected a data point for category 49")
	}
}

func TestAccountValueComponentsObserved(t *testing.T) {
	st := store.New()
	st.SetAccountValue(&value.Account{
		Buy:  map[string]int64{"total": 1000, "wallet": 600},
		Sell: map[string]int64{"total": 1200, "wallet": 600},
	}, time.Now())

	pts := intPoints(collect(t, st), "gw2.account.value")
	if len(pts) == 0 {
		t.Fatal("no gw2.account.value points observed")
	}
	var totalSell int64 = -1
	for _, p := range pts {
		comp, _ := p.Attributes.Value("gw2.component")
		basis, _ := p.Attributes.Value("gw2.basis")
		if comp.AsString() == "total" && basis.AsString() == "sell" {
			totalSell = p.Value
		}
	}
	if totalSell != 1200 {
		t.Errorf("total sell value = %d, want 1200", totalSell)
	}
}
