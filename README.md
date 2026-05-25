# FINRA BrokerCheck API Scraper

A Go scraper that pulls broker records from FINRA's BrokerCheck API for the
entire US, then enriches each broker with detailed registration, exam,
employment-history, and disclosure data.

## Subcommands

```sh
./brokercheck-scraper search    # Stage 1: discover all CRDs by zip-radius search
./brokercheck-scraper enrich    # Stage 2: fetch full detail per CRD
./brokercheck-scraper dedupe    # Re-run final dedup pass on brokers.jsonl
```

Running with no subcommand defaults to `search` (back-compat).

**Flag placement:** Put flags AFTER the subcommand, e.g.
`./brokercheck-scraper enrich --rps=2 --workers=4`. Putting flags before the
subcommand also works, but stray flags after the subcommand are rejected with
an error to avoid silently dropping them.

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

### Outputs

- `brokers.jsonl` — every broker emitted during the crawl (with overlap duplicates from subdivisions).
- `progress.jsonl` — one line per completed search point. Used for resume.
- `brokers_unique.json` / `brokers_unique.csv` — deduped by CRD; consumed by the enrich stage.

## Stage 2: enrich

The search stage returns minimal fields per broker (CRD, name, current
firm location). The enrich stage fetches `/search/individual/{CRD}` per
broker to capture:

- **Basic information** — middle name, aliases, BC/IA scope, days-in-industry start, sanction summary (e.g. permanent bar)
- **Current employments** (BD + IA) — full firm names, branch addresses, SEC numbers
- **Previous employments** (BD + IA) — work history with start/end dates
- **Disclosures** — regulatory actions, customer disputes, criminal/civil events with full allegations and resolutions
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
- `enrich_progress.jsonl` — completed CRDs (used for resume). Errored CRDs are also recorded here so resume skips them.
- `enrich_errors.jsonl` — CRDs that returned no data or malformed responses, with reason.
- `brokers_detail.json` — full structured array of all enriched brokers (sorted by CRD).
- `brokers_detail.csv` — flat CSV with the most-asked-for fields and counts.

## Runtime and dataset size for a full US run

For roughly 620,000 registered US brokers (FINRA's public count):

| Stage | Wall-clock | Notes |
|---|---|---|
| `search` | ~10–12 hours | Discovers every CRD in the country |
| `enrich` | ~3.6–4 days | Fetches detail per CRD at default 2 req/s |
| **End-to-end** | **~5 days** | From a clean state |

Expected output sizes (estimates from sampled records — brokers with many disclosures can be 30–50 KB each, so the actual JSON can land anywhere in the 3.5–5 GB range):

| File | Approx. size |
|---|---|
| `brokers_detail.jsonl` (compact, streaming) | ~2.8 GB |
| `brokers_detail.json` (pretty, sorted) | ~4 GB |
| `brokers_detail.csv` (flat) | ~210 MB |
| `enrich_progress.jsonl` | ~50 MB |

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

### Dedupe / back-compat
| Flag | Default | Notes |
|---|---|---|
| `--dedupe-only` | `false` | Deprecated alias for `dedupe` subcommand |

## Resume / restart

Just rerun the same command. Already-completed items are skipped via
`progress.jsonl` (search) or `enrich_progress.jsonl` (enrich).

## Running on your local machine

A full run is multi-day, so it's worth setting up to survive sleep, network
drops, and other interruptions. Some practical tips:

- **Disk space.** Reserve at least 10 GB free for the full run. The compact
  JSONL grows continuously, the pretty JSON is written once at the end, and
  intermediate files sit alongside.
- **Don't sleep your machine mid-run.** macOS/Windows sleep will pause the
  process. On macOS use `caffeinate -i ./brokercheck-scraper enrich`. On
  Linux either run inside `tmux`/`screen` or use `nohup ./brokercheck-scraper enrich &`.
  If you do close the laptop accidentally, just rerun the same command —
  resume support picks up where it left off.
- **Monitor progress.** In a second terminal:
  `wc -l /path/to/out/brokers_detail.jsonl` shows live broker count.
  Useful for spotting if the rate limiter has been throttled.
- **Stable network helps.** A flaky connection produces lots of transient
  errors; retries handle them but slow the run. Wired or strong wifi is best.
- **Don't open the 4 GB JSON in a text editor.** Use `jq` for queries,
  e.g. `jq -c '.[] | select(.disclosures | length > 0)' brokers_detail.json`.
  For browsing, the 210 MB CSV opens in Excel/Numbers/Google Sheets.
- **Smoke test before the full run.** First confirm the tool works on your
  machine with a tiny job: `./brokercheck-scraper enrich --limit=20 --resume=false`.
  Should finish in under a minute and produce all the output files in
  miniature.
- **You don't have to do search and enrich back-to-back.** They're separate
  commands; you can run `search` overnight, verify the output, then start
  `enrich` the next day. `enrich` reads `brokers_unique.json` produced by
  search and goes from there.
- **If you stop mid-run** (Ctrl-C, crash, reboot), nothing is lost. Just
  rerun the same command and the tool skips already-completed work.

## Resource

[FINRA BrokerCheck](https://brokercheck.finra.org/)
