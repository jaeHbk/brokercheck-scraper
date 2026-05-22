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

The BrokerCheck website's internal search API
(`https://api.brokercheck.finra.org/search/individual`) accepts a lat/lon +
radius query and returns brokers. The scraper handles:

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

## Stage 2: enrich

The search stage returns minimal fields per broker (CRD, name, current
firm location). The enrich stage fetches `/search/individual/{CRD}` per
broker to capture:

- **Basic information** — middle name, aliases, BC/IA scope, days-in-industry start, sanctions
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
- `brokers_detail.json` — full structured array of all enriched brokers (sorted by CRD).
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
