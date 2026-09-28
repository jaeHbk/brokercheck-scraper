package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

type disclosureExpectedField struct {
	Label      string   `json:"label"`
	Value      string   `json:"value"`
	Path       []string `json:"path"`
	Occurrence int      `json:"occurrence"`
}
type disclosureExpectedSource struct {
	Label  string                    `json:"label"`
	Fields []disclosureExpectedField `json:"fields"`
	Pages  []int                     `json:"physical_pages"`
}
type disclosureExpectedEvent struct {
	Type       string                     `json:"type"`
	Status     string                     `json:"status"`
	ReportedID string                     `json:"reported_id"`
	Fields     []disclosureExpectedField  `json:"fields"`
	Sources    []disclosureExpectedSource `json:"sources"`
	Pages      []int                      `json:"physical_pages"`
}
type disclosureExpectedCount struct {
	Category string `json:"category"`
	Status   string `json:"status"`
	Count    *int   `json:"count"`
	Raw      string `json:"raw"`
	Pages    []int  `json:"physical_pages"`
}

func disclosureEvidencePages(es []Evidence) []int {
	r := []int{}
	seen := map[int]bool{}
	for _, e := range es {
		if !seen[e.Page] {
			seen[e.Page] = true
			r = append(r, e.Page)
		}
	}
	return r
}
func disclosureCompareFields(t *testing.T, got []Field, want []disclosureExpectedField) {
	t.Helper()
	actual := []disclosureExpectedField{}
	for _, f := range got {
		actual = append(actual, disclosureExpectedField{f.Label, f.Value, f.Path, f.Occurrence})
		if f.ID == "" || len(f.Evidence) == 0 {
			t.Errorf("missing field provenance: %+v", f)
		}
	}
	if !reflect.DeepEqual(actual, want) {
		a, _ := json.MarshalIndent(actual, "", "  ")
		w, _ := json.MarshalIndent(want, "", "  ")
		t.Errorf("fields differ\ngot=%s\nwant=%s", a, w)
	}
}
func TestDisclosuresExactIndependentAnnotations(t *testing.T) {
	for _, fixture := range []struct{ name, crd string }{{"1691670", "1691670"}, {"synthetic-no-disclosures", "9000001"}, {"synthetic-multi-source", "9000002"}} {
		t.Run(fixture.name, func(t *testing.T) {
			doc := pinnedTextFixture(t, fixture.name, fixture.crd)
			data, err := os.ReadFile("testdata/pdf_expected/" + fixture.name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var expected struct {
				Events []disclosureExpectedEvent `json:"events"`
				Counts []disclosureExpectedCount `json:"counts"`
			}
			if err = json.Unmarshal(data, &expected); err != nil {
				t.Fatal(err)
			}
			actual := ParseDisclosures(doc)
			if len(actual.Issues) > 0 {
				t.Errorf("unexpected issues: %+v", actual.Issues)
			}
			if !reflect.DeepEqual(actual, ParseDisclosures(doc)) {
				t.Error("parser not deterministic")
			}
			if len(actual.Events) != len(expected.Events) {
				t.Fatalf("events got %d want %d", len(actual.Events), len(expected.Events))
			}
			ids := map[string]bool{}
			unique := func(id string) {
				if ids[id] || !strings.HasPrefix(id, doc.Report.SHA256+"-") {
					t.Errorf("invalid or duplicate report-scoped ID: %s", id)
				}
				ids[id] = true
			}
			for i, e := range actual.Events {
				want := expected.Events[i]
				unique(e.ID)
				if e.Type != want.Type || e.Status != want.Status || e.ReportedID != want.ReportedID || !reflect.DeepEqual(disclosureEvidencePages(e.Evidence), want.Pages) {
					t.Errorf("event %d metadata got %+v want %+v", i, e, want)
				}
				disclosureCompareFields(t, e.Fields, want.Fields)
				for _, f := range e.Fields {
					unique(f.ID)
				}
				if len(e.Sources) != len(want.Sources) {
					t.Fatalf("event%d sources got%d want%d", i, len(e.Sources), len(want.Sources))
				}
				for j, s := range e.Sources {
					w := want.Sources[j]
					unique(s.ID)
					if s.Label != w.Label || !reflect.DeepEqual(disclosureEvidencePages(s.Evidence), w.Pages) {
						t.Errorf("source%d metadata got %+v want %+v", j, s, w)
					}
					disclosureCompareFields(t, s.Fields, w.Fields)
					for _, f := range s.Fields {
						unique(f.ID)
					}
				}
			}
			counts := []disclosureExpectedCount{}
			for _, c := range actual.Counts {
				counts = append(counts, disclosureExpectedCount{c.Category, c.Status, c.Count, c.Raw, disclosureEvidencePages(c.Evidence)})
			}
			if !reflect.DeepEqual(counts, expected.Counts) {
				t.Errorf("counts got %+v want %+v", counts, expected.Counts)
			}
			for _, coverage := range actual.Coverage {
				used := map[string]bool{}
				for _, id := range coverage.ConsumedWordIDs {
					used[id] = true
				}
				for _, span := range coverage.Ignored {
					for _, id := range span.WordIDs {
						used[id] = true
					}
				}
				for _, id := range coverage.RegionWordIDs {
					if !used[id] {
						t.Errorf("unaccounted required word %s", id)
					}
				}
			}
		})
	}
}
func TestDisclosuresUnmatchedAndAmbiguousRemainBlocking(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Document)
	}{
		{"unknown content outside value column", func(d *Document) {
			p := &d.Pages[12]
			p.Words = append(p.Words, Word{ID: "unknown", LineID: "unknown", Text: "UNEXPLAINED", X0: 22, Y0: 579, X1: 150, Y1: 580})
		}},
		{"ambiguous event ordinal", func(d *Document) {
			for i := range d.Pages[2].Words {
				w := &d.Pages[2].Words[i]
				if w.Text == "2" && w.Y0 > 165 && w.Y0 < 185 {
					w.Text = "1"
				}
			}
		}},
		{"unlabeled source", func(d *Document) {
			p := &d.Pages[1]
			out := p.Words[:0]
			for _, w := range p.Words {
				if w.Text == "Broker" {
					continue
				}
				out = append(out, w)
			}
			p.Words = out
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name, crd := "synthetic-multi-source", "9000002"
			if strings.HasPrefix(tc.name, "unknown") {
				name, crd = "1691670", "1691670"
			}
			doc := pinnedTextFixture(t, name, crd)
			tc.mutate(&doc)
			got := ParseDisclosures(doc)
			found := false
			for _, issue := range got.Issues {
				found = found || issue.Severity == "blocking"
			}
			if !found {
				t.Fatal("ambiguous or unmatched content accepted despite unchanged summary counts")
			}
		})
	}
}

func TestDisclosuresFieldLossRejectedDespiteMatchingCounts(t *testing.T) {
	doc := pinnedTextFixture(t, "synthetic-multi-source", "9000002")
	p := ParsedReport{Report: doc.Report, SchemaVersion: SchemaVersion, ParserVersion: ParserVersion, ExtractorVersion: doc.ExtractorVersion, History: ParseHistory(doc), Disclosures: ParseDisclosures(doc)}
	// Preserve parent evidence and all event/count records: raw text and counts alone must not mask field loss.
	source := &p.Disclosures.Events[0].Sources[0]
	source.Fields = append(source.Fields[:1], source.Fields[2:]...)
	v := ValidateReport(doc, p)
	found := false
	for _, issue := range v.Issues {
		if issue.Code == "disclosure_artifact_inconsistent" && issue.Severity == "blocking" {
			found = true
		}
	}
	if v.Accepted || !found {
		t.Fatalf("dropped field was not independently rejected: %+v", v)
	}
}

func TestDisclosuresNarrativeProvenanceAndBoundaryTotal(t *testing.T) {
	doc := pinnedTextFixture(t, "synthetic-multi-source", "9000002")
	got := ParseDisclosures(doc)
	narrative := got.Events[0].Sources[0].Fields[6]
	if !reflect.DeepEqual(disclosureEvidencePages(narrative.Evidence), []int{2, 3}) {
		t.Fatalf("continued narrative lost physical-page evidence: %+v", narrative)
	}
	// Both displayed boundary totals are corrupted to three while summary count remains two.
	for pi := range doc.Pages {
		for wi := range doc.Pages[pi].Words {
			w := &doc.Pages[pi].Words[wi]
			if w.Text != "2" {
				continue
			}
			for _, line := range reportLines(doc.Pages[pi]) {
				if line.Words[0].LineID == w.LineID && strings.HasPrefix(line.Text, "Disclosure ") {
					if wi > 0 && doc.Pages[pi].Words[wi-1].Text == "of" {
						w.Text = "3"
					}
				}
			}
		}
	}
	rejected := ParseDisclosures(doc)
	found := false
	for _, issue := range rejected.Issues {
		found = found || issue.Code == "disclosure_boundary_count_mismatch"
	}
	if !found {
		t.Fatal("inconsistent explicit boundary total accepted")
	}
}

// Positioned excerpts retain original physical pages and word IDs; omitted pages
// are empty placeholders because these fixtures exercise only disclosure parsing.
func disclosureExcerpt(t *testing.T, name string) Document {
	t.Helper()
	data, err := os.ReadFile("testdata/pdf_disclosure_cases/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var doc Document
	if err = json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}
func TestDisclosuresExactAdviserSummaryPanel(t *testing.T) {
	doc := disclosureExcerpt(t, "2377028-summary")
	got := ParseDisclosures(doc)
	if len(got.Issues) != 0 || len(got.Events) != 0 || len(got.Counts) != 1 || got.Counts[0].Count == nil || *got.Counts[0].Count != 0 {
		t.Fatalf("verified summary panel rejected: %+v", got)
	}
	// Two retained reports indent the same URL inside the advisory column.
	for i := range doc.Pages[2].Words {
		if doc.Pages[2].Words[i].Text == "https://www.adviserinfo.sec.gov" {
			doc.Pages[2].Words[i].X0 += 50.4
			doc.Pages[2].Words[i].X1 += 50.4
		}
	}
	if len(ParseDisclosures(doc).Issues) != 0 {
		t.Fatal("verified indented advisory link rejected")
	}
	// Changing the complete panel must not become permission to ignore its text.
	for i := range doc.Pages[2].Words {
		if doc.Pages[2].Words[i].Text == "https://www.adviserinfo.sec.gov" {
			doc.Pages[2].Words[i].Text = "https://unverified.example.invalid"
		}
	}
	if len(ParseDisclosures(doc).Issues) == 0 {
		t.Fatal("unverified advisory panel was ignored")
	}
}
func TestDisclosuresTerminationSeparateSourceFields(t *testing.T) {
	doc := disclosureExcerpt(t, "4960324-termination")
	got := ParseDisclosures(doc)
	if len(got.Issues) != 0 || len(got.Events) != 1 {
		t.Fatalf("termination parse: %+v", got)
	}
	event := got.Events[0]
	if event.Type != "Employment Separation After Allegations" || event.Status != "" || event.ReportedID != "1" || len(event.Sources) != 2 {
		t.Fatalf("event metadata: %+v", event)
	}
	narrative := "CONDUCT INVOLVING FAILURE TO FOLLOW THE FIRM'S PROCEDURES\nFOR MONETARY DISBURSEMENTS AND WITHDRAWALS, INCLUDING\nPROVIDING INACCURATE INFORMATION IN INTERNAL SYSTEMS IN ORDER\nTO EXECUTE AUTHORIZED TRANSACTIONS"
	for i, label := range []string{"Firm", "Broker"} {
		source := event.Sources[i]
		if source.Label != label {
			t.Errorf("source%d label=%s", i, source.Label)
		}
		value := narrative
		if i == 0 {
			value += "."
		}
		want := []disclosureExpectedField{{"Employer Name:", "MERRILL LYNCH, PIERCE, FENNER & SMITH, INC", []string{}, 1}, {"Termination Type:", "Discharged", []string{}, 1}, {"Termination Date:", "03/04/2013", []string{}, 1}, {"Allegations:", value, []string{}, 1}, {"Product Type:", "No Product", []string{}, 1}}
		disclosureCompareFields(t, source.Fields, want)
		if !reflect.DeepEqual(disclosureEvidencePages(source.Evidence), []int{9}) {
			t.Error("source lost physical-page provenance")
		}
	}
	if len(got.Counts) != 4 || got.Counts[0].Category != "Termination" || got.Counts[0].Count == nil || *got.Counts[0].Count != 1 {
		t.Errorf("reported counts lost: %+v", got.Counts)
	}
}
