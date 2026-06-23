#!/usr/bin/env python3
"""Generate the GW2 Grafana dashboards as JSON (dashboards-as-code).

Run: `uv run python deploy/dashboards/generate.py` (writes *.json next to this file).
The file provider in deploy/grafana/provisioning mounts this directory into Grafana,
so regenerating + restarting (or the 30s provider rescan) updates the dashboards.

Datasource UIDs match the otel-lgtm dev stack: "prometheus" and "loki".
All Prometheus exprs collapse duplicate service.instance.id series with max/max-by
so multiple collector instances (e.g. across restarts) don't double-count.
"""
import json
import os

PROM = {"type": "prometheus", "uid": "prometheus"}
LOKI = {"type": "loki", "uid": "loki"}
HERE = os.path.dirname(os.path.abspath(__file__))


class Grid:
    """Left-to-right auto-layout on Grafana's 24-wide grid."""

    def __init__(self):
        self.panels, self.x, self.y, self.row_h, self._id = [], 0, 0, 0, 0

    def add(self, panel, w, h):
        if self.x + w > 24:
            self.x, self.y = 0, self.y + self.row_h
            self.row_h = 0
        self._id += 1
        panel["id"] = self._id
        panel["gridPos"] = {"x": self.x, "y": self.y, "w": w, "h": h}
        self.panels.append(panel)
        self.x += w
        self.row_h = max(self.row_h, h)
        return self

    def row(self):  # force a new row
        self.x, self.y, self.row_h = 0, self.y + self.row_h, 0
        return self


def target(expr, legend=None, instant=False):
    t = {"refId": chr(65), "expr": expr, "datasource": PROM}
    if legend is not None:
        t["legendFormat"] = legend
    if instant:
        t["instant"] = True
    return t


def stat(title, expr, unit="none", decimals=None, color="value"):
    fc = {"defaults": {"unit": unit, "color": {"mode": "thresholds"}}, "overrides": []}
    if decimals is not None:
        fc["defaults"]["decimals"] = decimals
    return {"type": "stat", "title": title, "datasource": PROM,
            "fieldConfig": fc, "options": {"colorMode": color, "graphMode": "area",
            "reduceOptions": {"calc": "lastNotNull"}}, "targets": [target(expr)]}


def timeseries(title, targets, unit="none", stack=False):
    custom = {"fillOpacity": 15 if not stack else 40, "showPoints": "never"}
    if stack:
        custom["stacking"] = {"mode": "normal"}
    return {"type": "timeseries", "title": title, "datasource": PROM,
            "fieldConfig": {"defaults": {"unit": unit, "custom": custom}, "overrides": []},
            "options": {"legend": {"displayMode": "table", "placement": "right", "calcs": ["lastNotNull"]}},
            "targets": targets}


def bargauge(title, expr, legend, unit="none", maxv=None):
    defaults = {"unit": unit, "color": {"mode": "continuous-GrYlRd"}}
    if maxv is not None:
        defaults["min"], defaults["max"] = 0, maxv
    return {"type": "bargauge", "title": title, "datasource": PROM,
            "fieldConfig": {"defaults": defaults, "overrides": []},
            "options": {"orientation": "horizontal", "displayMode": "gradient",
                        "reduceOptions": {"calc": "lastNotNull"}},
            "targets": [target(expr, legend, instant=True)]}


def logs(title, expr):
    return {"type": "logs", "title": title, "datasource": LOKI,
            "options": {"showTime": True, "sortOrder": "Descending", "wrapLogMessage": True},
            "targets": [{"refId": "A", "expr": expr, "datasource": LOKI}]}


def table(title, targets):
    return {"type": "table", "title": title, "datasource": PROM,
            "options": {"showHeader": True}, "targets": targets}


def dashboard(uid, title, grid):
    return {"uid": uid, "title": title, "tags": ["gw2"], "timezone": "browser",
            "schemaVersion": 39, "version": 1, "refresh": "30s",
            "time": {"from": "now-24h", "to": "now"}, "templating": {"list": []},
            "panels": grid.panels}


def gold(expr):  # copper expr -> gold
    return f"({expr}) / 10000"


# ---------------------------------------------------------------- Overview
def overview():
    g = Grid()
    g.add(stat("Account Value (gold)", gold('max(gw2_account_value{gw2_component="total",gw2_basis="sell"})'), decimals=1), 4, 4)
    g.add(stat("Gold", gold('max(gw2_account_wallet_balance{gw2_currency_name="Coin"})'), decimals=2), 4, 4)
    g.add(stat("Characters", "max(gw2_account_characters)"), 4, 4)
    g.add(stat("Playtime (h)", "sum(max by (gw2_character_name) (gw2_character_playtime_seconds_total)) / 3600", decimals=1), 4, 4)
    g.add(stat("Daily AP", 'max(gw2_account_achievement_points{gw2_period="daily"})'), 4, 4)
    g.add(stat("Luck", "max(gw2_account_luck_total)"), 4, 4)
    g.row()
    g.add(timeseries("Account value by component (sell, gold)",
                     [target(gold('max by (gw2_component) (gw2_account_value{gw2_basis="sell",gw2_component!="total"})'), "{{gw2_component}}")],
                     stack=True), 12, 9)
    g.add(bargauge("Collection completion %",
                   "100 * max by (gw2_collection) (gw2_account_unlocks_count) / max by (gw2_collection) (gw2_account_unlocks_total)",
                   "{{gw2_collection}}", unit="percent", maxv=100), 12, 9)
    return dashboard("gw2-overview", "GW2 Overview", g)


# ---------------------------------------------------------------- Wealth
def wealth():
    g = Grid()
    g.add(stat("Total value (gold)", gold('max(gw2_account_value{gw2_component="total",gw2_basis="sell"})'), decimals=1), 5, 4)
    g.add(stat("Liquid sell (gold, -15%)", gold('max(gw2_account_value{gw2_component="total",gw2_basis="sell"}) * 0.85'), decimals=1), 5, 4)
    g.add(stat("Gold", gold('max(gw2_account_wallet_balance{gw2_currency_name="Coin"})'), decimals=2), 5, 4)
    g.add(stat("Gems", 'max(gw2_account_wallet_balance{gw2_currency_name="Gem"})'), 4, 4)
    g.add(stat("Delivery (gold)", gold("max(gw2_commerce_delivery_coins)"), decimals=2), 5, 4)
    g.row()
    g.add(timeseries("Account value by component (sell, gold)",
                     [target(gold('max by (gw2_component) (gw2_account_value{gw2_basis="sell",gw2_component!="total"})'), "{{gw2_component}}")],
                     stack=True), 12, 8)
    g.add(timeseries("Total value: buy vs sell (gold)",
                     [target(gold('max(gw2_account_value{gw2_component="total",gw2_basis="buy"})'), "buy"),
                      {"refId": "B", "datasource": PROM, "legendFormat": "sell",
                       "expr": gold('max(gw2_account_value{gw2_component="total",gw2_basis="sell"})')}]), 12, 8)
    g.row()
    g.add(timeseries("Gem / coin exchange rate (copper per gem)",
                     [target('max by (gw2_direction) (gw2_commerce_exchange_coins_per_gem)', "{{gw2_direction}}")]), 12, 8)
    g.add(timeseries("Wallet balances (top 8)",
                     [target("topk(8, max by (gw2_currency_name) (gw2_account_wallet_balance))", "{{gw2_currency_name}}")]), 12, 8)
    g.row()
    g.add(bargauge("Tracked item flip margin (copper)",
                   "max by (gw2_item_name) (gw2_commerce_item_flip_margin)", "{{gw2_item_name}}"), 12, 8)
    g.add(timeseries("Tracked item prices (copper)",
                     [target('max by (gw2_item_name, gw2_side) (gw2_commerce_item_price)', "{{gw2_item_name}} {{gw2_side}}")]), 12, 8)
    g.row()
    g.add(timeseries("Open orders value by side (gold)",
                     [target(gold('max by (gw2_side) (gw2_commerce_orders_open_value)'), "{{gw2_side}}")]), 12, 8)
    g.add(timeseries("Tracked item supply vs demand (units)",
                     [target('max by (gw2_item_name) (gw2_commerce_item_supply)', "supply {{gw2_item_name}}"),
                      {"refId": "B", "datasource": PROM, "legendFormat": "demand {{gw2_item_name}}",
                       "expr": 'max by (gw2_item_name) (gw2_commerce_item_demand)'}]), 12, 8)
    return dashboard("gw2-wealth", "GW2 Wealth & Economy", g)


# ---------------------------------------------------------------- Progression
def progression():
    g = Grid()
    g.add(stat("Daily AP", 'max(gw2_account_achievement_points{gw2_period="daily"})'), 4, 4)
    g.add(stat("Monthly AP", 'max(gw2_account_achievement_points{gw2_period="monthly"})'), 4, 4)
    g.add(stat("Magic Find %", "clamp_max(max(gw2_account_luck_total) * 300 / 4295450, 300)", unit="percent", decimals=1), 4, 4)
    g.add(stat("Fractal level", "max(gw2_account_fractal_level)"), 4, 4)
    g.add(stat("Masteries unlocked", "max(gw2_account_masteries_unlocked)"), 4, 4)
    g.add(stat("Played (days, /age)", "max(gw2_account_age_seconds_total) / 86400", decimals=1), 4, 4)
    g.row()
    g.add(stat("Total AP (computed)", "max(gw2_account_achievement_points_total)"), 5, 4)
    g.add(stat("Achievements done", "max(gw2_account_achievements_done)"), 5, 4)
    g.add(stat("Achievements %", "100 * max(gw2_account_achievements_done) / max(gw2_account_achievements_tracked)", unit="percent", decimals=1), 4, 4)
    g.add(stat("Legendaries owned", "max(gw2_account_legendary_armory_owned)"), 5, 4)
    g.add(stat("Legendaries available", "max(gw2_account_legendary_armory_available)"), 5, 4)
    g.row()
    g.add(timeseries("Luck consumed", [target("max(gw2_account_luck_total)", "luck")]), 12, 8)
    g.add(timeseries("Wizard's Vault objectives completed",
                     [target("max by (gw2_period) (gw2_wizardsvault_objectives_completed)", "{{gw2_period}}")]), 12, 8)
    g.row()
    g.add(bargauge("Story completion % by season",
                   "100 * max by (gw2_season) (gw2_story_quests_completed) / max by (gw2_season) (gw2_story_quests_total)",
                   "{{gw2_season}}", unit="percent", maxv=100), 12, 9)
    g.add(table("Mastery points by region",
                [{"refId": "earned", "format": "table", "instant": True, "datasource": PROM,
                  "expr": "max by (gw2_region) (gw2_account_mastery_points_earned_total)"},
                 {"refId": "spent", "format": "table", "instant": True, "datasource": PROM,
                  "expr": "max by (gw2_region) (gw2_account_mastery_points_spent)"}]), 12, 9)
    g.row()
    g.add(timeseries("Reset-cycle completions (since reset)",
                     [target("max by (gw2_kind) (gw2_account_reset_completed)", "{{gw2_kind}}")],
                     unit="none"), 24, 7)
    return dashboard("gw2-progression", "GW2 Progression", g)


# ---------------------------------------------------------------- Collections
def collections():
    g = Grid()
    g.add(bargauge("Collection completion %",
                   "100 * max by (gw2_collection) (gw2_account_unlocks_count) / max by (gw2_collection) (gw2_account_unlocks_total)",
                   "{{gw2_collection}}", unit="percent", maxv=100), 12, 12)
    g.add(table("Owned / total per collection",
                [{"refId": "owned", "format": "table", "instant": True, "datasource": PROM,
                  "expr": "max by (gw2_collection) (gw2_account_unlocks_count)"},
                 {"refId": "total", "format": "table", "instant": True, "datasource": PROM,
                  "expr": "max by (gw2_collection) (gw2_account_unlocks_total)"}]), 12, 12)
    return dashboard("gw2-collections", "GW2 Collections", g)


# ---------------------------------------------------------------- Characters
def characters():
    g = Grid()
    g.add(stat("Characters", "max(gw2_account_characters)"), 8, 4)
    g.add(stat("Total playtime (h)", "sum(max by (gw2_character_name) (gw2_character_playtime_seconds_total)) / 3600", decimals=1), 8, 4)
    g.add(stat("Total deaths", "sum(max by (gw2_character_name) (gw2_character_deaths_total))"), 8, 4)
    g.row()
    g.add(bargauge("Level by character", "max by (gw2_character_name) (gw2_character_level)", "{{gw2_character_name}}", maxv=80), 8, 10)
    g.add(bargauge("Playtime by character (h)", "max by (gw2_character_name) (gw2_character_playtime_seconds_total) / 3600", "{{gw2_character_name}}"), 8, 10)
    g.add(bargauge("Deaths by character", "max by (gw2_character_name) (gw2_character_deaths_total)", "{{gw2_character_name}}"), 8, 10)
    g.row()
    g.add(bargauge("Crafting rating (top disciplines)",
                   "topk(16, max by (gw2_character_name, gw2_discipline) (gw2_character_crafting_rating))",
                   "{{gw2_character_name}} {{gw2_discipline}}", maxv=500), 24, 10)
    g.row()
    g.add(bargauge("Inventory slots used by character",
                   'max by (gw2_character_name) (gw2_character_inventory_slots{gw2_state="used"})',
                   "{{gw2_character_name}}"), 12, 8)
    g.add(bargauge("Inventory capacity by character",
                   'max by (gw2_character_name) (gw2_character_inventory_slots{gw2_state="capacity"})',
                   "{{gw2_character_name}}"), 12, 8)
    return dashboard("gw2-characters", "GW2 Characters", g)


# ---------------------------------------------------------------- PvP & Ops
def pvp_ops():
    g = Grid()
    g.add(stat("PvP rank", "max(gw2_pvp_rank)"), 4, 4)
    g.add(stat("Rank points", "max(gw2_pvp_rank_points)"), 4, 4)
    g.add(stat("Wins", 'max(gw2_pvp_matches_total{gw2_outcome="win"})'), 4, 4)
    g.add(stat("Losses", 'max(gw2_pvp_matches_total{gw2_outcome="loss"})'), 4, 4)
    g.add(stat("Win rate %",
               '100 * max(gw2_pvp_matches_total{gw2_outcome="win"}) / clamp_min(max(gw2_pvp_matches_total{gw2_outcome="win"}) + max(gw2_pvp_matches_total{gw2_outcome="loss"}), 1)',
               unit="percent", decimals=1), 8, 4)
    g.row()
    g.add(timeseries("PvP matches by outcome",
                     [target("max by (gw2_outcome) (gw2_pvp_matches_total)", "{{gw2_outcome}}")]), 12, 7)
    g.add(timeseries("PvP wins by profession & ladder",
                     [target('max by (gw2_profession) (gw2_pvp_profession_matches_total{gw2_outcome="win"})', "prof {{gw2_profession}}"),
                      {"refId": "B", "datasource": PROM, "legendFormat": "ladder {{gw2_ladder}}",
                       "expr": 'max by (gw2_ladder) (gw2_pvp_ladder_matches_total{gw2_outcome="win"})'}]), 12, 7)
    g.row()
    # --- collector operations / self-observability ---
    g.add(timeseries("API request rate (req/s) by endpoint",
                     [target("sum by (gw2_endpoint) (rate(gw2_api_requests_total[5m]))", "{{gw2_endpoint}}")], unit="reqps"), 12, 8)
    g.add(timeseries("API request p95 latency (s)",
                     [target("histogram_quantile(0.95, sum by (le) (rate(gw2_api_request_duration_seconds_bucket[5m])))", "p95")], unit="s"), 12, 8)
    g.row()
    g.add(table("Seconds since last successful poll",
                [{"refId": "A", "format": "table", "instant": True, "datasource": PROM,
                  "expr": "time() - max by (gw2_family) (gw2_poll_last_success_timestamp_seconds)"}]), 8, 9)
    g.add(logs("Recent events", '{service_name="gw2-otel-collector"}'), 16, 9)
    return dashboard("gw2-pvp-ops", "GW2 PvP & Collector Health", g)


# ---------------------------------------------------------------- WvW
def wvw():
    g = Grid()
    g.add(stat("Home team", "gw2_wvw_home_team", color="none"), 6, 4)  # shows team color as series name
    g.add(stat("Our score", "max(gw2_wvw_match_score * on(gw2_team) group_left gw2_wvw_home_team)"), 6, 4)
    g.add(stat("Our PPT", "max(gw2_wvw_match_ppt * on(gw2_team) group_left gw2_wvw_home_team)"), 6, 4)
    g.add(stat("Our objectives", "sum(gw2_wvw_objectives_held * on(gw2_team) group_left gw2_wvw_home_team)"), 6, 4)
    g.row()
    g.add(timeseries("War score by team", [target("max by (gw2_team) (gw2_wvw_match_score)", "{{gw2_team}}")]), 12, 8)
    g.add(timeseries("Victory points by team", [target("max by (gw2_team) (gw2_wvw_match_victory_points)", "{{gw2_team}}")]), 12, 8)
    g.row()
    g.add(timeseries("Kills by team", [target("max by (gw2_team) (gw2_wvw_match_kills)", "{{gw2_team}}")]), 8, 8)
    g.add(timeseries("KDR by team", [target("max by (gw2_team) (gw2_wvw_match_kills) / max by (gw2_team) (gw2_wvw_match_deaths)", "{{gw2_team}}")]), 8, 8)
    g.add(timeseries("PPT by team", [target("max by (gw2_team) (gw2_wvw_match_ppt)", "{{gw2_team}}")]), 8, 8)
    g.row()
    g.add(bargauge("Objectives held by team & type",
                   "max by (gw2_team, gw2_objective_type) (gw2_wvw_objectives_held)",
                   "{{gw2_team}} {{gw2_objective_type}}"), 24, 10)
    return dashboard("gw2-wvw", "GW2 WvW Matchup", g)


def main():
    boards = {
        "gw2-overview.json": overview(),
        "gw2-wealth.json": wealth(),
        "gw2-progression.json": progression(),
        "gw2-collections.json": collections(),
        "gw2-characters.json": characters(),
        "gw2-pvp-ops.json": pvp_ops(),
        "gw2-wvw.json": wvw(),
    }
    for fname, board in boards.items():
        with open(os.path.join(HERE, fname), "w") as f:
            json.dump(board, f, indent=2)
            f.write("\n")
        print(f"wrote {fname} ({len(board['panels'])} panels)")


if __name__ == "__main__":
    main()
