# BrokerCheck update: executable agent handoff

Prepared September 12, 2026. Status: ready to begin the foundation gate; no implementation or collection completed by this handoff.

This document and `2026-09-09-pdf-report-update.md` are the complete implementation brief. This document resolves tentative choices in the earlier plan. Paths below are relative to the repository root. The verified checkout was at `3dfb8f5`, with the requirements plan untracked. Recheck the checkout and applicable AGENTS.md instructions before editing; preserve existing user changes.

## 1. Required outcome and authorized execution boundary

The email requires: (R1) download, retain, and extract from every selected broker's Detailed Report PDF; (R2) extract all employment history and firm registration history, including prior firms; (R3) extract disclosure counts and every field in every disclosure record. Preserve different reporting-source versions of the same event without inflating its count.

Use PDFs as the source for R1–R3. Existing website JSON remains separate supplementary data. Full original PDFs, full structured JSON, linked CSVs, source references, and validation results are required deliverables. Unknown fields must be preserved as structured fields; raw text alone is not a substitute for extracting required information.

When the user invokes the start prompt at the end, use subagents to implement, test, and run the bounded pilots described here. Do not launch a nationwide crawl or a full population collection as a side effect of implementation. The release handoff includes a measured full-run command for the user's selected population. If the user separately authorizes that run, execute it after the pilot gates pass. Completion of software and pilot validation is distinct from completion of full collection.

Do not claim all historical employment or national population coverage: coverage is all information present in PDFs for the explicitly selected CRDs. Do not replace unavailable PDF data with API data and label it PDF-derived.

## 2. Architecture and fixed command behavior

Keep the Go program and existing `search`, `dedupe`, and `enrich` behavior. Add `reports` with its own FlagSet, routed before existing global parsing. Existing commands retain their flag semantics. The new command uses these defaults:

- `--out=.`; `--input=brokers_unique.json`. Resolve an absolute input as written and a relative input against `--out`. Accept the existing array of objects containing `ind_source_id`; deduplicate validated positive decimal CRDs, sort numerically, reject malformed IDs. A fixture input can use the same shape. Do not require a new search when a suitable input already exists.
- `--workers=2`, `--rps=0.2`, `--retries=5`, `--limit=0`, `--resume=true`. Retries means maximum attempts, consistent with the existing client. Limit means the first N eligible CRDs after filtering. Reject nonpositive workers/rate/attempts and negative limits. The initial rate is an engineering starting point, not an asserted FINRA-approved rate.
- Ordinary run: skip a CRD only when its selected PDF exists, its hash verifies, and extraction passed validation with the exact current schema, parser, and extractor versions. Otherwise reuse retained bytes for parsing. Automatically retry transient failures; keep review/unavailable/access-failure items visible but skip further work on them unless explicitly selected with a mode below.
- `--reparse`: no network calls. Process the most recently successfully retrieved PDF for each input CRD, even if previously accepted. Missing bytes are an unresolved item. Reparse with the current versions; preserve earlier extraction versions. For historical snapshots, the initial release keeps them archived; bulk historical reparse is outside this command's scope.
- `--refresh`: fetch each selected CRD even if accepted. Identical bytes reuse the content-addressed PDF and add a retrieval record. New bytes create a new report version. Retain the prior accepted extraction.
- `--retry-failed`: select the latest unresolved outcome for each CRD, including unavailable, review, access, and transient failures. Reuse a saved PDF for parse failures; fetch again for download failures. Do not process already accepted items.
- The three modes are mutually exclusive. Reject them with `--resume=false`. Ordinary `--resume=false` revisits all selected CRDs, reuses verified local bytes, and reprocesses; only refresh forces a fetch of an existing valid PDF.
- Exit 0 only if all selected CRDs are current-version accepted and outputs were published. Exit 2 for any unresolved selected CRDs. Exit 1 for invalid configuration, missing dependencies, or infrastructure/persistence failure. Cancellation exits nonzero after safe checkpointing. Report counts separately for input, selected, accepted, skipped-accepted, unresolved, and skipped-unresolved.

Use Poppler `pdfinfo` and `pdftotext -bbox-layout` through subprocess calls without a shell. Gate A must qualify this backend and record exact versions. The shared document representation always includes positioned words, line grouping, and one-based physical PDF page numbers. Normalize coordinates to points measured from the top left. Preserve printed page labels separately where available. No OCR in the first release: an image-only/unreadable required page is a blocking review result. If the backend cannot meet the fixtures, the coordinator replaces its adapter while preserving the agreed document interface before releasing parser tasks.

## 3. Shared contracts: freeze before parallel parser implementation

The coordinator owns `types_report.go` and `report_contract.go`. Define the following types with exported fields and snake_case JSON tags, plus interface-level tests. Agents may add private helper types in their owned files. Changes to shared types go through the coordinator.

Common representation:

- `ReportRef`: CRD, SHA256, SourceURL, PDFPath (output-relative), FetchedAt (UTC RFC3339), Bytes (int64), PageCount (int).
- `PDFInfo`: PageCount and backend metadata. `HTTPMetadata`: status code, content type, ETag, LastModified, retrieval time. HTTP status failures use typed errors preserving the status code.
- `Document`: Report (ReportRef), ExtractorVersion, Pages ([]Page), Issues ([]ParseIssue). `Page`: Number, PrintedLabel, Width, Height, Text, Words ([]Word). `Word`: ID, LineID, Text (strings), X0/Y0/X1/Y1 (float64). IDs are deterministic from page and extraction order. Text remains ordered within each page.
- `Evidence`: Page (int), WordIDs ([]string), RawText. One record may have multiple Evidence entries across pages.
- `RawDate`: Raw (string), ISO (*string), Precision (`day`, `month`, `year`, `present`, `unknown`). Missing normalized values are null. Never invent missing date components.
- `Field`: ID, Path ([]string), Label, Value (all strings except Path), Occurrence (int, one-based within source/path/label), Evidence ([]Evidence). Value is full text, including original meaningful line breaks. Preserve blank labeled values as empty strings; absent fields have no entry.
- `SectionState`: `present`, `explicitly_empty`, `absent`, `unreadable`, or `not_applicable`. The last requires evidence of an identified report variant, not a guess from missing text.
- `ParseIssue`: Code, Severity (`blocking` or `warning`), Section, Message, Evidence ([]Evidence).
- `Coverage`: Section, State, RegionWordIDs, ConsumedWordIDs, Ignored ([]IgnoredSpan). `IgnoredSpan`: WordIDs and Reason. The permitted reasons are repeated header/footer, column header, section heading, or identified guidance text. Ignoring arbitrary remaining content is forbidden. Region spans must come from independent section-boundary detection in `report_text.go`, not just the rows the parser recognized.

History representation:

- `Registration`: ID, Kind (`current` or `previous`), Scope (`BD`, `IA`, `other`, `unknown`), FirmName, FirmCRD, Location, Start/End (RawDate), Fields ([]Field), Evidence ([]Evidence).
- `EmploymentRecord`: ID, Employer, Position, InvestmentRelated (*bool), Location, Start/End (RawDate), Fields ([]Field), Evidence ([]Evidence). Keep the original investment-related value in Fields. Do not equate employment rows with firm registrations.
- `HistoryResult`: Registrations ([]Registration), Employments ([]EmploymentRecord), OtherBusinessText (string), Coverage ([]Coverage), Issues ([]ParseIssue).

Disclosure representation:

- `DisclosureEvent`: ID, ReportedID, Type, Status (strings), Fields ([]Field), Sources ([]DisclosureSource), Evidence ([]Evidence). Event-level fields must survive even if no reporting source is labeled.
- `DisclosureSource`: ID, Label, Fields ([]Field), Evidence ([]Evidence). Preserve multiple versions bearing the same source label.
- `DisclosureCount`: Category, Status (strings), Count (*int), Raw (string), Evidence ([]Evidence). Count null means unknown or N/A, distinct from zero. Use `all` for an explicit report total; do not invent a reported total from source counts.
- `DisclosureResult`: Events ([]DisclosureEvent), Counts ([]DisclosureCount), Coverage ([]Coverage), Issues ([]ParseIssue).
- `ParsedReport`: Report, SchemaVersion, ParserVersion, ExtractorVersion, History (HistoryResult), Disclosures (DisclosureResult), Issues ([]ParseIssue).
- `ValidationResult`: Accepted (bool), Issues ([]ParseIssue), ReportedEventCount (*int), ExtractedEventCount (int), RequiredWordCoverage (float64). Accepted is false for any blocking issue.

Required function boundaries, all in package main:

```go
ProbePDF(ctx context.Context, path string) (PDFInfo, error)
ExtractDocument(ctx context.Context, ref ReportRef) (Document, error)
ParseHistory(doc Document) HistoryResult
ParseDisclosures(doc Document) DisclosureResult
ValidateReport(doc Document, parsed ParsedReport) ValidationResult
DownloadReport(ctx context.Context, c *Client, cfg DownloadConfig, crd string) (ReportRef, error)
```

`DownloadConfig` has OutDir and BaseURL strings; production BaseURL is `https://files.brokercheck.finra.org/individual`, producing `individual_<crd>.pdf`. Inject a local test server URL. The coordinator supplies `Client.FetchPDF(ctx, url, temporaryPath) (HTTPMetadata, error)` with the same shared limiter, bounded retries, context-aware backoff, both Retry-After formats, and truncation of the temporary file before each retry. This method downloads bytes only; DownloadReport verifies the PDF, hashes it, and publishes it. The limiter floor must not exceed its ceiling.

IDs are scoped to report hash and document order: registration/employment/event ordinal; source ordinal within event; field ordinal within parent. Preserve ReportedID independently. IDs are deterministic for identical bytes and versions; never promise cross-report event identity. Dates and normalized fields supplement full ordered Fields. Current registrations must be read from the report sections that actually contain them, not assumed to live under the prior Registration History heading.

## 4. Persistence, completeness, and export decisions

The coordinator implements one writer for progress and publication. Agent parsers are pure functions; they perform no network or disk I/O. An output-directory lock prevents simultaneous writers from separate processes; interrupted lock recovery must check whether the recorded process is still alive.

Storage under `--out`:

- `reports/pdf/<crd>/<hash>.pdf`: immutable validated bytes; never replace an older report with different bytes.
- `reports/text/<hash>/<extractor-version>.json`: full positioned Document.
- `reports/parsed/<crd>/<hash>/<schema-parser-extractor-key>.json`: full result plus validation, including partial results when rejected. The version key uses filesystem-safe version identifiers.
- `reports/manifest.jsonl`: append-only retrieval and processing transitions, with attempt/run IDs, versions, hashes, UTC timestamps, outcome, error code, and artifact references.
- `reports/run_summary.json`: selected input hash, counts, versions, measured throughput/size, failures, and export generation ID.

Track download states separately (`pending`, `downloaded`, `transient_failure`, `unavailable`, `access_failure`, `invalid_pdf`) from extraction states (`pending`, `parsed`, `accepted`, `needs_review`, `parse_failure`). A downloaded PDF is not a completed broker. CRD mismatch, missing required section, unreadable required page, unmatched required content, ambiguous event/source boundaries, duplicate field IDs, count mismatch, and invalid child references are blocking. Unknown labels are allowed only if fully represented as Fields with an unambiguous parent.

Use same-directory temporary files, check every write/flush/close result, sync before publication, then atomically rename. Log completion only after artifact publication. Ignore only a torn final manifest line on recovery; malformed interior records produce a visible infrastructure failure. Verify referenced artifacts and hashes before treating a prior result as complete. Add fault-injection tests for each publication boundary.

A newer failed refresh never silently falls back to an older report as current. The selected snapshot is the latest successfully retrieved PDF (retrieval time, then hash for deterministic ties); processing status also includes later failed download attempts. Preserve a link to the last accepted artifact. Broker summary reports the latest attempt's unresolved state, selected snapshot, and last accepted hash separately. Current accepted exports exclude a CRD whose latest required attempt is unresolved; historical/partial results remain in the archive. Reparse without a new retrieval keeps the selected snapshot and replaces only the selected extraction version after successful publication.

Reconcile distinct events against each comparable reported total and category/status cell. Source-version counts never stand in for event counts. Do not sum summary and matrix totals together. Explicit zero requires report evidence. Absent counts stay null; acceptance without a count requires an identified, fixture-tested report variant and complete event/field coverage. Unidentified variants remain needs_review.

All required-section words must be assigned to extracted evidence or an allowed ignored span. This is a useful omission check, not proof that grouping is correct. Manual fixture comparisons must separately check row boundaries, event/source boundaries, and complete field values. No acceptance based solely on word coverage or counts. Required-region detection must also verify section headings against the report outline/table of contents where present.

Publish exports as an atomic generation directory under `reports/exports/<run-id>/`, then atomically update `reports/exports/current.json` to point to it. This supersedes the earlier plan's root-level PDF export locations and prevents mixed generations after a crash. Never modify existing `brokers_detail.*` files.

Each generation contains:

- `brokers_pdf.jsonl`: full accepted ParsedReport and ValidationResult per selected accepted CRD.
- `brokers_pdf.csv`: every selected CRD, latest attempt status, selected/last-accepted hashes, PDF path, versions, nullable reported count, extracted count, history counts, and blocking issue count. Missing values stay empty, not zero.
- `registration_history.csv`, `employment_history.csv`: normalized record columns defined above, plus provenance.
- `history_fields.csv`: all history Fields, including extra/repeated fields, linked by record kind/ID.
- `disclosures.csv`, `disclosure_sources.csv`, `disclosure_fields.csv`: event, source, and full ordered field records. Event-level fields have an empty source ID.
- `reports_validation.jsonl`: outcomes and issues for every selected CRD, including unresolved and skipped-unresolved records.

Every child CSV includes crd, report_sha256, schema_version, parser_version, extractor_version, record/parent IDs as applicable, and evidence_json. Export Fields with path_json, occurrence, label, value, and evidence_json. Dictionary documents exact header order. Use CSV encoding for quotes and multiline text. Stream one broker at a time in numeric CRD and document order; finalization must not load the full extracted corpus. Child CSVs contain accepted records only. Partial data stays in the parsed archive and validation output.

## 5. Agent dispatch and ownership

Use at most three concurrent subagents plus the coordinator. Dispatch each with this document, its assignment, owned files, available fixture paths, and completion gate. Agents report changed files, tests run/results, unresolved issues, and interface requests. They must not edit another owner's files, change shared contracts unilaterally, commit/push, or silently weaken acceptance tests. Coordinate through messages; the coordinator integrates the shared working tree. Read applicable skills as needed during implementation.

### Gate A: foundation before parser work

**Coordinator:** inspect current code/instructions and baseline test results; create the shared types and interfaces above, CLI configuration types, Client.FetchPDF, and scaffolding needed to compile. Own `main.go`, `types_report.go`, `report_contract.go`, `httpclient.go`, existing HTTP tests, go.mod/go.sum, `.gitignore`, orchestration, persistence, validation, exports, and README. Keep existing detail types untouched unless a demonstrated integration need requires otherwise.

**Agent F — fixtures and extractor:** own `report_text.go`, `report_text_test.go`, `testdata/pdf/`, `testdata/pdf_expected/`, `testdata/pdf_inputs/`, `testdata/pdf_coverage.json`, and `docs/pdf-backend.md`. Qualify Poppler, implement ProbePDF and ExtractDocument, record versions and report hashes, and annotate expected results from rendered originals before using parser output. Begin with CRD 1691670. Existing fixture IDs 2819404 and 5634972 are candidates only; verify their PDFs rather than assuming current content. Coordinate type requests with the root.

Gate A requires: a compiling contract; working PDF adapter; one pinned real report end-to-end through positioned text; independently annotated fixtures covering no disclosures, prior/current registration, separate employment, multiple events, multiple source versions, and page continuations. A fixture can satisfy several cases. If a real case cannot be found in the bounded search, use a clearly labeled synthetic fixture to develop the behavior and mark real-report coverage pending; do not claim it verified. Root spot-checks annotations against rendered PDFs. Record a coverage checklist by category/layout and expected field counts. Freeze interfaces and pass fixture paths to subsequent agents.

### Gate B: three parallel implementation assignments

**Agent D — acquisition:** own `report_download.go`, `report_download_test.go`, and `testdata/pdf_download/`. Implement DownloadReport against the fixed Client.FetchPDF and ProbePDF interfaces. Verify status/body, hash, PDF structure, atomic retention, CRD-safe paths, same-content refresh, and missing/corrupt local artifact recovery through integration with coordinator storage. Request HTTP changes from root. Gate: offline local-server tests cover PDF success, HTML response, 404/410, 403, 429/5xx, partial bodies, cancellation, and interrupted publication. Retained bytes must match their hash.

**Agent H — histories:** own `report_history.go`, `report_history_test.go`, and additional test inputs only under `testdata/pdf_history_cases/`. Implement current/prior registrations and separate employment with every field, continuations, null/date semantics, scope separation, and evidence/coverage. Consume F's immutable expectations; propose expectation corrections to F/root rather than changing them alone. Gate: exact comparisons for every annotated history row and field; unknown layouts and unreadable required regions remain blocking.

**Agent X — disclosures:** own `report_disclosures.go`, `report_disclosures_test.go`, and additional test inputs only under `testdata/pdf_disclosure_cases/`. Implement event/source boundaries, full ordered fields, repeated/unknown labels, nested subsections, narratives, counts, and coverage. Gate: exact comparisons against annotated event/source/field records; regression case where two source versions remain one event; dropped field or ambiguous boundary triggers rejection even if counts match.

**Coordinator during Gate B:** implement `stage_reports.go`, `report_store.go`, `report_validate.go`, `report_export.go` and their tests, CLI routing, atomic export generations, and documentation. Use fixed constructed contract records for storage/export tests while parsers are in progress. Own the integration tests. Do not run a live bulk crawl while agents are implementing.

### Gate C: independent review and integrated verification

After F and implementation tasks finish, reuse F (or a new subagent in the freed slot) as **QA reviewer**, with read-only access to implementation and ownership of `docs/pdf-acceptance.md` plus new independent tests in `report_acceptance_test.go`. Review required fields directly against rendered PDFs, not just golden-test success. Root and QA agree that each required source field survives into JSON and applicable CSVs. Route code fixes to their owners; rerun affected checks after fixes. No interface or acceptance changes solely to make tests pass.

Required verification, run from the repository root after dependencies are available:

```sh
go test ./...
go test -race ./...
go vet ./...
go build -o ./run/brokercheck-scraper .
```

Create `run/` before building. Use the existing Go version constraint; document an environment blocker if unmet. Poppler-dependent tests must run in the acceptance environment, with a recorded version; skipping them is not a pass. CI/offline tests must not contact FINRA. Test the binary with a local server or injected test runner for mode selection, input-path rules, limit, exit codes, current-version resume, cancellation, PDF hash verification, and export consistency. Do not silently update the expectations to match parser output.

## 6. Pilot gates, research deliverables, and stopping conditions

The implementation start prompt authorizes at most 200 distinct live report downloads for fixture discovery and pilots combined; retries are bounded by the configured attempts. Reuse retained PDFs. Default rate remains 0.2 requests/sec across the process. No proxy rotation or access-control workarounds. Sustained access rejection is recorded as a blocker, not treated as permission to change acquisition methods.

Pilot 1: 20 deliberately varied real PDFs, including 1691670. Manually compare every required history row and disclosure event/source/field against rendered originals. Record expected/actual counts, missing/misgrouped field checks, report hashes, versions, and reviewer findings. All 20 must be accepted without unresolved required content before claiming the pilot passed. If a selected PDF is unavailable, report it and replace it for the 20-report validation sample without erasing its outcome from the run summary. If breadth cannot be obtained within the acquisition cap, complete unaffected implementation and report the exact validation gap.

Pilot 2: expand to 100 distinct real reports total, including Pilot 1, using a saved explicit CRD list rather than whichever IDs happen to be first in the old dataset. Run machine checks on all 100. QA manually reviews every new layout/category, every anomaly, and a fixed-seed sample of at least 10 additional accepted reports. Unknown layouts remain needs_review and receive fixtures/fixes before the gate passes. Expand toward 200 only when findings require it. Selection need not be statistically representative; describe it accurately.

Release evidence in `docs/pdf-acceptance.md`: baseline and final check results; backend/schema/parser versions; fixture and pilot input hashes; category/layout coverage with synthetic and real coverage distinguished; per-requirement evidence; all unresolved failures; example export paths; measured bytes/report, reports/sec, peak memory, and interruption/resume result. Estimate full-run disk and time from these measurements and the actual selected CRD count, including retries and retained snapshots. Do not reuse old README estimates for this workflow.

R1 passes when each accepted CRD has a verified retained PDF, exact hash references, and successful resume/refresh tests. R2 passes when independent comparisons find zero missing or incorrectly grouped required history rows/fields in the validation corpus. R3 passes when independent comparisons find zero missing disclosure events/source versions/fields and all comparable counts reconcile. Repeated/unknown labels, blank values, and long narratives are mandatory tests. Export traceability and rejection of deliberately corrupted/incomplete records are mandatory across all three requirements.

Full pilot acceptance requires zero unresolved required content among the reports used to claim success, passing automated checks, independent QA, and a documented coverage limit. A successful sample establishes tested coverage, not universal parser correctness. Any selected full-run CRD without a PDF or accepted parse keeps the full collection incomplete, even when failure accounting is complete.

Final implementation handoff: implemented commands; retained sample PDFs and example exports; field dictionary (`docs/pdf-data-dictionary.md`); executable local instructions; acceptance evidence; known limitations; and the proposed full-run command with its input and measured resource estimate. Bulk artifacts stay in ignored run/output directories; small pinned fixtures and acceptance documentation are reviewable. Do not mark implementation accepted with only skeletons, raw text dumps, skipped integration tests, or unverified claims of every-field coverage.

## 7. New-chat start prompt

Copy this prompt into a new task opened in the brokercheck-scraper repository:

> Implement the BrokerCheck PDF update using `docs/superpowers/plans/2026-09-12-pdf-agent-execution.md` and `docs/superpowers/plans/2026-09-09-pdf-report-update.md`. Read both fully and inspect the current checkout and applicable instructions. Act as coordinator and dispatch up to three concurrent subagents using the assignments, file ownership, shared contracts, and gates in the execution handoff. Start with Gate A, freeze and compile the shared interfaces, then dispatch the download, history, and disclosure agents. Complete integration, independent QA, and the bounded pilots; this request authorizes the handoff's maximum of 200 distinct live report downloads, with its default rate and retry limits. Preserve existing search/enrichment behavior and user changes. Do not launch full-population collection, push, or publish. Resolve routine implementation choices autonomously within the fixed contracts, record deviations, and continue until the implementation and pilot gates pass or an evidenced external blocker prevents further work. Report R1/R2/R3 results, tests, artifact paths, remaining coverage gaps, and the measured full-run recommendation. Do not equate downloaded PDFs, passing counts, or retained raw text with complete extraction.
