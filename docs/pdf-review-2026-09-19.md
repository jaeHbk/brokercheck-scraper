# Independent PDF requirement review — September 19–21, 2026

Final status (September 21, after replacement): independent original-to-export R1/R2/R3 review is complete for **20 retained real reports**, with zero remaining required-field omissions found. The 20-report manual review requirement is met; the 100-report gate remains unmet. The dated sections below preserve earlier evidence and findings; the last section records the 20th-report result.

## Initial scope and decision

Reviewer: independent QA subagent; no implementation ownership. Both implementation plans were read fully. This review inspected the current validation/export path, storage and command orchestration, and compared the retained real CRD 1691670 report directly with its structured exports. It did not add tests, make live requests, or claim the 20/100-report pilot gates passed. The user requested functionality-focused work with reduced testing.

For this single pinned real report, R1 traceability, R2 history extraction and R3 disclosure extraction pass the comparisons below. This is tested sample coverage, not universal parser acceptance. Other reports and subsequent parser changes require their own evidence. The coordinator owns the current overall acceptance decision and live-acquisition results in `docs/pdf-acceptance.md`.

## Original-to-export comparison

Original: `testdata/pdf/1691670.pdf`, SHA256 `7eb32c9390001378a37398eaae209e42700b4f889d1f698efde97f7b99c0ee54`, 14 physical pages. Retained copy: `run/pdf-live-check/reports/pdf/1691670/7eb32c9390001378a37398eaae209e42700b4f889d1f698efde97f7b99c0ee54.pdf`.

The reviewer rerendered and visually inspected physical pages 3, 10, 12 and 13 on September 19 using Poppler. The review used the original PDF, not merely successful golden comparisons. These correspond to printed pages 1, 8, 10 and 11. The annotations in `testdata/pdf_expected/1691670.json` agree with the inspected original. Review images are temporary under `/private/tmp/brokercheck-review-20260919/`.

The immutable export generation inspected was `run/pdf-live-check/reports/exports/20260913T204036.327938000-74934/`. Its schema/parser/extractor versions are `1.0.0` / `1.0.0` / `poppler-bbox-1.0.0`. The current-generation pointer is mutable and had moved during the coordinator's September 19 work; this review deliberately identifies the exact generation it checked.

- **R1:** The exported SHA256 matches the retained PDF bytes. Every child CSV inspected contains the matching CRD, PDF hash and three versions. Registration/employment/field/event/source parent references reconcile. There are no orphan fields in this generation.
- **R2:** Physical page 3 contains two current Merrill Lynch registrations: IA since 03/29/1996 and BD since 12/22/1987, both CRD 7691 and the complete Wayne branch address. Physical page 10 contains the previous BD registration, PML Securities Company CRD 4082, 08/1987–06/1988, with a blank branch location. It also contains two separate employments: Bank of America, N.A., 06/2011–Present, and Merrill Lynch, 10/1987–Present. Employer, position, investment-related Y, location and displayed wrapping all survive. Three registration rows, two employment rows and all 22 ordered history fields match the original and both JSON/CSV representations. Month/day precision, blank current end dates and Present/null semantics are preserved. Adjacent Other Business Activities content is not misclassified as employment.
- **R3:** Physical page 13 contains one Customer Dispute event, reported ordinal 1, with one Broker source. All 12 fields match: employing firm; full allegations; product type; other product; alleged damages; complaint-received date; complaint pending; status; status date; blank settlement amount; blank individual contribution amount; full broker statement. Multiline labels/values and the Customer Complaint Information subsection survive. Event disposition and source-level status remain distinct. Physical pages 3 and 12 show category total 1, Pending 0, Final 1 and On Appeal N/A. JSON preserves N/A as a null numeric count and does not invent an overall total. The single source remains part of one event.

A read-only comparison script verified all 34 history/disclosure fields across JSON and CSV, including label, full value, ordered path, occurrence and evidence. It also checked every child row's provenance and field-parent links. CSV row counts were 3 registrations, 2 employments, 22 history fields, 1 event, 1 source and 12 disclosure fields. No missing or incorrectly grouped required rows/fields were found in this report.

## Implementation review findings

The September 13 finding about cached validation values is fixed in the reviewed implementation: `currentAccepted` verifies manifest-recorded document/parsed artifact hashes, recomputes validation, and the accepted export path uses that recomputed result. PDF hash/size verification remains part of loading an accepted record.

A material September 19 finding was reported immediately: an ordinary resume after a transiently failed or interrupted refresh could reuse a prior verified PDF and silently clear the outstanding fetch. `processReport` originally considered those download states only under `--retry-failed`. The coordinator confirmed the finding and is fixing ordinary-resume fetch selection for pending/transient download states while preserving explicit offline `--reparse` behavior. The coordinator also identified and is fixing replacement of earlier parsed/document artifacts on a same-hash reparse/refresh, so a last-accepted artifact reference remains valid. Final fix verification belongs in the overall acceptance record; this document does not claim unobserved checks passed.

Storage review found append-only transition writes followed by sync, same-directory atomic file publication, retained PDF verification, visible malformed interior manifest failures, and kernel-held output locking. Command review found separate input/config parsing, numeric CRD selection, offline reparse, explicit unresolved accounting and current accepted export filtering. This focused review did not perform an exhaustive fault/race audit or rerun broad automated suites.

## Coverage limits

This independent original-to-export review covers one real report with a customer-dispute category and one reporting source. Synthetic no-disclosure, multi-source and continuation fixtures are implementation evidence, but were not independently visually re-reviewed in this resumed task. Other real layouts, categories, multiple-source boundaries and the 20/100-real-report pilots are not established by this review. September 19 acquisition and parser-expansion outcomes are maintained by the coordinator separately; the earlier two HTTP 403 results must not be presented as the current access status without checking those new outcomes.


## Expanded nine-report independent review

After the initial review, access recovered and the coordinator expanded the real corpus. The independent reviewer visually inspected 21 rendered pages across nine additional retained reports. All current registrations, every detailed Registration History and Employment History row, wrapped values, blank fields, Y/N indicators and raw dates were compared to structured records. The summary's explicit “No” disclosure answer was inspected for each report. The following results are tied to immutable export generation `run/pdf-live-check/reports/exports/20260919T202206.251155000-3414/`, schema/parser/extractor `1.0.0` / `1.1.0` / `poppler-bbox-1.1.0`.

- CRD **1031496**: physical pages 3 and 11; 5 registrations, 3 employments, 35 history fields, 0 disclosure events. SHA256 `559abab6f153ba085bcb904cfa167d7a26d415483142c7823c54b511320f9d0a`.
- CRD **1994130**: pages 3 and 9; 6 registrations, 3 employments, 39 history fields, 0 events. SHA256 `5fd1106a6858cfd0234a2c274edeb95107527905d7245b26f30a545f4bf06dbc`.
- CRD **2211137**: pages 3, 11 and 12; 12 registrations, 4 employments, 68 history fields, 0 events. SHA256 `2fbc86718f25211c277b9434f65096656023b6759aa048b7871f3c96a45aa2d4`.
- CRD **5095403**: pages 3 and 8; 2 registrations, 5 employments, 33 history fields, 0 events. SHA256 `82c4c2637b8543fec168c863877beca66ccd0076f434684e610ab4d126b805bd`.
- CRD **5418639**: pages 3, 9 and 10; 5 registrations, 5 employments, 45 history fields, 0 events. SHA256 `4424c939ea9f1a9e36e76bf269171f45b1913c74543e21d986d8ff019aca7227`.
- CRD **6150754**: pages 3 and 10; 5 registrations, 4 employments, 40 history fields, 0 events. SHA256 `b1b871441ae182bd6b6db648d91efe28d3f1071ac8211bab77116681fcedcdc4`.
- CRD **6519243**: pages 3 and 8; 6 registrations, 4 employments, 44 history fields, 0 events. SHA256 `4c618e49866af63504875422d31f11d5b9219d5c37c56674783f92690f29120b`.
- CRD **7051607**: pages 3 and 9; 3 registrations, 4 employments, 32 history fields, 0 events. SHA256 `1f4510d5d6e09644aed0f23d2c6a711f022b34df408abf79c20e61bc86fa79f8`.
- CRD **7197925**: pages 3, 7 and 8; 2 registrations, 12 employments, 68 history fields, 0 events. SHA256 `0d7852a55c0c0677c9c0e5c2ba7fe73d62d9751e557d81f4d90f3d6af355c0b9`.

The read-only export comparison verified all nine PDF hashes, matching versions/provenance, registration/employment normalized CSV values, every one of the **404 history fields**, their labels/full values/paths/occurrences/evidence, and field-parent references. All **46 registrations and 44 employments** match the corresponding current-registration or detailed-history source rows. All nine reports explicitly disclose zero events; none acquired zero by treating absence as evidence. These observations establish R1 traceability and R3 explicit-zero behavior for the nine reviewed reports.

### Required correction found in summary history

The review identified additional prior-registration **summary locations** that differ from detailed **Branch Location**. The detailed rows were correct, but the summary-only values were missing from structured fields. The coordinator agreed that these are required distinct source information and routed the correction to the history owner. R2 complete-field acceptance for affected reports remains pending that correction and an export recheck. Do not replace detailed branch locations with the summary values or add duplicate registration events.

Examples independently read from originals:

- 1031496: previous IA UBS CRD 8174, 03/1998–06/2020: summary `WEEHAWKEN, NJ`; detail `WASHINGTON, DC`.
- 1994130: previous IA First Command CRD 3641, 09/2005–12/2015 and 04/2003–04/2004: summary `FT WORTH, TX`; detail `MCLEAN, VA`. The separate IA First Command Bank CRD 128851 row says `FORT WORTH, TX` in both places and needs no extra field.
- 5418639: previous IA Raymond James CRD 149018: summary `SAINT PETERSBURG, FL`; detail `Ashton, MD`. Previous IA CRD 6694: summary `ST. PETERSBURG, FL`; detail `Ashton, MD`.
- 6150754: previous IA RBC CRD 31194, 11/2013–06/2016: summary `NEW YORK, NY`; detail `CHEVY CHASE, MD`.
- 6519243: previous IA Prudential CRD 5685: summary `NEWARK, NJ`; detail `VIENNA, VA`. Previous IA Edward Jones CRD 250: summary `ST. LOUIS, MO`; detail `Washington, DC`.

No other missing or incorrectly grouped fields were observed in these nine reports. Other Business Activities continuation pages were inspected to confirm they do not contain additional employment rows.

### Closed implementation findings

Independent code inspection confirmed the coordinator's two fixes. Ordinary resume now fetches when the previous download state is pending or transient failure, preserving the explicit offline reparse override. Changed JSON content now publishes under a content-hash-suffixed artifact path while byte-identical content reuses its existing path; earlier document and parsed artifacts remain available. The coordinator reported two focused regressions plus section tests passing in 1.55 seconds; the independent reviewer did not rerun those tests. The cached-validation export finding remains closed.

### Pilot status

The coordinator reports 19 retained real PDFs, with replacement selections 7555243 and 7805971 returning HTTP 403 and live acquisition stopped. That is short of the required 20-report gate, and the 100-report gate remains unmet. The nine-report review here plus the original 1691670 review does not assert completion of the coordinator/other reviewers' remaining reports or synthetic visual checks. The exact final population, outcomes, parser versions, correction status and export generation belong in `docs/pdf-acceptance.md`.


## Final verification — September 20, 2026

The coordinator regenerated the 19-report accepted corpus under `run/pdf-live-check/reports/exports/20260920T125427.655772000-39690/`, with schema/parser/extractor `1.1.0` / `1.1.0` / `poppler-bbox-1.1.0`. This paragraph supersedes the pending summary-location status above. The independent reviewer verified all **11 added Firm Location fields** under path `Report Summary` against the rendered source summaries and final JSON/CSV. The fields preserve the differing summary location while retaining the detailed branch location and original registration row. Eight additions apply to the nine reviewed reports, two to 4525442 (ATLANTA, GA), and one to 7854812 (MINNEAPOLIS, MN); 7854812 physical page 3 was separately rendered and inspected. The earlier apparent FT/FORT spelling discrepancy for 1994130's bank row was a reviewer reading mistake, corrected above: both originals say FORT WORTH.

R2's identified omission is **closed**. The nine detailed-history comparisons now encompass **46 registrations, 44 employments and 412 history fields**, with zero missing or misgrouped required fields observed. Final per-report history-field counts are: 1031496 36; 1994130 41; 2211137 68; 5095403 33; 5418639 47; 6150754 41; 6519243 46; 7051607 32; 7197925 68. Record counts and PDF hashes are unchanged from the earlier list. Read-only checks also verified all 19 retained PDF hashes, all final history-field CSV values/labels/paths/occurrences/evidence and parent references, and matching versions/provenance. This machine check of all 19 does not imply the reviewer visually inspected all 19 complete histories.

### Additional disclosure originals independently checked

The reviewer inspected CRD **5634972** physical pages 3 and 8–15, using the original renderings and comparing each field with the final exported JSON and the source transcriptions in `docs/pdf-disclosure-5634972.md`. PDF SHA256 `88170918982a9a3ccb51a714b2c03f559f75c9cfa34ecb394d64fc3fd1845981`. Result: **3 events, 3 Regulator sources, 57 fields**, split 21 / 18 / 18. Both Regulatory events and the Criminal event match the originals. All long questions, explicitly blank values, sanctions, nested charge information, allegation narratives and the complete continued sentence/penalty are retained. Numbering reset between categories does not merge events. The two summary counts and six matrix cells survive: Regulatory total 2 and Criminal total 1; both Pending 0 and On Appeal 0; Finals 2 and 1. No missing or misgrouped disclosure fields were found.

The reviewer inspected CRD **4525442** physical pages 3 and 12–17. PDF SHA256 `527dfb44ff94242c2c60c14d41f8f2f01bdc734ad8f41a3d45639b69e8631033`. Result: **3 Customer Dispute events, 5 sources, 66 fields**, split 10 / 14 / 14 / 14 / 14. The settled event has one Broker source; each of two closed events has separate Firm and Broker versions. Full allegations, damage explanations, complaint dates/statuses, yes/no responses, settlement/contribution amounts and blanks match. Cross-page source continuations preserve their parent event; five sources do not inflate the three-event total. The four reported count cells survive: category total 3, Pending 0, Final 3 and On Appeal N/A/null. No missing or misgrouped disclosure fields were found.

All **123 fields across these two disclosure reports** were then checked against `disclosure_fields.csv` for exact label/value/path/occurrence/evidence equality and correct event/source parent references. All **12 count cells** match `disclosure_counts.csv`, including the distinct null versus zero values. This supplements the previously completed independent 1691670 comparison. The actual displayed fields agree with the owner annotations; clipped duplicate text fragments described there do not appear as invented extra fields or appended narrative text in the final exports.

### Scoped acceptance conclusion

The reported implementation findings are closed: cached validation, interrupted/transient refresh resume, immutable changed JSON artifacts, and differing summary locations. R1 retained-byte/export traceability passes for the inspected final 19-report snapshot. R2 passes the independent original-to-export comparisons for 1691670 and the nine assigned complete histories, with the summary-location correction confirmed. R3 passes the independent comparisons for 1691670, the nine explicit-zero reports and the two additional disclosure reports. Remaining report reviews and the overall cohort decision are documented by the coordinator and other assigned reviewers.

No new tests or live calls were made by this reviewer. The snapshot inspected contains 19 accepted reports; neither the complete 20-report manual pilot nor the 100-report expansion is established by this review. Refer to `docs/pdf-acceptance.md` for later acquisition outcomes and the final gate status. Successful sample review does not establish universal parser correctness or full-population completeness.


## Completed retained-corpus review — September 21, 2026

Final frozen generation: `run/pdf-live-check/reports/exports/20260920T125812.308919000-42226/`. It contains the same 19 complete parsed reports and byte-identical seven child CSVs as the previously reviewed `20260920T125427.655772000-39690` generation. Versions remain schema/parser `1.1.0`, extractor `poppler-bbox-1.1.0`. A read-only comparison confirmed this equality, so the prior exact JSON/CSV and hash checks apply to the final generation.

The reviewer completed every remaining original-history comparison, including full labels/values, scope, raw dates, positions, Y/N indicators, locations and row boundaries. All seven following originals explicitly state that there are no disclosure events; their zero counts are source-backed. Physical pages reviewed and registration/employment/history-field counts follow:

- **2819404:** pages 3 and 7; **3 / 2 / 22**. SHA256 `f1da84e7bc9454e24efe348847f22852a950319d904cfa210e4e5c26f92cba85`.
- **4594871:** pages 3 and 7; **1 / 2 / 14**. SHA256 `1167e6a0901b13534316fcb46bb876bae3d82a754ae79ec4e05ed821d52a3516`.
- **6002745:** pages 3 and 7; **1 / 5 / 29**. SHA256 `027e9bc7f434cc320dd52867833c90630530c02dd1b61ee13ee21f092816d04f`.
- **6613236:** pages 3 and 7; **1 / 4 / 24**. SHA256 `1996aba49c85d22c65bca0e198993593656694b9e9898c07f7fd29db22134e0c`.
- **7267953:** pages 3 and 11; **2 / 8 / 48**. SHA256 `6157c9b7fa692fc2604c899309255c3abf30d1c1b1b65a686acb129fc9b701e7`.
- **7419771:** pages 3 and 7; **2 / 5 / 33**. SHA256 `b2758b8caaa7f258d92e223931e51d620e9724e31792dcbd6af048df23033a35`.
- **7854812:** pages 3, 7 and 8; **4 / 15 / 92**. SHA256 `7938281412d2bc1c272bc648b0c8cf7117c13fcf837135db765a8974f84f44b8`.

No missing or incorrectly grouped required history fields were found. The five empty prior-registration tables remain explicitly empty, with their printed headers and empty declarations distinguished from records. CRD 7854812's city/state-only current IA address is preserved independently of its BD street address; all 15 employment rows survive the page continuation. CRD 6002745's printed 09/2026 start value remains literal.

The reviewer also completed **5634972 history**, physical page 7 plus its previously inspected page 3: one prior registration, three employments, 19 fields. Explicit current nonregistration does not erase employment rows ending Present. Finally, **4525442 history**, physical pages 3, 10 and 11: seven registrations, two employments, 40 fields including the two distinct summary locations. Physical page 11 contains only continued Other Business Activities, with no omitted employment row. These checks extend the prior full disclosure reviews for both reports.

Across all **19 retained originals**, independent comparison found **71 registrations, 92 employment records and 755 history fields**, plus **7 disclosure events, 9 reporting-source versions and 135 disclosure fields**. All required row/event/source/field comparisons are now complete for this retained corpus, including explicit zero-disclosure reports and all printed comparable counts. The final CSVs preserve the reviewed structured information and provenance; all retained hashes were verified. **R1/R2/R3 pass within this 19-report corpus**, with no unresolved required content or known functional omission remaining in the reviewed sample.

This closes the previously stated manual-review scope gap. It does **not** satisfy the specified 20-real-report pilot or 100-report expansion, prove universal layout coverage, or establish full-population collection. Acquisition failures and final pilot gate status remain documented in `docs/pdf-acceptance.md`. No tests or live requests were added by the reviewer during this final pass.


## Twentieth-report replacement — September 21, 2026

**CRD 6958923: PASS for R1/R2/R3.** The reviewer independently rendered and inspected original physical pages **3 and 7** (printed pages 1 and 5), compared all required fields with positioned/parsed artifacts, and verified the applicable JSON/CSV exports. The eight-page original has SHA256 `2ede1fd03acc36e22e92d61011e679eab1829bfe6d4b784671ae9c58ae71fc23`, 76,953 bytes, retrieved at `2026-09-21T22:36:52.218931Z` from the report URL for that same CRD. Retained original: `run/pdf-live-check/reports/pdf/6958923/2ede1fd03acc36e22e92d61011e679eab1829bfe6d4b784671ae9c58ae71fc23.pdf`.

Reviewed export generation: `run/pdf-live-check/reports/exports/20260921T223651.772156000-61626/`. This replacement-only generation contains one accepted report; the previously reviewed 19-report generation remains separately identified above. Schema/parser/extractor versions remain `1.1.0` / `1.1.0` / `poppler-bbox-1.1.0`. PDF, positioned-document and parsed-artifact hashes match the manifest, and the accepted JSON exactly matches the archived parsed result. Child identifiers, evidence word IDs and physical pages resolve correctly; CSV provenance matches the retained original and versions.

Physical page 3 has **one current BD registration**, MOELIS & COMPANY LLC, CRD 145115, registered since 07/20/2019, with the complete Washington branch address. The prior-registration summary and detailed table on page 7 both explicitly say `No information reported.` They are correctly represented as explicitly empty, not missing and not as artificial rows.

Physical page 7 has **five distinct employment rows**: three Moelis rows (Vice President from 12/2022, Associate 07/2021–12/2022 in Washington, Associate 07/2019–07/2021 in New York), University of Virginia (Student 08/2017–05/2019), and SS&C Technologies (formerly Primatics Financial), Lead 07/2012–07/2017. All **29 history fields** match the originals and CSV values/labels/paths/occurrences/evidence. Repeated employer names remain separate rows; wrapped employer/location text is complete; the Y/Y/Y/N/N indicators and date precision are preserved. The absent current-registration end date remains null/unknown; the employment value Present retains its qualified meaning. Other Business Activities is explicitly empty and does not create employment data.

The original summary explicitly answers **No** to disclosed events. There are **0 events, 0 source versions and 0 disclosure fields**; the reported count cell exports as `all / all / 0`, raw `No`, with page-3 evidence. No absent section is silently converted into a zero. This report contains no disclosure narratives or blank disclosure values to exercise; those cases were reviewed in the earlier positive-disclosure originals.

The read-only export checks confirmed normalized registration/employment values, raw/ISO/precision date cells, booleans, all 29 fields, source-backed count CSV and lack of spurious disclosure child rows. **No missing or misgrouped required information, invalid child references, hash mismatch or unresolved blocking issue was found.** No implementation edit, new test or live request was made by this reviewer.

With this replacement, the manually reviewed real corpus totals **20 reports, 72 registrations, 97 employments and 784 history fields**, plus **7 disclosure events, 9 source versions and 135 disclosure fields**. The required original-to-export manual comparison for Pilot 1's 20 real reports is now complete and passes within this corpus. Previously failed download outcomes remain separate from the replacement sample and must remain visible in the coordinator's accounting. The coordinator's final integrated 20-report run and overall acceptance record govern publication of the Pilot 1 gate result; this review does not claim the 100-report expansion is complete or imply universal parser correctness.
