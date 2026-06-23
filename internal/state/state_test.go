package state

import (
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestPrevIntRoundTrip(t *testing.T) {
	s := openTemp(t)
	if _, ok := s.PrevInt("k"); ok {
		t.Fatal("expected not-found for unset key")
	}
	if err := s.SetInt("k", 42); err != nil {
		t.Fatalf("SetInt: %v", err)
	}
	v, ok := s.PrevInt("k")
	if !ok || v != 42 {
		t.Errorf("PrevInt = (%d, %v), want (42, true)", v, ok)
	}
}

func TestSeenSet(t *testing.T) {
	s := openTemp(t)
	if s.HasSeen("x") {
		t.Fatal("unexpected HasSeen before MarkSeen")
	}
	if err := s.MarkSeen("x"); err != nil {
		t.Fatalf("MarkSeen: %v", err)
	}
	if !s.HasSeen("x") {
		t.Error("expected HasSeen after MarkSeen")
	}
}

func TestPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")

	s1, err := Open(path)
	if err != nil {
		t.Fatalf("Open 1: %v", err)
	}
	_ = s1.SetInt("level", 7)
	_ = s1.MarkSeen("tx:1")
	_ = s1.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("Open 2: %v", err)
	}
	defer s2.Close()

	if v, ok := s2.PrevInt("level"); !ok || v != 7 {
		t.Errorf("after reopen PrevInt = (%d, %v), want (7, true)", v, ok)
	}
	if !s2.HasSeen("tx:1") {
		t.Error("seen-set did not survive reopen")
	}
}
