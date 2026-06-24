package main

import (
	"github.com/grafana/grafana-foundation-sdk/go/cog"
	v2 "github.com/grafana/grafana-foundation-sdk/go/dashboardv2beta1"
)

// shorthand for a list of prometheus targets
func tg(instant bool, items ...ql) []cog.Builder[v2.PanelQueryKind] {
	return promTargets(instant, items...)
}

// ts wraps the common timeseries call (range query, default placement/calcs).
func ts(title string, targets []cog.Builder[v2.PanelQueryKind], unit string, stack bool,
	placement string, overrides []v2.Dashboardv2beta1FieldConfigSourceOverrides) *v2.PanelBuilder {
	return timeseries(title, targets, unit, stack, placement, nil, overrides, nil)
}

// ---------------------------------------------------------------- Overview
func overview() *Grid {
	g := &Grid{}
	g.add(stat("Account Value", gold(`max(gw2_account_value{gw2_component="total",gw2_basis="sell"})`),
		statOpts{decimals: fp(1), color: "background", thr: thr(base(cGold))}), 4, 5)
	g.add(stat("Liquid (−15%)", gold(`max(gw2_account_value{gw2_component="total",gw2_basis="sell"}) * 0.85`),
		statOpts{decimals: fp(1), thr: thr(base(cGold))}), 4, 5)
	g.add(stat("Gold", gold(`max(gw2_account_wallet_balance{gw2_currency_name="Coin"})`),
		statOpts{decimals: fp(1), thr: thr(base(cGold))}), 4, 5)
	g.add(stat("Gems", `max(gw2_account_wallet_balance{gw2_currency_name="Gem"})`,
		statOpts{thr: thr(base(cPurple))}), 4, 5)
	g.add(stat("Playtime", "sum(max by (gw2_character_name) (gw2_character_playtime_seconds_total))",
		statOpts{unit: "s", decimals: fp(0), thr: thr(base(cBlue))}), 4, 5)
	g.add(stat("Characters", "max(gw2_account_characters)", statOpts{thr: thr(base(cGreen))}), 4, 5)
	g.row()
	g.add(piechart("Wealth composition (sell, gold)",
		tg(true, ql{gold(`max by (gw2_component) (gw2_account_value{gw2_basis="sell",gw2_component!="total"})`), "{{gw2_component}}"}),
		"", []string{"name", "percent"}, []string{"value", "percent"}, nil), 8, 9)
	g.add(timeseries("Account value over time (gold, stacked)",
		tg(false, ql{gold(`max by (gw2_component) (gw2_account_value{gw2_basis="sell",gw2_component!="total"})`), "{{gw2_component}}"}),
		"", true, "", []string{"lastNotNull", "max"}, nil, nil), 16, 9)
	g.row()
	g.add(gauge("Achievements done %",
		"100 * max(gw2_account_achievements_done) / clamp_min(max(gw2_account_achievements_tracked), 1)",
		"percent", 0, 100, nil), 5, 8)
	g.add(gauge("Story completion %",
		"100 * sum(gw2_story_quests_completed) / clamp_min(sum(gw2_story_quests_total), 1)",
		"percent", 0, 100, nil), 5, 8)
	g.add(gauge("Magic find %", "clamp_max(max(gw2_account_luck_total) * 300 / 4295450, 300)",
		"percent", 0, 300, thr(base(cBlue), at(150, cGold), at(250, cGreen))), 5, 8)
	g.add(bargauge("Collection completion %",
		"100 * max by (gw2_collection) (gw2_account_unlocks_count) / clamp_min(max by (gw2_collection) (gw2_account_unlocks_total), 1)",
		"{{gw2_collection}}", "percent", fp(100), pctRamp(), "", nil), 9, 8)
	g.row()
	g.add(logsPanel("Recent activity", `{service_name="gw2-otel-collector"}`), 24, 8)
	return g
}

// ---------------------------------------------------------------- Wealth
func wealth() *Grid {
	g := &Grid{}
	g.add(stat("Total value", gold(`max(gw2_account_value{gw2_component="total",gw2_basis="sell"})`),
		statOpts{decimals: fp(1), color: "background", thr: thr(base(cGold))}), 4, 5)
	g.add(stat("Liquid (−15%)", gold(`max(gw2_account_value{gw2_component="total",gw2_basis="sell"}) * 0.85`),
		statOpts{decimals: fp(1), thr: thr(base(cGold))}), 4, 5)
	g.add(stat("Gold", gold(`max(gw2_account_wallet_balance{gw2_currency_name="Coin"})`),
		statOpts{decimals: fp(1), thr: thr(base(cGold))}), 4, 5)
	g.add(stat("Gems", `max(gw2_account_wallet_balance{gw2_currency_name="Gem"})`,
		statOpts{thr: thr(base(cPurple))}), 4, 5)
	g.add(stat("Delivery box", gold("max(gw2_commerce_delivery_coins)"),
		statOpts{decimals: fp(1), thr: thr(base(cBlue))}), 4, 5)
	g.add(stat("Open orders", gold("sum(max by (gw2_side) (gw2_commerce_orders_open_value))"),
		statOpts{decimals: fp(1), thr: thr(base(cBlue))}), 4, 5)
	g.row()
	g.add(piechart("Wealth composition (sell, gold)",
		tg(true, ql{gold(`max by (gw2_component) (gw2_account_value{gw2_basis="sell",gw2_component!="total"})`), "{{gw2_component}}"}),
		"", []string{"name", "percent"}, []string{"value", "percent"}, nil), 8, 9)
	g.add(ts("Account value over time (gold): components",
		tg(false, ql{gold(`max by (gw2_component) (gw2_account_value{gw2_basis="sell",gw2_component!="total"})`), "{{gw2_component}}"}),
		"", true, "", nil), 16, 9)
	g.row()
	g.add(piechart("Material value by category (gold)",
		tg(true, ql{gold("max by (gw2_material_category_name) (gw2_account_material_value)"), "{{gw2_material_category_name}}"}),
		"", []string{"percent"}, []string{"value"}, nil), 8, 8)
	g.add(ts("Gem ⇄ coin exchange (copper per gem)",
		tg(false, ql{"max by (gw2_direction) (gw2_commerce_exchange_coins_per_gem)", "{{gw2_direction}}"}),
		"", false, "bottom", nil), 8, 8)
	g.add(bargauge("Wallet balances (top 10 currencies)",
		"topk(10, max by (gw2_currency_name) (gw2_account_wallet_balance))",
		"{{gw2_currency_name}}", "", nil, nil, "", nil), 8, 8)
	g.row()
	g.add(ts("Tracked item prices (copper): buy vs sell",
		tg(false, ql{"max by (gw2_item_name, gw2_side) (gw2_commerce_item_price)", "{{gw2_item_name}} {{gw2_side}}"}),
		"", false, "bottom", nil), 12, 8)
	g.add(barchart("Tracked item supply vs demand (units)",
		tg(true, ql{"max by (gw2_item_name) (gw2_commerce_item_supply)", "supply"},
			ql{"max by (gw2_item_name) (gw2_commerce_item_demand)", "demand"}),
		"", "", []v2.Dashboardv2beta1FieldConfigSourceOverrides{byName("supply", cBlue), byName("demand", cRed)}), 12, 8)
	g.row()
	g.add(bargauge("Flip margin (copper, sell×0.85 − buy)",
		"max by (gw2_item_name) (gw2_commerce_item_flip_margin)", "{{gw2_item_name}}",
		"", nil, profitRamp(), "", nil), 8, 8)
	g.add(bargauge("Crafting profit (copper)",
		"max by (gw2_item_name) (gw2_commerce_craft_profit)", "{{gw2_item_name}}",
		"", nil, profitRamp(), "", nil), 8, 8)
	g.add(bargauge("24h movers (sell-price change %)",
		`(max by (gw2_item_name) (gw2_commerce_item_price{gw2_side="sell"}) / max by (gw2_item_name) (gw2_commerce_item_price{gw2_side="sell"} offset 24h) - 1) * 100`,
		"{{gw2_item_name}}", "percent", nil, thr(base(cRed), at(0, cGrey), at(0.0001, cGreen)), "", nil), 8, 8)
	return g
}

// ---------------------------------------------------------------- Progression
func progression() *Grid {
	g := &Grid{}
	g.add(stat("Total AP", "max(gw2_account_achievement_points_total)",
		statOpts{color: "background", thr: thr(base(cGold))}), 4, 5)
	g.add(stat("Daily AP", `max(gw2_account_achievement_points{gw2_period="daily"})`,
		statOpts{thr: thr(base(cGold))}), 4, 5)
	g.add(stat("Monthly AP", `max(gw2_account_achievement_points{gw2_period="monthly"})`,
		statOpts{thr: thr(base(cGold))}), 4, 5)
	g.add(stat("Fractal level", "max(gw2_account_fractal_level)", statOpts{thr: thr(base(cRed))}), 4, 5)
	g.add(stat("Masteries", "max(gw2_account_masteries_unlocked)", statOpts{thr: thr(base(cGreen))}), 4, 5)
	g.add(stat("Played", "max(gw2_account_age_seconds_total)", statOpts{unit: "s", thr: thr(base(cBlue))}), 4, 5)
	g.row()
	g.add(gauge("Achievements done %",
		"100 * max(gw2_account_achievements_done) / clamp_min(max(gw2_account_achievements_tracked), 1)",
		"percent", 0, 100, nil), 6, 8)
	g.add(gauge("Magic find %", "clamp_max(max(gw2_account_luck_total) * 300 / 4295450, 300)",
		"percent", 0, 300, thr(base(cBlue), at(150, cGold), at(250, cGreen))), 6, 8)
	g.add(bargauge("Story completion % by season",
		"100 * max by (gw2_season) (gw2_story_quests_completed) / clamp_min(max by (gw2_season) (gw2_story_quests_total), 1)",
		"{{gw2_season}}", "percent", fp(100), pctRamp(), "", nil), 12, 8)
	g.row()
	g.add(bargauge("Legendary collections done / total %",
		"100 * max by (gw2_legendary_category) (gw2_legendary_collections_done) / clamp_min(max by (gw2_legendary_category) (gw2_legendary_collections_total), 1)",
		"{{gw2_legendary_category}}", "percent", fp(100), pctRamp(), "", nil), 12, 8)
	g.add(bargauge("Legendary items obtained % (started collections)",
		"100 * max by (gw2_legendary_category) (gw2_legendary_items_current) / clamp_min(max by (gw2_legendary_category) (gw2_legendary_items_max), 1)",
		"{{gw2_legendary_category}}", "percent", fp(100), pctRamp(), "", nil), 12, 8)
	g.row()
	g.add(barchart("Mastery points by region (earned vs spent)",
		tg(true, ql{"max by (gw2_region) (gw2_account_mastery_points_earned_total)", "earned"},
			ql{"max by (gw2_region) (gw2_account_mastery_points_spent)", "spent"}),
		"", "", []v2.Dashboardv2beta1FieldConfigSourceOverrides{byName("earned", cGreen), byName("spent", cGold)}), 12, 8)
	g.add(ts("Wizard's Vault objectives completed by period",
		tg(false, ql{"max by (gw2_period) (gw2_wizardsvault_objectives_completed)", "{{gw2_period}}"}),
		"", false, "bottom", nil), 12, 8)
	g.row()
	g.add(barchart("Reset-cycle completions (since reset)",
		tg(true, ql{"max by (gw2_kind) (gw2_account_reset_completed)", "{{gw2_kind}}"}),
		"vertical", "", nil), 24, 7)
	return g
}

// ---------------------------------------------------------------- Collections
func collections() *Grid {
	g := &Grid{}
	g.add(stat("Skins unlocked", "sum(max by (gw2_skin_type, gw2_rarity) (gw2_wardrobe_skins))",
		statOpts{color: "background", thr: thr(base(cPurple))}), 8, 5)
	g.add(stat("Dyes unlocked", "sum(max by (gw2_rarity) (gw2_wardrobe_dyes))",
		statOpts{thr: thr(base(cGreen))}), 8, 5)
	g.add(stat("Avg collection completion %",
		"100 * sum(max by (gw2_collection) (gw2_account_unlocks_count)) / clamp_min(sum(max by (gw2_collection) (gw2_account_unlocks_total)), 1)",
		statOpts{unit: "percent", decimals: fp(1), thr: thr(base(cGold))}), 8, 5)
	g.row()
	g.add(bargauge("Collection completion %",
		"100 * max by (gw2_collection) (gw2_account_unlocks_count) / clamp_min(max by (gw2_collection) (gw2_account_unlocks_total), 1)",
		"{{gw2_collection}}", "percent", fp(100), pctRamp(), "", nil), 24, 10)
	g.row()
	g.add(piechart("Skins by type",
		tg(true, ql{"sum by (gw2_skin_type) (max by (gw2_skin_type, gw2_rarity) (gw2_wardrobe_skins))", "{{gw2_skin_type}}"}),
		"", []string{"name", "value"}, []string{"value", "percent"}, nil), 8, 9)
	g.add(piechart("Skins by rarity",
		tg(true, ql{"sum by (gw2_rarity) (max by (gw2_skin_type, gw2_rarity) (gw2_wardrobe_skins))", "{{gw2_rarity}}"}),
		"pie", []string{"name", "value"}, []string{"value", "percent"}, rarityOverrides()), 8, 9)
	g.add(piechart("Dyes by rarity",
		tg(true, ql{"max by (gw2_rarity) (gw2_wardrobe_dyes)", "{{gw2_rarity}}"}),
		"pie", []string{"name", "value"}, []string{"value", "percent"}, rarityOverrides()), 8, 9)
	return g
}

// ---------------------------------------------------------------- Characters
func characters() *Grid {
	g := &Grid{}
	g.add(stat("Characters", "max(gw2_account_characters)",
		statOpts{color: "background", thr: thr(base(cGreen))}), 6, 5)
	g.add(stat("Total playtime", "sum(max by (gw2_character_name) (gw2_character_playtime_seconds_total))",
		statOpts{unit: "s", thr: thr(base(cBlue))}), 6, 5)
	g.add(stat("Total deaths", "sum(max by (gw2_character_name) (gw2_character_deaths_total))",
		statOpts{thr: thr(base(cRed))}), 6, 5)
	g.add(stat("Average level", "avg(max by (gw2_character_name) (gw2_character_level))",
		statOpts{decimals: fp(0), thr: thr(base(cGold))}), 6, 5)
	g.row()
	rosterOverride := override("byName", "Level",
		v2.DynamicConfigValue{Id: "custom.cellOptions", Value: map[string]any{"type": "color-background"}},
		v2.DynamicConfigValue{Id: "thresholds", Value: map[string]any{"mode": "absolute",
			"steps": []map[string]any{{"value": nil, "color": cRed}, {"value": 40, "color": "orange"}, {"value": 80, "color": cGreen}}}},
		v2.DynamicConfigValue{Id: "custom.width", Value: 90})
	g.add(tablePanel("Character roster (by level)",
		[]cog.Builder[v2.PanelQueryKind]{
			promTargetTable(`max by (gw2_character_name, gw2_character_profession, gw2_character_race) (gw2_character_level{gw2_character_name=~"$character"})`, "A")},
		[]v2.Dashboardv2beta1FieldConfigSourceOverrides{rosterOverride},
		transform("organize", map[string]any{
			"excludeByName": map[string]any{"Time": true},
			"indexByName":   map[string]any{"gw2_character_name": 0, "gw2_character_profession": 1, "gw2_character_race": 2, "Value": 3},
			"renameByName":  map[string]any{"gw2_character_name": "Character", "gw2_character_profession": "Profession", "gw2_character_race": "Race", "Value": "Level"}}),
		transform("sortBy", map[string]any{"fields": map[string]any{}, "sort": []map[string]any{{"field": "Level", "desc": true}}})), 24, 9)
	g.row()
	g.add(bargauge("Level by character",
		`max by (gw2_character_name) (gw2_character_level{gw2_character_name=~"$character"})`,
		"{{gw2_character_name}}", "", fp(80), thr(base(cRed), at(40, "orange"), at(80, cGreen)), "", nil), 8, 9)
	g.add(bargauge("Playtime by character (h)",
		`max by (gw2_character_name) (gw2_character_playtime_seconds_total{gw2_character_name=~"$character"}) / 3600`,
		"{{gw2_character_name}}", "h", nil, nil, "", nil), 8, 9)
	g.add(bargauge("Deaths by character",
		`max by (gw2_character_name) (gw2_character_deaths_total{gw2_character_name=~"$character"})`,
		"{{gw2_character_name}}", "", nil, nil, "continuous-RdYlGr", nil), 8, 9)
	g.row()
	g.add(barchart("Crafting rating by character & discipline",
		tg(true, ql{`max by (gw2_character_name, gw2_discipline) (gw2_character_crafting_rating{gw2_character_name=~"$character"})`, "{{gw2_character_name}} · {{gw2_discipline}}"}),
		"horizontal", "", nil), 12, 10)
	g.add(barchart("Inventory: used vs capacity by character",
		tg(true, ql{`max by (gw2_character_name) (gw2_character_inventory_slots{gw2_state="used",gw2_character_name=~"$character"})`, "used"},
			ql{`max by (gw2_character_name) (gw2_character_inventory_slots{gw2_state="capacity",gw2_character_name=~"$character"})`, "capacity"}),
		"horizontal", "none",
		[]v2.Dashboardv2beta1FieldConfigSourceOverrides{byName("used", cBlue), byName("capacity", "#444444")}), 12, 10)
	return g
}

// ---------------------------------------------------------------- PvP & Health
func pvpOps() *Grid {
	g := &Grid{}
	g.add(stat("PvP rank", "max(gw2_pvp_rank)", statOpts{color: "background", thr: thr(base(cPurple))}), 4, 5)
	g.add(stat("Rank points", "max(gw2_pvp_rank_points)", statOpts{thr: thr(base(cPurple))}), 4, 5)
	g.add(stat("Wins", `max(gw2_pvp_matches_total{gw2_outcome="win"})`, statOpts{thr: thr(base(cGreen))}), 4, 5)
	g.add(stat("Losses", `max(gw2_pvp_matches_total{gw2_outcome="loss"})`, statOpts{thr: thr(base(cRed))}), 4, 5)
	g.add(stat("Total games",
		`max(gw2_pvp_matches_total{gw2_outcome="win"}) + max(gw2_pvp_matches_total{gw2_outcome="loss"})`,
		statOpts{thr: thr(base(cBlue))}), 4, 5)
	g.add(stat("Desertions", `max(gw2_pvp_matches_total{gw2_outcome="desertion"})`, statOpts{thr: thr(base(cGrey))}), 4, 5)
	g.row()
	g.add(gauge("Win rate %",
		`100 * max(gw2_pvp_matches_total{gw2_outcome="win"}) / clamp_min(max(gw2_pvp_matches_total{gw2_outcome="win"}) + max(gw2_pvp_matches_total{gw2_outcome="loss"}), 1)`,
		"percent", 0, 100, thr(base(cRed), at(45, "orange"), at(55, cGreen))), 6, 8)
	g.add(piechart("Wins by profession",
		tg(true, ql{`max by (gw2_profession) (gw2_pvp_profession_matches_total{gw2_outcome="win"})`, "{{gw2_profession}}"}),
		"", []string{"name", "value"}, []string{"value"}, nil), 9, 8)
	g.add(barchart("Win/Loss by profession",
		tg(true, ql{`max by (gw2_profession) (gw2_pvp_profession_matches_total{gw2_outcome="win"})`, "wins"},
			ql{`max by (gw2_profession) (gw2_pvp_profession_matches_total{gw2_outcome="loss"})`, "losses"}),
		"", "normal",
		[]v2.Dashboardv2beta1FieldConfigSourceOverrides{byName("wins", cGreen), byName("losses", cRed)}), 9, 8)
	g.row()
	g.add(textPanel("", "### 🩺 Collector health"), 24, 2)
	g.row()
	g.add(bargauge("Seconds since last successful poll (by family)",
		"time() - max by (gw2_family) (gw2_poll_last_success_timestamp_seconds)",
		"{{gw2_family}}", "s", nil, thr(base(cGreen), at(900, "orange"), at(1800, cRed)), "", nil), 8, 9)
	g.add(ts("API request rate (req/s) by endpoint",
		tg(false, ql{"sum by (gw2_endpoint) (rate(gw2_api_requests_total[5m]))", "{{gw2_endpoint}}"}),
		"reqps", false, "bottom", nil), 8, 9)
	g.add(ts("API request p95 latency",
		tg(false, ql{"histogram_quantile(0.95, sum by (le) (rate(gw2_api_request_duration_seconds_bucket[5m])))", "p95"}),
		"s", false, "bottom", nil), 8, 9)
	g.row()
	g.add(logsPanel("Recent events", `{service_name="gw2-otel-collector"}`), 24, 8)
	return g
}

// ---------------------------------------------------------------- WvW
func wvw() *Grid {
	g := &Grid{}
	home := "* on(gw2_team) group_left gw2_wvw_home_team"
	g.add(stat("Home team", "gw2_wvw_home_team",
		statOpts{text: "name", legend: "{{gw2_team}}", color: "background", graph: "none",
			instant: true, over: teamOverrides()}), 4, 5)
	g.add(stat("Our score", "max(gw2_wvw_match_score "+home+")", statOpts{thr: thr(base(cGold)), instant: true}), 4, 5)
	g.add(stat("Our VP", "max(gw2_wvw_match_victory_points "+home+")", statOpts{thr: thr(base(cGold)), instant: true}), 4, 5)
	g.add(stat("Our PPT", "max(gw2_wvw_match_ppt "+home+")", statOpts{thr: thr(base(cGreen)), instant: true}), 4, 5)
	g.add(stat("Our kills", "max(gw2_wvw_match_kills "+home+")", statOpts{thr: thr(base(cRed)), instant: true}), 4, 5)
	g.add(stat("Our objectives", "sum(gw2_wvw_objectives_held "+home+")", statOpts{thr: thr(base(cBlue)), instant: true}), 4, 5)
	g.row()
	fill20 := 20
	g.add(timeseries("War score by team",
		tg(false, ql{"max by (gw2_team) (gw2_wvw_match_score)", "{{gw2_team}}"}),
		"", false, "right", nil, teamOverrides(), &fill20), 12, 8)
	g.add(timeseries("Victory points by team",
		tg(false, ql{"max by (gw2_team) (gw2_wvw_match_victory_points)", "{{gw2_team}}"}),
		"", false, "right", nil, teamOverrides(), &fill20), 12, 8)
	g.row()
	g.add(ts("Kills by team", tg(false, ql{"max by (gw2_team) (gw2_wvw_match_kills)", "{{gw2_team}}"}),
		"", false, "", teamOverrides()), 8, 8)
	g.add(ts("KDR by team",
		tg(false, ql{"max by (gw2_team) (gw2_wvw_match_kills) / max by (gw2_team) (gw2_wvw_match_deaths)", "{{gw2_team}}"}),
		"", false, "", teamOverrides()), 8, 8)
	g.add(ts("PPT by team", tg(false, ql{"max by (gw2_team) (gw2_wvw_match_ppt)", "{{gw2_team}}"}),
		"", false, "", teamOverrides()), 8, 8)
	g.row()
	g.add(bargauge("Objectives held by team & type",
		"max by (gw2_team, gw2_objective_type) (gw2_wvw_objectives_held)",
		"{{gw2_team}} · {{gw2_objective_type}}", "", nil, nil, "continuous-GrYlRd", teamOverrides()), 12, 10)
	g.add(barchart("Objectives held by type (stacked by team)",
		tg(true, ql{"max by (gw2_objective_type, gw2_team) (gw2_wvw_objectives_held)", "{{gw2_team}}"}),
		"horizontal", "normal", teamOverrides()), 12, 10)
	g.row()
	g.add(ts("Per-map score (Center = EBG)",
		tg(false, ql{"max by (gw2_map, gw2_team) (gw2_wvw_map_score)", "{{gw2_map}} · {{gw2_team}}"}),
		"", false, "bottom", teamOverrides()), 12, 8)
	g.add(ts("Per-map kills",
		tg(false, ql{"max by (gw2_map, gw2_team) (gw2_wvw_map_kills)", "{{gw2_map}} · {{gw2_team}}"}),
		"", false, "bottom", teamOverrides()), 12, 8)
	return g
}
