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


def target(expr, legend=None, instant=False, ref=None):
    t = {"refId": ref or "A", "expr": expr, "datasource": PROM}
    if legend is not None:
        t["legendFormat"] = legend
    if instant:
        t["instant"] = True
    return t


def targets(*pairs, instant=False):
    """Build a list of targets from (expr, legend) pairs, auto-assigning refIds."""
    out = []
    for i, p in enumerate(pairs):
        expr, legend = (p if isinstance(p, tuple) else (p, None))
        out.append(target(expr, legend, instant=instant, ref=chr(65 + i)))
    return out


# ---------------------------------------------------------------- style primitives
# Team / faction colours used across WvW & PvP so a colour means the same thing
# everywhere. GW2's three WvW teams; Grafana semantic names elsewhere.
TEAM = {"red": "#E0252A", "blue": "#3573D6", "green": "#56A64B", "Red": "#E0252A",
        "Blue": "#3573D6", "Green": "#56A64B", "Neutral": "#8A8A8A"}


# Schema-v2 annotations: keep the built-in alerts layer and overlay GW2 domain
# events from Loki (level-ups, sales, PvP games, daily completions) on the graphs.
EVENT_ANNOTATIONS = [
    {"kind": "AnnotationQuery", "spec": {
        "name": "Annotations & Alerts", "enable": True, "hide": True, "builtIn": True,
        "iconColor": "rgba(0, 211, 255, 1)", "legacyOptions": {"type": "dashboard"},
        "query": {"kind": "DataQuery", "group": "grafana", "version": "v0",
                  "datasource": {"name": "-- Grafana --"}, "spec": {}}}},
    {"kind": "AnnotationQuery", "spec": {
        "name": "GW2 events", "enable": True, "hide": False, "iconColor": "#F2B01E",
        "query": {"kind": "DataQuery", "group": "loki", "version": "v0",
                  "datasource": {"name": "loki"},
                  "spec": {"expr": '{service_name="gw2-otel-collector"}', "refId": "Anno"}}}},
]

# Schema-v2 template variable: per-character drill-down used on the Characters tab.
VARIABLES = [
    {"kind": "QueryVariable", "spec": {
        "name": "character", "label": "Character", "hide": "dontHide",
        "refresh": "onDashboardLoad", "skipUrlSync": False, "sort": "alphabeticalAsc",
        "multi": True, "includeAll": True, "allowCustomValue": False,
        "current": {"text": "All", "value": "$__all"}, "options": [], "regex": "",
        "query": {"kind": "DataQuery", "group": "prometheus", "version": "v0",
                  "datasource": {"name": "prometheus"},
                  "spec": {"query": "label_values(gw2_character_level, gw2_character_name)", "refId": "A"}}}},
]


def thresholds(*steps):
    """steps: (value|None, color) pairs, ascending. None = base step."""
    return {"mode": "absolute",
            "steps": [{"value": v, "color": c} for v, c in steps]}


# Common threshold ramps.
PCT_RAMP = thresholds((None, "#E0252A"), (40, "orange"), (75, "#56A64B"))      # completion %
GOOD_HIGH = thresholds((None, "#8A8A8A"), (0.0001, "#56A64B"))                 # any positive = good
PROFIT_RAMP = thresholds((None, "#E0252A"), (0, "#8A8A8A"), (1, "#56A64B"))    # profit copper


def mappings(table):
    """table: {value: (text, color)} -> Grafana value mappings."""
    opts = {str(k): {"text": v[0], "color": v[1]} for k, v in table.items()}
    return [{"type": "value", "options": opts}]


def by_name(name, color):
    return {"matcher": {"id": "byName", "options": name},
            "properties": [{"id": "color", "value": {"mode": "fixed", "fixedColor": color}}]}


def by_regexp(rx, props):
    return {"matcher": {"id": "byRegexp", "options": rx}, "properties": props}


def team_overrides(prefix=""):
    """Per-series colour overrides matching team names (optionally prefixed)."""
    return [by_regexp(f".*{c}.*", [{"id": "color", "value": {"mode": "fixed", "fixedColor": col}}])
            for c, col in (("[Rr]ed", "#E0252A"), ("[Bb]lue", "#3573D6"), ("[Gg]reen", "#56A64B"))]


# GW2's canonical rarity colours (item + dye tiers), so rarity reads the same
# everywhere it appears.
RARITY = {"Basic": "#AAAAAA", "Junk": "#AAAAAA", "Fine": "#62A4DA", "Masterwork": "#1A9306",
          "Rare": "#FCD00B", "Exotic": "#FFA405", "Ascended": "#FB3E8D", "Legendary": "#4C139D",
          "Starter": "#8A8A8A", "Common": "#56A64B", "Uncommon": "#62A4DA", "Exclusive": "#FFA405"}


def rarity_overrides():
    return [by_name(name, col) for name, col in RARITY.items()]


# ---------------------------------------------------------------- panel builders
def stat(title, expr, unit="none", decimals=None, color="value", graph="area",
         thr=None, maps=None, text="value", inst=False, legend=None, overrides=None):
    defaults = {"unit": unit, "color": {"mode": "thresholds"},
                "thresholds": thr or thresholds((None, "#3573D6"))}
    if decimals is not None:
        defaults["decimals"] = decimals
    if maps:
        defaults["mappings"] = maps
    return {"type": "stat", "title": title, "datasource": PROM,
            "fieldConfig": {"defaults": defaults, "overrides": overrides or []},
            "options": {"colorMode": color, "graphMode": graph, "justifyMode": "auto",
                        "textMode": text, "reduceOptions": {"calc": "lastNotNull"}},
            "targets": [target(expr, legend, instant=inst)]}


def gauge(title, expr, unit="none", minv=0, maxv=100, thr=None, decimals=None, legend=None):
    defaults = {"unit": unit, "min": minv, "max": maxv,
                "color": {"mode": "thresholds"}, "thresholds": thr or PCT_RAMP}
    if decimals is not None:
        defaults["decimals"] = decimals
    return {"type": "gauge", "title": title, "datasource": PROM,
            "fieldConfig": {"defaults": defaults, "overrides": []},
            "options": {"showThresholdLabels": False, "showThresholdMarkers": True,
                        "reduceOptions": {"calc": "lastNotNull"}},
            "targets": [target(expr, legend, instant=True)]}


def piechart(title, tgts, unit="none", pie="donut", labels=("percent",),
             values=("value",), overrides=None, palette="palette-classic"):
    return {"type": "piechart", "title": title, "datasource": PROM,
            "fieldConfig": {"defaults": {"unit": unit, "color": {"mode": palette}},
                            "overrides": overrides or []},
            "options": {"pieType": pie, "tooltip": {"mode": "single", "sort": "desc"},
                        "displayLabels": list(labels),
                        "legend": {"displayMode": "table", "placement": "right",
                                   "values": list(values)},
                        "reduceOptions": {"calc": "lastNotNull", "values": False}},
            "targets": tgts}


def barchart(title, tgts, unit="none", orientation="horizontal", stacking="none",
             overrides=None, palette="palette-classic", showValue="auto"):
    return {"type": "barchart", "title": title, "datasource": PROM,
            "fieldConfig": {"defaults": {"unit": unit, "color": {"mode": palette},
                                         "custom": {"fillOpacity": 85, "lineWidth": 1,
                                                    "gradientMode": "hue"}},
                            "overrides": overrides or []},
            "options": {"orientation": orientation, "stacking": stacking, "showValue": showValue,
                        "xTickLabelRotation": 0, "groupWidth": 0.8, "barWidth": 0.9,
                        "legend": {"displayMode": "list", "placement": "bottom"},
                        "tooltip": {"mode": "single", "sort": "desc"}},
            "targets": tgts}


def timeseries(title, tgts, unit="none", stack=False, fill=None, overrides=None,
               placement="right", calcs=("lastNotNull",), colorMode=None,
               draw="line", points="never", interp="smooth"):
    custom = {"fillOpacity": fill if fill is not None else (35 if stack else 12),
              "showPoints": points, "drawStyle": draw, "lineWidth": 2,
              "gradientMode": "opacity", "lineInterpolation": interp,
              "spanNulls": True}
    if stack:
        custom["stacking"] = {"mode": "normal"}
    defaults = {"unit": unit, "custom": custom}
    if colorMode:
        defaults["color"] = {"mode": colorMode}
    return {"type": "timeseries", "title": title, "datasource": PROM,
            "fieldConfig": {"defaults": defaults, "overrides": overrides or []},
            "options": {"legend": {"displayMode": "table", "placement": placement,
                                   "calcs": list(calcs)},
                        "tooltip": {"mode": "multi", "sort": "desc"}},
            "targets": tgts}


def statetimeline(title, tgts, maps=None, thr=None, overrides=None, rowHeight=0.9):
    defaults = {"color": {"mode": "thresholds"}, "thresholds": thr or thresholds((None, "#8A8A8A")),
                "custom": {"fillOpacity": 80, "lineWidth": 0}}
    if maps:
        defaults["mappings"] = maps
    return {"type": "state-timeline", "title": title, "datasource": PROM,
            "fieldConfig": {"defaults": defaults, "overrides": overrides or []},
            "options": {"showValue": "never", "mergeValues": True, "alignValue": "center",
                        "rowHeight": rowHeight,
                        "legend": {"displayMode": "list", "placement": "bottom"},
                        "tooltip": {"mode": "single"}},
            "targets": tgts}


def bargauge(title, expr, legend, unit="none", maxv=None, mode="gradient",
             thr=None, colorMode="continuous-GrYlRd", overrides=None):
    defaults = {"unit": unit}
    if thr:
        defaults["color"], defaults["thresholds"] = {"mode": "thresholds"}, thr
    else:
        defaults["color"] = {"mode": colorMode}
    if maxv is not None:
        defaults["min"], defaults["max"] = 0, maxv
    return {"type": "bargauge", "title": title, "datasource": PROM,
            "fieldConfig": {"defaults": defaults, "overrides": overrides or []},
            "options": {"orientation": "horizontal", "displayMode": mode,
                        "valueMode": "color" if thr else "text",
                        "reduceOptions": {"calc": "lastNotNull"}},
            "targets": [target(expr, legend, instant=True)]}


def logs(title, expr):
    return {"type": "logs", "title": title, "datasource": LOKI,
            "options": {"showTime": True, "sortOrder": "Descending", "wrapLogMessage": True,
                        "enableLogDetails": True},
            "targets": [{"refId": "A", "expr": expr, "datasource": LOKI}]}


def table(title, tgts, overrides=None, cell=None, transformations=None):
    fc = {"defaults": {}, "overrides": overrides or []}
    if cell:
        fc["defaults"]["custom"] = {"cellOptions": {"type": cell}}
    p = {"type": "table", "title": title, "datasource": PROM, "fieldConfig": fc,
         "options": {"showHeader": True, "cellHeight": "sm"}, "targets": tgts}
    if transformations:
        p["transformations"] = transformations
    return p


def text(title, markdown):
    return {"type": "text", "title": title, "datasource": None,
            "options": {"mode": "markdown", "content": markdown}, "targets": []}


CLASSIC_CHARACTER_VAR = {
    "name": "character", "label": "Character", "type": "query",
    "datasource": {"type": "prometheus", "uid": "prometheus"},
    "query": {"query": "label_values(gw2_character_level, gw2_character_name)", "refId": "A"},
    "refresh": 1, "includeAll": True, "multi": True, "sort": 1,
    "current": {"text": "All", "value": "$__all"}}


def dashboard(uid, title, grid):
    return {"uid": uid, "title": title, "tags": ["gw2"], "timezone": "browser",
            "schemaVersion": 39, "version": 1, "refresh": "30s",
            "time": {"from": "now-24h", "to": "now"},
            "templating": {"list": [CLASSIC_CHARACTER_VAR]},
            "panels": grid.panels}


def gold(expr):  # copper expr -> gold
    return f"({expr}) / 10000"


# ---------------------------------------------------------------- Overview
def overview():
    g = Grid()
    # --- hero KPI band: big coloured numbers with sparkline history ---
    g.add(stat("Account Value", gold('max(gw2_account_value{gw2_component="total",gw2_basis="sell"})'),
               decimals=1, color="background", thr=thresholds((None, "#F2B01E")), text="value"), 4, 5)
    g.add(stat("Liquid (−15%)", gold('max(gw2_account_value{gw2_component="total",gw2_basis="sell"}) * 0.85'),
               decimals=1, thr=thresholds((None, "#F2B01E"))), 4, 5)
    g.add(stat("Gold", gold('max(gw2_account_wallet_balance{gw2_currency_name="Coin"})'),
               decimals=1, thr=thresholds((None, "#F2B01E"))), 4, 5)
    g.add(stat("Gems", 'max(gw2_account_wallet_balance{gw2_currency_name="Gem"})',
               thr=thresholds((None, "#8F3BB8"))), 4, 5)
    g.add(stat("Playtime", "sum(max by (gw2_character_name) (gw2_character_playtime_seconds_total))",
               unit="s", decimals=0, thr=thresholds((None, "#3573D6"))), 4, 5)
    g.add(stat("Characters", "max(gw2_account_characters)", thr=thresholds((None, "#56A64B"))), 4, 5)
    g.row()
    # --- wealth composition + trend ---
    g.add(piechart("Wealth composition (sell, gold)",
                   [target(gold('max by (gw2_component) (gw2_account_value{gw2_basis="sell",gw2_component!="total"})'),
                           "{{gw2_component}}", instant=True)],
                   labels=("name", "percent"), values=("value", "percent")), 8, 9)
    g.add(timeseries("Account value over time (gold, stacked)",
                     [target(gold('max by (gw2_component) (gw2_account_value{gw2_basis="sell",gw2_component!="total"})'),
                             "{{gw2_component}}")],
                     stack=True, calcs=("lastNotNull", "max")), 16, 9)
    g.row()
    # --- at-a-glance progress dials ---
    g.add(gauge("Achievements done %",
                "100 * max(gw2_account_achievements_done) / clamp_min(max(gw2_account_achievements_tracked), 1)",
                unit="percent"), 5, 8)
    g.add(gauge("Story completion %",
                "100 * sum(gw2_story_quests_completed) / clamp_min(sum(gw2_story_quests_total), 1)",
                unit="percent"), 5, 8)
    g.add(gauge("Magic find %", "clamp_max(max(gw2_account_luck_total) * 300 / 4295450, 300)",
                unit="percent", maxv=300, thr=thresholds((None, "#3573D6"), (150, "#F2B01E"), (250, "#56A64B"))), 5, 8)
    g.add(bargauge("Collection completion %",
                   "100 * max by (gw2_collection) (gw2_account_unlocks_count) / clamp_min(max by (gw2_collection) (gw2_account_unlocks_total), 1)",
                   "{{gw2_collection}}", unit="percent", maxv=100, thr=PCT_RAMP), 9, 8)
    g.row()
    g.add(logs("Recent activity", '{service_name="gw2-otel-collector"}'), 24, 8)
    return dashboard("gw2-overview", "GW2 Overview", g)


# ---------------------------------------------------------------- Wealth
def wealth():
    g = Grid()
    # --- hero KPI band ---
    g.add(stat("Total value", gold('max(gw2_account_value{gw2_component="total",gw2_basis="sell"})'),
               decimals=1, color="background", thr=thresholds((None, "#F2B01E")), text="value"), 4, 5)
    g.add(stat("Liquid (−15%)", gold('max(gw2_account_value{gw2_component="total",gw2_basis="sell"}) * 0.85'),
               decimals=1, thr=thresholds((None, "#F2B01E"))), 4, 5)
    g.add(stat("Gold", gold('max(gw2_account_wallet_balance{gw2_currency_name="Coin"})'),
               decimals=1, thr=thresholds((None, "#F2B01E"))), 4, 5)
    g.add(stat("Gems", 'max(gw2_account_wallet_balance{gw2_currency_name="Gem"})',
               thr=thresholds((None, "#8F3BB8"))), 4, 5)
    g.add(stat("Delivery box", gold("max(gw2_commerce_delivery_coins)"), decimals=1,
               thr=thresholds((None, "#3573D6"))), 4, 5)
    g.add(stat("Open orders", gold('sum(max by (gw2_side) (gw2_commerce_orders_open_value))'),
               decimals=1, thr=thresholds((None, "#3573D6"))), 4, 5)
    g.row()
    # --- composition + trend ---
    g.add(piechart("Wealth composition (sell, gold)",
                   [target(gold('max by (gw2_component) (gw2_account_value{gw2_basis="sell",gw2_component!="total"})'),
                           "{{gw2_component}}", instant=True)],
                   labels=("name", "percent"), values=("value", "percent")), 8, 9)
    g.add(timeseries("Account value over time (gold): buy vs sell + components",
                     [target(gold('max by (gw2_component) (gw2_account_value{gw2_basis="sell",gw2_component!="total"})'),
                             "{{gw2_component}}")],
                     stack=True, calcs=("lastNotNull", "max")), 16, 9)
    g.row()
    # --- materials, exchange, wallet ---
    g.add(piechart("Material value by category (gold)",
                   [target(gold("max by (gw2_material_category_name) (gw2_account_material_value)"),
                           "{{gw2_material_category_name}}", instant=True)],
                   labels=("percent",), values=("value",)), 8, 8)
    g.add(timeseries("Gem ⇄ coin exchange (copper per gem)",
                     [target('max by (gw2_direction) (gw2_commerce_exchange_coins_per_gem)', "{{gw2_direction}}")],
                     placement="bottom"), 8, 8)
    g.add(bargauge("Wallet balances (top 10 currencies)",
                   "topk(10, max by (gw2_currency_name) (gw2_account_wallet_balance))",
                   "{{gw2_currency_name}}"), 8, 8)
    g.row()
    # --- trading post: prices, depth, flip & craft economics ---
    g.add(timeseries("Tracked item prices (copper): buy vs sell",
                     [target('max by (gw2_item_name, gw2_side) (gw2_commerce_item_price)',
                             "{{gw2_item_name}} {{gw2_side}}")], placement="bottom"), 12, 8)
    g.add(barchart("Tracked item supply vs demand (units)",
                   targets(('max by (gw2_item_name) (gw2_commerce_item_supply)', "supply"),
                           ('max by (gw2_item_name) (gw2_commerce_item_demand)', "demand"),
                           instant=True),
                   overrides=[by_name("supply", "#3573D6"), by_name("demand", "#E0252A")]), 12, 8)
    g.row()
    g.add(bargauge("Flip margin (copper, sell×0.85 − buy)",
                   "max by (gw2_item_name) (gw2_commerce_item_flip_margin)", "{{gw2_item_name}}",
                   thr=PROFIT_RAMP), 8, 8)
    g.add(bargauge("Crafting profit (copper)",
                   "max by (gw2_item_name) (gw2_commerce_craft_profit)", "{{gw2_item_name}}",
                   thr=PROFIT_RAMP), 8, 8)
    g.add(bargauge("24h movers (sell-price change %)",
                   '(max by (gw2_item_name) (gw2_commerce_item_price{gw2_side="sell"}) / '
                   'max by (gw2_item_name) (gw2_commerce_item_price{gw2_side="sell"} offset 24h) - 1) * 100',
                   "{{gw2_item_name}}", unit="percent",
                   thr=thresholds((None, "#E0252A"), (0, "#8A8A8A"), (0.0001, "#56A64B"))), 8, 8)
    return dashboard("gw2-wealth", "GW2 Wealth & Economy", g)


# ---------------------------------------------------------------- Progression
def progression():
    g = Grid()
    # --- hero KPI band ---
    g.add(stat("Total AP", "max(gw2_account_achievement_points_total)",
               color="background", thr=thresholds((None, "#F2B01E")), text="value"), 4, 5)
    g.add(stat("Daily AP", 'max(gw2_account_achievement_points{gw2_period="daily"})',
               thr=thresholds((None, "#F2B01E"))), 4, 5)
    g.add(stat("Monthly AP", 'max(gw2_account_achievement_points{gw2_period="monthly"})',
               thr=thresholds((None, "#F2B01E"))), 4, 5)
    g.add(stat("Fractal level", "max(gw2_account_fractal_level)", thr=thresholds((None, "#E0252A"))), 4, 5)
    g.add(stat("Masteries", "max(gw2_account_masteries_unlocked)", thr=thresholds((None, "#56A64B"))), 4, 5)
    g.add(stat("Played", "max(gw2_account_age_seconds_total)", unit="s", thr=thresholds((None, "#3573D6"))), 4, 5)
    g.row()
    # --- achievement / luck dials + story progress ---
    g.add(gauge("Achievements done %",
                "100 * max(gw2_account_achievements_done) / clamp_min(max(gw2_account_achievements_tracked), 1)",
                unit="percent"), 6, 8)
    g.add(gauge("Magic find %", "clamp_max(max(gw2_account_luck_total) * 300 / 4295450, 300)",
                unit="percent", maxv=300,
                thr=thresholds((None, "#3573D6"), (150, "#F2B01E"), (250, "#56A64B"))), 6, 8)
    g.add(bargauge("Story completion % by season",
                   "100 * max by (gw2_season) (gw2_story_quests_completed) / clamp_min(max by (gw2_season) (gw2_story_quests_total), 1)",
                   "{{gw2_season}}", unit="percent", maxv=100, thr=PCT_RAMP), 12, 8)
    g.row()
    # --- legendary journey ---
    g.add(bargauge("Legendary collections done / total %",
                   "100 * max by (gw2_legendary_category) (gw2_legendary_collections_done) / "
                   "clamp_min(max by (gw2_legendary_category) (gw2_legendary_collections_total), 1)",
                   "{{gw2_legendary_category}}", unit="percent", maxv=100, thr=PCT_RAMP), 12, 8)
    g.add(bargauge("Legendary items obtained % (started collections)",
                   "100 * max by (gw2_legendary_category) (gw2_legendary_items_current) / "
                   "clamp_min(max by (gw2_legendary_category) (gw2_legendary_items_max), 1)",
                   "{{gw2_legendary_category}}", unit="percent", maxv=100, thr=PCT_RAMP), 12, 8)
    g.row()
    # --- masteries & vault ---
    g.add(barchart("Mastery points by region (earned vs spent)",
                   targets(("max by (gw2_region) (gw2_account_mastery_points_earned_total)", "earned"),
                           ("max by (gw2_region) (gw2_account_mastery_points_spent)", "spent"),
                           instant=True),
                   overrides=[by_name("earned", "#56A64B"), by_name("spent", "#F2B01E")]), 12, 8)
    g.add(timeseries("Wizard's Vault objectives completed by period",
                     [target("max by (gw2_period) (gw2_wizardsvault_objectives_completed)", "{{gw2_period}}")],
                     placement="bottom"), 12, 8)
    g.row()
    g.add(barchart("Reset-cycle completions (since reset)",
                   [target("max by (gw2_kind) (gw2_account_reset_completed)", "{{gw2_kind}}", instant=True)],
                   orientation="vertical"), 24, 7)
    return dashboard("gw2-progression", "GW2 Progression", g)


# ---------------------------------------------------------------- Collections
def collections():
    g = Grid()
    # --- hero KPI band ---
    g.add(stat("Skins unlocked", "sum(max by (gw2_skin_type, gw2_rarity) (gw2_wardrobe_skins))",
               color="background", thr=thresholds((None, "#8F3BB8")), text="value"), 8, 5)
    g.add(stat("Dyes unlocked", "sum(max by (gw2_rarity) (gw2_wardrobe_dyes))",
               thr=thresholds((None, "#56A64B"))), 8, 5)
    g.add(stat("Avg collection completion %",
               "100 * sum(max by (gw2_collection) (gw2_account_unlocks_count)) / "
               "clamp_min(sum(max by (gw2_collection) (gw2_account_unlocks_total)), 1)",
               unit="percent", decimals=1, thr=thresholds((None, "#F2B01E"))), 8, 5)
    g.row()
    # --- per-collection completion ---
    g.add(bargauge("Collection completion %",
                   "100 * max by (gw2_collection) (gw2_account_unlocks_count) / clamp_min(max by (gw2_collection) (gw2_account_unlocks_total), 1)",
                   "{{gw2_collection}}", unit="percent", maxv=100, thr=PCT_RAMP), 24, 10)
    g.row()
    # --- wardrobe composition ---
    g.add(piechart("Skins by type",
                   [target("sum by (gw2_skin_type) (max by (gw2_skin_type, gw2_rarity) (gw2_wardrobe_skins))",
                           "{{gw2_skin_type}}", instant=True)],
                   labels=("name", "value"), values=("value", "percent")), 8, 9)
    g.add(piechart("Skins by rarity",
                   [target("sum by (gw2_rarity) (max by (gw2_skin_type, gw2_rarity) (gw2_wardrobe_skins))",
                           "{{gw2_rarity}}", instant=True)],
                   pie="pie", labels=("name", "value"), values=("value", "percent"),
                   overrides=rarity_overrides()), 8, 9)
    g.add(piechart("Dyes by rarity",
                   [target("max by (gw2_rarity) (gw2_wardrobe_dyes)", "{{gw2_rarity}}", instant=True)],
                   pie="pie", labels=("name", "value"), values=("value", "percent"),
                   overrides=rarity_overrides()), 8, 9)
    return dashboard("gw2-collections", "GW2 Collections", g)


# ---------------------------------------------------------------- Characters
def characters():
    g = Grid()
    # --- hero KPI band ---
    g.add(stat("Characters", "max(gw2_account_characters)",
               color="background", thr=thresholds((None, "#56A64B")), text="value"), 6, 5)
    g.add(stat("Total playtime", "sum(max by (gw2_character_name) (gw2_character_playtime_seconds_total))",
               unit="s", thr=thresholds((None, "#3573D6"))), 6, 5)
    g.add(stat("Total deaths", "sum(max by (gw2_character_name) (gw2_character_deaths_total))",
               thr=thresholds((None, "#E0252A"))), 6, 5)
    g.add(stat("Average level",
               "avg(max by (gw2_character_name) (gw2_character_level))", decimals=0,
               thr=thresholds((None, "#F2B01E"))), 6, 5)
    g.row()
    # --- roster table: one row per character (single query carries all labels) ---
    roster_overrides = [
        {"matcher": {"id": "byName", "options": "Level"},
         "properties": [{"id": "custom.cellOptions", "value": {"type": "color-background"}},
                        {"id": "custom.width", "value": 90},
                        {"id": "thresholds", "value": thresholds((None, "#E0252A"), (40, "orange"), (80, "#56A64B"))}]},
    ]
    g.add(table("Character roster (by level)",
                [target('max by (gw2_character_name, gw2_character_profession, gw2_character_race) (gw2_character_level{gw2_character_name=~"$character"})',
                        ref="A", instant=True)],
                overrides=roster_overrides,
                transformations=[
                    {"id": "organize", "options": {
                        "excludeByName": {"Time": True},
                        "indexByName": {"gw2_character_name": 0, "gw2_character_profession": 1,
                                        "gw2_character_race": 2, "Value": 3},
                        "renameByName": {
                            "gw2_character_name": "Character", "gw2_character_profession": "Profession",
                            "gw2_character_race": "Race", "Value": "Level"}}},
                    {"id": "sortBy", "options": {"fields": {}, "sort": [{"field": "Level", "desc": True}]}}]), 24, 9)
    for t in g.panels[-1]["targets"]:
        t["format"] = "table"
    g.row()
    # --- per-character bars ---
    g.add(bargauge("Level by character",
                   'max by (gw2_character_name) (gw2_character_level{gw2_character_name=~"$character"})',
                   "{{gw2_character_name}}",
                   maxv=80, thr=thresholds((None, "#E0252A"), (40, "orange"), (80, "#56A64B"))), 8, 9)
    g.add(bargauge("Playtime by character (h)",
                   'max by (gw2_character_name) (gw2_character_playtime_seconds_total{gw2_character_name=~"$character"}) / 3600',
                   "{{gw2_character_name}}", unit="h"), 8, 9)
    g.add(bargauge("Deaths by character",
                   'max by (gw2_character_name) (gw2_character_deaths_total{gw2_character_name=~"$character"})',
                   "{{gw2_character_name}}", colorMode="continuous-RdYlGr"), 8, 9)
    g.row()
    g.add(barchart("Crafting rating by character & discipline",
                   [target('max by (gw2_character_name, gw2_discipline) (gw2_character_crafting_rating{gw2_character_name=~"$character"})',
                           "{{gw2_character_name}} · {{gw2_discipline}}", instant=True)],
                   orientation="horizontal"), 12, 10)
    g.add(barchart("Inventory: used vs capacity by character",
                   targets(('max by (gw2_character_name) (gw2_character_inventory_slots{gw2_state="used",gw2_character_name=~"$character"})', "used"),
                           ('max by (gw2_character_name) (gw2_character_inventory_slots{gw2_state="capacity",gw2_character_name=~"$character"})', "capacity"),
                           instant=True),
                   stacking="none",
                   overrides=[by_name("used", "#3573D6"), by_name("capacity", "#444444")]), 12, 10)
    return dashboard("gw2-characters", "GW2 Characters", g)


# ---------------------------------------------------------------- PvP & Ops
def pvp_ops():
    g = Grid()
    # --- PvP KPIs ---
    g.add(stat("PvP rank", "max(gw2_pvp_rank)",
               color="background", thr=thresholds((None, "#8F3BB8")), text="value"), 4, 5)
    g.add(stat("Rank points", "max(gw2_pvp_rank_points)", thr=thresholds((None, "#8F3BB8"))), 4, 5)
    g.add(stat("Wins", 'max(gw2_pvp_matches_total{gw2_outcome="win"})', thr=thresholds((None, "#56A64B"))), 4, 5)
    g.add(stat("Losses", 'max(gw2_pvp_matches_total{gw2_outcome="loss"})', thr=thresholds((None, "#E0252A"))), 4, 5)
    g.add(stat("Total games",
               'max(gw2_pvp_matches_total{gw2_outcome="win"}) + max(gw2_pvp_matches_total{gw2_outcome="loss"})',
               thr=thresholds((None, "#3573D6"))), 4, 5)
    g.add(stat("Desertions", 'max(gw2_pvp_matches_total{gw2_outcome="desertion"})', thr=thresholds((None, "#8A8A8A"))), 4, 5)
    g.row()
    g.add(gauge("Win rate %",
                '100 * max(gw2_pvp_matches_total{gw2_outcome="win"}) / '
                'clamp_min(max(gw2_pvp_matches_total{gw2_outcome="win"}) + max(gw2_pvp_matches_total{gw2_outcome="loss"}), 1)',
                unit="percent",
                thr=thresholds((None, "#E0252A"), (45, "orange"), (55, "#56A64B"))), 6, 8)
    g.add(piechart("Wins by profession",
                   [target('max by (gw2_profession) (gw2_pvp_profession_matches_total{gw2_outcome="win"})',
                           "{{gw2_profession}}", instant=True)],
                   labels=("name", "value"), values=("value",)), 9, 8)
    g.add(barchart("Win/Loss by profession",
                   targets(('max by (gw2_profession) (gw2_pvp_profession_matches_total{gw2_outcome="win"})', "wins"),
                           ('max by (gw2_profession) (gw2_pvp_profession_matches_total{gw2_outcome="loss"})', "losses"),
                           instant=True),
                   stacking="normal",
                   overrides=[by_name("wins", "#56A64B"), by_name("losses", "#E0252A")]), 9, 8)
    g.row()
    # --- collector health header ---
    g.add(text("", "### 🩺 Collector health"), 24, 2)
    g.row()
    g.add(bargauge("Seconds since last successful poll (by family)",
                   "time() - max by (gw2_family) (gw2_poll_last_success_timestamp_seconds)",
                   "{{gw2_family}}", unit="s",
                   thr=thresholds((None, "#56A64B"), (900, "orange"), (1800, "#E0252A"))), 8, 9)
    g.add(timeseries("API request rate (req/s) by endpoint",
                     [target("sum by (gw2_endpoint) (rate(gw2_api_requests_total[5m]))", "{{gw2_endpoint}}")],
                     unit="reqps", placement="bottom"), 8, 9)
    g.add(timeseries("API request p95 latency",
                     [target("histogram_quantile(0.95, sum by (le) (rate(gw2_api_request_duration_seconds_bucket[5m])))", "p95")],
                     unit="s", placement="bottom"), 8, 9)
    g.row()
    g.add(logs("Recent events", '{service_name="gw2-otel-collector"}'), 24, 8)
    return dashboard("gw2-pvp-ops", "GW2 PvP & Collector Health", g)


# ---------------------------------------------------------------- WvW
def wvw():
    g = Grid()
    home = "* on(gw2_team) group_left gw2_wvw_home_team"
    # --- our-team KPIs (filtered to the account's home team) ---
    g.add(stat("Home team", "gw2_wvw_home_team", text="name", legend="{{gw2_team}}",
               color="background", graph="none", inst=True, overrides=team_overrides()), 4, 5)
    g.add(stat("Our score", f"max(gw2_wvw_match_score {home})", thr=thresholds((None, "#F2B01E")), inst=True), 4, 5)
    g.add(stat("Our VP", f"max(gw2_wvw_match_victory_points {home})", thr=thresholds((None, "#F2B01E")), inst=True), 4, 5)
    g.add(stat("Our PPT", f"max(gw2_wvw_match_ppt {home})", thr=thresholds((None, "#56A64B")), inst=True), 4, 5)
    g.add(stat("Our kills", f"max(gw2_wvw_match_kills {home})", thr=thresholds((None, "#E0252A")), inst=True), 4, 5)
    g.add(stat("Our objectives", f"sum(gw2_wvw_objectives_held {home})", thr=thresholds((None, "#3573D6")), inst=True), 4, 5)
    g.row()
    # --- the matchup, in team colours ---
    g.add(timeseries("War score by team", [target("max by (gw2_team) (gw2_wvw_match_score)", "{{gw2_team}}")],
                     overrides=team_overrides(), fill=20), 12, 8)
    g.add(timeseries("Victory points by team", [target("max by (gw2_team) (gw2_wvw_match_victory_points)", "{{gw2_team}}")],
                     overrides=team_overrides(), fill=20), 12, 8)
    g.row()
    g.add(timeseries("Kills by team", [target("max by (gw2_team) (gw2_wvw_match_kills)", "{{gw2_team}}")],
                     overrides=team_overrides()), 8, 8)
    g.add(timeseries("KDR by team",
                     [target("max by (gw2_team) (gw2_wvw_match_kills) / max by (gw2_team) (gw2_wvw_match_deaths)", "{{gw2_team}}")],
                     overrides=team_overrides()), 8, 8)
    g.add(timeseries("PPT by team", [target("max by (gw2_team) (gw2_wvw_match_ppt)", "{{gw2_team}}")],
                     overrides=team_overrides()), 8, 8)
    g.row()
    g.add(bargauge("Objectives held by team & type",
                   "max by (gw2_team, gw2_objective_type) (gw2_wvw_objectives_held)",
                   "{{gw2_team}} · {{gw2_objective_type}}", colorMode="continuous-GrYlRd",
                   overrides=team_overrides()), 12, 10)
    g.add(barchart("Objectives held by type (stacked by team)",
                   [target("max by (gw2_objective_type, gw2_team) (gw2_wvw_objectives_held)",
                           "{{gw2_team}}", instant=True)],
                   stacking="normal", orientation="horizontal", overrides=team_overrides()), 12, 10)
    g.row()
    g.add(timeseries("Per-map score (Center = EBG)",
                     [target("max by (gw2_map, gw2_team) (gw2_wvw_map_score)", "{{gw2_map}} · {{gw2_team}}")],
                     overrides=team_overrides(), placement="bottom"), 12, 8)
    g.add(timeseries("Per-map kills",
                     [target("max by (gw2_map, gw2_team) (gw2_wvw_map_kills)", "{{gw2_map}} · {{gw2_team}}")],
                     overrides=team_overrides(), placement="bottom"), 12, 8)
    return dashboard("gw2-wvw", "GW2 WvW Matchup", g)


# ---------------------------------------------------------------- v2 tabbed model
# Grafana 12+ schema v2: panels live in an `elements` map and the layout
# (TabsLayout) references them. We reuse the classic panel builders above as tab
# content and convert each panel to a v2 Panel element + GridLayoutItem.

def panel_to_element(panel, pid):
    queries = []
    for i, t in enumerate(panel.get("targets", [])):
        ds = (t.get("datasource") or {}).get("uid", "prometheus")
        qspec = {"expr": t["expr"]}
        if "legendFormat" in t:
            qspec["legendFormat"] = t["legendFormat"]
        if t.get("instant"):
            qspec["instant"], qspec["range"] = True, False
        if t.get("format"):
            qspec["format"] = t["format"]
        queries.append({"kind": "PanelQuery", "spec": {
            "query": {"kind": "DataQuery", "group": "loki" if ds == "loki" else "prometheus",
                      "version": "v0", "datasource": {"name": ds}, "spec": qspec},
            "refId": t.get("refId", chr(65 + i)), "hidden": False}})
    return {"kind": "Panel", "spec": {
        "id": pid, "title": panel["title"], "description": "", "links": [],
        "data": {"kind": "QueryGroup", "spec": {"queries": queries, "transformations": [], "queryOptions": {}}},
        "vizConfig": {"kind": "VizConfig", "group": panel["type"], "version": "", "spec": {
            "options": panel.get("options", {}),
            "fieldConfig": panel.get("fieldConfig", {"defaults": {}, "overrides": []})}}}}


def tabbed_dashboard(uid, title, tabs):
    elements, tab_specs, pid = {}, [], 0
    for tab_title, grid in tabs:
        items = []
        for p in grid.panels:
            pid += 1
            name = f"panel-{pid}"
            elements[name] = panel_to_element(p, pid)
            gp = p["gridPos"]
            items.append({"kind": "GridLayoutItem", "spec": {
                "x": gp["x"], "y": gp["y"], "width": gp["w"], "height": gp["h"],
                "element": {"kind": "ElementReference", "name": name}}})
        tab_specs.append({"kind": "TabsLayoutTab", "spec": {
            "title": tab_title, "layout": {"kind": "GridLayout", "spec": {"items": items}}}})
    spec = {
        "title": title, "tags": ["gw2"], "editable": True, "preload": False,
        "liveNow": False, "cursorSync": "Off", "links": [],
        "annotations": EVENT_ANNOTATIONS, "variables": VARIABLES,
        "timeSettings": {"from": "now-24h", "to": "now", "autoRefresh": "30s",
                         "autoRefreshIntervals": ["5s", "10s", "30s", "1m", "5m", "15m", "30m", "1h"],
                         "hideTimepicker": False, "timezone": "browser", "fiscalYearStartMonth": 0},
        "elements": elements,
        "layout": {"kind": "TabsLayout", "spec": {"tabs": tab_specs}},
    }
    return {"apiVersion": "dashboard.grafana.app/v2beta1", "kind": "Dashboard",
            "metadata": {"name": uid, "namespace": "default"}, "spec": spec}


def main():
    # Each section builder returns a classic dashboard dict; we wrap its Grid as a tab.
    # (overview/wealth/... build a Grid `g`; we re-run them and read g via panels.)
    tabs = [
        ("Overview", _grid(overview())),
        ("Wealth", _grid(wealth())),
        ("Progression", _grid(progression())),
        ("Collections", _grid(collections())),
        ("Characters", _grid(characters())),
        ("PvP & Health", _grid(pvp_ops())),
        ("WvW", _grid(wvw())),
    ]
    board = tabbed_dashboard("gw2", "GW2 Account", tabs)
    with open(os.path.join(HERE, "gw2.json"), "w") as f:
        json.dump(board, f, indent=2)
        f.write("\n")
    npanels = len(board["spec"]["elements"])
    print(f"wrote gw2.json ({len(tabs)} tabs, {npanels} panels)")


class _GridView:
    """Adapts a classic dashboard dict (with panels[]) to a .panels accessor."""
    def __init__(self, panels):
        self.panels = panels


def _grid(board):
    return _GridView(board["panels"])


if __name__ == "__main__":
    main()
