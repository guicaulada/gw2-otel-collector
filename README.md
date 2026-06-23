# gw2-otel-collector

An OpenTelemetry collector for **Guild Wars 2**. It polls the official
[GW2 API v2](https://wiki.guildwars2.com/wiki/API:Main) with an account API key,
turns the data into OpenTelemetry **metrics, logs, and traces**, and exports them
to a backend (Prometheus + Loki + Tempo, or any OTLP target) so you can build
Grafana dashboards that track the same things [gw2efficiency](https://gw2efficiency.com/)
and [gw2storytracker](https://gw2storytracker.com/) surface — but in your own
observability stack, with alerting and custom panels.

## Is this project worth building?

**Short answer: yes — it's a genuinely cool project, with one constraint you must
design around.** See [`docs/viability.md`](docs/viability.md) for the full
assessment. The headline points:

- **The GW2 API is snapshot-only.** It exposes *current* state (prices, balances,
  ranks, unlock sets) and a handful of *lifetime cumulative* fields (playtime,
  deaths, AP, PvP wins, luck). It stores **almost no history itself**. Every
  "over time" graph — gold history, account-value curve, price charts, win-rate
  trends — has to be built by *something that polls and persists*. That is exactly
  what an OTel → Prometheus/Loki pipeline is for. **The collector's core value-add
  is being the historian the API isn't.** This is a perfect fit, not a workaround.
- **The economy and progression data is excellent.** Wallet (~70 currencies),
  trading-post prices and your own transactions, account value, achievement points,
  masteries, luck, playtime, collection-completion percentages — all map cleanly to
  gauges/counters and make dense, compelling time series.
- **What you cannot get from the API:** combat telemetry (DPS, boons, rotations —
  that needs arcdps logs, the [gw2wingman](https://gw2wingman.nevermindcreations.de/)
  domain), per-account WvW kills/deaths (team-level only), and PvP history beyond
  the last 10 games. Know these blind spots up front.

**Verdict:** A high-value personal-infrastructure / learning project. You essentially
re-implement gw2efficiency's tracking inside Grafana, gaining alerting, retention you
control, and panels those sites don't offer. The data is rich enough to be meaningful.

## Documentation

| Document | Contents |
|---|---|
| [`docs/viability.md`](docs/viability.md) | Full viability assessment, what's possible vs impossible, comparison to existing tools |
| [`docs/api-reference.md`](docs/api-reference.md) | Every relevant endpoint enumerated, by family, with fields and signal mapping |
| [`docs/api-empirical-findings.md`](docs/api-empirical-findings.md) | Results of probing every authenticated endpoint with a real key — verified shapes, real values, corrections |
| [`docs/telemetry-design.md`](docs/telemetry-design.md) | Metric/log/trace catalog, gauge-vs-counter rules, cardinality guidance |
| [`docs/collector-design.md`](docs/collector-design.md) | API mechanics: auth, rate limits, pagination, schema versioning, polling-interval table, self-observability |
| [`docs/dashboards.md`](docs/dashboards.md) | Prioritized Grafana dashboard ideas mapped to endpoints and to existing community tools |
| [`docs/architecture-research.md`](docs/architecture-research.md) | Pre-implementation research: OTel standards, architecture model, language, patterns, Grafana-stack integration, proposed project structure |

## Required API key scopes

Create a key at [account.arena.net/applications](https://account.arena.net/applications)
with these permissions (the project assumes all of them):

`account`, `tradingpost`, `characters`, `wvw`, `pvp`, `progression`, `wallet`,
`guilds`, `builds`, `inventories`, `unlocks`

> Note: `account` is mandatory and always present. There is **no `wvw` scope** in the
> permissions UI in the classic sense — the account's WvW *rank* is gated behind
> `progression`; public WvW match data needs no key at all. Guild sub-resources require
> a key belonging to the **guild leader**. See [`docs/collector-design.md`](docs/collector-design.md).

## Quick start (local dev)

Requires Go 1.26+ and Docker.

```sh
cp .env.example .env        # add your GW2_API_KEY
make build                  # build the binary
make test && make vet       # checks

# Run the full local stack (Grafana LGTM) + collector:
GW2_API_KEY=<key> make dev
# Grafana → http://localhost:3000 (metrics arrive within ~15s)

# …or run the collector alone against an existing OTLP endpoint:
GW2_API_KEY=<key> OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 ./gw2-collector
```

Switching dev → Alloy → Grafana Cloud is config-only: point
`OTEL_EXPORTER_OTLP_ENDPOINT` (and `OTEL_EXPORTER_OTLP_HEADERS` for auth) at the target.

## Status

**v1 scaffold (implemented):** a Go daemon that polls `account`, `account/wallet`, and
`characters?ids=all` on independent intervals, caches snapshots in memory, and exports
them as OpenTelemetry observable metrics over OTLP. Verified end-to-end against the live
API. See [`docs/architecture-research.md`](docs/architecture-research.md) §7 for the layout.

**Next:** more endpoint families, the snapshot-diff → event/log machinery with `bbolt`
watermarks, reference-data (id→name) enrichment, traces, and as-code dashboards.
