package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/grafana/grafana-foundation-sdk/go/cog"
	v2 "github.com/grafana/grafana-foundation-sdk/go/dashboardv2beta1"
)

// eventAnnotations keeps the built-in alerts layer and overlays GW2 domain
// events from Loki (level-ups, sales, PvP games, …) on the time graphs.
func eventAnnotations() []cog.Builder[v2.AnnotationQueryKind] {
	builtIn := true
	return []cog.Builder[v2.AnnotationQueryKind]{
		L(v2.AnnotationQueryKind{Kind: "AnnotationQuery", Spec: v2.AnnotationQuerySpec{
			Name: "Annotations & Alerts", Enable: true, Hide: true, BuiltIn: &builtIn,
			IconColor: "rgba(0, 211, 255, 1)",
			Query: v2.DataQueryKind{Kind: "DataQuery", Group: "grafana", Version: "v0",
				Datasource: &v2.Dashboardv2beta1DataQueryKindDatasource{Name: sp("-- Grafana --")},
				Spec:       map[string]any{}}}}),
		L(v2.AnnotationQueryKind{Kind: "AnnotationQuery", Spec: v2.AnnotationQuerySpec{
			Name: "GW2 events", Enable: true, Hide: false, IconColor: cGold,
			Query: v2.DataQueryKind{Kind: "DataQuery", Group: "loki", Version: "v0",
				Datasource: &v2.Dashboardv2beta1DataQueryKindDatasource{Name: sp("loki")},
				Spec:       map[string]any{"expr": `{service_name="gw2-otel-collector"}`, "refId": "Anno"}}}}),
	}
}

// characterVar is the per-character drill-down used on the Characters tab.
func characterVar() cog.Builder[v2.QueryVariableKind] {
	return v2.NewQueryVariableBuilder("character").
		Label("Character").
		Query(L(v2.DataQueryKind{Kind: "DataQuery", Group: "prometheus", Version: "v0",
			Datasource: &v2.Dashboardv2beta1DataQueryKindDatasource{Name: sp("prometheus")},
			Spec:       map[string]any{"query": "label_values(gw2_character_level, gw2_character_name)", "refId": "A"}})).
		Refresh(v2.VariableRefreshOnDashboardLoad).
		Sort(v2.VariableSortAlphabeticalAsc).
		Multi(true).IncludeAll(true).AllowCustomValue(false).
		Current(v2.VariableOption{Text: v2.StringOrArrayOfString{String: sp("All")}, Value: v2.StringOrArrayOfString{String: sp("$__all")}})
}

func timeSettings() cog.Builder[v2.TimeSettingsSpec] {
	return L(v2.TimeSettingsSpec{From: "now-24h", To: "now", AutoRefresh: "30s",
		Timezone:             sp("browser"),
		AutoRefreshIntervals: []string{"5s", "10s", "30s", "1m", "5m", "15m", "30m", "1h"}})
}

func main() {
	tabs := []struct {
		title string
		g     *Grid
	}{
		{"Overview", overview()},
		{"Wealth", wealth()},
		{"Progression", progression()},
		{"Collections", collections()},
		{"Characters", characters()},
		{"PvP", pvp()},
		{"WvW", wvw()},
		{"Health", health()},
	}

	db := v2.NewDashboardBuilder("GW2 Account").
		Tags([]string{"gw2"}).
		Editable(true).
		Annotations(eventAnnotations()).
		QueryVariable(characterVar()).
		TimeSettings(timeSettings())

	var tabKinds []cog.Builder[v2.TabsLayoutTabKind]
	pid := 0
	for _, t := range tabs {
		var items []cog.Builder[v2.GridLayoutItemKind]
		for _, p := range t.g.panels {
			pid++
			key := fmt.Sprintf("panel-%d", pid)
			db = db.Panel(key, p.b.Id(float64(pid)))
			items = append(items, v2.NewGridItemBuilder().
				X(p.x).Y(p.y).Width(p.w).Height(p.h).Name(key))
		}
		tabKinds = append(tabKinds, v2.NewTabBuilder().Title(t.title).
			GridLayout(v2.NewGridBuilder().Items(items)))
	}
	db = db.TabsLayout(v2.NewTabsBuilder().Tabs(tabKinds))

	spec, err := db.Build()
	if err != nil {
		fmt.Fprintln(os.Stderr, "build dashboard:", err)
		os.Exit(1)
	}

	resource := map[string]any{
		"apiVersion": "dashboard.grafana.app/v2beta1",
		"kind":       "Dashboard",
		"metadata":   map[string]any{"name": "gw2", "namespace": "default"},
		"spec":       spec,
	}
	out, err := json.MarshalIndent(resource, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "marshal:", err)
		os.Exit(1)
	}

	_, thisFile, _, _ := runtime.Caller(0)
	dest := filepath.Join(filepath.Dir(thisFile), "gw2.json")
	if err := os.WriteFile(dest, append(out, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d tabs, %d panels)\n", dest, len(tabs), pid)
}
