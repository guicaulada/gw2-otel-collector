// Command gen-dashboards builds the GW2 Grafana dashboard as code using the
// Grafana Foundation SDK (schema v2beta1 / TabsLayout) and writes gw2.json.
//
// Run: `go run ./deploy/dashboards` (writes gw2.json next to the source).
// The file provider in deploy/grafana/provisioning mounts this directory into
// Grafana, so regenerating + the 30s provider rescan updates the dashboard.
//
// This file holds the reusable styling primitives and panel builders; tabs.go
// holds the per-tab layout and main.go assembles the tabbed dashboard.
package main

import (
	"github.com/grafana/grafana-foundation-sdk/go/cog"
	v2 "github.com/grafana/grafana-foundation-sdk/go/dashboardv2beta1"
)

// lit adapts a plain value into a cog.Builder so directly-constructed leaf kinds
// (queries, viz configs) can be passed to the SDK's builder-typed setters.
type lit[T any] struct{ v T }

func (b lit[T]) Build() (T, error) { return b.v, nil }

func L[T any](v T) cog.Builder[T] { return lit[T]{v} }

func sp(s string) *string   { return &s }
func fp(f float64) *float64 { return &f }

// ---------------------------------------------------------------- palette
// Faction/team colours, so a colour means the same thing on every panel.
const (
	cRed    = "#E0252A"
	cBlue   = "#3573D6"
	cGreen  = "#56A64B"
	cGold   = "#F2B01E"
	cPurple = "#8F3BB8"
	cGrey   = "#8A8A8A"
)

// GW2 canonical rarity colours (item + dye tiers).
var rarityColor = map[string]string{
	"Basic": "#AAAAAA", "Junk": "#AAAAAA", "Fine": "#62A4DA", "Masterwork": "#1A9306",
	"Rare": "#FCD00B", "Exotic": "#FFA405", "Ascended": "#FB3E8D", "Legendary": "#4C139D",
	"Starter": "#8A8A8A", "Common": "#56A64B", "Uncommon": "#62A4DA", "Exclusive": "#FFA405",
}

// ---------------------------------------------------------------- thresholds
type step struct {
	v *float64
	c string
}

func base(color string) step          { return step{nil, color} }
func at(v float64, color string) step { return step{fp(v), color} }

func thr(steps ...step) *v2.ThresholdsConfig {
	out := make([]v2.Threshold, len(steps))
	for i, s := range steps {
		out[i] = v2.Threshold{Value: s.v, Color: s.c}
	}
	return &v2.ThresholdsConfig{Mode: v2.ThresholdsModeAbsolute, Steps: out}
}

// Common ramps.
func pctRamp() *v2.ThresholdsConfig { return thr(base(cRed), at(40, "orange"), at(75, cGreen)) }
func profitRamp() *v2.ThresholdsConfig {
	return thr(base(cRed), at(0, cGrey), at(1, cGreen))
}

// ---------------------------------------------------------------- overrides
func override(matcherID, options string, props ...v2.DynamicConfigValue) v2.Dashboardv2beta1FieldConfigSourceOverrides {
	return v2.Dashboardv2beta1FieldConfigSourceOverrides{
		Matcher:    v2.MatcherConfig{Id: matcherID, Options: options},
		Properties: props,
	}
}

func colorProp(color string) v2.DynamicConfigValue {
	return v2.DynamicConfigValue{Id: "color", Value: map[string]any{"mode": "fixed", "fixedColor": color}}
}

func byName(name, color string) v2.Dashboardv2beta1FieldConfigSourceOverrides {
	return override("byName", name, colorProp(color))
}

func byRegexp(rx, color string) v2.Dashboardv2beta1FieldConfigSourceOverrides {
	return override("byRegexp", rx, colorProp(color))
}

// teamOverrides colours any series whose name contains a team name.
func teamOverrides() []v2.Dashboardv2beta1FieldConfigSourceOverrides {
	return []v2.Dashboardv2beta1FieldConfigSourceOverrides{
		byRegexp(".*[Rr]ed.*", cRed), byRegexp(".*[Bb]lue.*", cBlue), byRegexp(".*[Gg]reen.*", cGreen),
	}
}

func rarityOverrides() []v2.Dashboardv2beta1FieldConfigSourceOverrides {
	// Stable order for deterministic output.
	order := []string{"Basic", "Junk", "Fine", "Masterwork", "Rare", "Exotic", "Ascended",
		"Legendary", "Starter", "Common", "Uncommon", "Exclusive"}
	out := make([]v2.Dashboardv2beta1FieldConfigSourceOverrides, 0, len(order))
	for _, name := range order {
		out = append(out, byName(name, rarityColor[name]))
	}
	return out
}

// ---------------------------------------------------------------- field config
// fc collects the typed field-config defaults shared by most panels.
type fc struct {
	unit      string
	decimals  *float64
	min, max  *float64
	colorMode string // thresholds | palette-classic | continuous-* | none
	thr       *v2.ThresholdsConfig
	custom    map[string]any // panel-specific (fillOpacity, stacking, cellOptions, …)
	mappings  []v2.ValueMapping
	overrides []v2.Dashboardv2beta1FieldConfigSourceOverrides
}

func (f fc) source() v2.FieldConfigSource {
	d := v2.FieldConfig{}
	unit := f.unit
	if unit == "" {
		unit = "none"
	}
	d.Unit = sp(unit)
	d.Decimals = f.decimals
	d.Min, d.Max = f.min, f.max
	// Only set a colour mode when intended: an explicit mode, or thresholds
	// (which imply value-based colouring). Otherwise leave it unset so Grafana
	// uses the classic multi-colour palette (e.g. multi-series timeseries).
	mode := f.colorMode
	if mode == "" && f.thr != nil {
		mode = "thresholds"
	}
	if mode != "" {
		d.Color = &v2.FieldColor{Mode: v2.FieldColorModeId(mode)}
	}
	if f.thr != nil {
		d.Thresholds = f.thr
	}
	if f.custom != nil {
		d.Custom = f.custom
	}
	if f.mappings != nil {
		d.Mappings = f.mappings
	}
	overrides := f.overrides
	if overrides == nil {
		overrides = []v2.Dashboardv2beta1FieldConfigSourceOverrides{}
	}
	return v2.FieldConfigSource{Defaults: d, Overrides: overrides}
}

func viz(group string, options map[string]any, f fc) cog.Builder[v2.VizConfigKind] {
	return L(v2.VizConfigKind{Kind: "VizConfig", Group: group, Version: "",
		Spec: v2.VizConfigSpec{Options: options, FieldConfig: f.source()}})
}

// ---------------------------------------------------------------- queries
func promTarget(expr, legend, ref string, instant bool) cog.Builder[v2.PanelQueryKind] {
	spec := map[string]any{"expr": expr}
	if legend != "" {
		spec["legendFormat"] = legend
	}
	if instant {
		spec["instant"], spec["range"] = true, false
	}
	return v2.NewTargetBuilder().RefId(ref).Query(L(v2.DataQueryKind{
		Kind: "DataQuery", Group: "prometheus", Version: "v0",
		Datasource: &v2.Dashboardv2beta1DataQueryKindDatasource{Name: sp("prometheus")},
		Spec:       spec,
	}))
}

// promTargetTable is an instant prometheus target in table format (for table panels).
func promTargetTable(expr, ref string) cog.Builder[v2.PanelQueryKind] {
	return v2.NewTargetBuilder().RefId(ref).Query(L(v2.DataQueryKind{
		Kind: "DataQuery", Group: "prometheus", Version: "v0",
		Datasource: &v2.Dashboardv2beta1DataQueryKindDatasource{Name: sp("prometheus")},
		Spec:       map[string]any{"expr": expr, "instant": true, "range": false, "format": "table"},
	}))
}

func lokiTarget(expr string) cog.Builder[v2.PanelQueryKind] {
	return v2.NewTargetBuilder().RefId("A").Query(L(v2.DataQueryKind{
		Kind: "DataQuery", Group: "loki", Version: "v0",
		Datasource: &v2.Dashboardv2beta1DataQueryKindDatasource{Name: sp("loki")},
		Spec:       map[string]any{"expr": expr},
	}))
}

// q is shorthand for one (expr, legend) prometheus target with auto refId.
type ql struct {
	expr, legend string
}

func promTargets(instant bool, items ...ql) []cog.Builder[v2.PanelQueryKind] {
	out := make([]cog.Builder[v2.PanelQueryKind], len(items))
	for i, it := range items {
		out[i] = promTarget(it.expr, it.legend, string(rune('A'+i)), instant)
	}
	return out
}

func dataGroup(targets []cog.Builder[v2.PanelQueryKind], transformations ...cog.Builder[v2.TransformationKind]) *v2.QueryGroupBuilder {
	g := v2.NewQueryGroupBuilder().Targets(targets)
	if len(transformations) > 0 {
		g = g.Transformations(transformations)
	}
	return g
}

func gold(expr string) string { return "(" + expr + ") / 10000" }

// ---------------------------------------------------------------- panel builders
func panel(group, title string, data *v2.QueryGroupBuilder, options map[string]any, f fc) *v2.PanelBuilder {
	return v2.NewPanelBuilder().Title(title).Data(data).Visualization(viz(group, options, f))
}

// statOpts/etc. keep the call sites readable while mirroring the Python helpers.
type statOpts struct {
	unit     string
	decimals *float64
	color    string // value | background | none
	graph    string // area | none
	thr      *v2.ThresholdsConfig
	text     string // value | name | value_and_name
	legend   string
	instant  bool
	over     []v2.Dashboardv2beta1FieldConfigSourceOverrides
}

func stat(title, expr string, o statOpts) *v2.PanelBuilder {
	color, graph, text := o.color, o.graph, o.text
	if color == "" {
		color = "value"
	}
	if graph == "" {
		graph = "area"
	}
	if text == "" {
		text = "value"
	}
	opts := map[string]any{"colorMode": color, "graphMode": graph, "justifyMode": "auto",
		"textMode": text, "reduceOptions": map[string]any{"calc": "lastNotNull"}}
	tgt := promTarget(expr, o.legend, "A", o.instant)
	t := o.thr
	if t == nil {
		t = thr(base(cBlue))
	}
	return panel("stat", title, dataGroup([]cog.Builder[v2.PanelQueryKind]{tgt}),
		opts, fc{unit: o.unit, decimals: o.decimals, thr: t, overrides: o.over})
}

func gauge(title, expr, unit string, minv, maxv float64, t *v2.ThresholdsConfig) *v2.PanelBuilder {
	if t == nil {
		t = pctRamp()
	}
	opts := map[string]any{"showThresholdLabels": false, "showThresholdMarkers": true,
		"reduceOptions": map[string]any{"calc": "lastNotNull"}}
	return panel("gauge", title, dataGroup([]cog.Builder[v2.PanelQueryKind]{promTarget(expr, "", "A", true)}),
		opts, fc{unit: unit, min: fp(minv), max: fp(maxv), thr: t})
}

func piechart(title string, targets []cog.Builder[v2.PanelQueryKind], pie string, labels, values []string,
	overrides []v2.Dashboardv2beta1FieldConfigSourceOverrides) *v2.PanelBuilder {
	if pie == "" {
		pie = "donut"
	}
	opts := map[string]any{"pieType": pie,
		"tooltip":       map[string]any{"mode": "single", "sort": "desc"},
		"displayLabels": labels,
		"legend":        map[string]any{"displayMode": "table", "placement": "right", "values": values},
		"reduceOptions": map[string]any{"calc": "lastNotNull", "values": false}}
	return panel("piechart", title, dataGroup(targets), opts,
		fc{colorMode: "palette-classic", overrides: overrides})
}

func barchart(title string, targets []cog.Builder[v2.PanelQueryKind], orientation, stacking string,
	overrides []v2.Dashboardv2beta1FieldConfigSourceOverrides) *v2.PanelBuilder {
	return barchartT(title, targets, nil, orientation, stacking, overrides, nil)
}

// barchartT is barchart with explicit transformations — used to pivot long
// Prometheus output into the wide shape a grouped/stacked bar chart needs (one
// x-category per row, one series per column), which avoids duplicated legends.
func barchartT(title string, targets []cog.Builder[v2.PanelQueryKind],
	transforms []cog.Builder[v2.TransformationKind], orientation, stacking string,
	overrides []v2.Dashboardv2beta1FieldConfigSourceOverrides, mappings []v2.ValueMapping) *v2.PanelBuilder {
	if orientation == "" {
		orientation = "horizontal"
	}
	if stacking == "" {
		stacking = "none"
	}
	opts := map[string]any{"orientation": orientation, "stacking": stacking, "showValue": "auto",
		"xTickLabelRotation": 0, "groupWidth": 0.8, "barWidth": 0.9,
		"legend":  map[string]any{"displayMode": "list", "placement": "bottom"},
		"tooltip": map[string]any{"mode": "single", "sort": "desc"}}
	return panel("barchart", title, dataGroup(targets, transforms...), opts,
		fc{colorMode: "palette-classic", overrides: overrides, mappings: mappings,
			custom: map[string]any{"fillOpacity": 90, "lineWidth": 1, "gradientMode": "none"}})
}

// groupedBars compares several named metrics across a dimension (e.g. earned vs
// spent by region). Each metric is one table-format target; joinByField pivots
// them so the dimension is the x-axis and each metric is a distinct series.
func groupedBars(title, dimField, dimName string, series []ql, orientation, stacking string,
	overrides []v2.Dashboardv2beta1FieldConfigSourceOverrides) *v2.PanelBuilder {
	targets := make([]cog.Builder[v2.PanelQueryKind], len(series))
	rename := map[string]any{dimField: dimName}
	for i, s := range series {
		ref := string(rune('A' + i))
		targets[i] = promTargetTable(s.expr, ref)
		rename["Value #"+ref] = s.legend
	}
	transforms := []cog.Builder[v2.TransformationKind]{
		transform("joinByField", map[string]any{"byField": dimField, "mode": "outer"}),
		transform("organize", map[string]any{
			"excludeByName": map[string]any{"Time": true},
			"renameByName":  rename}),
	}
	return barchartT(title, targets, transforms, orientation, stacking, overrides, nil)
}

// matrixBars pivots a single metric carrying two labels into a matrix: rowField
// becomes the x-axis category and each colField value becomes a series. mappings
// optionally relabels the row-field categories for display.
func matrixBars(title, expr, rowField, colField, orientation, stacking string,
	overrides []v2.Dashboardv2beta1FieldConfigSourceOverrides, mappings []v2.ValueMapping) *v2.PanelBuilder {
	targets := []cog.Builder[v2.PanelQueryKind]{promTargetTable(expr, "A")}
	transforms := []cog.Builder[v2.TransformationKind]{
		transform("groupingToMatrix", map[string]any{
			"columnField": colField, "rowField": rowField, "valueField": "Value"}),
	}
	return barchartT(title, targets, transforms, orientation, stacking, overrides, mappings)
}

func timeseries(title string, targets []cog.Builder[v2.PanelQueryKind], unit string, stack bool,
	placement string, calcs []string, overrides []v2.Dashboardv2beta1FieldConfigSourceOverrides, fillOverride *int) *v2.PanelBuilder {
	fill := 12
	if stack {
		fill = 35
	}
	if fillOverride != nil {
		fill = *fillOverride
	}
	custom := map[string]any{"fillOpacity": fill, "showPoints": "never", "drawStyle": "line",
		"lineWidth": 2, "gradientMode": "opacity", "lineInterpolation": "smooth", "spanNulls": true}
	if stack {
		custom["stacking"] = map[string]any{"mode": "normal"}
	}
	if placement == "" {
		placement = "right"
	}
	if calcs == nil {
		calcs = []string{"lastNotNull"}
	}
	opts := map[string]any{
		"legend":  map[string]any{"displayMode": "table", "placement": placement, "calcs": calcs},
		"tooltip": map[string]any{"mode": "multi", "sort": "desc"}}
	return panel("timeseries", title, dataGroup(targets), opts,
		fc{unit: unit, custom: custom, overrides: overrides})
}

func bargauge(title, expr, legend, unit string, maxv *float64, t *v2.ThresholdsConfig,
	colorMode string, overrides []v2.Dashboardv2beta1FieldConfigSourceOverrides) *v2.PanelBuilder {
	f := fc{unit: unit, overrides: overrides}
	if t != nil {
		f.colorMode, f.thr = "thresholds", t
	} else {
		if colorMode == "" {
			colorMode = "continuous-GrYlRd"
		}
		f.colorMode = colorMode
	}
	f.min, f.max = func() (*float64, *float64) {
		if maxv != nil {
			return fp(0), maxv
		}
		return nil, nil
	}()
	valueMode := "text"
	if t != nil {
		valueMode = "color"
	}
	opts := map[string]any{"orientation": "horizontal", "displayMode": "gradient",
		"valueMode": valueMode, "reduceOptions": map[string]any{"calc": "lastNotNull"}}
	return panel("bargauge", title, dataGroup([]cog.Builder[v2.PanelQueryKind]{promTarget(expr, legend, "A", true)}),
		opts, f)
}

// sortedBargauge is a bargauge whose bars are sorted high→low. A multi-series
// bargauge renders one bar per series in the order Prometheus returns them, so
// wrapping the expr in sort_desc() yields value-ordered bars while keeping the
// per-bar gradient/threshold colouring. The expr must group by one label.
func sortedBargauge(title, expr, legend, unit string, maxv *float64, t *v2.ThresholdsConfig,
	colorMode string, overrides []v2.Dashboardv2beta1FieldConfigSourceOverrides) *v2.PanelBuilder {
	return bargauge(title, "sort_desc("+expr+")", legend, unit, maxv, t, colorMode, overrides)
}

func logsPanel(title, expr string) *v2.PanelBuilder {
	opts := map[string]any{"showTime": true, "sortOrder": "Descending",
		"wrapLogMessage": true, "enableLogDetails": true}
	return v2.NewPanelBuilder().Title(title).
		Data(dataGroup([]cog.Builder[v2.PanelQueryKind]{lokiTarget(expr)})).
		Visualization(viz("logs", opts, fc{}))
}

func tablePanel(title string, targets []cog.Builder[v2.PanelQueryKind],
	overrides []v2.Dashboardv2beta1FieldConfigSourceOverrides,
	transformations ...cog.Builder[v2.TransformationKind]) *v2.PanelBuilder {
	opts := map[string]any{"showHeader": true, "cellHeight": "sm"}
	return v2.NewPanelBuilder().Title(title).
		Data(dataGroup(targets, transformations...)).
		Visualization(viz("table", opts, fc{overrides: overrides}))
}

// transform builds a transformation kind from an id + options map.
func transform(id string, options map[string]any) cog.Builder[v2.TransformationKind] {
	return L(v2.TransformationKind{Kind: id, Spec: v2.DataTransformerConfig{Id: id, Options: options}})
}

// ---------------------------------------------------------------- layout grid
// Grid is the left-to-right auto-layout on Grafana's 24-wide grid (mirrors the
// previous Python generator).
type placed struct {
	b          *v2.PanelBuilder
	x, y, w, h int64
}

type Grid struct {
	panels     []placed
	x, y, rowH int64
}

func (g *Grid) add(b *v2.PanelBuilder, w, h int64) *Grid {
	if g.x+w > 24 {
		g.x, g.y, g.rowH = 0, g.y+g.rowH, 0
	}
	g.panels = append(g.panels, placed{b, g.x, g.y, w, h})
	g.x += w
	if h > g.rowH {
		g.rowH = h
	}
	return g
}

func (g *Grid) row() *Grid {
	g.x, g.y, g.rowH = 0, g.y+g.rowH, 0
	return g
}
