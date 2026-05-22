# Broker Detail Enrichment — Design

**Date:** 2026-05-22
**Status:** Approved (awaiting implementation plan)

## Problem

The current scraper only calls FINRA's *search* endpoint (`/search/individual`), which returns minimal fields per broker: CRD, first/last name, and current employment city/state/zip. The "more detailed info" panel on brokercheck.finra.org — employment history, exam credentials, registered states/SROs, disclosures — is fetched from a separate *detail* endpoint (`/search/individual/{CRD}`) that the scraper does not touch. As a result, the output `brokers_unique.json` cannot answer questions like:

- Where else has this broker worked? When did they start in the industry?
- What licenses do they hold (Series 6, 7, 63, 65, SIE, etc.)?
- In which states and through which SROs are they registered?
- Have they had regulatory actions, customer disputes, or sanctions?

This spec adds a second pipeline stage that enriches each broker with the full detail payload, on a schedule that completes a full ~620k-broker enrichment in ~3.6 days from a single residential IP.

## Goals

1. Capture all top-level sections returned by the detail endpoint: `basicInformation`, `currentEmployments`, `currentIAEmployments`, `previousEmployments`, `previousIAEmployments`, `disclosures`, exam categories (state/principal/product), `registeredStates`, `registeredSROs`, registration counts, broker details flags.
2. Default configuration completes ~620k brokers in approximately 3.6 days end-to-end.
3. Single-IP safe: aggregate request rate stays below FINRA's WAF threshold even as concurrency increases. Auto-throttle on 429s.
4. Resumable across crashes/restarts.
5. Zero behavior change for the existing search stage's outputs and on-disk formats.

## Non-goals

- Proxy rotation. The implementation assumes a single residential IP; adding rotating-proxy support is a future change.
- Schema for FINRA *firm* search (separate `/search/firm` endpoint).
- Historical-snapshot tracking. Each enrich run overwrites prior detail data.
- Live web scraping. Only the public JSON API is used.

## Architecture

Two independent pipeline stages, sharing a refactored networking core:

```
main.go (CLI dispatcher: search | enrich | dedupe)
├─ stage_search.go   — existing zip-based crawl (refactored to share limiter)
├─ stage_enrich.go   — NEW: detail-by-CRD fetcher
├─ httpclient.go     — NEW: shared http.Client, global rate limiter, adaptive breaker
└─ types.go          — moved type definitions, plus new BrokerDetail types
```

CLI surface:

```sh
./brokercheck-scraper                       # default = search (backwards compat)
./brokercheck-scraper search                # explicit
./brokercheck-scraper enrich                # NEW
./brokercheck-scraper dedupe                # replaces --dedupe-only (alias kept)
```

### Data flow

```
[search stage]                          [enrich stage]
uszips.csv                              brokers_unique.json
    │                                       │
    ▼                                       ▼
┌──────────────────┐                ┌──────────────────┐
│ Worker pool (N)  │                │ Worker pool (4)  │
└────────┬─────────┘                └────────┬─────────┘
         │ acquire token                     │ acquire token
         ▼                                   ▼
┌──────────────────────────────────────────────────────┐
│ Shared httpclient (global limiter + breaker + UA)    │
└──────────────────────────┬───────────────────────────┘
                           │
                           ▼
                    api.brokercheck.finra.org
                    /search/individual           (search)
                    /search/individual/{CRD}     (enrich)

brokers.jsonl                           brokers_detail.jsonl
progress.jsonl                          enrich_progress.jsonl
   │                                       │
   ▼ dedupe                                ▼ finalize
brokers_unique.{json,csv}               brokers_detail.{json,csv}
```

The stages are independent: search produces `brokers_unique.json`, enrich consumes it. They can run on different days, on different machines (copy the file), or enrich can be rerun without re-scraping.

## Components

### 1. Shared networking core (`httpclient.go`)

Used by both stages.

- One process-wide `http.Client` configured with HTTP/2 (`ForceAttemptHTTP2=true`), `MaxIdleConnsPerHost=8`, `IdleConnTimeout=90s`, 30s request timeout. Replaces the stdlib-default client at `main.go:92`.
- One process-wide `golang.org/x/time/rate.Limiter`. Every outbound request must `Wait(ctx)` on it before sending. Aggregate request rate at FINRA's edge is capped by the limiter regardless of worker count.
- `adaptiveBreaker` wraps the limiter:
  - **On 429 or 5xx response:** halve the limiter's rate, sleep 60s, then resume.
  - **On 5 minutes of clean 200s:** increase rate by 25% toward the configured ceiling.
  - **Rate floor:** 0.25 req/s. **Ceiling:** the configured `--rps`.
  - Every adjustment logged with `old → new` rate and the trigger.
- User-Agent / Referer / Origin rotation preserved from current code.
- `Retry-After` header honored as before. When a 429 carries both a Retry-After value and triggers the breaker, the longer of (Retry-After seconds, 60s breaker pause) is used.
- `FetchedAt` timestamps in `brokers_detail.jsonl` use RFC3339 UTC, matching `progress.jsonl` convention.

Replaces the existing per-worker `jitteredSleep` calls (`main.go:359, 418`) and inlined retry logic (`main.go:284-332`).

### 2. Search stage (`stage_search.go`)

Refactored, behavior preserved.

- Same input (`uszips.csv`), same outputs (`brokers.jsonl`, `progress.jsonl`, `brokers_unique.{json,csv}`), same subdivision algorithm (`scrapePoint`, `subdivide`, `paginate`), same spatial dedup (`spatialDedup`).
- Behavioral change: `--workers=N` now means "N concurrent scrapers" *without* multiplying the server-facing rate, because rate is controlled by the global limiter. The README's "keep at 1" warning is removed.
- Existing `progress.jsonl` and `brokers.jsonl` from in-flight runs remain valid — same on-disk format, same resume mechanism.
- Default `--rps` for search: 0.17 req/s (= 6s/req, matching today's `--delay=6000` default behavior).

### 3. Enrich stage (`stage_enrich.go`)

New.

- **Input:** `brokers_unique.json` (default; configurable via `--input`). Errors clearly with remediation hint if missing.
- **Worker pool:** Default 4 workers. Each pulls CRDs from a buffered channel.
- **Per-broker request:** `GET https://api.brokercheck.finra.org/search/individual/{CRD}`. Same headers as search.
- **Response shape:** `hits.hits[0]._source.content` is a *JSON-encoded string*. Parse twice: outer envelope (HitData), then inner content (BrokerDetail).
- **Output:**
  - `brokers_detail.jsonl` — streaming, append-only, one parsed broker per line. Resume-safe.
  - `enrich_progress.jsonl` — one line per CRD completed. Used for resume. Distinct from search's `progress.jsonl`.
  - `enrich_errors.jsonl` — one line per CRD that failed permanently (malformed JSON, total=0). Marked "complete" so resume doesn't retry.
  - On stage end: `brokers_detail.json` (pretty array) and `brokers_detail.csv` (flat).
- **Resume:** On startup, load CRDs from `enrich_progress.jsonl` and `enrich_errors.jsonl` into a set; skip those. `--resume=false` to force re-fetch.
- **Default `--rps` for enrich:** 2 req/s. With 4 workers, expected wall-clock ≈ 620k / 2 / 86400 ≈ **3.6 days**. Workers exist to mask per-request latency variance and JSON parse time; they do not raise the aggregate rate above `--rps`.

### 4. Data model (`types.go`)

New types matching the detail payload (verified against probed real responses for CRDs 2819404 and 5634972):

```go
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
    FirmID                int                  `json:"firmId"`
    FirmName              string               `json:"firmName"`
    IAOnly                string               `json:"iaOnly"`
    RegistrationBeginDate string               `json:"registrationBeginDate"`
    RegistrationEndDate   string               `json:"registrationEndDate,omitempty"`
    FirmBCScope           string               `json:"firmBCScope"`
    FirmIAScope           string               `json:"firmIAScope"`
    IASECNumber           string               `json:"iaSECNumber,omitempty"`
    IASECNumberType       string               `json:"iaSECNumberType,omitempty"`
    BDSECNumber           string               `json:"bdSECNumber,omitempty"`
    BranchOfficeLocations []BranchOfficeLoc    `json:"branchOfficeLocations,omitempty"`
    City                  string               `json:"city,omitempty"`
    State                 string               `json:"state,omitempty"`
    Country               string               `json:"country,omitempty"`
}

type BranchOfficeLoc struct {
    DisplayOrder            int      `json:"displayOrder"`
    LocatedAtFlag           string   `json:"locatedAtFlag"`
    SupervisedFromFlag      string   `json:"supervisedFromFlag"`
    PrivateResidenceFlag    string   `json:"privateResidenceFlag"`
    BranchOfficeID          string   `json:"branchOfficeId"`
    Street1                 string   `json:"street1"`
    Street2                 string   `json:"street2,omitempty"`
    City                    string   `json:"city"`
    State                   string   `json:"state"`
    Country                 string   `json:"country"`
    ZipCode                 string   `json:"zipCode"`
    Latitude                string   `json:"latitude"`
    Longitude               string   `json:"longitude"`
    GeoLocation             string   `json:"geoLocation"`
    NonRegisteredOfficeFlag string   `json:"nonRegisteredOfficeFlag"`
    ELABeginDate            string   `json:"elaBeginDate,omitempty"`
}

type Disclosure struct {
    EventDate             string                 `json:"eventDate"`
    DisclosureType        string                 `json:"disclosureType"`
    DisclosureResolution  string                 `json:"disclosureResolution"`
    IsIapdExcludedCCFlag  string                 `json:"isIapdExcludedCCFlag"`
    IsBcExcludedCCFlag    string                 `json:"isBcExcludedCCFlag"`
    BCCtgryType           int                    `json:"bcCtgryType"`
    DisclosureDetail      map[string]any         `json:"disclosureDetail"`
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
    ApprovedSRORegistrationCount      int `json:"approvedSRORegistrationCount"`
    ApprovedFinraRegistrationCount    int `json:"approvedFinraRegistrationCount"`
    ApprovedStateRegistrationCount    int `json:"approvedStateRegistrationCount"`
    ApprovedIAStateRegistrationCount  int `json:"approvedIAStateRegistrationCount"`
}

type BrokerDetailsInner struct {
    HasBCComments                  string `json:"hasBCComments"`
    HasIAComments                  string `json:"hasIAComments"`
    LegacyReportStatusDescription  string `json:"legacyReportStatusDescription"`
}
```

`Disclosure.DisclosureDetail` is kept as `map[string]any` because the inner shape varies by disclosure category (regulatory action vs customer dispute vs civil judgment vs criminal). Disclosure category-specific parsers are out of scope — consumers can introspect.

### Flat CSV columns (`brokers_detail.csv`)

```
CRD, FirstName, MiddleName, LastName, OtherNamesCount, BCScope, IAScope,
DaysInIndustryStart, CurrentFirmName, CurrentBranchStreet, CurrentBranchCity,
CurrentBranchState, CurrentBranchZip, CurrentEmploymentCount,
PreviousEmploymentCount, ExamCategoriesList (semicolon-joined),
RegisteredStatesCount, RegisteredStatesList (semicolon-joined),
RegisteredSROsList (semicolon-joined), DisclosureCount, HasDisclosure,
HasBCComments, FetchedAt
```

The full nested data lives in `brokers_detail.jsonl` and `brokers_detail.json`; the CSV is for quick filtering and spreadsheet workflows.

## Error handling

| Condition | Action |
|---|---|
| Transport error / 5xx | Existing exponential backoff, up to `--retries` attempts |
| HTTP 429 | Trigger breaker (halve rate, 60s pause, resume), then retry |
| HTTP 200 with `hits.total = 0` | Broker no longer in BrokerCheck (deregistered/removed). Log + skip + write to `enrich_errors.jsonl` + mark complete (resume won't retry) |
| HTTP 200 with malformed `content` JSON | Log warning, write to `enrich_errors.jsonl` with raw body excerpt, mark complete (skip on resume) |
| Stage interrupted (Ctrl-C, crash, OOM) | All work-in-progress already flushed to `*.jsonl`; resume picks up where it left off |

## Testing

Three layers, light:

1. **Unit tests for parsers (`stage_enrich_test.go`)** — feed saved fixture responses (real ones captured during probing for CRDs 2819404 and 5634972) into the detail-content parser, assert struct fields populate correctly. Edge cases: broker with 0 disclosures, broker with multiple disclosures, broker with empty `currentIAEmployments`, broker with `total=0`. Pure-function tests, no network.
2. **Unit tests for adaptive breaker (`httpclient_test.go`)** — fake clock + fake `rate.Limiter`, drive synthetic 429/200 sequences, assert rate transitions match spec (halve on 429, recover by 25% after 5min clean).
3. **Smoke test (manual)** — `./brokercheck-scraper enrich --limit=20` against a small CRD list, verify it produces non-empty `brokers_detail.jsonl` with correct shape, no errors logged.

No integration test against live FINRA API in CI — too fragile and rude to FINRA.

## Backwards compatibility

- `./brokercheck-scraper` with no args defaults to `search` (existing behavior preserved exactly).
- `--dedupe-only` flag kept as alias for `dedupe` subcommand; prints a one-line deprecation hint.
- Existing `brokers.jsonl` / `progress.jsonl` / `brokers_unique.{json,csv}` formats: unchanged.
- Existing flags (`--zips`, `--out`, `--radius`, `--spacing`, `--delay`, `--retries`, `--limit`, `--resume`, `--workers`) preserved. `--delay` becomes a back-compat shim that sets `--rps = 1000/delay_ms`. New flags (`--rps`, `--input` for enrich) added without breaking existing usage.

## CLI flags reference

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
| `--delay` | `6000` | (deprecated) Mean delay in ms; equivalent `--rps` overrides |

### Enrich-specific
| Flag | Default | Notes |
|---|---|---|
| `--input` | `brokers_unique.json` | Source CRD list |

### Dedupe-specific
| Flag | Default | Notes |
|---|---|---|
| (none) | | Reads `brokers.jsonl` from `--out` |

## Expected runtime

- **Search:** unchanged from today (~10–12 hours for the country at default `--rps=0.17`).
- **Enrich:** ~620k brokers ÷ 2 req/s ≈ **3.6 days** at default settings. Adaptive breaker may pause occasionally; budget +10% headroom = ~4 days worst case.
- **End-to-end (search + enrich, sequential):** ~5 days from clean state.

## Open questions

None — all design decisions resolved during brainstorming on 2026-05-22.
