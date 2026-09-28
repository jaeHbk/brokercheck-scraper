# PDF data dictionary

PDF-derived outputs are separate from website enrichment. All dates retain raw text, nullable ISO normalization, and day/month/year/present/unknown precision. Present does not establish current employment. JSON nulls and empty CSV cells represent unknown normalized values and counts; an explicit zero requires PDF evidence. Blank labeled fields remain ordered fields with empty values; absent labels have no field.

Each field has a report-local deterministic ID, section path, original label, complete value with meaningful line breaks, one-based occurrence, and evidence. Evidence contains one-based physical PDF page numbers, deterministic positioned word IDs, and raw text. Printed page labels are stored separately in positioned Documents. Sources preserve repeated labels as separate versions within one event. IDs do not imply identity across refreshed reports.

PDF snapshots are immutable by SHA256. Text and parsed artifact hashes are recorded in the append-only manifest. Versions identify schema, parser and extractor. Selected snapshot and last accepted snapshot are distinct. Child CSVs and full brokers_pdf.jsonl contain current accepted records only; rejected/partial structured results remain in the parsed archive. reports_validation.jsonl includes every selected CRD, including skipped unresolved items.

An atomic reports/exports/current.json points to one complete generation. Its path is relative to the output directory. CSV quoting preserves commas, quotes, and multiline values. Child records carry report provenance, parent IDs and evidence_json. Event-level disclosure fields have an empty source_id. The CSV headers below specify exact order.

## brokers_pdf.csv

crd, latest_attempt_status, selected_sha256, last_accepted_sha256, pdf_path, schema_version, parser_version, extractor_version, reported_event_count, extracted_event_count, registration_count, employment_count, blocking_issue_count

## registration_history.csv

crd, report_sha256, schema_version, parser_version, extractor_version, record_id, kind, scope, firm_name, firm_crd, location, start_raw, start_iso, start_precision, end_raw, end_iso, end_precision, evidence_json

## employment_history.csv

crd, report_sha256, schema_version, parser_version, extractor_version, record_id, employer, position, investment_related, location, start_raw, start_iso, start_precision, end_raw, end_iso, end_precision, evidence_json

## history_fields.csv

crd, report_sha256, schema_version, parser_version, extractor_version, record_kind, record_id, field_id, path_json, occurrence, label, value, evidence_json

## disclosures.csv

crd, report_sha256, schema_version, parser_version, extractor_version, event_id, reported_id, type, status, evidence_json

## disclosure_sources.csv

crd, report_sha256, schema_version, parser_version, extractor_version, event_id, source_id, label, evidence_json

## disclosure_fields.csv

crd, report_sha256, schema_version, parser_version, extractor_version, event_id, source_id, field_id, path_json, occurrence, label, value, evidence_json

## disclosure_counts.csv

crd, report_sha256, schema_version, parser_version, extractor_version, count_id, category, status, count, raw, evidence_json
