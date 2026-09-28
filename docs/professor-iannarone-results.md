# BrokerCheck PDF update — results for Prof. Iannarone

The scraper now downloads and retains BrokerCheck Detailed Report PDFs, extracts every registration and employment entry shown in each accepted report, and extracts disclosure counts and every labeled field of each disclosure record. It preserves separate reporting-source versions within a single event. Each structured record links to its immutable PDF, report hash, original page/word evidence, and parser version. Existing website detail data remain separate.

The current saved validation input contains **37 real reports, all accepted** by the final offline parser and validation run. Their exports contain **140 registration records, 173 employment records, 1,438 history fields, eight distinct disclosure events, 11 reporting-source versions, 145 disclosure fields, and 53 reported disclosure-count cells**. An independent reviewer compared the first **20 original PDFs** field by field with their JSON and CSV records and found no omitted or incorrectly grouped required item. Five additional reports with new disclosure layouts were inspected independently; after narrow parser fixes, their exported events, sources, fields, and counts match the originals. The remaining 12 newly accepted reports passed machine completeness and traceability checks but have not all received full manual comparison.

The new termination layout illustrates why source versions are separate: one event in CRD 4960324 has both Firm and Broker accounts, five fields in each, and a small punctuation difference between their allegations. The export retains both accounts while counting one event. Four other newly reviewed reports explicitly answer “No” to disclosures and contain standard adviser-information guidance; that guidance is excluded from broker disclosure fields only when its full verified wording matches.

Safe retries, interrupted-download recovery, immutable refresh history, hash-verified resume, corrupted-artifact rejection, incomplete-content review, count reconciliation, and atomic exports are implemented. The final Go tests, static check, build, and 37-report offline reparse pass. The schema/parser/extractor versions are `1.1.0` / `1.2.0` / `poppler-bbox-1.1.0`.

**Coverage limit:** 42 distinct live CRDs were attempted; 37 were retained and accepted. Five others returned HTTP 403 and remain explicit access failures. The separate engineering target of 100 reviewed reports was not completed under the direction to finish only Prof. Iannarone's stated functionality. No full-population collection was run, so this result does not assert coverage of every broker in the saved population or universal correctness for unseen layouts.

Evidence and deliverables:

- [Requirement results and acceptance record](pdf-acceptance.md)
- [Independent 20-report original review](pdf-review-2026-09-19.md)
- [Independent new-layout review](pdf-review-pilot2-anomalies-2026-09-28.md)
- [PDF data dictionary](pdf-data-dictionary.md)
- Final run summary: `run/pdf-live-check/reports/run_summary.json`
- Final linked exports: `run/pdf-live-check/reports/exports/20260928T151608.017607000-73937/`
- Retained original PDFs and manifest: `run/pdf-live-check/reports/pdf/` and `run/pdf-live-check/reports/manifest.jsonl`

No full-population collection or dataset publication was performed.
