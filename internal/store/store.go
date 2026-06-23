// Package store holds the latest snapshot fetched from each endpoint family.
//
// The background poller writes snapshots here on its own schedule; the metric
// observable callbacks read them at collection time. This decouples polling
// from metric export (the "poll-and-cache" model) so the API is never hit
// synchronously during a metrics scrape. See docs/architecture-research.md.
package store

import (
	"sync"
	"time"

	"github.com/guicaulada/gw2-otel-collector/internal/gw2"
	"github.com/guicaulada/gw2-otel-collector/internal/value"
)

// Commerce is the derived commerce snapshot the collector tracks as gauges.
type Commerce struct {
	CoinsPerGemBuy  int64 // coins to buy one gem (/exchange/coins)
	CoinsPerGemSell int64 // coins received per gem sold (/exchange/gems)
	DeliveryCoins   int64 // copper awaiting pickup
	DeliveryItems   int64 // distinct item stacks awaiting pickup
}

// MasteryRegionPoints is earned/spent mastery points for one region.
type MasteryRegionPoints struct {
	Earned int64
	Spent  int64
}

// Progression is the derived progression snapshot.
type Progression struct {
	Luck               int64
	MasteriesCount     int
	PointsByRegion     map[string]MasteryRegionPoints
	FractalAugments    map[string]int64 // progression id -> value (non-luck)
	LegendaryOwned     int              // distinct legendaries owned
	LegendaryCopies    int64            // total copies owned
	LegendaryAvailable int              // distinct legendaries in the armory (denominator)
}

// Achievements is the derived achievement summary.
type Achievements struct {
	TotalAP int64
	Done    int
	Total   int
}

// Storage is the derived bank/material/shared-inventory snapshot.
type Storage struct {
	BankUsed, BankCapacity     int64
	SharedUsed, SharedCapacity int64
	MaterialsByCategory        map[int]int64
}

// Store is a concurrency-safe cache of the latest snapshot per family.
type Store struct {
	mu             sync.RWMutex
	account        *gw2.Account
	wallet         []gw2.CurrencyAmount
	characters     []gw2.Character
	commerce       *Commerce
	progression    *Progression
	storage        *Storage
	unlocks        map[string]int
	guilds         []GuildInfo
	pvp            *gw2.PvPStats
	pvpStandings   []gw2.PvPStanding
	prices         []gw2.ItemPrice
	wizardsvault   []WizardsVaultPeriod
	storyCompleted []int
	accountValue   *value.Account
	achievements   *Achievements
	resets         map[string]int // reset kind -> count completed since reset
	wvw            *WvW
	lastSuccess    map[string]time.Time
}

// WvW is the derived WvW matchup snapshot, keyed by team color.
type WvW struct {
	MatchID        string
	HomeColor      string                    // our world's team color in this match
	Score          map[string]int64          // color -> war score
	VictoryPoints  map[string]int64          // color -> victory points
	Kills          map[string]int64          // color -> kills
	Deaths         map[string]int64          // color -> deaths
	PPT            map[string]int64          // color -> points-per-tick (derived)
	ObjectivesHeld map[string]map[string]int // color -> objective type -> count
}

// WizardsVaultPeriod is the derived Wizard's Vault snapshot for one period.
type WizardsVaultPeriod struct {
	Period           string
	HasMeta          bool
	MetaCurrent      int64
	MetaComplete     int64
	Objectives       int
	Completed        int
	UnclaimedAcclaim int64
}

// GuildInfo bundles a guild's detail with derived internals. Fields default to
// zero / -1 when the key does not lead the guild (no access to those endpoints).
type GuildInfo struct {
	Guild             gw2.Guild
	UpgradesCompleted int   // -1 when unknown
	TreasuryItems     int   // distinct items in the treasury
	StashCoins        int64 // total copper across stash sections
	StashSlotsUsed    int64
	StashSlotsSize    int64
	StorageItems      int // distinct guild-storage consumables
}

// New returns an empty Store.
func New() *Store {
	return &Store{lastSuccess: make(map[string]time.Time)}
}

// SetAccount stores the latest account snapshot and marks the family successful.
func (s *Store) SetAccount(a *gw2.Account, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.account = a
	s.lastSuccess["account"] = at
}

// Account returns the latest account snapshot, or nil if none has been fetched.
func (s *Store) Account() *gw2.Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.account
}

// SetWallet stores the latest wallet snapshot.
func (s *Store) SetWallet(w []gw2.CurrencyAmount, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wallet = w
	s.lastSuccess["wallet"] = at
}

// Wallet returns the latest wallet snapshot.
func (s *Store) Wallet() []gw2.CurrencyAmount {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.wallet
}

// SetCharacters stores the latest character snapshot.
func (s *Store) SetCharacters(ch []gw2.Character, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.characters = ch
	s.lastSuccess["characters"] = at
}

// Characters returns the latest character snapshot.
func (s *Store) Characters() []gw2.Character {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.characters
}

// SetCommerce stores the latest commerce snapshot.
func (s *Store) SetCommerce(c *Commerce, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commerce = c
	s.lastSuccess["commerce"] = at
}

// Commerce returns the latest commerce snapshot.
func (s *Store) Commerce() *Commerce {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.commerce
}

// SetProgression stores the latest progression snapshot.
func (s *Store) SetProgression(p *Progression, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progression = p
	s.lastSuccess["progression"] = at
}

// Progression returns the latest progression snapshot.
func (s *Store) Progression() *Progression {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.progression
}

// SetStorage stores the latest storage snapshot.
func (s *Store) SetStorage(st *Storage, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storage = st
	s.lastSuccess["storage"] = at
}

// Storage returns the latest storage snapshot.
func (s *Store) Storage() *Storage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.storage
}

// SetUnlocks stores the latest per-collection unlocked counts.
func (s *Store) SetUnlocks(u map[string]int, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unlocks = u
	s.lastSuccess["unlocks"] = at
}

// Unlocks returns the latest per-collection unlocked counts.
func (s *Store) Unlocks() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.unlocks
}

// SetGuilds stores the latest guild snapshots.
func (s *Store) SetGuilds(g []GuildInfo, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.guilds = g
	s.lastSuccess["guild"] = at
}

// Guilds returns the latest guild snapshots.
func (s *Store) Guilds() []GuildInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.guilds
}

// SetPvP stores the latest PvP stats.
func (s *Store) SetPvP(p *gw2.PvPStats, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pvp = p
	s.lastSuccess["pvp"] = at
}

// PvP returns the latest PvP stats.
func (s *Store) PvP() *gw2.PvPStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pvp
}

// SetPvPStandings stores the latest PvP season standings.
func (s *Store) SetPvPStandings(st []gw2.PvPStanding, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pvpStandings = st
}

// PvPStandings returns the latest PvP season standings (empty if no active season).
func (s *Store) PvPStandings() []gw2.PvPStanding {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pvpStandings
}

// SetPrices stores the latest tracked-item prices.
func (s *Store) SetPrices(p []gw2.ItemPrice, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prices = p
	s.lastSuccess["prices"] = at
}

// Prices returns the latest tracked-item prices.
func (s *Store) Prices() []gw2.ItemPrice {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.prices
}

// SetWizardsVault stores the latest Wizard's Vault snapshot.
func (s *Store) SetWizardsVault(wv []WizardsVaultPeriod, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wizardsvault = wv
	s.lastSuccess["wizardsvault"] = at
}

// WizardsVault returns the latest Wizard's Vault snapshot.
func (s *Store) WizardsVault() []WizardsVaultPeriod {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.wizardsvault
}

// SetStoryCompleted stores the union of completed story-quest ids across characters.
func (s *Store) SetStoryCompleted(ids []int, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storyCompleted = ids
	s.lastSuccess["story"] = at
}

// StoryCompleted returns the union of completed story-quest ids.
func (s *Store) StoryCompleted() []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.storyCompleted
}

// SetAccountValue stores the latest computed account value breakdown.
func (s *Store) SetAccountValue(v *value.Account, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accountValue = v
	s.lastSuccess["value"] = at
}

// AccountValue returns the latest computed account value breakdown.
func (s *Store) AccountValue() *value.Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.accountValue
}

// SetAchievements stores the latest achievement summary.
func (s *Store) SetAchievements(a *Achievements, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.achievements = a
	s.lastSuccess["achievements"] = at
}

// Achievements returns the latest achievement summary.
func (s *Store) Achievements() *Achievements {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.achievements
}

// SetResets stores the latest reset-cycle completion counts by kind.
func (s *Store) SetResets(r map[string]int, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resets = r
	s.lastSuccess["resets"] = at
}

// Resets returns the latest reset-cycle completion counts by kind.
func (s *Store) Resets() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.resets
}

// SetWvW stores the latest WvW matchup snapshot.
func (s *Store) SetWvW(w *WvW, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wvw = w
	s.lastSuccess["wvw"] = at
}

// WvW returns the latest WvW matchup snapshot.
func (s *Store) WvW() *WvW {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.wvw
}

// LastSuccess returns the time of the last successful poll for each family.
func (s *Store) LastSuccess() map[string]time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]time.Time, len(s.lastSuccess))
	for k, v := range s.lastSuccess {
		out[k] = v
	}
	return out
}
