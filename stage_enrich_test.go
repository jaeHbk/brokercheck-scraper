package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParseDetail_FullPayload(t *testing.T) {
	body, err := os.ReadFile("testdata/detail_2819404.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	d, err := parseDetail("2819404", body)
	if err != nil {
		t.Fatalf("parseDetail: %v", err)
	}
	if d.CRD != "2819404" {
		t.Errorf("CRD: got %q, want 2819404", d.CRD)
	}
	if d.BasicInfo.FirstName != "LAURENCE" {
		t.Errorf("FirstName: got %q", d.BasicInfo.FirstName)
	}
	if d.BasicInfo.MiddleName != "ANTHONY" {
		t.Errorf("MiddleName: got %q", d.BasicInfo.MiddleName)
	}
	if len(d.CurrentEmployments) == 0 {
		t.Errorf("expected current employments")
	}
	if len(d.PreviousEmployments) == 0 {
		t.Errorf("expected previous employments")
	}
	if d.ExamsCount.StateExamCount != 2 {
		t.Errorf("StateExamCount: got %d, want 2", d.ExamsCount.StateExamCount)
	}
	if len(d.RegisteredStates) != 2 {
		t.Errorf("RegisteredStates len: got %d, want 2", len(d.RegisteredStates))
	}
	if len(d.RegisteredSROs) == 0 {
		t.Errorf("expected SROs")
	}
}

func TestParseDetail_WithDisclosures(t *testing.T) {
	body, err := os.ReadFile("testdata/detail_5634972.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	d, err := parseDetail("5634972", body)
	if err != nil {
		t.Fatalf("parseDetail: %v", err)
	}
	if len(d.Disclosures) == 0 {
		t.Fatalf("expected disclosures, got 0")
	}
	if d.Disclosures[0].DisclosureType == "" {
		t.Errorf("disclosure type empty")
	}
	if d.Disclosures[0].DisclosureDetail == nil {
		t.Errorf("disclosure detail empty")
	}
}

func TestParseDetail_NotFound(t *testing.T) {
	body, err := os.ReadFile("testdata/detail_notfound.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	_, err = parseDetail("9999999", body)
	if !errors.Is(err, errBrokerNotFound) {
		t.Fatalf("expected errBrokerNotFound, got %v", err)
	}
}

func TestEnrich_ProducesOneLinePerCRD(t *testing.T) {
	// Synthetic detail server that echoes a minimal valid envelope
	// keyed on the CRD path segment.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		crd := parts[len(parts)-1]
		inner := fmt.Sprintf(`{"basicInformation":{"individualId":%s,"firstName":"X","lastName":"Y"}}`, crd)
		envelope := map[string]any{
			"hits": map[string]any{
				"total": 1,
				"hits":  []map[string]any{{"_source": map[string]string{"content": inner}}},
			},
		}
		_ = json.NewEncoder(w).Encode(envelope)
	}))
	defer srv.Close()

	dir := t.TempDir()
	crds := []string{"100", "200", "300"}

	cfg := enrichConfig{
		Out:     dir,
		Workers: 2,
		Resume:  true,
		BaseURL: srv.URL,
		CRDs:    crds,
	}
	c := newClient(newLimiter(100, 1), 3)
	if err := runEnrichWith(context.Background(), c, cfg); err != nil {
		t.Fatalf("runEnrichWith: %v", err)
	}

	// Verify jsonl has 3 lines
	f, err := os.Open(filepath.Join(dir, "brokers_detail.jsonl"))
	if err != nil {
		t.Fatalf("open jsonl: %v", err)
	}
	defer f.Close()
	count := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		count++
	}
	if count != 3 {
		t.Errorf("jsonl line count: got %d, want 3", count)
	}

	// Verify progress.jsonl has 3 lines
	pf, _ := os.Open(filepath.Join(dir, "enrich_progress.jsonl"))
	defer pf.Close()
	pc := 0
	psc := bufio.NewScanner(pf)
	for psc.Scan() {
		pc++
	}
	if pc != 3 {
		t.Errorf("progress line count: got %d, want 3", pc)
	}
}

func TestEnrich_ResumeSkipsCompleted(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		parts := strings.Split(r.URL.Path, "/")
		crd := parts[len(parts)-1]
		inner := fmt.Sprintf(`{"basicInformation":{"individualId":%s,"firstName":"X","lastName":"Y"}}`, crd)
		envelope := map[string]any{
			"hits": map[string]any{
				"total": 1,
				"hits":  []map[string]any{{"_source": map[string]string{"content": inner}}},
			},
		}
		_ = json.NewEncoder(w).Encode(envelope)
	}))
	defer srv.Close()

	dir := t.TempDir()
	// Pre-populate progress with one completed CRD
	pre := filepath.Join(dir, "enrich_progress.jsonl")
	if err := os.WriteFile(pre, []byte(`{"crd":"100","ts":"2026-05-22T00:00:00Z"}`+"\n"), 0644); err != nil {
		t.Fatalf("seed progress: %v", err)
	}

	cfg := enrichConfig{
		Out: dir, Workers: 2, Resume: true,
		BaseURL: srv.URL, CRDs: []string{"100", "200", "300"},
	}
	c := newClient(newLimiter(100, 1), 3)
	if err := runEnrichWith(context.Background(), c, cfg); err != nil {
		t.Fatalf("runEnrichWith: %v", err)
	}

	if hits != 2 {
		t.Errorf("expected 2 server hits (resume skipped 100), got %d", hits)
	}
}
