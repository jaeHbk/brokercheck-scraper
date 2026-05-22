package main

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func nearly(a, b, tol float64) bool { return math.Abs(a-b) < tol }

func TestLimiter_StartsAtCeiling(t *testing.T) {
	l := newLimiter(2.0, 0.25)
	if !nearly(l.currentRate(), 2.0, 1e-6) {
		t.Fatalf("expected initial rate=2.0, got %v", l.currentRate())
	}
}

func TestLimiter_OnErrorHalves(t *testing.T) {
	l := newLimiter(2.0, 0.25)
	l.onError()
	if !nearly(l.currentRate(), 1.0, 1e-6) {
		t.Fatalf("after 1 error expected 1.0, got %v", l.currentRate())
	}
	l.onError()
	if !nearly(l.currentRate(), 0.5, 1e-6) {
		t.Fatalf("after 2 errors expected 0.5, got %v", l.currentRate())
	}
}

func TestLimiter_OnErrorFloors(t *testing.T) {
	l := newLimiter(2.0, 0.25)
	for i := 0; i < 10; i++ {
		l.onError()
	}
	if !nearly(l.currentRate(), 0.25, 1e-6) {
		t.Fatalf("expected rate floored at 0.25, got %v", l.currentRate())
	}
}

func TestLimiter_OnSuccessBefore5MinNoChange(t *testing.T) {
	l := newLimiter(2.0, 0.25)
	l.onError() // drop to 1.0
	// Mark the last-adjustment time as 1 minute ago
	l.cleanSince = time.Now().Add(-1 * time.Minute)
	l.onSuccess()
	if !nearly(l.currentRate(), 1.0, 1e-6) {
		t.Fatalf("expected rate unchanged, got %v", l.currentRate())
	}
}

func TestLimiter_OnSuccessAfter5MinIncreases(t *testing.T) {
	l := newLimiter(2.0, 0.25)
	l.onError() // drop to 1.0
	l.cleanSince = time.Now().Add(-6 * time.Minute)
	l.onSuccess()
	if !nearly(l.currentRate(), 1.25, 1e-6) {
		t.Fatalf("expected rate=1.25 after recovery, got %v", l.currentRate())
	}
}

func TestLimiter_OnSuccessCapsAtCeiling(t *testing.T) {
	l := newLimiter(2.0, 0.25)
	// Already at ceiling; should not exceed it
	l.cleanSince = time.Now().Add(-6 * time.Minute)
	l.onSuccess()
	if !nearly(l.currentRate(), 2.0, 1e-6) {
		t.Fatalf("expected rate capped at 2.0, got %v", l.currentRate())
	}
}

func TestClient_DoSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newClient(newLimiter(100, 1), 3)
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("body mismatch: %s", body)
	}
}

func TestClient_RetriesOn429(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	// Override the breaker pause to keep the test fast.
	c := newClient(newLimiter(100, 1), 3)
	c.breakerPause = 10 * time.Millisecond
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("body mismatch: %s", body)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 calls, got %d", calls.Load())
	}
}

func TestClient_FailsAfterRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := newClient(newLimiter(100, 1), 2)
	c.breakerPause = 1 * time.Millisecond
	c.backoffBase = 1 * time.Millisecond
	_, err := c.Get(context.Background(), srv.URL)
	if err == nil {
		t.Fatalf("expected error after exhausted retries")
	}
}

func TestClient_4xxOtherFailsImmediately(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(404)
		w.Write([]byte("not found"))
	}))
	defer srv.Close()

	c := newClient(newLimiter(100, 1), 5)
	_, err := c.Get(context.Background(), srv.URL)
	if err == nil {
		t.Fatalf("expected error on 404")
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 call (no retry on 4xx), got %d", calls.Load())
	}
}
