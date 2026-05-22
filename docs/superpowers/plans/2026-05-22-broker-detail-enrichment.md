# Broker Detail Enrichment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a second pipeline stage that enriches each broker with the full detail payload from `/search/individual/{CRD}` (employment history, exams, registered states/SROs, disclosures), targeting ~3.6 days for ~620k brokers via a global rate limiter and adaptive circuit breaker.

**Architecture:** Refactor the existing scraper into shared networking core + per-stage logic, add a CLI subcommand dispatcher, then layer in the enrich stage. Rate is controlled globally so worker count no longer multiplies server-facing load.

**Tech Stack:** Go 1.25, stdlib `net/http` + `encoding/json`, `golang.org/x/time/rate` (new dep), table-driven tests with `testing` package.

**Spec:** [`docs/superpowers/specs/2026-05-22-broker-detail-enrichment-design.md`](../specs/2026-05-22-broker-detail-enrichment-design.md)

---

## File Structure

**New files:**

| File | Responsibility |
|---|---|
| `types.go` | Existing `BrokerSource`/`BrokerHit`/etc. types, moved out of `main.go` |
| `types_detail.go` | New `BrokerDetail`, `BasicInformation`, `DetailedEmployment`, `Disclosure`, `Exam`, etc. |
| `httpclient.go` | Shared `Limiter` (rate.Limiter wrapper with adaptive breaker) and `Client` (http.Client wrapper with retries) |
| `httpclient_test.go` | Unit tests for `Limiter` and `Client` |
| `stage_search.go` | Existing search-stage logic moved out of `main.go`; uses shared `Client` |
| `stage_enrich.go` | New enrich-stage logic — fetch, parse, persist |
| `stage_enrich_test.go` | Parser unit tests using saved fixture responses |
| `testdata/detail_2819404.json` | Real captured response fixture |
| `testdata/detail_5634972.json` | Real captured response fixture (broker with disclosures) |
| `testdata/detail_notfound.json` | Synthetic `total=0` fixture |

**Modified files:**

| File | Change |
|---|---|
| `main.go` | Reduced to CLI dispatcher (subcommands: `search` / `enrich` / `dedupe`) and shared flag definitions |
| `go.mod` | Add `golang.org/x/time` |
| `.gitignore` | Add `brokers_detail.*`, `enrich_progress.jsonl`, `enrich_errors.jsonl`, `run/` |
| `README.md` | Document the new `enrich` subcommand and the global rate limiter |

---

### Task 1: Add rate dependency, scaffold types.go

Setup task — no behavior change. Move existing type definitions out of `main.go` into `types.go` and add the rate-limit dependency. This keeps later tasks focused on real changes.

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `types.go`
- Modify: `main.go` (remove moved types)

- [ ] **Step 1: Add the rate library dependency**

Run:
```bash
cd /Users/jaehunb/Documents/brokercheck-scraper
go get golang.org/x/time/rate@latest
go mod tidy
```

Expected: `go.mod` now lists `golang.org/x/time` as a direct dependency, `go.sum` updated.

- [ ] **Step 2: Create types.go**

Create `types.go` with the moved type definitions:

```go
package main

// --- Search-API response types ---

type BrokerResponse struct {
	ErrorCode    int     `json:"errorCode"`
	ErrorMessage string  `json:"errorMessage"`
	Hits         HitData `json:"hits"`
}

type HitData struct {
	Total int         `json:"total"`
	Hits  []BrokerHit `json:"hits"`
}

type BrokerHit struct {
	Source BrokerSource `json:"_source"`
}

type BrokerSource struct {
	CRD                string       `json:"ind_source_id"`
	FirstName          string       `json:"ind_firstname"`
	LastName           string       `json:"ind_lastname"`
	CurrentEmployments []Employment `json:"ind_current_employments"`
}

type Employment struct {
	FirmName string `json:"firm_name"`
	City     string `json:"branch_city"`
	State    string `json:"branch_state"`
	Zip      string `json:"branch_zip"`
}

// --- Domain types ---

type ZipLocation struct {
	Zip  string
	Lat  float64
	Lon  float64
	City string
}

// SearchPoint is a (lat,lon,radius) tuple — either an original zip
// or a subdivision generated when an area exceeds the API cap.
type SearchPoint struct {
	ID     string
	Lat    float64
	Lon    float64
	Radius float64
	Depth  int
}
```

- [ ] **Step 3: Remove the same types from main.go**

In `main.go`, delete lines 21–69 (the type definitions block from `// --- Response types ---` through the close of `SearchPoint`).

- [ ] **Step 4: Verify build still works**

Run: `go build .`
Expected: builds cleanly with no errors. Existing binary `brokercheck-scraper` overwritten.

- [ ] **Step 5: Verify behavior preserved with smoke run**

Run: `./brokercheck-scraper --limit=1 --out=/tmp/scraper-task1`
Expected: completes in <30s, writes some lines to `/tmp/scraper-task1/brokers.jsonl`, no panic, no API errors.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum types.go main.go
git commit -m "Add x/time dep and extract types into types.go"
```

---

### Task 2: Build the shared Limiter (TDD)

Write a process-wide rate limiter with an adaptive breaker.

**Files:**
- Create: `httpclient.go`
- Create: `httpclient_test.go`

- [ ] **Step 1: Write the failing test**

Create `httpclient_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestLimiter -v`
Expected: FAIL with errors like `undefined: newLimiter`.

- [ ] **Step 3: Implement Limiter**

Create `httpclient.go`:

```go
package main

import (
	"context"
	"log"
	"math"
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -run TestLimiter -v`
Expected: all 6 limiter tests pass.

- [ ] **Step 5: Commit**

```bash
git add httpclient.go httpclient_test.go
git commit -m "Add Limiter with adaptive circuit breaker"
```

---

### Task 3: Build the shared Client (TDD)

Wrap `http.Client` with a method that performs rate-limited GETs with retry/backoff and integrates with the Limiter.

**Files:**
- Modify: `httpclient.go`
- Modify: `httpclient_test.go`

- [ ] **Step 1: Write the failing test**

Append to `httpclient_test.go`:

```go
import (
	// add to existing import block:
	"net/http"
	"net/http/httptest"
	"sync/atomic"
)

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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./... -run TestClient -v`
Expected: FAIL with `undefined: newClient`.

- [ ] **Step 3: Implement Client**

Append to `httpclient.go`:

```go
import (
	// add to existing imports:
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
)

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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

```

The imports for `httpclient.go` should be exactly: `context`, `fmt`, `io`, `log`, `math`, `math/rand`, `net/http`, `strconv`, `sync`, `time`, `golang.org/x/time/rate`. No `strings` import in this file.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... -run TestClient -v`
Expected: all 4 client tests pass.

- [ ] **Step 5: Commit**

```bash
git add httpclient.go httpclient_test.go
git commit -m "Add Client wrapper with global rate limiting and retries"
```

---

### Task 4: Move search-stage logic into stage_search.go and use shared Client

Refactor `main.go` so the search-specific code lives in its own file and uses the shared `Client`. Behavior should be unchanged from the user's perspective (same flags, same outputs, same subdivision algorithm), but rate is now controlled globally.

**Files:**
- Create: `stage_search.go`
- Modify: `main.go`

- [ ] **Step 1: Create stage_search.go with the existing search functions**

Move the following from `main.go` into `stage_search.go` (preserving function names and signatures): `haversineMiles`, `offsetMiles`, `loadZipCodes`, `max3`, `spatialDedup`, `scrapePoint`, `paginate`, `subdivide`, `streamWriter` and its methods, `loadCompleted`, `dedupeStream`, `writeJSON`, `writeCSV`. Add `package main` at the top.

Replace the body of `fetchPage` with a call into the shared client. New signature:

```go
// fetchPage performs one search-API call. Caller owns the Client; rate
// limiting and retries are handled there.
func fetchPage(ctx context.Context, c *Client, sp SearchPoint, start, rows int) (*BrokerResponse, bool, error) {
	q := url.Values{}
	q.Set("lat", strconv.FormatFloat(sp.Lat, 'f', 6, 64))
	q.Set("lon", strconv.FormatFloat(sp.Lon, 'f', 6, 64))
	q.Set("includePrevious", "true")
	q.Set("hl", "true")
	q.Set("nrows", strconv.Itoa(rows))
	q.Set("start", strconv.Itoa(start))
	q.Set("r", strconv.FormatFloat(sp.Radius, 'f', 0, 64))
	q.Set("sort", "score+desc")
	q.Set("wt", "json")

	full := apiURL + "?" + q.Encode()
	body, err := c.Get(ctx, full)
	if err != nil {
		return nil, false, err
	}

	var br BrokerResponse
	if err := json.Unmarshal(body, &br); err != nil {
		return nil, false, fmt.Errorf("decode: %w", err)
	}
	if br.ErrorCode != 0 || strings.Contains(strings.ToLower(br.ErrorMessage), "exceeded") {
		return &br, true, nil
	}
	return &br, false, nil
}
```

Update `scrapePoint` and `paginate` to take `(ctx context.Context, c *Client, ...)` and pass through to `fetchPage`. Remove `jitteredSleep` calls inside them — pacing now happens inside `c.Get`.

- [ ] **Step 2: Trim main.go down to flag parsing + dispatcher**

Replace `main.go` with:

```go
package main

import (
	"context"
	"flag"
	"log"
	"sync"
	"time"
)

const (
	apiURL            = "https://api.brokercheck.finra.org/search/individual"
	pageSize          = 100
	apiPaginationCap  = 9000
	maxSubdivideDepth = 8
)

var (
	flagZipFile    = flag.String("zips", "uszips.csv", "Path to zip code CSV (header expected)")
	flagOutDir     = flag.String("out", ".", "Output directory")
	flagInitRadius = flag.Float64("radius", 25.0, "Initial search radius in miles")
	flagMinSpacing = flag.Float64("spacing", 40.0, "Minimum miles between representative zips. 0 disables.")
	flagDelayMs    = flag.Int("delay", 6000, "(deprecated; use --rps) Mean delay between requests in ms")
	flagRPS        = flag.Float64("rps", 0, "Global rate limit ceiling in requests/sec. 0 = derive from --delay (search default 0.17, enrich default 2.0).")
	flagMaxRetries = flag.Int("retries", 5, "Max retries on 429/5xx")
	flagLimit      = flag.Int("limit", 0, "Limit number of search points (0 = no limit). Smoke testing.")
	flagResume     = flag.Bool("resume", true, "Skip search points listed in progress.jsonl")
	flagWorkers    = flag.Int("workers", 1, "Concurrent workers (search default 1, enrich default 4).")
	flagDedupeOnly = flag.Bool("dedupe-only", false, "(deprecated alias for `dedupe` subcommand)")
	flagInput      = flag.String("input", "brokers_unique.json", "Source CRD list for `enrich` subcommand")
)

func main() {
	flag.Parse()

	subcmd := "search"
	if flag.NArg() >= 1 {
		subcmd = flag.Arg(0)
	}
	if *flagDedupeOnly {
		subcmd = "dedupe"
	}

	switch subcmd {
	case "search":
		runSearch()
	case "enrich":
		runEnrich()
	case "dedupe":
		log.Println("dedupe-only mode: reading brokers.jsonl")
		if err := dedupeStream(*flagOutDir); err != nil {
			log.Fatalf("dedupe failed: %v", err)
		}
		log.Println("done")
	default:
		log.Fatalf("unknown subcommand %q (expected search, enrich, or dedupe)", subcmd)
	}
}

// runSearch is implemented in stage_search.go.
// runEnrich is implemented in stage_enrich.go (Task 7).
```

Stub out `runEnrich` for now in `stage_enrich.go` so the build works:

Create `stage_enrich.go`:

```go
package main

import "log"

func runEnrich() {
	log.Fatal("enrich subcommand not yet implemented")
}
```

- [ ] **Step 3: Move runSearch and friends into stage_search.go**

Move the existing `main()`'s search loop body into a new `runSearch()` function in `stage_search.go`. At the top of `runSearch`:

```go
func runSearch() {
	ctx := context.Background()
	rps := *flagRPS
	if rps <= 0 {
		// Back-compat: derive from --delay
		rps = 1000.0 / float64(*flagDelayMs)
	}
	limiter := newLimiter(rps, 0.05)
	client := newClient(limiter, *flagMaxRetries)

	log.Printf("loading zip codes from %s", *flagZipFile)
	zips, err := loadZipCodes(*flagZipFile)
	if err != nil {
		log.Fatalf("load zips: %v", err)
	}
	log.Printf("loaded %d zip codes", len(zips))

	if *flagMinSpacing > 0 {
		before := len(zips)
		zips = spatialDedup(zips, *flagMinSpacing)
		log.Printf("spatial dedup: %d → %d points (min spacing %.1f mi)", before, len(zips), *flagMinSpacing)
	}

	completed, err := loadCompleted(*flagOutDir)
	if err != nil {
		log.Fatalf("load progress: %v", err)
	}
	if *flagResume && len(completed) > 0 {
		log.Printf("resume: %d points already completed", len(completed))
	}

	var points []SearchPoint
	for _, z := range zips {
		sp := SearchPoint{ID: z.Zip, Lat: z.Lat, Lon: z.Lon, Radius: *flagInitRadius}
		if *flagResume && completed[sp.ID] {
			continue
		}
		points = append(points, sp)
	}
	if *flagLimit > 0 && len(points) > *flagLimit {
		points = points[:*flagLimit]
	}
	log.Printf("scraping %d search points (rps=%.2f workers=%d)", len(points), rps, *flagWorkers)

	sw, err := newStreamWriter(*flagOutDir)
	if err != nil {
		log.Fatalf("open output: %v", err)
	}
	defer sw.close()

	start := time.Now()
	totalEmitted := runSearchWorkers(ctx, client, points, sw)
	log.Printf("scraping complete: %d records emitted in %s", totalEmitted, time.Since(start).Round(time.Second))
	log.Println("running final dedup pass...")
	if err := dedupeStream(*flagOutDir); err != nil {
		log.Fatalf("dedup: %v", err)
	}
	log.Println("done")
}

func runSearchWorkers(ctx context.Context, c *Client, points []SearchPoint, sw *streamWriter) int {
	if *flagWorkers <= 1 {
		total := 0
		startTs := time.Now()
		for i, sp := range points {
			n, err := scrapePoint(ctx, c, sp, sw.emit)
			total += n
			if err != nil {
				log.Printf("[%d/%d] %s error: %v", i+1, len(points), sp.ID, err)
				continue
			}
			sw.markDone(sp.ID, n)
			if (i+1)%10 == 0 || i+1 == len(points) {
				elapsed := time.Since(startTs)
				rate := float64(i+1) / elapsed.Seconds()
				eta := time.Duration(float64(len(points)-i-1)/rate) * time.Second
				log.Printf("[%d/%d] %s: +%d brokers | rate=%.2f pts/s | ETA=%s | total=%d",
					i+1, len(points), sp.ID, n, rate, eta.Round(time.Second), total)
			}
		}
		return total
	}

	var wg sync.WaitGroup
	jobs := make(chan SearchPoint, *flagWorkers*2)
	var counter struct {
		mu   sync.Mutex
		done int
		emit int
	}
	for w := 0; w < *flagWorkers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for sp := range jobs {
				n, err := scrapePoint(ctx, c, sp, sw.emit)
				if err != nil {
					log.Printf("worker %d %s error: %v", id, sp.ID, err)
				} else {
					sw.markDone(sp.ID, n)
				}
				counter.mu.Lock()
				counter.done++
				counter.emit += n
				if counter.done%10 == 0 {
					log.Printf("[%d/%d] total=%d", counter.done, len(points), counter.emit)
				}
				counter.mu.Unlock()
			}
		}(w)
	}
	for _, sp := range points {
		jobs <- sp
	}
	close(jobs)
	wg.Wait()
	return counter.emit
}
```

Update `scrapePoint` and `paginate` signatures in `stage_search.go` to accept `(ctx context.Context, c *Client, ...)`. Their bodies already match — remove the `delayMs` parameter and the `jitteredSleep(delayMs)` calls; replace the inline `fetchPage(sp, ...)` with `fetchPage(ctx, c, sp, ...)`.

Delete now-dead code in main.go and stage_search.go: `jitteredSleep`, `pickUserAgent`, `httpClient` package var, `userAgents` slice, `rng`, `rngMu`, `backoffSleep`, the original `fetchPage` body — all replaced by the shared `Client`.

- [ ] **Step 4: Verify build**

Run: `go build .`
Expected: builds cleanly. If go vet flags unused imports, remove them.

- [ ] **Step 5: Smoke test that search behavior is preserved**

Run: `./brokercheck-scraper search --limit=1 --out=/tmp/scraper-task4 --rps=0.2`
Expected:
- Completes in 30-90s
- Logs include `rps=0.20 workers=1`
- `/tmp/scraper-task4/brokers.jsonl` has at least 1 line of valid JSON with the expected fields
- `/tmp/scraper-task4/brokers_unique.json` has the dedup result

Also run with no subcommand (back-compat):
Run: `./brokercheck-scraper --limit=1 --out=/tmp/scraper-task4-bc --rps=0.2`
Expected: same behavior — defaults to `search`.

- [ ] **Step 6: Commit**

```bash
git add main.go stage_search.go stage_enrich.go
git commit -m "Refactor search stage to use shared Client, add CLI subcommand dispatcher"
```

---

### Task 5: Define detail types and capture test fixtures

Add the new types and pre-capture two real responses we'll use in parser tests.

**Files:**
- Create: `types_detail.go`
- Create: `testdata/detail_2819404.json`
- Create: `testdata/detail_5634972.json`
- Create: `testdata/detail_notfound.json`

- [ ] **Step 1: Capture real fixtures**

Run:
```bash
mkdir -p testdata
curl -s "https://api.brokercheck.finra.org/search/individual/2819404" \
  -H "Referer: https://brokercheck.finra.org/" \
  -H "User-Agent: Mozilla/5.0" > testdata/detail_2819404.json
curl -s "https://api.brokercheck.finra.org/search/individual/5634972" \
  -H "Referer: https://brokercheck.finra.org/" \
  -H "User-Agent: Mozilla/5.0" > testdata/detail_5634972.json
```

Verify both files are non-empty and contain `"hits":` JSON. If either is empty, retry once.

- [ ] **Step 2: Write the synthetic not-found fixture**

Create `testdata/detail_notfound.json`:

```json
{"hits":{"total":0,"hits":[]}}
```

- [ ] **Step 3: Create types_detail.go**

```go
package main

// --- Detail-API response types ---
//
// The detail endpoint returns a doubly-wrapped JSON document. The outer
// shape is HitData (re-used from types.go); the single hit's _source.content
// is itself a JSON-encoded *string* whose unmarshaled shape is BrokerDetail.

type DetailEnvelope struct {
	Hits struct {
		Total int `json:"total"`
		Hits  []struct {
			Source struct {
				Content string `json:"content"`
			} `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}

type BrokerDetail struct {
	CRD                   string                 `json:"crd"`
	BasicInfo             BasicInformation       `json:"basicInformation"`
	CurrentEmployments    []DetailedEmployment   `json:"currentEmployments"`
	CurrentIAEmployments  []DetailedEmployment   `json:"currentIAEmployments"`
	PreviousEmployments   []DetailedEmployment   `json:"previousEmployments"`
	PreviousIAEmployments []DetailedEmployment   `json:"previousIAEmployments"`
	Disclosures           []Disclosure           `json:"disclosures"`
	DisclosureFlag        string                 `json:"disclosureFlag"`
	IADisclosureFlag      string                 `json:"iaDisclosureFlag"`
	StateExams            []Exam                 `json:"stateExamCategory"`
	PrincipalExams        []Exam                 `json:"principalExamCategory"`
	ProductExams          []Exam                 `json:"productExamCategory"`
	ExamsCount            ExamsCount             `json:"examsCount"`
	RegisteredStates      []RegisteredState      `json:"registeredStates"`
	RegisteredSROs        []RegisteredSRO        `json:"registeredSROs"`
	RegistrationCount     RegistrationCount      `json:"registrationCount"`
	BrokerDetails         BrokerDetailsInner     `json:"brokerDetails"`
	FetchedAt             string                 `json:"fetched_at"`
}

type BasicInformation struct {
	IndividualID                 int      `json:"individualId"`
	FirstName                    string   `json:"firstName"`
	MiddleName                   string   `json:"middleName"`
	LastName                     string   `json:"lastName"`
	OtherNames                   []string `json:"otherNames"`
	BCScope                      string   `json:"bcScope"`
	IAScope                      string   `json:"iaScope"`
	DaysInIndustryCalculatedDate string   `json:"daysInIndustryCalculatedDate"`
}

type DetailedEmployment struct {
	FirmID                int               `json:"firmId"`
	FirmName              string            `json:"firmName"`
	IAOnly                string            `json:"iaOnly"`
	RegistrationBeginDate string            `json:"registrationBeginDate"`
	RegistrationEndDate   string            `json:"registrationEndDate,omitempty"`
	FirmBCScope           string            `json:"firmBCScope"`
	FirmIAScope           string            `json:"firmIAScope"`
	IASECNumber           string            `json:"iaSECNumber,omitempty"`
	IASECNumberType       string            `json:"iaSECNumberType,omitempty"`
	BDSECNumber           string            `json:"bdSECNumber,omitempty"`
	BranchOfficeLocations []BranchOfficeLoc `json:"branchOfficeLocations,omitempty"`
	City                  string            `json:"city,omitempty"`
	State                 string            `json:"state,omitempty"`
	Country               string            `json:"country,omitempty"`
}

type BranchOfficeLoc struct {
	DisplayOrder            int    `json:"displayOrder"`
	LocatedAtFlag           string `json:"locatedAtFlag"`
	SupervisedFromFlag      string `json:"supervisedFromFlag"`
	PrivateResidenceFlag    string `json:"privateResidenceFlag"`
	BranchOfficeID          string `json:"branchOfficeId"`
	Street1                 string `json:"street1"`
	Street2                 string `json:"street2,omitempty"`
	City                    string `json:"city"`
	State                   string `json:"state"`
	Country                 string `json:"country"`
	ZipCode                 string `json:"zipCode"`
	Latitude                string `json:"latitude"`
	Longitude               string `json:"longitude"`
	GeoLocation             string `json:"geoLocation"`
	NonRegisteredOfficeFlag string `json:"nonRegisteredOfficeFlag"`
	ELABeginDate            string `json:"elaBeginDate,omitempty"`
}

type Disclosure struct {
	EventDate            string         `json:"eventDate"`
	DisclosureType       string         `json:"disclosureType"`
	DisclosureResolution string         `json:"disclosureResolution"`
	IsIapdExcludedCCFlag string         `json:"isIapdExcludedCCFlag"`
	IsBcExcludedCCFlag   string         `json:"isBcExcludedCCFlag"`
	BCCtgryType          int            `json:"bcCtgryType"`
	DisclosureDetail     map[string]any `json:"disclosureDetail"`
}

type Exam struct {
	ExamCategory  string `json:"examCategory"`
	ExamName      string `json:"examName"`
	ExamTakenDate string `json:"examTakenDate"`
	ExamScope     string `json:"examScope"`
}

type ExamsCount struct {
	StateExamCount     int `json:"stateExamCount"`
	PrincipalExamCount int `json:"principalExamCount"`
	ProductExamCount   int `json:"productExamCount"`
}

type RegisteredState struct {
	State    string `json:"state"`
	RegScope string `json:"regScope"`
	Status   string `json:"status"`
	RegDate  string `json:"regDate"`
}

type RegisteredSRO struct {
	SRO            string   `json:"sro"`
	Status         string   `json:"status"`
	CategoriesList []string `json:"CategoriesList"`
}

type RegistrationCount struct {
	ApprovedSRORegistrationCount     int `json:"approvedSRORegistrationCount"`
	ApprovedFinraRegistrationCount   int `json:"approvedFinraRegistrationCount"`
	ApprovedStateRegistrationCount   int `json:"approvedStateRegistrationCount"`
	ApprovedIAStateRegistrationCount int `json:"approvedIAStateRegistrationCount"`
}

type BrokerDetailsInner struct {
	HasBCComments                 string `json:"hasBCComments"`
	HasIAComments                 string `json:"hasIAComments"`
	LegacyReportStatusDescription string `json:"legacyReportStatusDescription"`
}
```

- [ ] **Step 4: Verify build**

Run: `go build .`
Expected: builds cleanly.

- [ ] **Step 5: Commit**

```bash
git add types_detail.go testdata/detail_2819404.json testdata/detail_5634972.json testdata/detail_notfound.json
git commit -m "Add BrokerDetail types and captured response fixtures"
```

---

### Task 6: Implement and test the detail parser (TDD)

Add the parser that turns a raw response body into a `BrokerDetail`, with fixture-driven tests for the three edge cases.

**Files:**
- Create: `stage_enrich_test.go`
- Modify: `stage_enrich.go`

- [ ] **Step 1: Write the failing test**

Create `stage_enrich_test.go`:

```go
package main

import (
	"errors"
	"os"
	"testing"
)

func TestParseDetail_FullPayload(t *testing.T) {
	body, err := os.ReadFile("testdata/detail_2819404.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	d, err := parseDetail("2819404", body)
	if err != nil {
		t.Fatalf("parseDetail: %v", err)
	}
	if d.CRD != "2819404" {
		t.Errorf("CRD: got %q, want 2819404", d.CRD)
	}
	if d.BasicInfo.FirstName != "LAURENCE" {
		t.Errorf("FirstName: got %q", d.BasicInfo.FirstName)
	}
	if d.BasicInfo.MiddleName != "ANTHONY" {
		t.Errorf("MiddleName: got %q", d.BasicInfo.MiddleName)
	}
	if len(d.CurrentEmployments) == 0 {
		t.Errorf("expected current employments")
	}
	if len(d.PreviousEmployments) == 0 {
		t.Errorf("expected previous employments")
	}
	if d.ExamsCount.StateExamCount != 2 {
		t.Errorf("StateExamCount: got %d, want 2", d.ExamsCount.StateExamCount)
	}
	if len(d.RegisteredStates) != 2 {
		t.Errorf("RegisteredStates len: got %d, want 2", len(d.RegisteredStates))
	}
	if len(d.RegisteredSROs) == 0 {
		t.Errorf("expected SROs")
	}
}

func TestParseDetail_WithDisclosures(t *testing.T) {
	body, err := os.ReadFile("testdata/detail_5634972.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	d, err := parseDetail("5634972", body)
	if err != nil {
		t.Fatalf("parseDetail: %v", err)
	}
	if len(d.Disclosures) == 0 {
		t.Fatalf("expected disclosures, got 0")
	}
	if d.Disclosures[0].DisclosureType == "" {
		t.Errorf("disclosure type empty")
	}
	if d.Disclosures[0].DisclosureDetail == nil {
		t.Errorf("disclosure detail empty")
	}
}

func TestParseDetail_NotFound(t *testing.T) {
	body, err := os.ReadFile("testdata/detail_notfound.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	_, err = parseDetail("9999999", body)
	if !errors.Is(err, errBrokerNotFound) {
		t.Fatalf("expected errBrokerNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./... -run TestParseDetail -v`
Expected: FAIL with `undefined: parseDetail` and `undefined: errBrokerNotFound`.

- [ ] **Step 3: Implement parseDetail**

Replace `stage_enrich.go` content with:

```go
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
)

var errBrokerNotFound = errors.New("broker not found")

// parseDetail unwraps the outer envelope, parses the nested content
// JSON string into a BrokerDetail, and stamps the CRD.
func parseDetail(crd string, body []byte) (*BrokerDetail, error) {
	var env DetailEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("envelope: %w", err)
	}
	if env.Hits.Total == 0 || len(env.Hits.Hits) == 0 {
		return nil, errBrokerNotFound
	}
	contentStr := env.Hits.Hits[0].Source.Content
	if contentStr == "" {
		return nil, fmt.Errorf("empty content field")
	}
	var d BrokerDetail
	if err := json.Unmarshal([]byte(contentStr), &d); err != nil {
		return nil, fmt.Errorf("content: %w", err)
	}
	d.CRD = crd
	return &d, nil
}

func runEnrich() {
	log.Fatal("enrich subcommand not yet implemented (parser only)")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... -run TestParseDetail -v`
Expected: all 3 parser tests pass.

- [ ] **Step 5: Commit**

```bash
git add stage_enrich.go stage_enrich_test.go
git commit -m "Implement BrokerDetail parser with envelope unwrapping"
```

---

### Task 7: Implement enrich worker pool with resume

Wire up the full enrich pipeline: load CRDs, run a worker pool that fetches and emits, persist progress.

**Files:**
- Modify: `stage_enrich.go`
- Modify: `stage_enrich_test.go`

- [ ] **Step 1: Write the failing test**

Append to `stage_enrich_test.go`:

```go
import (
	// add to existing imports:
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
)

func TestEnrich_ProducesOneLinePerCRD(t *testing.T) {
	// Synthetic detail server that echoes a minimal valid envelope
	// keyed on the CRD path segment.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		crd := parts[len(parts)-1]
		inner := fmt.Sprintf(`{"basicInformation":{"individualId":%s,"firstName":"X","lastName":"Y"}}`, crd)
		envelope := map[string]any{
			"hits": map[string]any{
				"total": 1,
				"hits":  []map[string]any{{"_source": map[string]string{"content": inner}}},
			},
		}
		_ = json.NewEncoder(w).Encode(envelope)
	}))
	defer srv.Close()

	dir := t.TempDir()
	crds := []string{"100", "200", "300"}

	cfg := enrichConfig{
		Out:      dir,
		Workers:  2,
		Resume:   true,
		BaseURL:  srv.URL,
		CRDs:     crds,
	}
	c := newClient(newLimiter(100, 1), 3)
	if err := runEnrichWith(context.Background(), c, cfg); err != nil {
		t.Fatalf("runEnrichWith: %v", err)
	}

	// Verify jsonl has 3 lines
	f, err := os.Open(filepath.Join(dir, "brokers_detail.jsonl"))
	if err != nil {
		t.Fatalf("open jsonl: %v", err)
	}
	defer f.Close()
	count := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		count++
	}
	if count != 3 {
		t.Errorf("jsonl line count: got %d, want 3", count)
	}

	// Verify progress.jsonl has 3 lines
	pf, _ := os.Open(filepath.Join(dir, "enrich_progress.jsonl"))
	defer pf.Close()
	pc := 0
	psc := bufio.NewScanner(pf)
	for psc.Scan() {
		pc++
	}
	if pc != 3 {
		t.Errorf("progress line count: got %d, want 3", pc)
	}
}

func TestEnrich_ResumeSkipsCompleted(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		parts := strings.Split(r.URL.Path, "/")
		crd := parts[len(parts)-1]
		inner := fmt.Sprintf(`{"basicInformation":{"individualId":%s,"firstName":"X","lastName":"Y"}}`, crd)
		envelope := map[string]any{
			"hits": map[string]any{
				"total": 1,
				"hits":  []map[string]any{{"_source": map[string]string{"content": inner}}},
			},
		}
		_ = json.NewEncoder(w).Encode(envelope)
	}))
	defer srv.Close()

	dir := t.TempDir()
	// Pre-populate progress with one completed CRD
	pre := filepath.Join(dir, "enrich_progress.jsonl")
	if err := os.WriteFile(pre, []byte(`{"crd":"100","ts":"2026-05-22T00:00:00Z"}`+"\n"), 0644); err != nil {
		t.Fatalf("seed progress: %v", err)
	}

	cfg := enrichConfig{
		Out: dir, Workers: 2, Resume: true,
		BaseURL: srv.URL, CRDs: []string{"100", "200", "300"},
	}
	c := newClient(newLimiter(100, 1), 3)
	if err := runEnrichWith(context.Background(), c, cfg); err != nil {
		t.Fatalf("runEnrichWith: %v", err)
	}

	if hits != 2 {
		t.Errorf("expected 2 server hits (resume skipped 100), got %d", hits)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./... -run TestEnrich_ -v`
Expected: FAIL with `undefined: enrichConfig` and `undefined: runEnrichWith`.

- [ ] **Step 3: Implement enrichment**

Replace `stage_enrich.go` content with:

```go
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var errBrokerNotFound = errors.New("broker not found")

type enrichConfig struct {
	Out     string
	Workers int
	Resume  bool
	BaseURL string // e.g. "https://api.brokercheck.finra.org/search/individual"
	CRDs    []string
	Limit   int
}

func runEnrich() {
	rps := *flagRPS
	if rps <= 0 {
		rps = 2.0 // enrich default
	}
	workers := *flagWorkers
	if workers < 1 {
		workers = 4 // enrich default
	}

	crds, err := loadCRDsFromUnique(filepath.Join(*flagOutDir, *flagInput))
	if err != nil {
		log.Fatalf("load CRDs: %v", err)
	}
	log.Printf("loaded %d CRDs from %s", len(crds), *flagInput)

	cfg := enrichConfig{
		Out:     *flagOutDir,
		Workers: workers,
		Resume:  *flagResume,
		BaseURL: "https://api.brokercheck.finra.org/search/individual",
		CRDs:    crds,
		Limit:   *flagLimit,
	}
	limiter := newLimiter(rps, 0.25)
	client := newClient(limiter, *flagMaxRetries)

	log.Printf("enrichment starting (rps=%.2f workers=%d)", rps, workers)
	if err := runEnrichWith(context.Background(), client, cfg); err != nil {
		log.Fatalf("enrich failed: %v", err)
	}

	log.Println("enrichment complete; running finalize step...")
	if err := finalizeEnrich(cfg.Out); err != nil {
		log.Fatalf("finalize: %v", err)
	}
	log.Println("done")
}

// runEnrichWith is the testable core: pure inputs/outputs, no flag globals.
func runEnrichWith(ctx context.Context, c *Client, cfg enrichConfig) error {
	if err := os.MkdirAll(cfg.Out, 0755); err != nil {
		return fmt.Errorf("mkdir out: %w", err)
	}

	completed, err := loadEnrichProgress(cfg.Out)
	if err != nil {
		return fmt.Errorf("load progress: %w", err)
	}
	if cfg.Resume && len(completed) > 0 {
		log.Printf("enrich resume: %d CRDs already completed", len(completed))
	}

	var todo []string
	for _, crd := range cfg.CRDs {
		if cfg.Resume && completed[crd] {
			continue
		}
		todo = append(todo, crd)
	}
	if cfg.Limit > 0 && len(todo) > cfg.Limit {
		todo = todo[:cfg.Limit]
	}
	log.Printf("enrich: %d CRDs to process", len(todo))

	ew, err := newEnrichWriter(cfg.Out)
	if err != nil {
		return err
	}
	defer ew.close()

	jobs := make(chan string, cfg.Workers*2)
	var wg sync.WaitGroup
	var counter struct {
		mu   sync.Mutex
		done int
	}
	startTs := time.Now()

	for w := 0; w < cfg.Workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for crd := range jobs {
				if err := fetchAndEmit(ctx, c, cfg.BaseURL, crd, ew); err != nil {
					log.Printf("worker %d crd=%s: %v", id, crd, err)
				}
				counter.mu.Lock()
				counter.done++
				if counter.done%100 == 0 || counter.done == len(todo) {
					elapsed := time.Since(startTs)
					rate := float64(counter.done) / elapsed.Seconds()
					eta := time.Duration(float64(len(todo)-counter.done)/rate) * time.Second
					log.Printf("[%d/%d] rate=%.2f/s ETA=%s", counter.done, len(todo), rate, eta.Round(time.Second))
				}
				counter.mu.Unlock()
			}
		}(w)
	}
	for _, crd := range todo {
		jobs <- crd
	}
	close(jobs)
	wg.Wait()
	return nil
}

func fetchAndEmit(ctx context.Context, c *Client, baseURL, crd string, ew *enrichWriter) error {
	url := baseURL + "/" + crd
	body, err := c.Get(ctx, url)
	if err != nil {
		return err // do not mark complete; transient
	}
	d, err := parseDetail(crd, body)
	if err != nil {
		if errors.Is(err, errBrokerNotFound) {
			ew.markError(crd, "not_found")
			return nil
		}
		ew.markError(crd, fmt.Sprintf("parse: %v", err))
		return nil
	}
	d.FetchedAt = time.Now().UTC().Format(time.RFC3339)
	ew.emit(d)
	ew.markDone(crd)
	return nil
}

// --- Persistence ---

type enrichWriter struct {
	mu       sync.Mutex
	out      *os.File
	progress *os.File
	errs     *os.File
}

func newEnrichWriter(outDir string) (*enrichWriter, error) {
	out, err := os.OpenFile(filepath.Join(outDir, "brokers_detail.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	prog, err := os.OpenFile(filepath.Join(outDir, "enrich_progress.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		out.Close()
		return nil, err
	}
	errs, err := os.OpenFile(filepath.Join(outDir, "enrich_errors.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		out.Close()
		prog.Close()
		return nil, err
	}
	return &enrichWriter{out: out, progress: prog, errs: errs}, nil
}

func (e *enrichWriter) emit(d *BrokerDetail) {
	e.mu.Lock()
	defer e.mu.Unlock()
	enc, err := json.Marshal(d)
	if err != nil {
		return
	}
	e.out.Write(enc)
	e.out.Write([]byte("\n"))
}

func (e *enrichWriter) markDone(crd string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec := map[string]string{"crd": crd, "ts": time.Now().UTC().Format(time.RFC3339)}
	enc, _ := json.Marshal(rec)
	e.progress.Write(enc)
	e.progress.Write([]byte("\n"))
}

func (e *enrichWriter) markError(crd, reason string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec := map[string]string{"crd": crd, "reason": reason, "ts": time.Now().UTC().Format(time.RFC3339)}
	enc, _ := json.Marshal(rec)
	e.errs.Write(enc)
	e.errs.Write([]byte("\n"))
	// Errored CRDs also count as 'done' so resume skips them.
	e.progress.Write(enc)
	e.progress.Write([]byte("\n"))
}

func (e *enrichWriter) close() {
	e.out.Close()
	e.progress.Close()
	e.errs.Close()
}

func loadEnrichProgress(outDir string) (map[string]bool, error) {
	done := map[string]bool{}
	f, err := os.Open(filepath.Join(outDir, "enrich_progress.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return done, nil
		}
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var rec struct {
			CRD string `json:"crd"`
		}
		if err := json.Unmarshal(sc.Bytes(), &rec); err == nil && rec.CRD != "" {
			done[rec.CRD] = true
		}
	}
	return done, sc.Err()
}

// loadCRDsFromUnique reads brokers_unique.json (the search-stage output)
// and extracts the CRD list.
func loadCRDsFromUnique(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w (run `./brokercheck-scraper search` first)", err)
	}
	defer f.Close()
	var bs []BrokerSource
	if err := json.NewDecoder(f).Decode(&bs); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		if b.CRD != "" {
			out = append(out, b.CRD)
		}
	}
	return out, nil
}

// finalizeEnrich is implemented in Task 8; placeholder no-op.
func finalizeEnrich(outDir string) error { return nil }

// parseDetail (from Task 6) is unchanged.
func parseDetail(crd string, body []byte) (*BrokerDetail, error) {
	var env DetailEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("envelope: %w", err)
	}
	if env.Hits.Total == 0 || len(env.Hits.Hits) == 0 {
		return nil, errBrokerNotFound
	}
	contentStr := env.Hits.Hits[0].Source.Content
	if contentStr == "" {
		return nil, fmt.Errorf("empty content field")
	}
	var d BrokerDetail
	if err := json.Unmarshal([]byte(contentStr), &d); err != nil {
		return nil, fmt.Errorf("content: %w", err)
	}
	d.CRD = crd
	return &d, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... -v`
Expected: all tests pass — Limiter (6), Client (4), ParseDetail (3), Enrich (2). 15 total.

- [ ] **Step 5: Commit**

```bash
git add stage_enrich.go stage_enrich_test.go
git commit -m "Implement enrich worker pool with resume"
```

---

### Task 8: Implement enrich finalize (JSON + CSV output)

After the worker loop drains, read back `brokers_detail.jsonl`, dedupe by CRD, and write `brokers_detail.json` + `brokers_detail.csv`.

**Files:**
- Modify: `stage_enrich.go`
- Modify: `stage_enrich_test.go`

- [ ] **Step 1: Write the failing test**

Append to `stage_enrich_test.go`:

```go
import (
	// add to existing imports:
	"encoding/csv"
)

func TestFinalize_WritesJSONAndCSV(t *testing.T) {
	dir := t.TempDir()
	jsonl := filepath.Join(dir, "brokers_detail.jsonl")
	rec1 := `{"crd":"100","basicInformation":{"firstName":"A","lastName":"B"},"currentEmployments":[{"firmName":"FirmX"}],"examsCount":{"productExamCount":1},"productExamCategory":[{"examCategory":"Series 7"}],"registeredStates":[{"state":"NY"}]}`
	rec2 := `{"crd":"200","basicInformation":{"firstName":"C","lastName":"D"},"disclosures":[{"disclosureType":"Regulatory"}]}`
	if err := os.WriteFile(jsonl, []byte(rec1+"\n"+rec2+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := finalizeEnrich(dir); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	// JSON: 2 entries
	jb, err := os.ReadFile(filepath.Join(dir, "brokers_detail.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed []BrokerDetail
	if err := json.Unmarshal(jb, &parsed); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(parsed) != 2 {
		t.Errorf("json entries: got %d, want 2", len(parsed))
	}

	// CSV: 1 header + 2 rows
	cf, err := os.Open(filepath.Join(dir, "brokers_detail.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer cf.Close()
	rows, err := csv.NewReader(cf).ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("csv rows: got %d, want 3", len(rows))
	}
	header := rows[0]
	want := []string{"CRD", "FirstName", "MiddleName", "LastName", "OtherNamesCount",
		"BCScope", "IAScope", "DaysInIndustryStart", "CurrentFirmName",
		"CurrentBranchStreet", "CurrentBranchCity", "CurrentBranchState",
		"CurrentBranchZip", "CurrentEmploymentCount", "PreviousEmploymentCount",
		"ExamCategoriesList", "RegisteredStatesCount", "RegisteredStatesList",
		"RegisteredSROsList", "DisclosureCount", "HasDisclosure", "HasBCComments", "FetchedAt"}
	if len(header) != len(want) {
		t.Fatalf("csv header len: got %d (%v), want %d", len(header), header, len(want))
	}
	for i, h := range want {
		if header[i] != h {
			t.Errorf("csv col %d: got %q, want %q", i, header[i], h)
		}
	}
	// Spot-check row for CRD=100: ExamCategoriesList contains "Series 7"
	for _, r := range rows[1:] {
		if r[0] == "100" {
			if !strings.Contains(r[15], "Series 7") {
				t.Errorf("expected 'Series 7' in ExamCategoriesList, got %q", r[15])
			}
		}
		if r[0] == "200" {
			if r[20] != "1" {
				t.Errorf("expected DisclosureCount=1 for 200, got %q", r[20])
			}
			if r[21] != "Y" {
				t.Errorf("expected HasDisclosure=Y for 200, got %q", r[21])
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestFinalize -v`
Expected: FAIL — finalize is currently a no-op so JSON file won't exist.

- [ ] **Step 3: Implement finalize**

Replace the `finalizeEnrich` stub in `stage_enrich.go` with:

```go
func finalizeEnrich(outDir string) error {
	jsonlPath := filepath.Join(outDir, "brokers_detail.jsonl")
	f, err := os.Open(jsonlPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", jsonlPath, err)
	}
	defer f.Close()

	uniq := map[string]*BrokerDetail{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		var d BrokerDetail
		if err := json.Unmarshal(sc.Bytes(), &d); err != nil {
			continue
		}
		if d.CRD != "" {
			uniq[d.CRD] = &d
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}

	list := make([]*BrokerDetail, 0, len(uniq))
	for _, d := range uniq {
		list = append(list, d)
	}

	// Pretty JSON
	jp := filepath.Join(outDir, "brokers_detail.json")
	jf, err := os.Create(jp)
	if err != nil {
		return err
	}
	defer jf.Close()
	enc := json.NewEncoder(jf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(list); err != nil {
		return err
	}

	// Flat CSV
	cp := filepath.Join(outDir, "brokers_detail.csv")
	cf, err := os.Create(cp)
	if err != nil {
		return err
	}
	defer cf.Close()
	w := csv.NewWriter(cf)
	defer w.Flush()
	w.Write([]string{
		"CRD", "FirstName", "MiddleName", "LastName", "OtherNamesCount",
		"BCScope", "IAScope", "DaysInIndustryStart", "CurrentFirmName",
		"CurrentBranchStreet", "CurrentBranchCity", "CurrentBranchState",
		"CurrentBranchZip", "CurrentEmploymentCount", "PreviousEmploymentCount",
		"ExamCategoriesList", "RegisteredStatesCount", "RegisteredStatesList",
		"RegisteredSROsList", "DisclosureCount", "HasDisclosure", "HasBCComments", "FetchedAt",
	})
	for _, d := range list {
		w.Write(detailToCSVRow(d))
	}
	return nil
}

func detailToCSVRow(d *BrokerDetail) []string {
	bi := d.BasicInfo
	var firmName, street, city, state, zip string
	if len(d.CurrentEmployments) > 0 {
		ce := d.CurrentEmployments[0]
		firmName = ce.FirmName
		if len(ce.BranchOfficeLocations) > 0 {
			b := ce.BranchOfficeLocations[0]
			street, city, state, zip = b.Street1, b.City, b.State, b.ZipCode
		} else {
			city, state = ce.City, ce.State
		}
	}
	exams := []string{}
	for _, e := range d.StateExams {
		exams = append(exams, e.ExamCategory)
	}
	for _, e := range d.PrincipalExams {
		exams = append(exams, e.ExamCategory)
	}
	for _, e := range d.ProductExams {
		exams = append(exams, e.ExamCategory)
	}
	states := []string{}
	for _, s := range d.RegisteredStates {
		states = append(states, s.State)
	}
	sros := []string{}
	for _, s := range d.RegisteredSROs {
		sros = append(sros, s.SRO)
	}
	hasDisc := "N"
	if len(d.Disclosures) > 0 {
		hasDisc = "Y"
	}
	return []string{
		d.CRD,
		bi.FirstName, bi.MiddleName, bi.LastName,
		fmt.Sprintf("%d", len(bi.OtherNames)),
		bi.BCScope, bi.IAScope, bi.DaysInIndustryCalculatedDate,
		firmName, street, city, state, zip,
		fmt.Sprintf("%d", len(d.CurrentEmployments)),
		fmt.Sprintf("%d", len(d.PreviousEmployments)),
		strings.Join(exams, ";"),
		fmt.Sprintf("%d", len(states)),
		strings.Join(states, ";"),
		strings.Join(sros, ";"),
		fmt.Sprintf("%d", len(d.Disclosures)),
		hasDisc,
		d.BrokerDetails.HasBCComments,
		d.FetchedAt,
	}
}
```

You'll need to add to the imports of `stage_enrich.go`: `"encoding/csv"` and `"strings"`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... -v`
Expected: 16 tests pass total.

- [ ] **Step 5: Wire finalize into runEnrich**

The placeholder `finalizeEnrich` was already called from `runEnrich` in Task 7. Re-verify by running:

Run: `grep -n "finalizeEnrich" stage_enrich.go`
Expected: shows two lines — the call inside `runEnrich` and the function definition.

- [ ] **Step 6: Commit**

```bash
git add stage_enrich.go stage_enrich_test.go
git commit -m "Add finalize step writing brokers_detail.json and .csv"
```

---

### Task 9: End-to-end smoke test against live API

Manually verify the enrichment pipeline against a small CRD list.

**Files:**
- Create: `testdata/smoke_crds.json` (small synthetic input)

- [ ] **Step 1: Build the binary**

Run: `go build .`
Expected: success.

- [ ] **Step 2: Create a small CRD list disguised as brokers_unique.json**

Create `testdata/smoke_crds.json`:

```json
[
  {"ind_source_id":"2819404","ind_firstname":"LAURENCE","ind_lastname":"VELOUDIS-IRIZARRY"},
  {"ind_source_id":"5634972","ind_firstname":"X","ind_lastname":"Y"},
  {"ind_source_id":"9999999","ind_firstname":"NOT","ind_lastname":"FOUND"}
]
```

- [ ] **Step 3: Run the smoke enrichment**

Run:
```bash
./brokercheck-scraper enrich \
  --input=testdata/smoke_crds.json \
  --out=/tmp/scraper-task9 \
  --rps=1.0 --workers=2 --resume=false
```

Expected:
- Completes in under 30 seconds
- Logs include `enrichment starting (rps=1.00 workers=2)` and `enrichment complete; running finalize step...`
- `/tmp/scraper-task9/brokers_detail.jsonl` has at least 2 lines (the two real CRDs)
- `/tmp/scraper-task9/enrich_errors.jsonl` has 1 line for CRD 9999999 with `"reason":"not_found"`
- `/tmp/scraper-task9/brokers_detail.json` is a valid JSON array
- `/tmp/scraper-task9/brokers_detail.csv` has 3 lines (1 header + 2 rows)

- [ ] **Step 4: Re-run with resume=true to verify resume works**

Run:
```bash
./brokercheck-scraper enrich \
  --input=testdata/smoke_crds.json \
  --out=/tmp/scraper-task9 \
  --rps=1.0 --workers=2
```

Expected:
- Logs `enrich resume: 3 CRDs already completed`
- Completes in <2 seconds (no API calls made)
- No new lines added to `brokers_detail.jsonl`

- [ ] **Step 5: Commit the test fixture**

```bash
git add testdata/smoke_crds.json
git commit -m "Add smoke-test fixture for enrich subcommand"
```

---

### Task 10: Update README and .gitignore

Document the new subcommand, the global rate limiter behavior change, and add the new output files to gitignore.

**Files:**
- Modify: `README.md`
- Modify: `.gitignore`

- [ ] **Step 1: Update .gitignore**

Replace the contents of `.gitignore` with:

```
.env
.vscode/
brokercheck-scraper
brokers.jsonl
progress.jsonl
brokers_unique.json
brokers_unique.csv
brokers_detail.jsonl
brokers_detail.json
brokers_detail.csv
enrich_progress.jsonl
enrich_errors.jsonl
run/
```

- [ ] **Step 2: Update README**

Replace `README.md` with the following (preserves the structure of the current README, adds a section for `enrich`, updates the flag table):

```markdown
# FINRA BrokerCheck API Scraper

A Go scraper that pulls broker records from FINRA's BrokerCheck API for the
entire US, then enriches each broker with detailed registration, exam,
employment-history, and disclosure data.

## Subcommands

```sh
./brokercheck-scraper search    # Stage 1: discover all CRDs by zip-radius search
./brokercheck-scraper enrich    # Stage 2: fetch full detail per CRD (NEW)
./brokercheck-scraper dedupe    # Re-run final dedup pass on brokers.jsonl
```

Running with no subcommand defaults to `search` (back-compat).

## Stage 1: search

Same behavior as before. The BrokerCheck website's internal search API
(`https://api.brokercheck.finra.org/search/individual`) accepts a lat/lon
+ radius query and returns brokers. The scraper handles:

- **Spatial dedup** of US zip codes — 33,782 zips → 1,382 representative
  search points whose 25mi circles tile the country with minimal overlap.
- **Adaptive radius subdivision** — when a query returns more than 9000
  brokers (the API's pagination cap), the area is recursively split into
  4 smaller circles until each contains ≤9000 brokers.
- **Streaming output + checkpoint/resume** — every broker is appended to
  `brokers.jsonl` and every completed search point is logged to
  `progress.jsonl`. Re-running with `--resume` (default) skips already-done
  points.
- **Final dedup pass** — `brokers.jsonl` is deduped by CRD into
  `brokers_unique.json` and `brokers_unique.csv`.

## Stage 2: enrich (NEW)

The search stage returns minimal fields per broker (CRD, name, current
firm location). The enrich stage fetches `/search/individual/{CRD}` per
broker to capture:

- **Basic information** — middle name, aliases, BC/IA scope, days-in-industry start
- **Current employments** (BD + IA) — full firm names, branch addresses, SEC numbers
- **Previous employments** (BD + IA) — work history with start/end dates
- **Disclosures** — regulatory actions, customer disputes, sanctions
- **Exams** — Series 6, 7, 63, 65, SIE, etc. with dates
- **Registered states** — every state the broker is licensed in
- **Registered SROs** — FINRA, NYSE, etc. with registration categories

### Quick start

```sh
go build .
./brokercheck-scraper search                     # ~10-12 hours
./brokercheck-scraper enrich                     # ~3.6 days
```

Or smoke-test first:

```sh
./brokercheck-scraper enrich --limit=20 --resume=false
```

### Outputs

- `brokers_detail.jsonl` — streaming, append-only, one parsed broker per line.
- `enrich_progress.jsonl` — completed CRDs (used for resume).
- `enrich_errors.jsonl` — CRDs that returned no data or malformed responses.
- `brokers_detail.json` — full structured array of all enriched brokers.
- `brokers_detail.csv` — flat CSV with the most-asked-for fields and counts.

## Rate limiting

A single global rate limiter (`golang.org/x/time/rate`) caps the
process-wide request rate, so `--workers=N` no longer multiplies the
server-facing rate. An adaptive circuit breaker halves the rate on any
429/5xx and gradually recovers after 5 minutes of clean responses.

| Stage | Default rate | Approx. duration | Notes |
|---|---|---|---|
| search | 0.17 req/s (= 6s/req, matches old `--delay=6000`) | 10–12 hours | Conservative single-IP default |
| enrich | 2.0 req/s | ~3.6 days for 620k brokers | Detail endpoint is cheaper for FINRA than search |

To go faster, raise `--rps`. The breaker will auto-throttle if FINRA
pushes back. Going above ~3 req/s without proxies is risky.

## Flags

### Common
| Flag | Default | Notes |
|---|---|---|
| `--out` | `.` | Output directory |
| `--retries` | `5` | Max retries on 429/5xx |
| `--workers` | search:1, enrich:4 | Concurrent workers |
| `--rps` | search:0.17, enrich:2.0 | Global rate limit ceiling (req/s) |
| `--resume` | `true` | Skip already-completed items |
| `--limit` | `0` | Cap items processed (0 = unlimited; for smoke testing) |

### Search-specific
| Flag | Default | Notes |
|---|---|---|
| `--zips` | `uszips.csv` | Zip code CSV path |
| `--radius` | `25` | Initial search radius (miles) |
| `--spacing` | `40` | Min miles between representative zips |
| `--delay` | `6000` | (deprecated, use `--rps`) Mean delay in ms |

### Enrich-specific
| Flag | Default | Notes |
|---|---|---|
| `--input` | `brokers_unique.json` | Source CRD list (relative to `--out`) |

## Resume / restart

Just rerun the same command. Already-completed items are skipped via
`progress.jsonl` (search) or `enrich_progress.jsonl` (enrich).

## Resource

[FINRA BrokerCheck](https://brokercheck.finra.org/)
```

- [ ] **Step 3: Verify build still works**

Run: `go build . && ./brokercheck-scraper --help 2>&1 | head -30`
Expected: build succeeds; help output mentions the new flags.

- [ ] **Step 4: Commit**

```bash
git add README.md .gitignore
git commit -m "Document enrich subcommand and global rate limiter"
```

---

## Self-Review Notes

**Spec coverage:** Each section of the spec is covered:

- Shared networking core → Tasks 2, 3
- Search stage refactor → Task 4
- Enrich stage → Tasks 6, 7, 8
- Data model → Task 5
- Flat CSV columns → Task 8
- Error handling (429, 5xx, total=0, malformed) → Task 7 (`fetchAndEmit` + `markError`)
- Testing (parser fixtures, breaker unit tests, smoke test) → Tasks 2, 3, 6, 7, 8, 9
- Backwards compatibility (default subcommand, `--dedupe-only`, existing flags) → Task 4
- CLI flags → Task 4
- Output files → Tasks 7, 8

**Type consistency check:** `parseDetail`, `BrokerDetail.CRD`, `enrichConfig.BaseURL`, `enrichWriter.markDone/markError/emit/close`, `Limiter.onError/onSuccess/Wait/currentRate`, `Client.Get`, `runEnrichWith` are all referenced consistently across tasks.

**No placeholders:** All "TBD" / "TODO" / "implement later" patterns scanned — none found. All code blocks contain complete implementations.
