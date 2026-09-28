# Positioned PDF backend and fixture provenance

This file records the September 12 foundation qualification and its then-current acquisition state. For the final September 28 implementation, parser `1.2.0`, 37 accepted retained reports, and current coverage limits, see `docs/pdf-acceptance.md`. The historical three-attempt blocker and category-coverage statements below describe the foundation gate only.

Qualified on September 12, 2026 with Poppler **26.05.0** (`pdfinfo version 26.05.0`, `pdftotext version 26.05.0`, `pdftoppm version 26.05.0`). Extractor interface version: `poppler-bbox-1.0.0`. Go requirement: 1.25.3.

The adapter invokes `pdfinfo` and `pdftotext -bbox-layout -enc UTF-8` directly through context-aware subprocesses, without a shell. `PDFINFO` and `PDFTOTEXT` can point to pinned binaries; otherwise PATH is used. The desktop runtime's bundled pdftotext sibling is also discovered because that binary is not on its default PATH. For this acceptance environment it is `/Users/jaehunbaek/.cache/codex-runtimes/codex-primary-runtime/dependencies/native/poppler/poppler/bin/pdftotext`.

PDF pages and deterministic word/line IDs use physical one-based order. The pinned real PDF has 14 physical pages, including a cover and guidance page: physical3 is printed1; physical10 is printed8; physical13 is printed11. Bounds are points from the top-left origin. Poppler flow order and line grouping are preserved; parsers use coordinates to reconstruct columns. The original contains overlapping/repeated Other Business Activities text, including off-page coordinates; preserving those coordinates is intentional. Rendered appearance and extraction output disagree on some clipped OBA content; OBA structured extraction is outside the required history fields. No OCR is attempted. Empty-text pages cause blocking review issues, even if they might be decorative: the first release is conservative.

`DetectReportSections` determines independent complete regions from section headings and geometry. Its canonical names are `current_registration`, `registration_history`, `employment_history`, `disclosure_summary`, `disclosures`. Returned regions include unfamiliar content; parsers must account for all of it with structured evidence or specifically permitted ignored spans. Table-of-contents promises are checked independently during extraction. Broker identity is taken from the cover/summary, distinct from firm CRDs.

`RecognizedReportVariant` identifies the tested FINRA category-summary layout from its cover, contents, summary, history, disclosure-matrix guidance, and detail heading. That layout prints category counts rather than an all-category total. Recognition alone never grants acceptance: all category cells and complete field coverage must reconcile, and the reported total remains null.

## Frozen fixture expectations

`testdata/pdf_expected/*.json` were annotated before history/disclosure parsers were implemented. Real fields were transcribed from rendered physical pages3,10,12,13 and independently spot-checked by the coordinator. Wrapped labels/values are intentional; unlabeled current-firm summary values receive documented semantic labels. Meaningful source line breaks must survive; normalized scalar strings can supplement them. Source-label metadata is separate from the twelve disclosure fields.

- `1691670.pdf`: real; SHA256 `7eb32c9390001378a37398eaae209e42700b4f889d1f698efde97f7b99c0ee54`; 104544 bytes; 14 pages. Two current registrations, one prior registration, two employments, one Customer Dispute event, one Broker source, twelve disclosure fields, twenty-two history fields. Real blank values and wrapped rows are covered. Matrix cells: Pending0, Final1, OnAppeal N/A. Category total1 appears on summary; no all-category total exists.
- `synthetic-no-disclosures.pdf`: visibly labeled synthetic; explicit no registrations and no disclosures, one separate employment with `N` investment-related value.
- `synthetic-multi-source.pdf`: visibly labeled synthetic; two events, three source versions, eleven fields including one event-level field. Same-label Broker versions, repeated/unknown labels, a blank labeled field, a subsection, and a narrative across physical pages2–3. Source-authored expectations and all pages visually inspected. Explicit category and overall counts are separate.

All hashes, expected record counts, real-versus-synthetic coverage, and remaining categories are in `testdata/pdf_coverage.json`. The fixture input list includes synthetic test IDs and **must never be used for live acquisition**. `testdata/pdf_inputs/live-attempts.json` is the exact real attempted list; `acquisition_log.jsonl` records every attempt.

## Acquisition and pilot blocker

At most three distinct real CRDs were attempted. 1691670 returned200. 2819404 and5634972 each returned403 with at least five seconds between requests. Live acquisition stopped after these sustained access rejections, without user-agent/proxy changes or access-control workarounds. No other live requests were made by the fixture agent. Thus only one real report is retained, and neither the20-report nor100-report pilot can pass. Synthetic fixtures develop missing cases; they do not establish real multiple-event/source/continuation or no-disclosure coverage. Regulatory, criminal, civil-judicial, termination, and financial categories remain unverified.

## Offline adapter verification

`go test report_text.go report_text_test.go types_report.go report_contract.go` exercises the actual Poppler backend, deterministic extraction, physical/printed pages, pinned hashes, identity rejection, cancellation, malformed XML, missing promised sections, and unfamiliar required-region words. Missing Poppler causes test failure, never a silent skip. Whole-package tests are coordinator-owned and run after integration. Fixtures never contact FINRA during tests.
