package ratelimit

import (
	"testing"
	"time"
)

func TestLimiterAllowsAndLimits(t *testing.T) {
	l := New(time.Minute)
	// Burst of 2: first two allowed, third rejected.
	if !l.Allow("k", 0.001, 2) {
		t.Fatal("first request should be allowed")
	}
	if !l.Allow("k", 0.001, 2) {
		t.Fatal("second request should be allowed")
	}
	if l.Allow("k", 0.001, 2) {
		t.Fatal("third request should be limited")
	}
	// Distinct keys are independent.
	if !l.Allow("other", 0.001, 2) {
		t.Fatal("different key should be allowed")
	}
}

func TestDisabledLimiterAllowsAll(t *testing.T) {
	l := NewDisabled()
	for i := 0; i < 100; i++ {
		if !l.Allow("k", 0.0001, 1) {
			t.Fatalf("disabled limiter rejected request %d", i)
		}
	}
}
