package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestReportAtomicBoundaries(t *testing.T) {
	for _, boundary := range []string{"write", "sync", "close", "rename"} {
		t.Run(boundary, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "result")
			os.WriteFile(p, []byte("prior"), 0600)
			reportPublicationFault = func(b string) error {
				if b == boundary {
					return errors.New("injected")
				}
				return nil
			}
			defer func() { reportPublicationFault = nil }()
			if atomicReportFile(p, func(w io.Writer) error { _, e := w.Write([]byte("new")); return e }) == nil {
				t.Fatal("expected fault")
			}
			b, _ := os.ReadFile(p)
			if string(b) != "prior" {
				t.Fatal("prior changed")
			}
		})
	}
}
func TestReportStoreRecoveryAndLock(t *testing.T) {
	out := t.TempDir()
	s, e := OpenReportStore(out)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = OpenReportStore(out); e == nil {
		t.Fatal("duplicate writer")
	}
	if e = s.Append(ReportTransition{CRD: "1", AttemptID: "one", Outcome: "needs_review"}); e != nil {
		t.Fatal(e)
	}
	s.Close()
	p := filepath.Join(out, "reports", "manifest.jsonl")
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString(`{"torn":`)
	f.Close()
	s, e = OpenReportStore(out)
	if e != nil {
		t.Fatal(e)
	}
	if len(s.Latest) != 1 {
		t.Fatal(s.Latest)
	}
	s.Close()
	f, _ = os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString("broken\n")
	f.Close()
	if _, e = OpenReportStore(out); e == nil {
		t.Fatal("interior corruption accepted")
	}
}

func TestPriorExtractionSurvivesSameVersionPublication(t *testing.T) {
	s, e := OpenReportStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p := ParsedReport{Report: ReportRef{CRD: "1", SHA256: "hash"}, SchemaVersion: SchemaVersion, ParserVersion: ParserVersion, ExtractorVersion: ExtractorVersion}
	d := Document{Report: p.Report, ExtractorVersion: ExtractorVersion}
	oldDoc, oldParsed, e := s.Publish(d, p, ValidationResult{Accepted: true})
	if e != nil {
		t.Fatal(e)
	}
	before, e := os.ReadFile(filepath.Join(s.OutDir, oldParsed))
	if e != nil {
		t.Fatal(e)
	}
	d.Report.FetchedAt = "changed-retrieval"
	p.Report = d.Report
	newDoc, newParsed, e := s.Publish(d, p, ValidationResult{Accepted: false})
	if e != nil {
		t.Fatal(e)
	}
	if oldDoc == newDoc || oldParsed == newParsed {
		t.Fatal("same-version publication overwrote historical artifact")
	}
	after, e := os.ReadFile(filepath.Join(s.OutDir, oldParsed))
	if e != nil || string(before) != string(after) {
		t.Fatal("last accepted extraction changed", e)
	}
}
