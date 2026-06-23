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
)

// Store is a concurrency-safe cache of the latest snapshot per family.
type Store struct {
	mu          sync.RWMutex
	account     *gw2.Account
	wallet      []gw2.CurrencyAmount
	characters  []gw2.Character
	lastSuccess map[string]time.Time
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
