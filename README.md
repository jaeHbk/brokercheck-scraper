# FINRA BrokerCheck API Scraper

A Go scraper that pulls broker records from FINRA's BrokerCheck API for the
entire US, working around the API's rate limits and pagination cap.

## What it does

The BrokerCheck website's internal API
(`https://api.brokercheck.finra.org/search/individual`) accepts a lat/lon +
radius query and returns brokers. Two things make a full US scrape hard:

1. **Pagination cap.** The API rejects requests where `start + nrows > 9000`.
   So in dense metros, a single 25-mile-radius query returns ~120k brokers
   total but lets you read only the first 9000.
2. **Rate limiting.** Aggressive request rates produce 429s and IP blocks.

This scraper handles both:

- **Spatial dedup** of US zip codes — a 33,782-zip CSV is reduced to ~1,400
  representative search points whose 25mi circles tile the country with
  minimal overlap.
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

- ~1,400 spatial-dedup points × ~3 pages avg = ~4,200 base requests
- Adaptive subdivision adds ~1,000–2,000 more in dense metros
- **Total ≈ 10–12 hours** for a complete US scrape

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
