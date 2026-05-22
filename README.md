# FINRA BrokerCheck API Scraper

A Go scraper that pulls broker records from FINRA's BrokerCheck API for the
entire US, working around the API's rate limits and pagination cap.

## Recent changes (2026-05)

The scraper was rewritten from a single-coordinate fetcher into a full-US
workflow. Highlights:

- **Adaptive radius subdivision** added — discovered via probing that the
  API hard-caps pagination at `start + nrows ≤ 9000`. The old code silently
  hit this cap and dropped data in dense metros (NYC at 25mi reports
  122,223 brokers but only the first 9000 are reachable in one query).
  Subdivision recurses up to depth 8 (~0.2mi cells) when an area exceeds
  the cap.
- **Spatial dedup** of zip codes added — 33,782 zips → 1,382 representative
  search points (96% fewer redundant API calls), via haversine distance +
  bucketed grid lookup.
- **Polite single-IP rate limiting** — 6s jittered delay, exponential
  backoff with `Retry-After` honoring, rotating User-Agent / Referer /
  Origin headers (replaces the no-delay 10-worker pool that risked being
  blocked).
- **Streaming output + crash-resilient resume** — `brokers.jsonl` +
  `progress.jsonl` replace in-memory accumulation; `--resume` skips
  completed points.
- **CSV header bug fixed** — old code's header-skip was commented out and
  parsed the literal string `"zip"` as a coordinate.

Smoke-tested on a single NYC zip (10001) at 25mi radius: **89,121 unique
brokers** via 23 recursive subdivisions, vs ~9,000 the old code would have
captured.

## What it does

The BrokerCheck website's internal API
(`https://api.brokercheck.finra.org/search/individual`) accepts a lat/lon +
radius query and returns brokers. Two things make a full US scrape hard:

1. **Pagination cap.** The API rejects requests where `start + nrows > 9000`.
   So in dense metros, a single 25-mile-radius query returns ~120k brokers
   total but lets you read only the first 9000.
2. **Rate limiting.** Aggressive request rates produce 429s and IP blocks.

This scraper handles both:

- **Spatial dedup** of US zip codes — a 33,782-zip CSV is reduced to 1,382
  representative search points whose 25mi circles tile the country with
  minimal overlap (at default `--spacing=40`).
- **Adaptive radius subdivision** — when a query returns more brokers than
  the API will paginate through, the area is recursively split into 4
  smaller circles until each contains ≤9000 brokers (or max depth of 8 ≈
  0.2mi cells).
- **Polite single-IP rate limiting** — jittered ~6s mean delay, exponential
  backoff on 429/5xx, rotating User-Agent + Referer headers.
- **Streaming output + checkpoint/resume** — every broker is appended to
  `brokers.jsonl` and every completed search point is logged to
  `progress.jsonl`. Re-running with `--resume` (default) skips already-done
  points, so a multi-hour scrape survives interruptions.
- **Final dedup pass** — `brokers.jsonl` is deduped by CRD into
  `brokers_unique.json` and `brokers_unique.csv`.

## Quick start

```sh
go build .
./brokercheck-scraper            # full scrape with sensible defaults
./brokercheck-scraper --limit=5  # smoke test (5 search points)
```

## Flags

| Flag | Default | Notes |
|---|---|---|
| `--zips` | `uszips.csv` | Path to zip CSV. Must have `zip,lat,lng` columns. |
| `--out` | `.` | Output directory. |
| `--radius` | `25` | Initial search radius in miles. |
| `--spacing` | `40` | Min miles between representative zips (spatial dedup). 0 disables. |
| `--delay` | `6000` | Mean delay between requests in ms (jittered ±50%). |
| `--retries` | `5` | Max retries on 429/5xx with exponential backoff. |
| `--limit` | `0` | Limit number of search points (0 = no limit). For smoke testing. |
| `--resume` | `true` | Skip points listed in `progress.jsonl`. |
| `--workers` | `1` | Concurrent workers. Keep at 1 for single-IP politeness. |
| `--dedupe-only` | `false` | Skip scraping; just dedupe existing `brokers.jsonl`. |

## Expected runtime

Single-IP with default 6s delay:

- 1,382 spatial-dedup points × ~3 pages avg = ~4,200 base requests
- Adaptive subdivision adds ~1,000–2,000 more in dense metros
- **Total ≈ 10–12 hours** for a complete US scrape

The single NYC zip smoke test took ~9 hours alone (subdivision storm in
Manhattan); most of the country is sparse and finishes much faster.

If you have residential proxies, run with higher `--workers` and lower
`--delay`. The current code uses one stdlib HTTP client; bring your own
proxy support by editing `httpClient` initialization.

## Output files

- `brokers.jsonl` — every broker record streamed during scraping (with
  duplicates from overlapping subdivisions).
- `progress.jsonl` — one line per completed search point. Used for resume.
- `brokers_unique.json` — array of deduped brokers (by CRD).
- `brokers_unique.csv` — flat CSV: CRD, FirstName, LastName, FirmName,
  FirmCity, FirmState, FirmZip.

## Resume / restart

Just rerun the same command. Already-scraped search points are skipped via
`progress.jsonl`. To start fresh, delete `brokers.jsonl` + `progress.jsonl`.

## Re-dedup without rescraping

```sh
./brokercheck-scraper --dedupe-only
```

## Limits and caveats

- **Manhattan-class density.** With max subdivide depth 8, sub-circles get
  to ~0.2mi. If a 0.2mi circle still exceeds the 9000 cap, you'll see a
  `WARN` line and lose the brokers ranked below #9000 by API score in that
  cell. Watch the logs for `max subdivide depth reached`.
- **No proxy rotation built in.** Going faster than the default 6s/request
  on a single residential IP risks being blocked by FINRA's WAF.
- **Sort coupling.** Subdivision relies on the API consistently sorting by
  geographic relevance + score. If FINRA changes the sort behavior, dedup
  via CRD still works, but coverage guarantees may shift.

## Resource

[FINRA BrokerCheck](https://brokercheck.finra.org/)
