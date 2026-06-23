// Package state is a small bbolt-backed persistent store for the event machinery:
// previous values for snapshot diffing and a seen-set for exactly-once-ish event
// emission. It survives restarts so events are neither lost nor re-emitted.
// See docs/architecture-research.md §5.5.
package state

import (
	"encoding/binary"
	"time"

	"go.etcd.io/bbolt"
)

var (
	bucketInts = []byte("ints") // key -> previous int64 value (for delta diffing)
	bucketSeen = []byte("seen") // key -> presence (for dedupe)
)

// Store wraps a bbolt database.
type Store struct {
	db *bbolt.DB
}

// Open opens (or creates) the database at path and ensures the buckets exist.
func Open(path string) (*Store, error) {
	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, err
	}
	if err := db.Update(func(tx *bbolt.Tx) error {
		if _, e := tx.CreateBucketIfNotExists(bucketInts); e != nil {
			return e
		}
		_, e := tx.CreateBucketIfNotExists(bucketSeen)
		return e
	}); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// PrevInt returns the previously stored value for key, and whether it existed.
func (s *Store) PrevInt(key string) (int64, bool) {
	var v int64
	var found bool
	_ = s.db.View(func(tx *bbolt.Tx) error {
		if b := tx.Bucket(bucketInts).Get([]byte(key)); b != nil {
			v = int64(binary.BigEndian.Uint64(b))
			found = true
		}
		return nil
	})
	return v, found
}

// SetInt stores val for key.
func (s *Store) SetInt(key string, val int64) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, uint64(val))
		return tx.Bucket(bucketInts).Put([]byte(key), buf)
	})
}

// HasSeen reports whether key has been recorded.
func (s *Store) HasSeen(key string) bool {
	var seen bool
	_ = s.db.View(func(tx *bbolt.Tx) error {
		seen = tx.Bucket(bucketSeen).Get([]byte(key)) != nil
		return nil
	})
	return seen
}

// MarkSeen records key. Call this only AFTER the corresponding event has been
// emitted, so a crash in between re-emits (at-least-once) rather than drops.
func (s *Store) MarkSeen(key string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketSeen).Put([]byte(key), []byte{1})
	})
}
