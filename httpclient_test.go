package main

import (
	"math"
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
