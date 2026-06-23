package gw2

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c, err := NewClient(Options{
		BaseURL:         baseURL,
		APIKey:          "test-key",
		SchemaVersion:   "latest",
		RateLimitPerSec: 1000,
		RateBurst:       1000,
		MaxRetries:      3,
		RequestTimeout:  5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestAccountDecodesAndSendsAuthAndSchema(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		if got := r.URL.Query().Get("v"); got != "latest" {
			t.Errorf("v = %q, want latest", got)
		}
		if r.URL.Path != "/account" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"name":"Test.1234","age":42,"fractal_level":7}`))
	}))
	defer srv.Close()

	a, err := testClient(t, srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if a.Name != "Test.1234" || a.Age != 42 || a.FractalLevel != 7 {
		t.Errorf("got %+v", a)
	}
}

func TestRetriesOn429ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"name":"Retry.1"}`))
	}))
	defer srv.Close()

	a, err := testClient(t, srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if a.Name != "Retry.1" {
		t.Errorf("name = %q", a.Name)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("calls = %d, want 2 (one retry)", n)
	}
}

func TestDoesNotRetryOn404(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `{"text":"no such id"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := testClient(t, srv.URL).Account(context.Background()); err == nil {
		t.Fatal("expected error on 404")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("calls = %d, want 1 (no retry on 404)", n)
	}
}

func TestCountIDsAndCharactersBulkParam(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/characters" {
			if got := r.URL.Query().Get("ids"); got != "all" {
				t.Errorf("ids = %q, want all", got)
			}
			_, _ = w.Write([]byte(`[{"name":"A","level":80},{"name":"B","level":42}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	chars, err := testClient(t, srv.URL).Characters(context.Background())
	if err != nil {
		t.Fatalf("Characters: %v", err)
	}
	if len(chars) != 2 || chars[0].Level != 80 {
		t.Errorf("got %+v", chars)
	}
}
