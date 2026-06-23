// Package gw2 is a rate-limited, schema-pinned client for the Guild Wars 2 API v2.
//
// All requests share one process-wide token-bucket limiter (the API limit is
// per-IP). The client retries 429/5xx with jittered backoff and records its own
// request metrics. See docs/collector-design.md.
package gw2

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/time/rate"
)

// Client talks to the GW2 API v2.
type Client struct {
	http       *http.Client
	baseURL    string
	apiKey     string
	schema     string
	maxRetries int

	reqDuration metric.Float64Histogram
	reqCount    metric.Int64Counter
	tracer      trace.Tracer
}

// Options configures a Client.
type Options struct {
	BaseURL         string
	APIKey          string
	SchemaVersion   string
	RateLimitPerSec float64
	RateBurst       int
	MaxRetries      int
	RequestTimeout  time.Duration
}

// rateLimitedTransport blocks each request on the shared limiter before sending,
// so that retried attempts also consume tokens and cannot burst past the limit.
type rateLimitedTransport struct {
	base    http.RoundTripper
	limiter *rate.Limiter
}

func (t *rateLimitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.limiter.Wait(req.Context()); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(req)
}

// NewClient builds a Client with a shared rate limiter and self-observability
// instruments created from the global meter provider.
func NewClient(opts Options) (*Client, error) {
	limiter := rate.NewLimiter(rate.Limit(opts.RateLimitPerSec), opts.RateBurst)
	httpClient := &http.Client{
		Timeout:   opts.RequestTimeout,
		Transport: &rateLimitedTransport{base: http.DefaultTransport, limiter: limiter},
	}

	meter := otel.Meter("github.com/guicaulada/gw2-otel-collector/internal/gw2")
	reqDuration, err := meter.Float64Histogram(
		"gw2.api.request.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of GW2 API requests"),
	)
	if err != nil {
		return nil, fmt.Errorf("create request duration histogram: %w", err)
	}
	reqCount, err := meter.Int64Counter(
		"gw2.api.requests",
		metric.WithUnit("{request}"),
		metric.WithDescription("Count of GW2 API requests by endpoint and status"),
	)
	if err != nil {
		return nil, fmt.Errorf("create request counter: %w", err)
	}

	return &Client{
		http:        httpClient,
		baseURL:     opts.BaseURL,
		apiKey:      opts.APIKey,
		schema:      opts.SchemaVersion,
		maxRetries:  opts.MaxRetries,
		reqDuration: reqDuration,
		reqCount:    reqCount,
		tracer:      otel.Tracer("github.com/guicaulada/gw2-otel-collector/internal/gw2"),
	}, nil
}

// Account fetches /v2/account.
func (c *Client) Account(ctx context.Context) (*Account, error) {
	var a Account
	if err := c.get(ctx, "account", "account", nil, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// Wallet fetches /v2/account/wallet.
func (c *Client) Wallet(ctx context.Context) ([]CurrencyAmount, error) {
	var w []CurrencyAmount
	if err := c.get(ctx, "account/wallet", "account/wallet", nil, &w); err != nil {
		return nil, err
	}
	return w, nil
}

// Characters fetches all character overviews in one request (/v2/characters?ids=all).
func (c *Client) Characters(ctx context.Context) ([]Character, error) {
	var ch []Character
	params := url.Values{"ids": {"all"}}
	if err := c.get(ctx, "characters", "characters", params, &ch); err != nil {
		return nil, err
	}
	return ch, nil
}

// ExchangeCoins prices gems in coins: how many coins one gem costs when buying
// gems with gold (/v2/commerce/exchange/coins?quantity=<copper>). Public.
func (c *Client) ExchangeCoins(ctx context.Context, copper int64) (*Exchange, error) {
	var e Exchange
	params := url.Values{"quantity": {strconv.FormatInt(copper, 10)}}
	if err := c.get(ctx, "commerce/exchange/coins", "commerce/exchange/coins", params, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// ExchangeGems prices coins per gem when selling gems for gold
// (/v2/commerce/exchange/gems?quantity=<gems>). Public.
func (c *Client) ExchangeGems(ctx context.Context, gems int64) (*Exchange, error) {
	var e Exchange
	params := url.Values{"quantity": {strconv.FormatInt(gems, 10)}}
	if err := c.get(ctx, "commerce/exchange/gems", "commerce/exchange/gems", params, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// Delivery fetches the trading-post pickup box (/v2/commerce/delivery).
// Requires the tradingpost scope.
func (c *Client) Delivery(ctx context.Context) (*Delivery, error) {
	var d Delivery
	if err := c.get(ctx, "commerce/delivery", "commerce/delivery", nil, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// WizardsVault fetches /v2/account/wizardsvault/{period} where period is
// "daily", "weekly", or "special".
func (c *Client) WizardsVault(ctx context.Context, period string) (*WizardsVault, error) {
	var wv WizardsVault
	if err := c.get(ctx, "account/wizardsvault/"+period, "account/wizardsvault", nil, &wv); err != nil {
		return nil, err
	}
	return &wv, nil
}

// AccountAchievements fetches /v2/account/achievements.
func (c *Client) AccountAchievements(ctx context.Context) ([]AccountAchievement, error) {
	var out []AccountAchievement
	if err := c.get(ctx, "account/achievements", "account/achievements", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AchievementsByIDs fetches achievement definitions for the given ids, batched
// 200 at a time (no ids=all support on this endpoint). Public, static.
func (c *Client) AchievementsByIDs(ctx context.Context, ids []int) ([]AchievementDef, error) {
	var out []AchievementDef
	const batch = 200
	for i := 0; i < len(ids); i += batch {
		end := i + batch
		if end > len(ids) {
			end = len(ids)
		}
		var defs []AchievementDef
		params := url.Values{"ids": {joinInts(ids[i:end])}}
		if err := c.get(ctx, "achievements", "achievements", params, &defs); err != nil {
			return nil, err
		}
		out = append(out, defs...)
	}
	return out, nil
}

// AccountStringList fetches an account endpoint returning a JSON array of string
// ids (worldbosses/dungeons/raids/mapchests/dailycrafting — completed since reset).
func (c *Client) AccountStringList(ctx context.Context, path, label string) ([]string, error) {
	var out []string
	if err := c.get(ctx, path, label, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AccountProgression fetches /v2/account/progression (luck + fractal augmentations).
func (c *Client) AccountProgression(ctx context.Context) ([]ProgressionEntry, error) {
	var out []ProgressionEntry
	if err := c.get(ctx, "account/progression", "account/progression", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AccountLegendaryArmory fetches /v2/account/legendaryarmory (owned copies).
func (c *Client) AccountLegendaryArmory(ctx context.Context) ([]LegendaryArmoryEntry, error) {
	var out []LegendaryArmoryEntry
	if err := c.get(ctx, "account/legendaryarmory", "account/legendaryarmory", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// LegendaryArmory fetches /v2/legendaryarmory (max copies per item). Public, static.
func (c *Client) LegendaryArmory(ctx context.Context) ([]LegendaryArmoryDef, error) {
	var out []LegendaryArmoryDef
	if err := c.get(ctx, "legendaryarmory", "legendaryarmory", url.Values{"ids": {"all"}}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Masteries fetches /v2/account/masteries (trained mastery tracks).
func (c *Client) Masteries(ctx context.Context) ([]Mastery, error) {
	var m []Mastery
	if err := c.get(ctx, "account/masteries", "account/masteries", nil, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// MasteryPoints fetches /v2/account/mastery/points (earned/spent per region).
func (c *Client) MasteryPoints(ctx context.Context) (*MasteryPoints, error) {
	var mp MasteryPoints
	if err := c.get(ctx, "account/mastery/points", "account/mastery/points", nil, &mp); err != nil {
		return nil, err
	}
	return &mp, nil
}

// Luck fetches /v2/account/luck and returns total essence of luck consumed.
func (c *Client) Luck(ctx context.Context) (int64, error) {
	var l []LuckAmount
	if err := c.get(ctx, "account/luck", "account/luck", nil, &l); err != nil {
		return 0, err
	}
	var total int64
	for _, e := range l {
		total += e.Value
	}
	return total, nil
}

// Bank fetches /v2/account/bank (positional; nil entries are empty slots).
func (c *Client) Bank(ctx context.Context) ([]*Slot, error) {
	var b []*Slot
	if err := c.get(ctx, "account/bank", "account/bank", nil, &b); err != nil {
		return nil, err
	}
	return b, nil
}

// SharedInventory fetches /v2/account/inventory (shared inventory slots).
func (c *Client) SharedInventory(ctx context.Context) ([]*Slot, error) {
	var s []*Slot
	if err := c.get(ctx, "account/inventory", "account/inventory", nil, &s); err != nil {
		return nil, err
	}
	return s, nil
}

// Materials fetches /v2/account/materials (material storage by category).
func (c *Client) Materials(ctx context.Context) ([]MaterialAmount, error) {
	var m []MaterialAmount
	if err := c.get(ctx, "account/materials", "account/materials", nil, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// Guild fetches /v2/guild/:id (detailed fields require the guilds scope and
// appropriate guild permission).
func (c *Client) Guild(ctx context.Context, id string) (*Guild, error) {
	var g Guild
	if err := c.get(ctx, "guild/"+id, "guild/:id", nil, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

// GuildUpgradesCompleted fetches /v2/guild/:id/upgrades (leader only) and returns
// the count of completed upgrades.
func (c *Client) GuildUpgradesCompleted(ctx context.Context, id string) (int, error) {
	return c.CountIDs(ctx, "guild/"+id+"/upgrades", "guild/:id/upgrades")
}

// TransactionHistory fetches a page of completed transactions for the given side
// ("buys" or "sells"): /v2/commerce/transactions/history/{side}. Requires the
// tradingpost scope.
func (c *Client) TransactionHistory(ctx context.Context, side string) ([]Transaction, error) {
	var txs []Transaction
	path := "commerce/transactions/history/" + side
	params := url.Values{"page": {"0"}, "page_size": {"200"}}
	if err := c.get(ctx, path, "commerce/transactions/history", params, &txs); err != nil {
		return nil, err
	}
	return txs, nil
}

// GuildTreasury fetches /v2/guild/:id/treasury (leader only).
func (c *Client) GuildTreasury(ctx context.Context, id string) ([]GuildTreasuryEntry, error) {
	var out []GuildTreasuryEntry
	if err := c.get(ctx, "guild/"+id+"/treasury", "guild/:id/treasury", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GuildStash fetches /v2/guild/:id/stash (leader only).
func (c *Client) GuildStash(ctx context.Context, id string) ([]GuildStashSection, error) {
	var out []GuildStashSection
	if err := c.get(ctx, "guild/"+id+"/stash", "guild/:id/stash", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GuildStorage fetches /v2/guild/:id/storage (leader only).
func (c *Client) GuildStorage(ctx context.Context, id string) ([]GuildStorageEntry, error) {
	var out []GuildStorageEntry
	if err := c.get(ctx, "guild/"+id+"/storage", "guild/:id/storage", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GuildLog fetches the recent /v2/guild/:id/log entries (leader only). The
// caller (event emitter) de-dupes by a persisted watermark.
func (c *Client) GuildLog(ctx context.Context, id string) ([]GuildLogEntry, error) {
	var out []GuildLogEntry
	if err := c.get(ctx, "guild/"+id+"/log", "guild/:id/log", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// WvWMatchByWorld fetches the current WvW matchup for a world
// (/v2/wvw/matches?world=<id>). Public, no auth.
func (c *Client) WvWMatchByWorld(ctx context.Context, world int) (*WvWMatch, error) {
	var m WvWMatch
	params := url.Values{"world": {strconv.Itoa(world)}}
	if err := c.get(ctx, "wvw/matches", "wvw/matches", params, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// TransactionsCurrent fetches open (unfulfilled) orders for the given side
// ("buys" or "sells"): /v2/commerce/transactions/current/{side}. tradingpost scope.
func (c *Client) TransactionsCurrent(ctx context.Context, side string) ([]Transaction, error) {
	var txs []Transaction
	path := "commerce/transactions/current/" + side
	params := url.Values{"page": {"0"}, "page_size": {"200"}}
	if err := c.get(ctx, path, "commerce/transactions/current", params, &txs); err != nil {
		return nil, err
	}
	return txs, nil
}

// PvPStats fetches /v2/pvp/stats.
func (c *Client) PvPStats(ctx context.Context) (*PvPStats, error) {
	var s PvPStats
	if err := c.get(ctx, "pvp/stats", "pvp/stats", nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// PvPStandings fetches /v2/pvp/standings (empty array when no active season).
func (c *Client) PvPStandings(ctx context.Context) ([]PvPStanding, error) {
	var out []PvPStanding
	if err := c.get(ctx, "pvp/standings", "pvp/standings", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AccountIntList fetches an account endpoint returning a JSON array of integer
// ids (account/skins, account/dyes — the unlocked wardrobe sets).
func (c *Client) AccountIntList(ctx context.Context, path string) ([]int, error) {
	var out []int
	if err := c.get(ctx, path, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SkinsByIDs fetches skin definitions for the given ids, batched 200 at a time.
// Public, static reference data (type/rarity never change for a skin id).
func (c *Client) SkinsByIDs(ctx context.Context, ids []int) ([]Skin, error) {
	var out []Skin
	const batch = 200
	for i := 0; i < len(ids); i += batch {
		end := i + batch
		if end > len(ids) {
			end = len(ids)
		}
		var defs []Skin
		params := url.Values{"ids": {joinInts(ids[i:end])}}
		if err := c.get(ctx, "skins", "skins", params, &defs); err != nil {
			return nil, err
		}
		out = append(out, defs...)
	}
	return out, nil
}

// ColorsByIDs fetches color (dye) definitions for the given ids, batched 200 at
// a time. Public, static reference data.
func (c *Client) ColorsByIDs(ctx context.Context, ids []int) ([]Color, error) {
	var out []Color
	const batch = 200
	for i := 0; i < len(ids); i += batch {
		end := i + batch
		if end > len(ids) {
			end = len(ids)
		}
		var defs []Color
		params := url.Values{"ids": {joinInts(ids[i:end])}}
		if err := c.get(ctx, "colors", "colors", params, &defs); err != nil {
			return nil, err
		}
		out = append(out, defs...)
	}
	return out, nil
}

// PvPGames fetches /v2/pvp/games?ids=all — the account's last ~10 matches as
// full objects (the list is small enough to request all at once).
func (c *Client) PvPGames(ctx context.Context) ([]PvPGame, error) {
	var out []PvPGame
	params := url.Values{"ids": {"all"}}
	if err := c.get(ctx, "pvp/games", "pvp/games", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CountIDs fetches an endpoint that returns a JSON array and returns its length.
// Works for account unlock lists (id or object arrays) and reference index lists.
func (c *Client) CountIDs(ctx context.Context, path, label string) (int, error) {
	var arr []json.RawMessage
	if err := c.get(ctx, path, label, nil, &arr); err != nil {
		return 0, err
	}
	return len(arr), nil
}

// Prices fetches aggregated best bid/ask for the given item ids
// (/v2/commerce/prices?ids=). Public. Returns nil for an empty id list.
func (c *Client) Prices(ctx context.Context, ids []int) ([]ItemPrice, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var out []ItemPrice
	params := url.Values{"ids": {joinInts(ids)}}
	if err := c.get(ctx, "commerce/prices", "commerce/prices", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Items fetches item definitions for the given ids (/v2/items?ids=). Public,
// static reference. Returns nil for an empty id list.
func (c *Client) Items(ctx context.Context, ids []int) ([]Item, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var out []Item
	params := url.Values{"ids": {joinInts(ids)}}
	if err := c.get(ctx, "items", "items", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PricesBatched fetches prices for many item ids in batches of 200 (the API
// cap) and returns them keyed by item id. Unpriced/untradable ids are absent.
func (c *Client) PricesBatched(ctx context.Context, ids []int) (map[int]ItemPrice, error) {
	out := make(map[int]ItemPrice, len(ids))
	const batch = 200
	for i := 0; i < len(ids); i += batch {
		end := i + batch
		if end > len(ids) {
			end = len(ids)
		}
		prices, err := c.Prices(ctx, ids[i:end])
		if err != nil {
			return nil, err
		}
		for _, p := range prices {
			out[p.ID] = p
		}
	}
	return out, nil
}

func joinInts(ids []int) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, ",")
}

// QuestsAll fetches all quest definitions (/v2/quests?ids=all). Public, static.
func (c *Client) QuestsAll(ctx context.Context) ([]Quest, error) {
	var out []Quest
	if err := c.get(ctx, "quests", "quests", url.Values{"ids": {"all"}}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// StoriesAll fetches all story definitions (/v2/stories?ids=all). Public, static.
func (c *Client) StoriesAll(ctx context.Context) ([]StoryDef, error) {
	var out []StoryDef
	if err := c.get(ctx, "stories", "stories", url.Values{"ids": {"all"}}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// StorySeasonsAll fetches all story seasons (/v2/stories/seasons?ids=all). Public.
func (c *Client) StorySeasonsAll(ctx context.Context) ([]StorySeason, error) {
	var out []StorySeason
	if err := c.get(ctx, "stories/seasons", "stories/seasons", url.Values{"ids": {"all"}}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CharacterQuests fetches the completed/active story quest ids for a character
// (/v2/characters/:name/quests).
func (c *Client) CharacterQuests(ctx context.Context, name string) ([]int, error) {
	var out []int
	path := "characters/" + url.PathEscape(name) + "/quests"
	if err := c.get(ctx, path, "characters/quests", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Build fetches the current game build number (/v2/build). Public endpoint; used
// as the cache-invalidation signal for static reference data.
func (c *Client) Build(ctx context.Context) (int, error) {
	var b struct {
		ID int `json:"id"`
	}
	if err := c.get(ctx, "build", "build", nil, &b); err != nil {
		return 0, err
	}
	return b.ID, nil
}

// Currencies fetches all currency definitions (/v2/currencies?ids=all). Public,
// static reference data.
func (c *Client) Currencies(ctx context.Context) ([]Currency, error) {
	var out []Currency
	params := url.Values{"ids": {"all"}}
	if err := c.get(ctx, "currencies", "currencies", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// MaterialCategories fetches all material storage category definitions
// (/v2/materials?ids=all). Public, static reference data.
func (c *Client) MaterialCategories(ctx context.Context) ([]Material, error) {
	var out []Material
	params := url.Values{"ids": {"all"}}
	if err := c.get(ctx, "materials", "materials", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// get fetches a path, retrying 429/5xx with jittered backoff, and decodes JSON
// into dest. endpoint is the low-cardinality label used for self-obs metrics.
func (c *Client) get(ctx context.Context, path, endpoint string, params url.Values, dest any) error {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			if err := sleepWithBackoff(ctx, attempt); err != nil {
				return err
			}
		}

		status, body, err := c.do(ctx, path, endpoint, params)
		if err != nil {
			lastErr = err
			continue // network/transport error — retry
		}
		if status == http.StatusTooManyRequests || status >= 500 {
			lastErr = fmt.Errorf("%s: retryable status %d", endpoint, status)
			continue
		}
		if status >= 400 {
			return fmt.Errorf("%s: status %d: %s", endpoint, status, truncate(body, 200))
		}
		if err := json.Unmarshal(body, dest); err != nil {
			return fmt.Errorf("%s: decode response: %w", endpoint, err)
		}
		return nil
	}
	return fmt.Errorf("%s: exhausted retries: %w", endpoint, lastErr)
}

// do performs a single request and records self-observability metrics.
func (c *Client) do(ctx context.Context, path, endpoint string, params url.Values) (int, []byte, error) {
	q := url.Values{}
	for k, v := range params {
		q[k] = v
	}
	q.Set("v", c.schema)
	u := fmt.Sprintf("%s/%s?%s", c.baseURL, path, q.Encode())

	// CLIENT span per HTTP attempt; the sync duration histogram recorded under
	// this span's context carries a trace exemplar.
	ctx, span := c.tracer.Start(ctx, "GET "+endpoint, trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("http.request.method", http.MethodGet),
			attribute.String("gw2.endpoint", endpoint),
		))
	defer span.End()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := c.http.Do(req)
	elapsed := time.Since(start).Seconds()

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		c.reqDuration.Record(ctx, elapsed, metric.WithAttributes(
			attribute.String("gw2.endpoint", endpoint),
			attribute.String("error.type", "transport"),
		))
		c.reqCount.Add(ctx, 1, metric.WithAttributes(
			attribute.String("gw2.endpoint", endpoint),
			attribute.String("error.type", "transport"),
		))
		return 0, nil, err
	}
	defer resp.Body.Close()

	attrs := metric.WithAttributes(
		attribute.String("gw2.endpoint", endpoint),
		attribute.Int("http.response.status_code", resp.StatusCode),
	)
	c.reqDuration.Record(ctx, elapsed, attrs)
	c.reqCount.Add(ctx, 1, attrs)
	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))
	if resp.StatusCode >= 400 {
		span.SetStatus(codes.Error, http.StatusText(resp.StatusCode))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

// sleepWithBackoff waits for a full-jitter exponential backoff, capped at 30s,
// or returns early if the context is cancelled.
func sleepWithBackoff(ctx context.Context, attempt int) error {
	const base = 500 * time.Millisecond
	const cap = 30 * time.Second
	backoff := base * time.Duration(1<<uint(attempt-1))
	if backoff > cap {
		backoff = cap
	}
	jittered := time.Duration(rand.Int64N(int64(backoff)))
	select {
	case <-time.After(jittered):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n])
	}
	return string(b)
}
