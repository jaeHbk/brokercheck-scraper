package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func pinnedTextFixture(t *testing.T, name, crd string) Document {
	t.Helper()
	path := "testdata/pdf/" + name + ".pdf"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(data)
	doc, err := ExtractDocument(context.Background(), ReportRef{CRD: crd, SHA256: hex.EncodeToString(h[:]), PDFPath: path, Bytes: int64(len(data))})
	if err != nil {
		t.Fatal("Poppler must run in acceptance environment (missing dependency is not a skip):", err)
	}
	return doc
}
func TestPositionedRealReport(t *testing.T) {
	doc := pinnedTextFixture(t, "1691670", "1691670")
	if len(doc.Pages) != 14 {
		t.Fatalf("physical pages=%d", len(doc.Pages))
	}
	if len(doc.Issues) != 0 {
		t.Fatalf("issues=%+v", doc.Issues)
	}
	if doc.Pages[2].PrintedLabel != "1" || doc.Pages[9].PrintedLabel != "8" || doc.Pages[12].PrintedLabel != "11" {
		t.Fatal("physical and printed page labels conflated")
	}
	for _, p := range doc.Pages {
		if p.Width != 792 || p.Height != 612 {
			t.Fatal("coordinate units are not points")
		}
		seen := map[string]bool{}
		for _, w := range p.Words {
			if seen[w.ID] || w.LineID == "" || w.ID == "" {
				t.Fatal("invalid deterministic IDs")
			}
			seen[w.ID] = true
			if w.X1 < w.X0 || w.Y1 < w.Y0 {
				t.Fatal("reversed coordinate bounds")
			}
		}
	}
	again := pinnedTextFixture(t, "1691670", "1691670")
	if !reflect.DeepEqual(doc, again) {
		t.Fatal("positioned extraction is not deterministic")
	}
	if RecognizedReportVariant(doc) != "finra-active-reports-category-summary-v1" {
		t.Fatal("fixture-qualified variant not recognized")
	}
}
func TestIndependentRequiredRegions(t *testing.T) {
	doc := pinnedTextFixture(t, "1691670", "1691670")
	regions := DetectReportSections(doc)
	expectedPages := map[string]int{"current_registration": 3, "registration_history": 10, "employment_history": 10, "disclosure_summary": 12, "disclosures": 13, "registration_summary": 3}
	words := map[string]int{}
	for _, p := range doc.Pages {
		for _, w := range p.Words {
			words[w.ID] = p.Number
		}
	}
	for _, r := range regions {
		if r.State != SectionPresent || len(r.RegionWordIDs) == 0 {
			t.Fatalf("missing region %s", r.Section)
		}
		found := false
		for _, id := range r.RegionWordIDs {
			if words[id] == expectedPages[r.Section] {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s missing physical page%d", r.Section, expectedPages[r.Section])
		}
	}
	// An unrecognized word inside a required region must remain visible to validation.
	doc.Pages[12].Words = append(doc.Pages[12].Words, Word{ID: "deliberately-unrecognized", LineID: "extra", Text: "UNRECOGNIZED", X0: 30, Y0: 570, X1: 150, Y1: 580})
	found := false
	for _, r := range DetectReportSections(doc) {
		if r.Section == "disclosures" {
			for _, id := range r.RegionWordIDs {
				found = found || id == "deliberately-unrecognized"
			}
		}
	}
	if !found {
		t.Fatal("region detector dropped unfamiliar content")
	}
}
func TestExtractorRejectsIdentityAndUnreadable(t *testing.T) {
	doc := pinnedTextFixture(t, "1691670", "1")
	if len(doc.Issues) == 0 || doc.Issues[0].Code != "crd_mismatch" {
		t.Fatal("mismatched report identity not rejected")
	}
	blank := Document{Report: ReportRef{CRD: "1"}, Pages: []Page{{Number: 1, Text: "CRD# 1"}, {Number: 2}}}
	inspectDocument(&blank)
	found := false
	for _, issue := range blank.Issues {
		found = found || issue.Code == "unreadable_page"
	}
	if !found {
		t.Fatal("unreadable page not blocking")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ProbePDF(ctx, "testdata/pdf/1691670.pdf"); err == nil {
		t.Fatal("canceled probe succeeded")
	}
}
func TestExtractorMalformedOutputAndOutline(t *testing.T) {
	for _, s := range []string{"not XML", "<doc><page width=\"0\" height=\"612\"></page></doc>", "<doc><word>x</word></doc>"} {
		if _, err := decodeBBox(strings.NewReader(s), ReportRef{}); err == nil {
			t.Fatalf("accepted malformed output %q", s)
		}
	}
	doc := Document{Report: ReportRef{CRD: "1"}, Pages: []Page{{Number: 1, Text: "BrokerCheck Report\nCRD# 1\nSection Title\nDisclosure Events", Words: []Word{{ID: "a", Text: "cover"}}}, {Number: 2, Text: "Employment History", Words: []Word{{ID: "b", Text: "history"}}}}}
	inspectDocument(&doc)
	found := false
	for _, i := range doc.Issues {
		found = found || i.Code == "outline_section_missing"
	}
	if !found {
		t.Fatal("missing promised outline section not blocking")
	}
	if RecognizedReportVariant(doc) != "" {
		t.Fatal("unknown variant recognized")
	}
}
func TestPinnedFixtureHashesAndSyntheticExtraction(t *testing.T) {
	for _, name := range []string{"1691670", "synthetic-no-disclosures", "synthetic-multi-source"} {
		raw, err := os.ReadFile("testdata/pdf_expected/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var spec struct {
			SHA256    string `json:"sha256"`
			CRD       string `json:"crd"`
			Synthetic bool   `json:"synthetic"`
		}
		if err = json.Unmarshal(raw, &spec); err != nil {
			t.Fatal(err)
		}
		doc := pinnedTextFixture(t, name, spec.CRD)
		if doc.Report.SHA256 != spec.SHA256 {
			t.Fatalf("fixture hash changed without independent review: %s", name)
		}
		if spec.Synthetic && !strings.Contains(doc.Pages[0].Text, "SYNTHETIC DEVELOPMENT FIXTURE") {
			t.Fatal("synthetic provenance not visible")
		}
	}
}
