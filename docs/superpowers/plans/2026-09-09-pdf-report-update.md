# BrokerCheck PDF collection and extraction plan

Date: September 9, 2026
Status: Ready for the foundation gate in the agent execution handoff; implementation and collection have not started.
Code baseline: `3dfb8f5`.

Execution contract: read [the agent execution handoff](2026-09-12-pdf-agent-execution.md) alongside this requirements plan. It fixes interfaces, agent assignments, command behavior, output publication, and acceptance gates. It takes precedence over tentative implementation choices below, including export locations. Recheck the actual checkout before changing code.

## Objective and requirements

Update the scraper to satisfy Prof. Iannarone’s email, reproduced in the task “Update BrokerCheck PDF scraping”:

1. Download and retain the Detailed Report PDF for every broker in the collection input, and extract the requested data from those PDFs.
2. Extract every entry in Employment History and Registration History, including prior firms.
3. Extract disclosure counts and every field in every disclosure record.

The PDF is the primary source for these requirements. Keep the existing website detail responses as supplementary information and a comparison source, with their provenance separate.

“All” means all information available in each collected report. It does not establish a lifetime employment record or complete national broker coverage. The email does not define a new broker population, so use the project's selected CRD input and record its provenance. Discovery coverage is a separate question.

The [example report for CRD 1691670](https://files.brokercheck.finra.org/individual/individual_1691670.pdf) separates prior registrations from employment history. Employment rows include position and investment-related status. Its disclosure guidance explains that several reporting sources can provide versions of one event. These distinctions require separate records and event counts that do not count source versions as additional events. Pin a downloaded snapshot before creating test expectations because the live report can change.

## Current code and gaps

- `main.go` dispatches `search`, `dedupe`, and `enrich`. `stage_search.go` discovers broker IDs; `stage_enrich.go` fetches the website's internal detail endpoint.
- `types_detail.go` models current and prior BD/IA registrations and disclosure records. It has no dedicated PDF Employment History model. Nested disclosure detail fields are retained dynamically, but that does not prove PDF field coverage.
- `httpclient.go` provides throttling and retries. It currently requests JSON and reads each response into memory; PDF downloading needs a binary streaming path with validation.
- `stage_enrich.go` exports a broker summary CSV, including disclosure count, rather than one row per employment or disclosure record. Its current/prior employment counts use BD arrays only.
- Existing enrichment progress treats parsing errors and not-found results as completed. Several persistence calls ignore write errors. Reusing that completion behavior for PDFs would permit silent gaps.
- The repository includes basic broker search output and detail-response fixtures, but no PDF fixtures or full PDF-derived dataset. Existing README runtime and storage estimates cover the old workflow and must be replaced with measured PDF estimates.

## Proposed workflow

Retain the existing commands and add a `reports` command that consumes a deduplicated CRD list. Internally separate download, extraction, validation, and export so saved PDFs can be reprocessed without another network request.

Proposed command behavior:

- `reports --input=... --out=... --limit=20`: download, parse, validate, and export a sample.
- `reports --reparse`: process locally saved report versions with the current parser, without network access.
- `reports --retry-failed`: retry failed or unresolved items explicitly.
- `reports --refresh`: fetch a new snapshot while retaining earlier PDF versions.

Reuse the common input, output, worker, rate, retry, and resume options. Document input-path resolution precisely and validate positive worker counts, retry counts, and rate settings. These commands and flags are proposed, not currently available.

## Implementation sequence

### 1. Establish fixtures and the extraction contract

Create a small, deliberately varied corpus, starting with CRD 1691670. Include reports with no disclosures, multiple disclosures, multiple reporting sources for an event, long narratives crossing pages, prior registrations, BD/IA registrations, and different employment layouts. Seek examples of each disclosure category; explicitly list categories not yet covered.

Manually annotate all required rows, event boundaries, source versions, field labels, values, and page references in each fixture. Use these independent annotations as expected results. Existing JSON fixtures are comparison inputs, not substitutes for PDF fixtures.

Evaluate a layout-aware text extraction backend, initially considering Poppler behind a Go interface. Choose and pin the dependency only after verifying reading order, multiline fields, page boundaries, installation requirements, and reproducibility on the corpus. Preserve page text and coordinates where available. Image-only or unreadable pages must produce an explicit review status; assess OCR only if encountered.

Deliverables: `testdata/pdf/`, expected extraction fixtures, and a field dictionary describing required fields, nulls, source labels, dates, and identifiers.

Acceptance: every required field in the annotated corpus has a representation, including unfamiliar and repeated labels.

### 2. Download and retain reports reliably

Add `stage_reports.go`, `report_download.go`, and `types_report.go`; extend `main.go` and the HTTP client where needed.

- Construct the Detailed Report URL from a validated CRD. Download to a temporary file, validate the response and PDF structure, then rename atomically.
- Record CRD, source URL, retrieval time, HTTP outcome, byte count, SHA-256 hash, and page count. Verify the report's CRD against the requested CRD during extraction.
- Save immutable PDFs under `reports/pdf/<crd>/<sha256>.pdf`; associate every extraction with that exact hash. Refreshes retain prior versions.
- Reuse throttling and retry behavior with PDF-appropriate headers, bounded resource use, timeouts, and cancellation-aware waits. Honor both numeric and HTTP-date Retry-After values. Ensure the limiter's floor never exceeds the configured ceiling.
- Distinguish unavailable reports, access failures, temporary network failures, invalid responses, and parse failures. None count as successful extraction.

Acceptance: interrupted downloads never appear as valid PDFs; retries and resume preserve existing files; HTML error pages are rejected; disk-write failures propagate.

### 3. Extract registration and employment history

Add `report_text.go` and `report_history.go`. Parse sections using headings, page structure, and continuation rules rather than a single regular expression over the whole document.

- Model current registrations and previous registrations separately, preserving BD/IA scope, firm name and CRD, dates, and locations when present.
- Model Employment History independently: employer, position, investment-related indicator, dates, location, and every additional labeled field encountered.
- Preserve raw date strings and date precision. Do not invent a day for month-only dates or turn “Present” into a confirmed current-employment fact.
- Preserve row order, raw text, original labels, and source pages. Handle wrapped firm names, repeated table headers, and rows spanning pages.
- Retain adjacent Other Business Activities text separately so it is not misclassified as employment. Structured extraction of that additional section is not an email requirement.
- Represent section states explicitly: present, explicitly empty, absent, or unreadable. An absent section is not automatically an empty history.

Acceptance: all annotated registration and employment rows match field by field, with no accidental merging between histories or scopes.

### 4. Extract disclosures without losing fields

Add `report_disclosures.go`. Separate disclosure events from their reporting-source versions.

- Extract the report summary counts and disclosure matrix, retaining category/status totals as reported.
- Capture event type, status, identifier if provided, and every reporting-source version. Preserve all label/value pairs, narratives, subrecords, comments, and continued content.
- Store fields as ordered entries with section paths and occurrence indexes, rather than a map that can overwrite repeated labels. Add normalized common fields for analysis while retaining original labels and values.
- Keep source versions even when they disagree. Assign deterministic local IDs scoped to the report hash when the report does not supply an identifier; do not imply those IDs match events across refreshed reports.
- Track unparsed content and ambiguous boundaries. Retaining raw text alone does not satisfy structured field extraction; unresolved required content must remain flagged.

Acceptance: every annotated event, source version, and field survives extraction, including multiline values and repeated labels. Distinct-event counts reconcile with the report totals and category/status matrix where available.

### 5. Add validation, checkpoints, and analysis-ready exports

Add `report_validate.go`, `report_store.go`, and `report_export.go`.

Keep PDF processing progress separate from `enrich_progress.jsonl`. Track download success, parse success, validation result, retryable failure, unavailable report, and manual-review status. Persist outputs successfully before recording completion. Resume must verify the saved artifact and parser version, recover partial writes, and retry failures without creating duplicate accepted records. Return a clear incomplete-run status when unresolved items remain.

Proposed outputs under the selected output directory:

- `reports/pdf/`: retained originals, organized by CRD and content hash.
- `reports/text/`: page-level extracted text, keyed by PDF hash and extractor version.
- `reports/manifest.jsonl`: report metadata, versions, processing outcomes, and provenance.
- `brokers_pdf.jsonl`: full nested PDF extraction, including ordered disclosure fields and raw evidence.
- `brokers_pdf.csv`: one broker/report summary, counts, report reference, and validation status.
- `registration_history.csv` and `employment_history.csv`: one row per history entry.
- `disclosures.csv`: one row per event; `disclosure_sources.csv`: one row per source version; `disclosure_fields.csv`: one row per labeled field, linked to event and source.
- `reports_validation.jsonl`: missing fields, unparsed regions, count mismatches, and optional PDF-versus-detail discrepancies.

All child records carry CRD, report hash, stable within-report IDs, and page references. Include schema and parser versions. Export multiline text with correct CSV quoting and represent unknown counts distinctly from zero. Keep a documented policy for selecting a report version in summary exports while preserving all snapshots.

Do not combine PDF counts with existing API counts or silently overwrite API fields. Use streaming or per-report storage so finalization does not require loading the entire PDF dataset into memory. Keep bulk generated artifacts outside Git; retain a small curated test corpus.

Acceptance: every exported row traces back to a retained PDF; rerunning produces no duplicate logical records; record totals and child references reconcile; unavailable and incomplete reports remain visible.

### 6. Test, pilot, and document the rollout

Run the existing Go tests as a baseline during implementation, then add meaningful offline tests for the new contract:

- Full field comparisons against manually annotated PDF fixtures.
- Unknown and repeated labels, source-version boundaries, page continuations, empty sections, and count mismatches.
- Local HTTP tests for valid PDFs, 404s, access errors, throttling, truncation, timeouts, and retry exhaustion.
- Failure/restart tests for interrupted writes, disk errors, parser upgrades, refreshes, and missing saved artifacts.
- Export round trips for multiline narratives, nulls, foreign-key consistency, and deterministic output.
- Regression checks for the existing search, dedupe, and enrichment commands.

First run approximately 20 varied reports and manually compare every required field against the saved originals. A passing count check alone is insufficient. Expand to approximately 100–200 reports to evaluate unseen layouts, throughput, retry rate, parser failures, memory, and disk use. Sample sizes are proposed engineering checkpoints, not statistical proof of universal completeness.

Only proceed to the selected full input after the manual sample passes and unresolved cases are reported explicitly. Estimate run time and storage from measured PDF sizes and end-to-end throughput; include retained versions, extracted text, exports, retries, and headroom.

Update `README.md` with installation, commands, output schemas, resume/reparse/refresh behavior, validation interpretation, and measured resource estimates. Add a small downloadable example dataset or repository fixture export and a data dictionary for the research team. Do not describe the selected CRD list as nationwide complete without a separate coverage assessment.

## Completion criteria

- Every input CRD has a retained, traceable PDF and validated extraction, or an explicit unresolved/unavailable outcome.
- Every required employment and registration row in the validated corpus is captured.
- Every disclosure event, reporting-source version, and field in the validated corpus is captured; event counts reconcile independently of source-version counts.
- Parse and persistence failures cannot be recorded as successful completion.
- A sample has been checked manually against the original PDFs; known layout/category gaps are documented.
- The research team receives retained PDFs, complete structured exports, a field dictionary, and a collection/validation summary.

Recommended order: fixtures and contract → reliable download → histories → disclosures → validation and exports → pilot and rollout. Resolve extraction-backend and layout questions in step 1 before committing to a full-run schedule.
