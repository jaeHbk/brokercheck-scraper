# Recovered real-report history verification

Reviewed September 19, 2026. The history owner rendered physical pages 3 and 7 of both retained originals and transcribed the expectations below independently of parser output. The full field values, scope, row count, and physical page comparisons then passed using the corrected independent section detector and existing history parser. No history-parser relaxation or arbitrary ignored content was needed. Review is limited to R2 history fields; it does not establish disclosure acceptance or complete pilot acceptance.

Artifacts are under `run/pdf-history-review/`: four original page renderings, `2819404-verified-history.json`, `5634972-verified-history.json`, and the focused verification source `verification-test.go.txt`. Rendered pages were inspected visually; the JSON was written only after exact comparison passed. `\n` below denotes a displayed line break retained in field values.

## CRD 2819404

Original: `run/pdf-live-check/reports/pdf/2819404/f1da84e7bc9454e24efe348847f22852a950319d904cfa210e4e5c26f92cba85.pdf`.

Physical page 3 contains two distinct current registrations. Both have Firm Name `EQUITABLE ADVISORS, LLC`, CRD# `6627`, Branch Location `1883 AVENIDA LAS AMERICAS\nPONCE, PR 00728`. The IA registration began `01/27/2011`; the BD registration began `12/10/1996`. Neither has a printed end date. Each contains four ordered fields: Firm Name, CRD#, Branch Location, Registered with this firm since.

Physical page 7 contains one prior BD registration with the four printed table fields:

- Registration Dates: `12/1996 - 01/2000`.
- Firm Name: `THE EQUITABLE LIFE ASSURANCE\nSOCIETY OF THE UNITED STATES`.
- CRD#: `4039`.
- Branch Location: `NEW YORK, NY`.

Physical page 7 contains two employment rows, independent of these registrations. Field order is Employment, Employer Name, Position, Investment Related, Employer Location:

1. `09/1999 - Present`; `EQUITABLE ADVISORS, LLC`; `REGISTERED\nREPRESENTATIVE`; `Y`; `NEW YORK, NY, United\nStates`.
2. `09/1999 - 06/2020`; `AXA ADVISORS, LLC`; `REGISTERED\nREPRESENTATIVE`; `Y`; `NEW YORK, NY, United\nStates`.

Expected and actual: 3 registrations, 2 employment rows, 22 fields; all exact field comparisons passed. The date precision remains day for current registrations and month for the historical ranges. `Present` retains its original qualified meaning.

New extraction issue: Poppler merged the long broker name and the summary title into one text line. The old detector's fallback admitted words from the center column into the current-registration region. The coordinator corrected region geometry using the title's positioned words. No current-registration content was ignored to compensate.

## CRD 5634972

Original: `run/pdf-live-check/reports/pdf/5634972/88170918982a9a3ccb51a714b2c03f559f75c9cfa34ecb394d64fc3fd1845981.pdf`.

Physical page 3 explicitly states `This broker is not currently registered.` Current registration is explicitly empty, with exact word/page evidence. This is not inferred from missing records. Physical page 7 has one prior BD registration:

- Registration Dates: `03/2009 - 12/2022`.
- Firm Name: `FARMERS FINANCIAL SOLUTIONS, LLC`.
- CRD#: `103863`.
- Branch Location: `WILLOWICK, OH`.

Physical page 7 has three employment rows. Field order is Employment, Employer Name, Position, Investment Related, Employer Location:

1. `01/2009 - Present`; `FARMERS FINANCIAL SOLUTIONS`; `REG REP`; `Y`; `INDEPENDENCE, OH,\nUnited States`.
2. `10/2008 - Present`; `FARMERS INSURANCE GROUP`; `AGENT`; `N`; `INDEPENDENCE, OH,\nUnited States`.
3. `10/2004 - Present`; `SELF EMPLOYED`; `OUTSIDE SALES`; `N`; `WILLOWICK, OH, United\nStates`.

Expected and actual: 0 current registrations, 1 prior registration, 3 employment rows, 19 fields; all exact field comparisons passed. In particular, employment end values remain `Present` even though the broker is no longer currently registered, and the original Y/N indicators survive alongside normalized booleans.

## Verification

The focused check ran `go test -run 'TestHistory(RecoveredVisualAnnotations|AnnotatedFixtures)' ./...` and passed, including all existing independently annotated fixture comparisons. The temporary recovered-report test was archived outside the compiled package because it depends on ignored pilot artifacts. It checked every field value, registration scope, record physical page, deterministic evidence integrity, and complete history-region coverage, rejecting all blocking history issues. The initial 1691670 history fixture remains unchanged.

## Additional retained pilot layouts, reviewed September 19–20

The owner visually inspected the additional original renderings in `run/pdf-history-review/`: `4594871-p7.png`, `6002745-p7.png`, `6613236-p7.png`, `7267953-p11.png`, `7419771-p7.png`, and `7854812-p3.png`, `7854812-p7.png`, `7854812-p8.png`. These are source renderings, not reconstructed parser output.

### Explicitly empty prior-registration tables

CRDs 4594871, 6002745, 6613236, 7267953, and 7419771 print all four headings (Registration Dates, Firm Name, CRD#, Branch Location), followed by `No information reported.` The table is explicitly empty despite retaining its headings. The previous parser rejected the headings as contradictory additional content. The fix classifies the recognized headings as column headers and retains the exact empty declaration as evidence. An actual row alongside the empty declaration remains blocking, as does any unidentified content. No headings or empty declarations become artificial registration records.

Affected source comparisons:

- 4594871, physical page 7: zero prior registrations; two employment rows. `MCLEAN SECURITIES LLC`, `10/2002 - Present`, position `REGISTERED\nREPRESENTATIVE`, Y; `THE MCLEAN GROUP LLC`, `03/2002 - Present`, position `DIRECTOR,\nVALUATION\nSERVICES`, N. Both locations are `MCLEAN, VA, United States`. With its existing current registration, extraction has 1 registration, 2 employment rows, 14 fields.
- 6002745, physical page 7: zero prior registrations; five employment rows beginning 09/2026, 02/2012, 01/2012, 11/2011, and 10/2013. Employer sequence is `72Beyond Group LLC`, `JB TAX SERVICES LLC`, `TRANSAMERICA FINANCIAL ADVISORS,\nINC`, `WORLD FINANCIAL GROUP, INC.`, `STITELY & KARSTETTER CPAS`; investment-related indicators N, N, Y, N, N. The final row ends 11/2022; the other four end `Present`. With its current registration, extraction has 1 registration, 5 employment rows, 29 fields. The source's 09/2026 value is preserved literally.
- 6613236, physical page 7: zero prior registrations; four employment rows: `XML Financial Group` and `XML Securities, LLC`, both 06/2021–Present, Compliance Officer, Y, Fairfax; `Unemployed`, 10/2020–05/2021, Homemaker, N, `Tamuning, Guam`; and `XML Financial Group`, 02/2016–10/2020, `Client Service\nAssociates`, Y, `Falls Church, VA, United\nStates`. Extraction has 1 registration, 4 employment rows, 24 fields.
- 7267953, physical page 11: zero prior registrations; eight separate employment rows. The two 06/2020–Present ANALYST rows belong to `JPMORGAN CHASE BANK NA` and `JPMORGAN SECURITIES LLC`. Six older rows remain distinct, including repeated HARVARD UNIVERSITY employer names and a HARVARD BUSINESS SCHOOL `RESEARCH\nASSISTANT` row. Extraction has 2 current registrations, 8 employment rows, 48 fields.
- 7419771, physical page 7: zero prior registrations; five employment rows for Forge Financial Group, COMMONWEALTH FINANCIAL NETWORK, Potomac Financial Private Client Group, Virginia Tech, and Hollywood Golf Course, with Y/Y/Y/N/N indicators. The wrapped `COMMONWEALTH FINANCIAL\nNETWORK`, `Registered Staff\nMember`, and location values remain intact. Extraction has 2 current registrations, 5 employment rows, 33 fields.

### Current registration with city/state only

CRD 7854812, physical page 3, prints current IA Firm Name `MML INVESTORS SERVICES, LLC`, location `Tampa, FL`, CRD# `10409`, and start date `10/06/2025`. It has no street line or ZIP code. The BD registration is separate: same firm/CRD, `7101 Wisconsin Ave\nSuite 1200\nBethesda, MD 20814`, start `10/03/2025`. The parser now accepts a bounded US city/state format directly before the labeled CRD; it does not invent missing address components or reuse the BD address.

Physical page 7 has two prior AMERIPRISE FINANCIAL SERVICES, LLC rows, CRD 6363: IA 05/2025–07/2025 and BD 01/2025–07/2025, each with detailed Branch Location `Hauppauge, NY`. There are 15 employment rows: 12 on physical page 7 and three on physical page 8. The final continuation rows are New York University (06/2018–12/2019, Student, N, New York), Jildor (08/2016–06/2018, Sales Associate, N, Woodbury), and Syosset Central School District (10/2014–06/2018, Student, N, Syosset). The repeated headers do not create rows or merge the last row of page 7 with the first row of page 8. The report has 4 registrations, 15 employment rows, 91 detailed fields, plus the one distinct summary field described below (92 total history fields).

### Preserve summary firm locations separately from detailed branches

Independent QA identified prior-registration firm locations in the report summary that differ from the detailed Branch Location. The coordinator added an independent `registration_summary` region. The history parser now matches each summary entry to one detailed prior registration by scope, firm CRD, and both raw dates. An absent or ambiguous match blocks acceptance. The summary may show only a subset of prior registrations; it never replaces the complete detailed table.

For redundant name, CRD, date, and location values, exact summary evidence is attached to the existing Field. For a differing location, a new Field has path `["Report Summary"]`, label `Firm Location`, and the original summary value. The detailed Branch Location and normalized record Location stay unchanged. Literal spelling/case differences are preserved; only equivalent whitespace wrapping is treated as redundant. A genuinely different summary firm name is likewise retained as a separate `Report Summary` field. Existing fixture field counts are unchanged.

The retained 19-report corpus produces these 11 additional location fields, each from physical page 3:

- 1031496, IA/8174, 03/1998–06/2020: `WEEHAWKEN, NJ`; detailed branch `WASHINGTON, DC`.
- 1994130, IA/3641, 09/2005–12/2015: `FT WORTH, TX`; detailed branch `MCLEAN, VA`.
- 1994130, IA/3641, 04/2003–04/2004: `FT WORTH, TX`; detailed branch `MCLEAN, VA`.
- 4525442, IA/17499, 02/2006–11/2009: `ATLANTA, GA`; detailed branch `ALEXANDRIA, VA`.
- 4525442, IA/17499, 07/2002–01/2006: `ATLANTA, GA`; detailed branch `ALEXANDRIA, VA`.
- 5418639, IA/149018, 01/2009–07/2026: `SAINT PETERSBURG, FL`; detailed branch `Ashton, MD`.
- 5418639, IA/6694, 04/2008–01/2009: `ST. PETERSBURG, FL`; detailed branch `Ashton, MD`.
- 6150754, IA/31194, 11/2013–06/2016: `NEW YORK, NY`; detailed branch `CHEVY CHASE, MD`.
- 6519243, IA/5685, 08/2021–11/2024: `NEWARK, NJ`; detailed branch `VIENNA, VA`.
- 6519243, IA/250, 09/2015–08/2021: `ST. LOUIS, MO`; detailed branch `Washington, DC`.
- 7854812, IA/6363, 05/2025–07/2025: `MINNEAPOLIS, MN`; detailed branch `Hauppauge, NY`.

The summary locations above were checked against positioned source text; independent QA performed the wider original-page review. The history owner additionally inspected 7854812 directly in its rendered original. These source comparisons should not be represented as a second independent visual review of all 19 reports by the history owner. For 1994130 IA/128851 (04/2004–09/2005), both source locations are `FORT WORTH, TX`, so no redundant field is added.

### Final history checks and limitations

`go test -run '^TestHistory' ./...` passed after the fixes, including the immutable original fixtures and focused regressions for empty tables, contradictory content, city/state-only current locations, preservation of different summary locations, and rejection of ambiguous summary matches. A temporary retained-corpus check also passed on all 19 real PDFs: zero blocking history issues, valid field evidence, and complete coverage of independently detected history/summary regions. The final successful combined run took 2.36 seconds in the recorded environment. The pilot-specific test is archived as `run/pdf-history-review/retained-verification-test.go.txt`, outside the normal test package; the per-report `*-verified-history.json` files contain the resulting complete histories.

The 19-report machine check alone does not establish complete independent manual acceptance, disclosure correctness, or satisfaction of the required 20- and 100-report pilots. The coordinator and QA acceptance records govern those gates. No live requests were issued by the history owner.

### Pinned hashes for the additional affected layouts

- 4594871: `1167e6a0901b13534316fcb46bb876bae3d82a754ae79ec4e05ed821d52a3516`.
- 6002745: `027e9bc7f434cc320dd52867833c90630530c02dd1b61ee13ee21f092816d04f`.
- 6613236: `1996aba49c85d22c65bca0e198993593656694b9e9898c07f7fd29db22134e0c`.
- 7267953: `6157c9b7fa692fc2604c899309255c3abf30d1c1b1b65a686acb129fc9b701e7`.
- 7419771: `b2758b8caaa7f258d92e223931e51d620e9724e31792dcbd6af048df23033a35`.
- 7854812: `7938281412d2bc1c272bc648b0c8cf7117c13fcf837135db765a8974f84f44b8`.
