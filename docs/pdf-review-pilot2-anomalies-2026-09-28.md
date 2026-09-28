# Independent Pilot 2 anomaly review — September 28, 2026

Final result: **all five anomaly corrections PASS independent artifact/export verification** against the original-page annotations below. The final verification section identifies the exact versioned generation.

Scope: original-page review of the five newly retained `needs_review` reports, before accepting parser fixes. The reviewer did not edit implementation, run tests, or download reports. Originals are under `run/pdf-live-check/reports/pdf/<crd>/<sha256>.pdf`; the initial reviewed parsed/document artifacts use schema/parser `1.1.0`, extractor `poppler-bbox-1.1.0`. Page numbers below are physical, one-based. Rendered review intermediates are in `/private/tmp/brokercheck-review-20260928/`.

## Four no-disclosure summary variants

CRDs **2377028, 4639973, 6339074 and 7314224**, each physical page **3**, visibly contain an explicit `No` answer to “Are there events disclosed about this broker?” All four have **0 events, 0 sources and 0 disclosure fields** and one source-backed count: category `all`, status `all`, count **0**, raw `No`, with page-3 evidence.

Below that disclosure area is a distinct heading and light-blue guidance box. It is standard referral guidance, not a disclosure event, count, field or narrative. The complete 35-word span, including its heading and URL, is:

```text
Investment Adviser Representative
Information
The information below represents the individual's
record as a broker. For details on this individual's
record as an investment adviser representative,
visit the SEC's Investment Adviser Public
Disclosure website at
https://www.adviserinfo.sec.gov
```

All four originals visibly contain this same wording; line wrapping differs slightly. The box explains the distinction between broker and investment-adviser information and refers readers to the SEC's site. It contains no individualized source data. Recognizing this exact qualified text as `identified guidance text` is consistent with the coverage contract. A fix should match the whole known template or its independently bounded box; it must not suppress arbitrary content merely because it appears after a no-disclosure answer or contains the word “Disclosure.” The original text and word evidence must remain available in the positioned document/coverage.

Pinned original hashes:

- 2377028: `e2968a75d93904e4d39f8c3683757c4bb63d9a6a632f6cead014340c9bf59ad2`.
- 4639973: `1b61a537f9808e4f09eb6fc2ac528287f1886c60b26d49a8dba339db52d3bbd7`.
- 6339074: `4d51f9744d5978dcf78ad565aa779f3aaa1df8fb55a0e5602af6813641269d1f`.
- 7314224: `0310f03fd837c0a3f3c6459a13bf755e4c390a934654cc2e3cbdca2f62a9444a`.

Initial parser outcomes correctly retain the explicit-zero counts, but reject the 35 guidance words as unmatched required disclosure-summary content. This is a qualified boundary/guidance-recognition gap. The historical rows of these reports were not independently re-reviewed in this anomaly-focused assignment; their current machine record counts are respectively 8/5, 7/12, 3/5 and 1/10 registrations/employments.

## CRD 4960324 — independent complete termination annotation

Pinned hash: `e3fb6ba2d3a60addbc29dde1166ed55bf9a5c32b04ad71d15aafc67544be811b`. Original physical pages **3, 8, 9 and 10** were rendered and inspected. Expected: **1 distinct event, 2 reporting-source versions, 10 ordered source fields, 0 event-level labeled fields**. Each source has five fields; all paths are empty and each occurrence is 1. There are no blank labeled source values, nested subrecords or page continuations. Page 10 is the explicit end-of-report page, confirming nothing continues after page 9.

### Event and source boundaries

Physical page 9 (printed page 7) has the displayed category heading **Employment Separation After Allegations**, followed by its standard explanatory guidance, then **Disclosure 1 of 1**. Preserve that heading and the reported ordinal 1. The report's summary/matrix comparable category is **Termination**. There is **no printed event-status suffix** on the heading. Do not misread the source field `Termination Type: Discharged` as a printed event status. The matrix independently reports one Final event; any normalized Final classification must be explicitly grounded in that count/qualified variant rather than fabricated from the heading.

Source 1 is **Firm**. The dotted horizontal divider after its Product Type separates source 2, **Broker**, within the same event. There is no new disclosure-number heading between the sources. Both versions must remain; they must not produce two events. The tiny `i` present in extracted text at the separator has no displayed substantive field in the rendered original.

### Source 1: Firm — all fields on physical page 9

1. Label `Employer Name:`; value `MERRILL LYNCH, PIERCE, FENNER & SMITH, INC`.
2. Label `Termination Type:`; value `Discharged`.
3. Label `Termination Date:`; value `03/04/2013`.
4. Label `Allegations:`; value exactly:

   ```text
   CONDUCT INVOLVING FAILURE TO FOLLOW THE FIRM'S PROCEDURES
   FOR MONETARY DISBURSEMENTS AND WITHDRAWALS, INCLUDING
   PROVIDING INACCURATE INFORMATION IN INTERNAL SYSTEMS IN ORDER
   TO EXECUTE AUTHORIZED TRANSACTIONS.
   ```

5. Label `Product Type:`; value `No Product`.

### Source 2: Broker — all fields on physical page 9

1. Label `Employer Name:`; value `MERRILL LYNCH, PIERCE, FENNER & SMITH, INC`.
2. Label `Termination Type:`; value `Discharged`.
3. Label `Termination Date:`; value `03/04/2013`.
4. Label `Allegations:`; value exactly:

   ```text
   CONDUCT INVOLVING FAILURE TO FOLLOW THE FIRM'S PROCEDURES
   FOR MONETARY DISBURSEMENTS AND WITHDRAWALS, INCLUDING
   PROVIDING INACCURATE INFORMATION IN INTERNAL SYSTEMS IN ORDER
   TO EXECUTE AUTHORIZED TRANSACTIONS
   ```

5. Label `Product Type:`; value `No Product`.

The Firm narrative ends in a period; the Broker narrative does not. This source difference is visible and must survive. There are no extra fields or untranscribed narrative lines.

### Every reported count cell

- Physical page 3 (printed page 1): category `Termination`, status `all`, count **1**, raw `1`.
- Physical page 8 (printed page 6): category `Termination`, status `Pending`, count **null**, raw `N/A`.
- Physical page 8: category `Termination`, status `Final`, count **1**, raw `1`.
- Physical page 8: category `Termination`, status `On Appeal`, count **null**, raw `N/A`.

There is no printed all-category total; do not add one or sum the category summary and matrix again. Distinct-event count is 1, independent of the two sources. All four cells require their original page/word evidence.

### Initial implementation finding and acceptance boundary

The initial artifact already contains the correct two source versions and all ten source fields, including the punctuation difference, but event type/status are empty and the event heading/guidance is unmatched. The failure is event-category recognition and count reconciliation for a heading without the usual status suffix. Expected source fields/counts above are fixed by the original, not by whether a revised parser accepts the report.

These annotations qualify the layouts and supply independent expected values. They do not by themselves mark the corrected artifacts accepted: the coordinator must reparse with the revised versions, ensure zero blocking content, and verify the final fields/counts/parent links and CSV output against these expectations. No additional real category or continuation coverage is claimed by these five reports.


## Final corrected artifact/export verification — PASS

The independent reviewer checked final generation `run/pdf-live-check/reports/exports/20260928T151608.017607000-73937/`, schema **1.1.0**, parser **1.2.0**, extractor **poppler-bbox-1.1.0**. This generation contains 37 accepted retained reports. The checks below concern the **five fixed anomaly reports**, not a claim of independent original-page review of every newly collected report.

All five are accepted with **required-word coverage 1.0 (100%)**, zero blocking issues, correct version/provenance columns and valid physical-page/word references. Each retained PDF matches its pinned original hash above. Each positioned-document and parsed-artifact hash matches its export transition, and each accepted JSON record exactly matches the retained parsed artifact. Broker summary CSVs report accepted status and zero blocking issues.

- **2377028, 4639973, 6339074, 7314224: PASS.** Each has exactly zero event/source/field records in JSON and the three disclosure child CSVs, and one count cell `all / all / 0`, raw `No`, with physical-page-3 evidence. Each original 35-word Investment Adviser Representative Information span was matched token-for-token in the retained positioned document; all 35 corresponding word IDs are assigned to `identified guidance text`. The guidance remains preserved and is not mistaken for an individualized disclosure or a reason to invent records.
- **4960324: PASS.** The final event preserves displayed type `Employment Separation After Allegations`, reported ordinal `1`, and an empty raw event status because no status suffix is printed. There is one event and two sources in the correct order, Firm then Broker. All ten fields match the fixed original transcription exactly, including complete four-line narratives and the Firm-only final period. Every field has empty path, occurrence 1 and physical-page-9 evidence. Event-level fields remain empty. The four original count cells match exactly, including Pending/On Appeal null versus raw N/A and Final 1; no invented all-category total appears.

The final `disclosures.csv`, `disclosure_sources.csv` and `disclosure_fields.csv` preserve the same event/source parent relationships, exact source labels, ordered field labels/values, paths, occurrences and evidence. `disclosure_counts.csv` preserves all eight count cells across the five reports (four explicit zeros plus the termination report's four cells), including original raw text, null/zero distinctions and source pages. All child CRD/hash/version references are consistent and resolve to the retained originals.

The original five anomalies are **closed with no remaining required-content or export-traceability blocker found**. Coverage is corroborated by the earlier independent rendered-original comparisons; acceptance is not based on the 100% metric alone. This review makes no broader universal-layout claim and does not establish completion of the 100-report Pilot 2 gate. No live downloads, implementation edits or new test-suite additions were made by the reviewer.
