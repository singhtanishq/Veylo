// Package ratelimit provides an in-process token-bucket rate limiter keyed by
// arbitrary strings (e.g. "login:1.2.3.4", "msg:<userID>"). This protects the
// single local API instance; a Redis-backed limiter is the documented upgrade
// path for multi-instance deployments.
package ratelimit

import (
	"os"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter manages named token buckets.
type Limiter struct {
	mu       sync.Mutex
	buckets  map[string]*entry
	maxIdle  time.Duration
	nowClock func() time.Time
	disabled bool
}

type entry struct {
	lim     *rate.Limiter
	lastUse time.Time
}

// New creates a limiter. Buckets idle for longer than maxIdle are pruned.
func New(maxIdle time.Duration) *Limiter {
	l := &Limiter{
		buckets:  make(map[string]*entry),
		maxIdle:  maxIdle,
		nowClock: time.Now,
	}
	go l.pruneLoop()
	return l
}

// NewDisabled creates a limiter that allows everything (used by the test
// environment, which exercises limits via its own dedicated unit test).
func NewDisabled() *Limiter {
	return &Limiter{buckets: make(map[string]*entry), disabled: true, nowClock: time.Now}
}

// Allow consumes one token for key at events/sec with burst size.
func (l *Limiter) Allow(key string, perSec float64, burst int) bool {
	if l.disabled {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.buckets[key]
	if !ok {
		e = &entry{lim: rate.NewLimiter(rate.Limit(perSec), burst)}
		l.buckets[key] = e
	}
	e.lastUse = l.nowClock()
	return e.lim.Allow()
}

func (l *Limiter) pruneLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().Add(-l.maxIdle)
		l.mu.Lock()
		for k, e := range l.buckets {
			if e.lastUse.Before(cutoff) {
				delete(l.buckets, k)
			}
		}
		l.mu.Unlock()
	}
}

// Common policy presets (events per second, burst).
type Policy struct {
	PerSec float64
	Burst  int
}

// perHourFromEnv builds an hourly policy from an env override so deployments
// (and E2E runs) can tune limits without code changes.
func perHourFromEnv(envKey string, fallback int) Policy {
	n := fallback
	if v := os.Getenv(envKey); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}
	return Policy{PerSec: float64(n) / 3600, Burst: n}
}

// perWindowFromEnv builds a policy of `n` events per `window` from env.
func perWindowFromEnv(envKey string, fallback int, window time.Duration) Policy {
	n := fallback
	if v := os.Getenv(envKey); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}
	return Policy{PerSec: float64(n) / window.Seconds(), Burst: n}
}

var (
	PolicyRegister  = perHourFromEnv("RATE_REGISTER_PER_HOUR", 20)               // per IP per hour
	PolicyLogin     = perWindowFromEnv("RATE_LOGIN_PER_5MIN", 30, 5*time.Minute) // per IP / per identifier
	PolicyAuthed    = Policy{PerSec: 100.0 / 60, Burst: 100}                     // 100 per min
	PolicySearch    = Policy{PerSec: 60.0 / 60, Burst: 30}                       // 60 per min
	PolicyVerify    = Policy{PerSec: 3.0 / 600, Burst: 3}                        // 3 per 10 min
	PolicyReset     = Policy{PerSec: 5.0 / 3600, Burst: 5}                       // 5 per hour
	PolicyMessageWS = Policy{PerSec: 60.0 / 60, Burst: 60}                       // 60 msg/min per user
	PolicyTyping    = Policy{PerSec: 1.0 / 2.5, Burst: 3}                        // typing ~2.5s throttle
	PolicyAttach    = Policy{PerSec: 20.0 / 60, Burst: 10}                       // 20 uploads/min
)
