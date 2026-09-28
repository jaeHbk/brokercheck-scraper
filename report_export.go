package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

var reportCSVHeaders = map[string][]string{
	"disclosure_counts.csv":    {"crd", "report_sha256", "schema_version", "parser_version", "extractor_version", "count_id", "category", "status", "count", "raw", "evidence_json"},
	"brokers_pdf.csv":          {"crd", "latest_attempt_status", "selected_sha256", "last_accepted_sha256", "pdf_path", "schema_version", "parser_version", "extractor_version", "reported_event_count", "extracted_event_count", "registration_count", "employment_count", "blocking_issue_count"},
	"registration_history.csv": {"crd", "report_sha256", "schema_version", "parser_version", "extractor_version", "record_id", "kind", "scope", "firm_name", "firm_crd", "location", "start_raw", "start_iso", "start_precision", "end_raw", "end_iso", "end_precision", "evidence_json"},
	"employment_history.csv":   {"crd", "report_sha256", "schema_version", "parser_version", "extractor_version", "record_id", "employer", "position", "investment_related", "location", "start_raw", "start_iso", "start_precision", "end_raw", "end_iso", "end_precision", "evidence_json"},
	"history_fields.csv":       {"crd", "report_sha256", "schema_version", "parser_version", "extractor_version", "record_kind", "record_id", "field_id", "path_json", "occurrence", "label", "value", "evidence_json"},
	"disclosures.csv":          {"crd", "report_sha256", "schema_version", "parser_version", "extractor_version", "event_id", "reported_id", "type", "status", "evidence_json"},
	"disclosure_sources.csv":   {"crd", "report_sha256", "schema_version", "parser_version", "extractor_version", "event_id", "source_id", "label", "evidence_json"},
	"disclosure_fields.csv":    {"crd", "report_sha256", "schema_version", "parser_version", "extractor_version", "event_id", "source_id", "field_id", "path_json", "occurrence", "label", "value", "evidence_json"},
}

func jsonCell(v any) string { b, _ := json.Marshal(v); return string(b) }
func nullableInt(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}
func nullableString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func dateCells(d RawDate) []string { return []string{d.Raw, nullableString(d.ISO), d.Precision} }
func currentAccepted(s *ReportStore, t ReportTransition) (StoredReport, bool) {
	if t.Outcome != "accepted" || t.SchemaVersion != SchemaVersion || t.ParserVersion != ParserVersion || t.ExtractorVersion != ExtractorVersion {
		return StoredReport{}, false
	}
	if t.DocumentSHA256 == "" || t.ParsedSHA256 == "" {
		return StoredReport{}, false
	}
	dh, e := reportArtifactHash(s.OutDir, t.DocumentPath)
	if e != nil || dh != t.DocumentSHA256 {
		return StoredReport{}, false
	}
	ph, e := reportArtifactHash(s.OutDir, t.ParsedPath)
	if e != nil || ph != t.ParsedSHA256 {
		return StoredReport{}, false
	}
	p, e := s.ReadParsed(t)
	if e != nil || !p.Validation.Accepted || p.Parsed.SchemaVersion != SchemaVersion || p.Parsed.ParserVersion != ParserVersion || p.Parsed.ExtractorVersion != ExtractorVersion {
		return p, false
	}
	dp, e := safeReportPath(s.OutDir, t.DocumentPath)
	if e != nil {
		return p, false
	}
	b, e := os.ReadFile(dp)
	if e != nil {
		return p, false
	}
	var doc Document
	if json.Unmarshal(b, &doc) != nil {
		return p, false
	}
	if doc.Report.CRD != t.CRD || doc.Report.SHA256 != t.Report.SHA256 {
		return p, false
	}
	checked := ValidateReport(doc, p.Parsed)
	p.Validation = checked
	return p, checked.Accepted
}
func PublishReportExports(s *ReportStore, runID string, selected []string) (string, error) {
	base := filepath.Join(s.OutDir, "reports", "exports")
	if e := os.MkdirAll(base, 0755); e != nil {
		return "", e
	}
	tmp, e := os.MkdirTemp(base, ".generation-*")
	if e != nil {
		return "", e
	}
	defer os.RemoveAll(tmp)
	files := map[string]*os.File{}
	writers := map[string]*csv.Writer{}
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()
	for name, header := range reportCSVHeaders {
		f, e := os.Create(filepath.Join(tmp, name))
		if e != nil {
			return "", e
		}
		files[name] = f
		w := csv.NewWriter(f)
		writers[name] = w
		if e = w.Write(header); e != nil {
			return "", e
		}
	}
	for _, name := range []string{"brokers_pdf.jsonl", "reports_validation.jsonl"} {
		f, e := os.Create(filepath.Join(tmp, name))
		if e != nil {
			return "", e
		}
		files[name] = f
	}
	emit := func(name string, row []string) error { return writers[name].Write(row) }
	for _, crd := range selected {
		t := s.Latest[crd]
		p, accepted := currentAccepted(s, t)
		summary := []string{crd, t.Outcome, "", t.LastAcceptedHash, "", SchemaVersion, ParserVersion, ExtractorVersion, "", "", "", "", ""}
		if t.Report != nil {
			summary[2] = t.Report.SHA256
			summary[4] = t.Report.PDFPath
		}
		validation := ValidationResult{Issues: []ParseIssue{blocking(t.ErrorCode, "report", t.Error)}}
		if accepted {
			validation = p.Validation
		} else if t.ParsedPath != "" {
			if stored, e := s.ReadParsed(t); e == nil {
				validation = stored.Validation
				p = stored
			}
		}
		if !accepted {
			validation.Accepted = false
			if t.Error != "" {
				validation.Issues = append(validation.Issues, blocking(t.ErrorCode, "report", t.Error))
			}
		}
		if t.ParsedPath != "" && p.Parsed.Report.CRD == crd {
			summary[8] = nullableInt(validation.ReportedEventCount)
			summary[9] = strconv.Itoa(validation.ExtractedEventCount)
			summary[10] = strconv.Itoa(len(p.Parsed.History.Registrations))
			summary[11] = strconv.Itoa(len(p.Parsed.History.Employments))
		}
		n := 0
		for _, i := range validation.Issues {
			if i.Severity == "blocking" {
				n++
			}
		}
		summary[12] = strconv.Itoa(n)
		if e = emit("brokers_pdf.csv", summary); e != nil {
			return "", e
		}
		if e = json.NewEncoder(files["reports_validation.jsonl"]).Encode(struct {
			CRD        string           `json:"crd"`
			Transition ReportTransition `json:"transition"`
			Validation ValidationResult `json:"validation"`
		}{crd, t, validation}); e != nil {
			return "", e
		}
		if !accepted {
			continue
		}
		if e = json.NewEncoder(files["brokers_pdf.jsonl"]).Encode(p); e != nil {
			return "", e
		}
		r := p.Parsed
		prefix := []string{crd, r.Report.SHA256, r.SchemaVersion, r.ParserVersion, r.ExtractorVersion}
		row := func(cells ...string) []string { return append(append([]string{}, prefix...), cells...) }
		fieldRows := func(name, parent, source string, fields []Field) error {
			for _, f := range fields {
				if e := emit(name, row(parent, source, f.ID, jsonCell(f.Path), strconv.Itoa(f.Occurrence), f.Label, f.Value, jsonCell(f.Evidence))); e != nil {
					return e
				}
			}
			return nil
		}
		for _, h := range r.History.Registrations {
			cells := row(h.ID, h.Kind, h.Scope, h.FirmName, h.FirmCRD, h.Location)
			cells = append(cells, dateCells(h.Start)...)
			cells = append(cells, dateCells(h.End)...)
			cells = append(cells, jsonCell(h.Evidence))
			if e = emit("registration_history.csv", cells); e != nil {
				return "", e
			}
			if e = fieldRows("history_fields.csv", "registration", h.ID, h.Fields); e != nil {
				return "", e
			}
		}
		for _, h := range r.History.Employments {
			investment := ""
			if h.InvestmentRelated != nil {
				investment = strconv.FormatBool(*h.InvestmentRelated)
			}
			cells := row(h.ID, h.Employer, h.Position, investment, h.Location)
			cells = append(cells, dateCells(h.Start)...)
			cells = append(cells, dateCells(h.End)...)
			cells = append(cells, jsonCell(h.Evidence))
			if e = emit("employment_history.csv", cells); e != nil {
				return "", e
			}
			if e = fieldRows("history_fields.csv", "employment", h.ID, h.Fields); e != nil {
				return "", e
			}
		}
		for i, c := range r.Disclosures.Counts {
			if e = emit("disclosure_counts.csv", row(r.Report.SHA256+"-count-"+strconv.Itoa(i+1), c.Category, c.Status, nullableInt(c.Count), c.Raw, jsonCell(c.Evidence))); e != nil {
				return "", e
			}
		}
		for _, d := range r.Disclosures.Events {
			if e = emit("disclosures.csv", row(d.ID, d.ReportedID, d.Type, d.Status, jsonCell(d.Evidence))); e != nil {
				return "", e
			}
			if e = fieldRows("disclosure_fields.csv", d.ID, "", d.Fields); e != nil {
				return "", e
			}
			for _, source := range d.Sources {
				if e = emit("disclosure_sources.csv", row(d.ID, source.ID, source.Label, jsonCell(source.Evidence))); e != nil {
					return "", e
				}
				if e = fieldRows("disclosure_fields.csv", d.ID, source.ID, source.Fields); e != nil {
					return "", e
				}
			}
		}
	}
	for _, w := range writers {
		w.Flush()
		if e = w.Error(); e != nil {
			return "", e
		}
	}
	for name, f := range files {
		if e = reportFault("export_sync"); e != nil {
			return "", e
		}
		if e = f.Sync(); e != nil {
			return "", e
		}
		if e = f.Close(); e != nil {
			return "", e
		}
		delete(files, name)
	}
	if e = downloadSyncDirectory(tmp); e != nil {
		return "", e
	}
	dest := filepath.Join(base, runID)
	if e = reportFault("export_generation"); e != nil {
		return "", e
	}
	if e = os.Rename(tmp, dest); e != nil {
		return "", e
	}
	d, e := os.Open(base)
	if e != nil {
		return "", e
	}
	e = d.Sync()
	ce := d.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return "", e
	}
	if e = reportFault("export_pointer"); e != nil {
		return "", e
	}
	rel := filepath.Join("reports", "exports", runID)
	if e = writeReportJSON(filepath.Join(base, "current.json"), map[string]string{"run_id": runID, "path": rel}); e != nil {
		return "", e
	}
	return rel, nil
}
func writePDFDictionary(path string) error {
	return atomicReportFile(path, func(w io.Writer) error {
		if _, e := fmt.Fprintln(w, "# PDF data dictionary\n\nAll records derive from retained PDFs. IDs are local to report hash and document order. Dates preserve raw value and precision; missing normalized dates, booleans and counts are null in JSON and empty CSV cells. Blank labeled values are retained; absent fields have no entry. Fields preserve order, repeated labels, path, one-based occurrence and meaningful line breaks. Evidence uses one-based physical PDF pages and deterministic word IDs; printed labels remain separate. Child CSVs contain accepted current snapshots only. Earlier/partial results remain archived.\n\nExact CSV header order:"); e != nil {
			return e
		}
		for _, name := range []string{"brokers_pdf.csv", "registration_history.csv", "employment_history.csv", "history_fields.csv", "disclosures.csv", "disclosure_sources.csv", "disclosure_fields.csv", "disclosure_counts.csv"} {
			if _, e := fmt.Fprintf(w, "\n## %s\n\n%s\n", name, jsonCell(reportCSVHeaders[name])); e != nil {
				return e
			}
		}
		return nil
	})
}

func isCurrentAccepted(s *ReportStore, t ReportTransition) bool {
	_, ok := currentAccepted(s, t)
	return ok
}
