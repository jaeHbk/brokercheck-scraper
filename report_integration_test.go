package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func integrationConfig(t *testing.T, base string) ReportsConfig {
	t.Helper()
	out := t.TempDir()
	b, e := os.ReadFile("testdata/pdf_inputs/fixtures.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(out, "brokers_unique.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	return ReportsConfig{OutDir: out, Input: "brokers_unique.json", Workers: 2, RPS: 10000, Retries: 1, Resume: true, BaseURL: base}
}
func fixtureReportServer(t *testing.T) (*httptest.Server, *atomic.Int64, *atomic.Int64) {
	t.Helper()
	calls := new(atomic.Int64)
	mode := new(atomic.Int64)
	fixtures := map[string][]byte{}
	for id, name := range map[string]string{"1691670": "1691670", "9000001": "synthetic-no-disclosures", "9000002": "synthetic-multi-source"} {
		b, e := os.ReadFile("testdata/pdf/" + name + ".pdf")
		if e != nil {
			t.Fatal(e)
		}
		fixtures["/individual_"+id+".pdf"] = b
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if mode.Load() == 403 {
			http.Error(w, "forbidden", 403)
			return
		}
		b, ok := fixtures[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Write(b)
		if mode.Load() == 1 {
			w.Write([]byte("\n% refreshed snapshot\n"))
		}
	}))
	t.Cleanup(s.Close)
	return s, calls, mode
}
func runReportsOK(t *testing.T, c ReportsConfig) ReportRunSummary {
	t.Helper()
	code, s, e := RunReports(context.Background(), c)
	if e != nil || code != 0 {
		for _, f := range s.Failures {
			t.Logf("failure %s %s parsed=%s", f.CRD, f.Outcome, f.ParsedPath)
			if f.ParsedPath != "" {
				b, _ := os.ReadFile(filepath.Join(c.OutDir, f.ParsedPath))
				var stored StoredReport
				json.Unmarshal(b, &stored)
				t.Logf("issues: %+v", stored.Validation.Issues)
			}
		}
		t.Fatalf("code=%d error=%v accepted=%d unresolved=%d", code, e, s.Accepted, s.Unresolved)
	}
	return s
}
func TestReportsIntegratedModesAndExports(t *testing.T) {
	server, calls, mode := fixtureReportServer(t)
	c := integrationConfig(t, server.URL)
	first := runReportsOK(t, c)
	if first.Accepted != 3 || calls.Load() != 3 {
		t.Fatal(first, calls.Load())
	}
	checkExportLinks(t, c.OutDir, first.ExportGeneration)
	resumed := runReportsOK(t, c)
	if resumed.SkippedAccepted != 3 || calls.Load() != 3 {
		t.Fatal("resume downloaded or failed to skip", resumed, calls.Load())
	}
	c.Reparse = true
	runReportsOK(t, c)
	if calls.Load() != 3 {
		t.Fatal("reparse used network")
	}
	c.Reparse = false
	c.Resume = false
	runReportsOK(t, c)
	if calls.Load() != 3 {
		t.Fatal("resume=false forced network")
	}
	c.Resume = true
	mode.Store(1)
	c.Refresh = true
	fresh := runReportsOK(t, c)
	if calls.Load() != 6 {
		t.Fatal(calls.Load())
	}
	checkExportLinks(t, c.OutDir, fresh.ExportGeneration)
	paths, _ := filepath.Glob(filepath.Join(c.OutDir, "reports", "pdf", "1691670", "*.pdf"))
	if len(paths) != 2 {
		t.Fatal("refresh lost snapshot", paths)
	}
	mode.Store(403)
	code, failed, e := RunReports(context.Background(), c)
	if code != 2 || e != nil || failed.Unresolved != 3 {
		t.Fatal(code, failed, e)
	}
	if b, _ := os.ReadFile(filepath.Join(c.OutDir, failed.ExportGeneration, "brokers_pdf.jsonl")); len(b) != 0 {
		t.Fatal("failed refresh exported old accepted content")
	}
	for _, f := range failed.Failures {
		if f.LastAcceptedHash == "" || f.Report == nil {
			t.Fatal("lost prior references", f)
		}
	}
	c.Refresh = false
	before := calls.Load()
	code, skipped, e := RunReports(context.Background(), c)
	if code != 2 || e != nil || skipped.SkippedUnresolved != 3 || calls.Load() != before {
		t.Fatal("unresolved resume semantics", code, skipped, e)
	}
	mode.Store(1)
	c.RetryFailed = true
	retried := runReportsOK(t, c)
	if retried.Accepted != 3 || calls.Load() != before+3 {
		t.Fatal("retry failed", retried, calls.Load())
	}
	empty := runReportsOK(t, c)
	if empty.Selected != 0 {
		t.Fatal("retry selected accepted", empty)
	}
}
func TestReportsCorruptionAndVersionRecovery(t *testing.T) {
	server, calls, _ := fixtureReportServer(t)
	c := integrationConfig(t, server.URL)
	c.Limit = 1
	first := runReportsOK(t, c)
	if first.Selected != 1 || first.Input != 3 {
		t.Fatal(first)
	}
	s, e := OpenReportStore(c.OutDir)
	if e != nil {
		t.Fatal(e)
	}
	tr := s.Latest["1691670"]
	s.Close()
	p := filepath.Join(c.OutDir, tr.ParsedPath)
	var stored StoredReport
	b, _ := os.ReadFile(p)
	json.Unmarshal(b, &stored)
	stored.Parsed.ParserVersion = "old"
	if e = writeReportJSON(p, stored); e != nil {
		t.Fatal(e)
	}
	repaired := runReportsOK(t, c)
	if repaired.SkippedAccepted != 0 || calls.Load() != 1 {
		t.Fatal("version reparse failed", repaired, calls.Load())
	}
	// Reparse publishes a new immutable artifact; corrupt the newly selected
	// artifact rather than an archived prior version.
	s, e = OpenReportStore(c.OutDir)
	if e != nil {
		t.Fatal(e)
	}
	tr = s.Latest["1691670"]
	s.Close()
	p = filepath.Join(c.OutDir, tr.ParsedPath)
	// Deliberately delete an otherwise valid-looking field; retained parent evidence must not mask it.
	b, _ = os.ReadFile(p)
	json.Unmarshal(b, &stored)
	stored.Parsed.Disclosures.Events[0].Sources[0].Fields = stored.Parsed.Disclosures.Events[0].Sources[0].Fields[:1]
	writeReportJSON(p, stored)
	repaired = runReportsOK(t, c)
	if repaired.SkippedAccepted != 0 || calls.Load() != 1 {
		t.Fatal("incomplete parse treated complete")
	}
	os.WriteFile(filepath.Join(c.OutDir, tr.Report.PDFPath), []byte("corrupt"), 0600)
	c.Reparse = true
	code, missing, e := RunReports(context.Background(), c)
	if code != 2 || e != nil || missing.Unresolved != 1 || calls.Load() != 1 {
		t.Fatal("reparse fetched corrupt bytes", code, missing, e)
	}
	c.Reparse = false
	runReportsOK(t, c)
	if calls.Load() != 2 {
		t.Fatal("corrupt PDF not recovered")
	}
}
func TestReportsCancellationAndMissingInput(t *testing.T) {
	server, calls, _ := fixtureReportServer(t)
	c := integrationConfig(t, server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, s, e := RunReports(ctx, c)
	if code == 0 || e == nil || s.Unresolved != 3 || calls.Load() != 0 {
		t.Fatal(code, s, e, calls.Load())
	}
	runReportsOK(t, c)
	c.Input = "missing.json"
	code, _, e = RunReports(context.Background(), c)
	if code != 1 || e == nil {
		t.Fatal("missing input should be config failure")
	}
}
func checkExportLinks(t *testing.T, out, generation string) {
	t.Helper()
	for name, header := range reportCSVHeaders {
		f, e := os.Open(filepath.Join(out, generation, name))
		if e != nil {
			t.Fatal(e)
		}
		rows, e := csv.NewReader(f).ReadAll()
		f.Close()
		if e != nil {
			t.Fatal(name, e)
		}
		if strings.Join(rows[0], "|") != strings.Join(header, "|") {
			t.Fatal("header mismatch", name)
		}
		for _, row := range rows[1:] {
			if name == "brokers_pdf.csv" {
				continue
			}
			if row[0] == "" || row[1] == "" || row[2] != SchemaVersion || row[3] != ParserVersion || row[4] != ExtractorVersion {
				t.Fatal("missing provenance", row)
			}
			paths, _ := filepath.Glob(filepath.Join(out, "reports", "pdf", row[0], row[1]+".pdf"))
			if len(paths) != 1 {
				t.Fatal("unlinked PDF", row)
			}
			if !json.Valid([]byte(row[len(row)-1])) {
				t.Fatal("invalid evidence", row)
			}
		}
	}
	f, e := os.Open(filepath.Join(out, generation, "disclosure_fields.csv"))
	if e != nil {
		t.Fatal(e)
	}
	rows, e := csv.NewReader(f).ReadAll()
	f.Close()
	if e != nil {
		t.Fatal(e)
	}
	foundMultiline, foundBlank := false, false
	for _, r := range rows[1:] {
		foundMultiline = foundMultiline || strings.Contains(r[11], "\n")
		foundBlank = foundBlank || r[11] == ""
	}
	if !foundMultiline || !foundBlank {
		t.Fatal(fmt.Sprintf("lost multiline=%v blank=%v", foundMultiline, foundBlank))
	}
}

func TestResumeRetriesInterruptedAndTransientRefresh(t *testing.T) {
	data, e := os.ReadFile("testdata/pdf/1691670.pdf")
	if e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int64
	var status atomic.Int64
	status.Store(200)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if status.Load() != 200 {
			w.WriteHeader(int(status.Load()))
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Write(data)
	}))
	defer server.Close()
	c := integrationConfig(t, server.URL)
	c.Limit = 1
	runReportsOK(t, c)
	status.Store(503)
	c.Refresh = true
	code, _, e := RunReports(context.Background(), c)
	if code != 2 || e != nil {
		t.Fatal(code, e)
	}
	status.Store(200)
	c.Refresh = false
	before := calls.Load()
	runReportsOK(t, c)
	if calls.Load() != before+1 {
		t.Fatal("ordinary resume silently accepted old bytes after failed refresh")
	}
	s, e := OpenReportStore(c.OutDir)
	if e != nil {
		t.Fatal(e)
	}
	tr := s.Latest["1691670"]
	tr.DownloadState = "pending"
	tr.ExtractionState = "pending"
	tr.Outcome = "pending"
	if e = s.Append(tr); e != nil {
		t.Fatal(e)
	}
	s.Close()
	before = calls.Load()
	runReportsOK(t, c)
	if calls.Load() != before+1 {
		t.Fatal("interrupted refresh did not refetch")
	}
}
