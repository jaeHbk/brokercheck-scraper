package main

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReportsConfigAndInput(t *testing.T) {
	for _, args := range [][]string{{"--workers=0"}, {"--rps=0"}, {"--rps=NaN"}, {"--retries=0"}, {"--limit=-1"}, {"--refresh", "--reparse"}, {"--retry-failed", "--resume=false"}, {"extra"}} {
		if _, e := parseReportsConfig(args, io.Discard); e == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	c, e := parseReportsConfig(nil, io.Discard)
	if e != nil || c.RPS != .2 || c.Workers != 2 || c.Retries != 5 || !c.Resume {
		t.Fatalf("defaults %+v %v", c, e)
	}
	c.OutDir = t.TempDir()
	os.WriteFile(filepath.Join(c.OutDir, c.Input), []byte(`[{"ind_source_id":"10"},{"ind_source_id":"2"},{"ind_source_id":"0002"}]`), 0600)
	ids, _, _, e := readReportInput(c)
	if e != nil || !reflect.DeepEqual(ids, []string{"2", "10"}) {
		t.Fatalf("%v %v", ids, e)
	}
	c.Input = filepath.Join(c.OutDir, c.Input)
	ids, _, _, e = readReportInput(c)
	if e != nil || len(ids) != 2 {
		t.Fatal(ids, e)
	}
}
func TestReportManifestFailureNotCompleted(t *testing.T) {
	s, e := OpenReportStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	reportPublicationFault = func(b string) error {
		if b == "manifest" {
			return io.ErrShortWrite
		}
		return nil
	}
	defer func() { reportPublicationFault = nil }()
	if e = s.Append(ReportTransition{CRD: "1", AttemptID: "one", Outcome: "accepted"}); e == nil {
		t.Fatal("failure swallowed")
	}
	if len(s.Latest) != 0 {
		t.Fatal("false completion")
	}
}
func TestReportExportPointerFailures(t *testing.T) {
	for _, boundary := range []string{"export_sync", "export_generation", "export_pointer"} {
		t.Run(boundary, func(t *testing.T) {
			s, e := OpenReportStore(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			if _, e = PublishReportExports(s, "prior", nil); e != nil {
				t.Fatal(e)
			}
			p := filepath.Join(s.OutDir, "reports", "exports", "current.json")
			before, _ := os.ReadFile(p)
			reportPublicationFault = func(b string) error {
				if b == boundary {
					return io.ErrShortWrite
				}
				return nil
			}
			defer func() { reportPublicationFault = nil }()
			if _, e = PublishReportExports(s, "next", nil); e == nil {
				t.Fatal("failure swallowed")
			}
			after, _ := os.ReadFile(p)
			if string(before) != string(after) {
				t.Fatal("mixed generation")
			}
		})
	}
}
