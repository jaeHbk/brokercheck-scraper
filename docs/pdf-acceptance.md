# BrokerCheck PDF acceptance — September 28, 2026

## Decision and scope

The requested PDF functionality is implemented. The final saved input contains **37 real reports; 37 are accepted and zero have unresolved required content** with schema `1.1.0`, parser `1.2.0`, and extractor `poppler-bbox-1.1.0`. The formal first pilot also passes: an independent reviewer compared every required history and disclosure item in **20 original PDFs** with their structured JSON/CSV records and found zero omissions or incorrect groupings. Five additional retained reports that initially needed review were independently examined; their corrected disclosure results match the originals.

The 100-report expansion in the execution handoff is incomplete. The user's latest direction limits further work to what is necessary for Prof. Iannarone's three stated requirements, so no additional reports were downloaded solely to reach that engineering sample size. This sample proves the reviewed layouts and the current selected set, not universal parser coverage. Full-population collection was not authorized or performed.

## Requirement-by-requirement results

### R1 — Detailed Report PDFs as the source

**PASS for the 37-report accepted input.** Every accepted CRD has retained immutable PDF bytes, an exact SHA256 reference, a positioned-text document, versioned parsed result, validation record, and linked export rows. Downloads reject HTML/error bodies and incomplete or identity-mismatched PDFs. Resume verifies hashes and versions; refresh retains older snapshots; incomplete attempts remain explicit. Offline tests cover interrupted downloads, retries, cancellation, corrupt artifacts, and atomic publication.

Across the authorized live attempts, **42 distinct CRDs** were tried. The 37 retained reports now pass. Five other CRDs—`4435504`, `5716633`, `6755615`, `7555243`, and `7805971`—returned HTTP 403 and remain recorded as `access_failure` in the manifest. No access-control workaround was used, and none of those failures is labeled an accepted PDF extraction.

### R2 — every registration and employment history entry, including prior firms

**PASS for the 20 independently reviewed originals; machine validation accepts all 37 retained reports.** The final 37-report exports contain **140 registration records, 173 employment records, and 1,438 ordered history fields**. Current and prior BD/IA registrations remain distinct from employment. Firm/employer, firm CRD, location, position, investment-related value, original date and precision, blank and repeated fields, multiline values, and physical-page evidence survive in linked records. Eleven differing report-summary firm locations are preserved separately from the detailed branch locations. Other Business Activities are not misclassified as employment.

Independent original-to-export review of the first 20 found **72 registrations, 97 employments, and 784 history fields** with zero missing or incorrectly grouped required items. The remaining 17 pass the parser's section, word-coverage, reference, and consistency checks; their complete histories have not all been manually compared to original pages.

### R3 — disclosure counts and every field in every disclosure record

**PASS for the 20 independently reviewed originals and the five independently reviewed new disclosure layouts; machine validation accepts all 37 retained reports.** The final exports contain **eight distinct events, 11 reporting-source versions, 145 ordered disclosure fields, and 53 reported count cells**. Sources remain nested within an event and do not inflate event counts. All comparable numeric category and status cells reconcile, while printed `N/A` remains null rather than becoming zero.

Reviewed positive layouts include customer disputes, regulatory events, a criminal event, and a termination event. The termination report, CRD `4960324`, preserves its exact `Employment Separation After Allegations` heading, one event, separate Firm/Broker sources, all ten source fields, and the period present only in the Firm allegations. Its summary `Termination` category and `Final` matrix cell reconcile without inventing a printed detail status. Four other newly reviewed originals contain explicit `No` disclosures and a distinct 35-word investment-adviser guidance panel; the exact panel is recognized as guidance, while altered text remains blocking. The other new accepted no-disclosure reports have machine-checked explicit-zero evidence.

## Evidence and artifact locations

- Final selected input: `run/pdf-live-check/retained-37-crds.json`; SHA256 `1b501b248ab4f492105eab44cd9d43c9bdf354d459f875cf23a14c767759a2cd`.
- Final run summary: `run/pdf-live-check/reports/run_summary.json`.
- Final atomic export generation: `run/pdf-live-check/reports/exports/20260928T151608.017607000-73937/`.
- Current-generation pointer: `run/pdf-live-check/reports/exports/current.json`.
- Immutable originals: `run/pdf-live-check/reports/pdf/<crd>/<sha256>.pdf`.
- Positioned text and parsed archives: `run/pdf-live-check/reports/text/` and `run/pdf-live-check/reports/parsed/`.
- All attempts, including the five access failures: `run/pdf-live-check/reports/manifest.jsonl`.
- Independent 20-report original review: `docs/pdf-review-2026-09-19.md`.
- Independent new-layout annotations and final comparison: `docs/pdf-review-pilot2-anomalies-2026-09-28.md`.
- Field definitions and backend: `docs/pdf-data-dictionary.md` and `docs/pdf-backend.md`.

The final generation contains `brokers_pdf.jsonl` and `brokers_pdf.csv` (**37 brokers each**), `registration_history.csv` (**140**), `employment_history.csv` (**173**), `history_fields.csv` (**1,438**), `disclosures.csv` (**8**), `disclosure_sources.csv` (**11**), `disclosure_fields.csv` (**145**), `disclosure_counts.csv` (**53**), and `reports_validation.jsonl` (**37**). CSV counts are logical records parsed with a CSV reader; multiline cells make physical line counts misleading. Child rows carry report hashes, version provenance, parent IDs, and physical-page/word evidence. Partial records remain in the parsed archive and are excluded from accepted child exports.

## Verification and resource estimate

After the final parser change, `go test ./... -count=1`, `go vet ./...`, and `go build -o ./run/brokercheck-scraper .` pass. The offline final reparse exited 0 with 37/37 accepted. An earlier integrated `go test -race ./... -count=1` also passed before the final disclosure-layout change; it was not repeated under the request to keep testing focused. The final parser version was raised to `1.2.0` so ordinary resume cannot mistake earlier `1.1.0` parses for current results. `git diff --check` passes. Tests use local servers and retained fixtures, not live FINRA calls.

The retained 37 PDFs total **3,152,171 bytes**, averaging **85,194 bytes/report**. The final offline reparse took **2.91 seconds** internally (**12.70 reports/second**). A bounded live 20-report acquisition batch took **95.72 seconds** internally at the configured 0.2 requests/second; its measured peak resident size was **23,379,968 bytes**. The integrated local-server checks exercise interruption and resume.

The existing saved population has **8,415 distinct CRDs** in `run/existing-population-crds.json` (SHA256 `d75ba73140823393733d760ddf62fb722f8982c412e8235a7b01dde11d584b6a`). At 0.2 requests/second, one attempt per CRD implies at least **11.69 hours**; five attempts for every CRD would be **58.44 hours** before processing overhead. At the observed mean PDF size, one snapshot for all 8,415 would be about **0.717 GB** of PDF bytes. Retained refreshes, extracted text, parsed results, exports, retries, and filesystem overhead justify a **10 GB free-space reserve**. These are engineering estimates from this sample, not a promise of full-run throughput or source availability.

Proposed full-run command, **not executed**:

```sh
./run/brokercheck-scraper reports --out=run/pdf-full --input=/Users/jaehunbaek/Documents/40_Projects/Software/brokercheck-scraper/run/existing-population-crds.json
```

Before claiming the handoff's separate 100-report pilot, obtain and validate 63 additional accepted reports, compare each new layout/anomaly with its original, and manually review the specified fixed-seed sample. The five HTTP 403 outcomes remain unresolved for those CRDs. No full-population collection or dataset publication was performed.
