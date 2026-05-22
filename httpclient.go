package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter is a process-wide token-bucket limiter with adaptive
// circuit-breaker behavior: rate halves on error responses and
// recovers gradually after sustained success.
type Limiter struct {
	mu         sync.Mutex
	rl         *rate.Limiter
	ceiling    float64
	floor      float64
	current    float64
	cleanSince time.Time // start of the current clean streak (or last adjustment)
}

func newLimiter(ratePerSec, floor float64) *Limiter {
	return &Limiter{
		rl:         rate.NewLimiter(rate.Limit(ratePerSec), 1),
		ceiling:    ratePerSec,
		floor:      floor,
		current:    ratePerSec,
		cleanSince: time.Now(),
	}
}

// Wait blocks until a token is available or ctx is canceled.
func (l *Limiter) Wait(ctx context.Context) error {
	return l.rl.Wait(ctx)
}

// onError halves the current rate (floored at l.floor) and resets
// the clean-streak timer. Returns the recommended pause duration.
func (l *Limiter) onError() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	old := l.current
	l.current = math.Max(l.floor, l.current/2)
	l.rl.SetLimit(rate.Limit(l.current))
	l.cleanSince = time.Now()
	if old != l.current {
		log.Printf("limiter: error response; rate %.2f -> %.2f req/s, pausing 60s", old, l.current)
	}
	return 60 * time.Second
}

// onSuccess increases the current rate toward the ceiling if the
// most recent adjustment was at least 5 minutes ago.
func (l *Limiter) onSuccess() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.current >= l.ceiling {
		return
	}
	if time.Since(l.cleanSince) >= 5*time.Minute {
		old := l.current
		l.current = math.Min(l.ceiling, l.current*1.25)
		l.rl.SetLimit(rate.Limit(l.current))
		l.cleanSince = time.Now()
		log.Printf("limiter: clean streak; rate %.2f -> %.2f req/s", old, l.current)
	}
}

// currentRate is exposed for testing.
func (l *Limiter) currentRate() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.current
}

var defaultUserAgents = []string{
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:125.0) Gecko/20100101 Firefox/125.0",
}

// Client is the shared HTTP wrapper used by both stages. All requests
// go through the Limiter and benefit from the adaptive breaker.
type Client struct {
	httpClient   *http.Client
	limiter      *Limiter
	retries      int
	rng          *rand.Rand
	rngMu        sync.Mutex
	breakerPause time.Duration // overridable in tests
	backoffBase  time.Duration // overridable in tests
}

func newClient(limiter *Limiter, retries int) *Client {
	tr := &http.Transport{
		MaxIdleConns:        16,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   true,
	}
	return &Client{
		httpClient:   &http.Client{Timeout: 30 * time.Second, Transport: tr},
		limiter:      limiter,
		retries:      retries,
		rng:          rand.New(rand.NewSource(time.Now().UnixNano())),
		breakerPause: 60 * time.Second,
		backoffBase:  5 * time.Second,
	}
}

// Get performs a rate-limited GET with retry/backoff. Returns the
// response body on 200, an error otherwise.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= c.retries; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, err
		}
		c.applyHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			log.Printf("  attempt %d: transport error: %v", attempt, err)
			c.sleep(c.computeBackoff(attempt))
			continue
		}

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			retryAfter := resp.Header.Get("Retry-After")
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			log.Printf("  attempt %d: HTTP %d (Retry-After=%s)", attempt, resp.StatusCode, retryAfter)
			// breakerPause is the configured pause (60s in production,
			// shrunk to milliseconds in tests). limiter.onError() returns
			// its own recommendation; take whichever is shorter so tests
			// stay fast.
			pause := c.breakerPause
			if rec := c.limiter.onError(); rec < pause {
				pause = rec
			}
			if retryAfter != "" {
				if secs, err := strconv.Atoi(retryAfter); err == nil {
					raDur := time.Duration(secs) * time.Second
					if raDur > pause {
						pause = raDur
					}
				}
			}
			c.sleep(pause)
			continue
		}

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			c.sleep(c.computeBackoff(attempt))
			continue
		}
		c.limiter.onSuccess()
		return body, nil
	}
	return nil, fmt.Errorf("exhausted retries: %v", lastErr)
}

func (c *Client) applyHeaders(req *http.Request) {
	c.rngMu.Lock()
	ua := defaultUserAgents[c.rng.Intn(len(defaultUserAgents))]
	c.rngMu.Unlock()
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", "https://brokercheck.finra.org/")
	req.Header.Set("Origin", "https://brokercheck.finra.org")
}

func (c *Client) computeBackoff(attempt int) time.Duration {
	c.rngMu.Lock()
	jitter := 0.7 + c.rng.Float64()*0.6
	c.rngMu.Unlock()
	base := float64(c.backoffBase) * math.Pow(3, float64(attempt-1))
	return time.Duration(base * jitter)
}

func (c *Client) sleep(d time.Duration) { time.Sleep(d) }
