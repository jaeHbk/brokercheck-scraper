package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

type historyExpectedRecord struct {
	Kind              string  `json:"kind"`
	Scope             string  `json:"scope"`
	FirmName          string  `json:"firm_name"`
	FirmCRD           string  `json:"firm_crd"`
	Employer          string  `json:"employer"`
	Position          string  `json:"position"`
	InvestmentRelated *bool   `json:"investment_related"`
	Location          string  `json:"location"`
	Start             string  `json:"start_raw"`
	End               string  `json:"end_raw"`
	Pages             []int   `json:"physical_pages"`
	Fields            []Field `json:"fields"`
}

func TestHistoryAnnotatedFixtures(t *testing.T) {
	for _, name := range []string{"1691670", "synthetic-no-disclosures", "synthetic-multi-source"} {
		t.Run(name, func(t *testing.T) {
			b, e := os.ReadFile("testdata/pdf_expected/" + name + ".json")
			if e != nil {
				t.Fatal(e)
			}
			var want struct {
				CRD           string                  `json:"crd"`
				Hash          string                  `json:"sha256"`
				Registrations []historyExpectedRecord `json:"registrations"`
				Employments   []historyExpectedRecord `json:"employments"`
			}
			if e = json.Unmarshal(b, &want); e != nil {
				t.Fatal(e)
			}
			doc, e := ExtractDocument(context.Background(), ReportRef{CRD: want.CRD, SHA256: want.Hash, PDFPath: "testdata/pdf/" + name + ".pdf"})
			if e != nil {
				t.Fatal(e)
			}
			got := ParseHistory(doc)
			if historyTestBlocking(got.Issues) {
				t.Errorf("history issues: %+v", got.Issues)
			}
			if len(got.Registrations) != len(want.Registrations) || len(got.Employments) != len(want.Employments) {
				t.Fatalf("row counts: registrations=%d want %d employment=%d want %d", len(got.Registrations), len(want.Registrations), len(got.Employments), len(want.Employments))
			}
			for i, w := range want.Registrations {
				g := got.Registrations[i]
				actual := historyExpectedRecord{Kind: g.Kind, Scope: g.Scope, FirmName: g.FirmName, FirmCRD: g.FirmCRD, Location: g.Location, Start: g.Start.Raw, End: g.End.Raw, Pages: historyTestPages(g.Evidence), Fields: historyTestFields(g.Fields)}
				if !reflect.DeepEqual(actual, w) {
					t.Errorf("registration %d\ngot %+v\nwant %+v", i, actual, w)
				}
			}
			for i, w := range want.Employments {
				g := got.Employments[i]
				actual := historyExpectedRecord{Employer: g.Employer, Position: g.Position, InvestmentRelated: g.InvestmentRelated, Location: g.Location, Start: g.Start.Raw, End: g.End.Raw, Pages: historyTestPages(g.Evidence), Fields: historyTestFields(g.Fields)}
				if !reflect.DeepEqual(actual, w) {
					t.Errorf("employment %d\ngot %+v\nwant %+v", i, actual, w)
				}
			}
			if !reflect.DeepEqual(got, ParseHistory(doc)) {
				t.Error("parser output is nondeterministic")
			}
			historyTestEvidence(t, doc, got)
		})
	}
}
func historyTestFields(fs []Field) []Field {
	out := []Field{}
	for _, f := range fs {
		out = append(out, Field{Path: f.Path, Label: f.Label, Value: f.Value, Occurrence: f.Occurrence})
	}
	return out
}
func historyTestPages(es []Evidence) []int {
	out := []int{}
	for _, e := range es {
		out = append(out, e.Page)
	}
	return out
}
func historyTestEvidence(t *testing.T, doc Document, r HistoryResult) {
	t.Helper()
	words := map[string]int{}
	for _, p := range doc.Pages {
		for _, w := range p.Words {
			words[w.ID] = p.Number
		}
	}
	seen := map[string]bool{}
	var fs []Field
	for _, r := range r.Registrations {
		fs = append(fs, r.Fields...)
	}
	for _, r := range r.Employments {
		fs = append(fs, r.Fields...)
	}
	for _, f := range fs {
		if f.ID == "" || seen[f.ID] {
			t.Errorf("invalid duplicate field ID %q", f.ID)
		}
		seen[f.ID] = true
		if len(f.Evidence) == 0 {
			t.Errorf("field %s has no evidence", f.ID)
		}
		for _, e := range f.Evidence {
			if len(e.WordIDs) == 0 {
				t.Errorf("no words for field %s", f.ID)
			}
			for _, id := range e.WordIDs {
				if words[id] != e.Page {
					t.Errorf("evidence %s page mismatch", id)
				}
			}
		}
	}
	for _, c := range r.Coverage {
		covered := map[string]bool{}
		for _, id := range c.ConsumedWordIDs {
			covered[id] = true
		}
		for _, s := range c.Ignored {
			switch s.Reason {
			case "section heading", "column header", "identified guidance text", "repeated header/footer":
			default:
				t.Errorf("invalid ignore reason %q", s.Reason)
			}
			for _, id := range s.WordIDs {
				covered[id] = true
			}
		}
		for _, id := range c.RegionWordIDs {
			if !covered[id] {
				t.Errorf("uncovered %s in %s", id, c.Section)
			}
		}
	}
}
func TestHistoryDatePrecision(t *testing.T) {
	for _, c := range []struct{ raw, iso, precision string }{{"03/29/1996", "1996-03-29", "day"}, {"08/1987", "1987-08", "month"}, {"1987", "1987", "year"}, {"Present", "", "present"}, {"", "", "unknown"}, {"02/30/2020", "", "unknown"}, {"13/2020", "", "unknown"}} {
		d := historyDate(c.raw)
		iso := ""
		if d.ISO != nil {
			iso = *d.ISO
		}
		if iso != c.iso || d.Raw != c.raw || d.Precision != c.precision {
			t.Errorf("%q: %+v", c.raw, d)
		}
	}
}

// Constructed positioned inputs exercise layout rules independently of PDFs.
func historyTestDoc() Document {
	return Document{Report: ReportRef{CRD: "9000003", SHA256: strings.Repeat("a", 64)}, Pages: []Page{{Number: 1, Width: 800, Height: 620}, {Number: 2, Width: 800, Height: 620}}}
}
func historyTestLine(doc *Document, page int, x, y float64, text string) {
	p := &doc.Pages[page-1]
	line := fmt.Sprintf("p%d-l%d", page, len(p.Words)+1)
	for _, s := range strings.Fields(text) {
		w := Word{ID: fmt.Sprintf("p%d-w%d", page, len(p.Words)+1), LineID: line, Text: s, X0: x, Y0: y, X1: x + float64(len(s))*4, Y1: y + 9}
		p.Words = append(p.Words, w)
		x = w.X1 + 4
	}
	p.Text += "\n" + text
}
func historyTestHeader(doc *Document, page int, y float64) {
	for i, s := range []string{"Employment", "Employer Name", "Position", "Investment Related", "Employer Location"} {
		historyTestLine(doc, page, []float64{25, 160, 380, 490, 620}[i], y, s)
	}
}
func historyTestBase(doc *Document) {
	historyTestLine(doc, 1, 20, 40, "Current Registrations")
	historyTestLine(doc, 1, 20, 60, "This broker is not currently registered.")
	historyTestLine(doc, 1, 20, 80, "Registration History")
	historyTestLine(doc, 1, 20, 100, "No information reported.")
	historyTestLine(doc, 1, 20, 130, "Employment History")
}
func TestHistoryContinuationAndUnknownFields(t *testing.T) {
	doc := historyTestDoc()
	historyTestBase(&doc)
	historyTestHeader(&doc, 1, 160)
	historyTestLine(&doc, 1, 735, 160, "Extra Label")
	for i, s := range []string{"01/2020 - Present", "Wrapped Employer", "Senior", "N", "Albany, NY", "first value"} {
		historyTestLine(&doc, 1, []float64{25, 160, 380, 490, 620, 735}[i], 500, s)
	}
	historyTestLine(&doc, 2, 20, 40, "Employment History, continued")
	historyTestHeader(&doc, 2, 70)
	historyTestLine(&doc, 2, 735, 70, "Extra Label")
	historyTestLine(&doc, 2, 160, 100, "Corporation")
	historyTestLine(&doc, 2, 380, 100, "Advisor")
	historyTestLine(&doc, 2, 735, 100, "continued value")
	historyTestLine(&doc, 2, 20, 130, "Other Business Activities")
	historyTestLine(&doc, 2, 20, 150, "Unrelated activity")
	r := ParseHistory(doc)
	if historyTestBlocking(r.Issues) {
		t.Fatalf("issues %+v", r.Issues)
	}
	if len(r.Employments) != 1 {
		t.Fatalf("rows %+v", r.Employments)
	}
	g := r.Employments[0]
	if g.Employer != "Wrapped Employer\nCorporation" || g.Position != "Senior\nAdvisor" || len(g.Fields) != 6 || g.Fields[5].Label != "Extra Label" || g.Fields[5].Value != "first value\ncontinued value" || !reflect.DeepEqual(historyTestPages(g.Evidence), []int{1, 2}) {
		t.Errorf("continuation incorrectly parsed %+v", g)
	}
	if !strings.Contains(r.OtherBusinessText, "Unrelated activity") {
		t.Error("OBA not retained")
	}
	historyTestEvidence(t, doc, r)
}
func TestHistoryUnknownLayoutBlocks(t *testing.T) {
	doc := historyTestDoc()
	historyTestBase(&doc)
	historyTestLine(&doc, 1, 20, 160, "Unsupported table layout")
	historyTestLine(&doc, 1, 20, 180, "Unknown content must never be ignored")
	historyTestLine(&doc, 1, 20, 200, "Other Business Activities")
	r := ParseHistory(doc)
	if !historyTestBlocking(r.Issues) {
		t.Fatal("unknown content silently accepted")
	}
	for _, c := range r.Coverage {
		for _, s := range c.Ignored {
			if s.Reason == "other" {
				t.Fatal("arbitrary ignore")
			}
		}
	}
}
func TestHistoryMissingSectionsBlock(t *testing.T) {
	r := ParseHistory(Document{})
	if len(r.Issues) != 3 {
		t.Fatalf("missing section issues: %+v", r.Issues)
	}
	for _, c := range r.Coverage {
		if c.State != SectionAbsent {
			t.Errorf("absent section became %s", c.State)
		}
	}
}

func TestHistoryRepeatedExtraColumnsRemainDistinct(t *testing.T) {
	doc := historyTestDoc()
	historyTestBase(&doc)
	historyTestHeader(&doc, 1, 160)
	historyTestLine(&doc, 1, 715, 160, "Extra")
	historyTestLine(&doc, 1, 760, 160, "Extra")
	for i, s := range []string{"2020 - 2021", "Employer", "Analyst", "N", "NY", "one", "two"} {
		historyTestLine(&doc, 1, []float64{25, 160, 380, 490, 620, 715, 760}[i], 190, s)
	}
	historyTestLine(&doc, 1, 20, 220, "Other Business Activities")
	r := ParseHistory(doc)
	if historyTestBlocking(r.Issues) {
		t.Fatalf("issues %+v", r.Issues)
	}
	fs := r.Employments[0].Fields
	if len(fs) != 7 || fs[5].Label != "Extra" || fs[6].Label != "Extra" || fs[5].Value != "one" || fs[6].Value != "two" || fs[5].Occurrence != 1 || fs[6].Occurrence != 2 {
		t.Fatalf("repeated fields lost: %+v", fs)
	}
	if r.Employments[0].Start.Precision != "year" || *r.Employments[0].End.ISO != "2021" {
		t.Fatal("year precision lost")
	}
}
func TestHistoryPreviousScopesAndBlankCells(t *testing.T) {
	doc := historyTestDoc()
	historyTestLine(&doc, 1, 20, 40, "Current Registrations")
	historyTestLine(&doc, 1, 20, 60, "This broker is not currently registered.")
	historyTestLine(&doc, 1, 20, 90, "Registration History")
	for i, s := range []string{"Registration Dates", "Firm Name", "CRD#", "Branch Location"} {
		historyTestLine(&doc, 1, []float64{25, 200, 420, 520}[i], 120, s)
	}
	for j, row := range [][]string{{"IA 01/2020 - 02/2021", "First Firm", "123", ""}, {"BD 2010 - 2019", "Second Firm", "456", "New York"}} {
		for i, s := range row {
			if s != "" {
				historyTestLine(&doc, 1, []float64{25, 200, 420, 520}[i], 150+float64(j)*30, s)
			}
		}
	}
	historyTestLine(&doc, 1, 20, 210, "Employment History")
	historyTestLine(&doc, 1, 20, 230, "No employment history reported.")
	historyTestLine(&doc, 1, 20, 250, "Other Business Activities")
	r := ParseHistory(doc)
	if historyTestBlocking(r.Issues) {
		t.Fatalf("issues %+v", r.Issues)
	}
	if len(r.Registrations) != 2 {
		t.Fatalf("rows %+v", r.Registrations)
	}
	if r.Registrations[0].Scope != "IA" || r.Registrations[1].Scope != "BD" || r.Registrations[0].Fields[3].Value != "" || len(r.Registrations[0].Fields[3].Evidence) == 0 {
		t.Errorf("scopes/blank value %+v", r.Registrations)
	}
	historyTestEvidence(t, doc, r)
}
func TestHistoryHeaderOnlySectionBlocks(t *testing.T) {
	doc := historyTestDoc()
	historyTestBase(&doc)
	historyTestHeader(&doc, 1, 160)
	historyTestLine(&doc, 1, 20, 200, "Other Business Activities")
	r := ParseHistory(doc)
	found := false
	for _, i := range r.Issues {
		if i.Code == "empty_unexplained_history_section" {
			found = true
		}
	}
	if !found {
		t.Fatal("empty section accepted without report evidence")
	}
}

func historyTestBlocking(issues []ParseIssue) bool {
	for _, issue := range issues {
		if issue.Severity == "blocking" {
			return true
		}
	}
	return false
}

func TestHistoryEmptyDeclarationsCarryEvidence(t *testing.T) {
	doc := historyTestDoc()
	historyTestBase(&doc)
	historyTestLine(&doc, 1, 20, 160, "No employment history reported.")
	historyTestLine(&doc, 1, 20, 200, "Other Business Activities")
	r := ParseHistory(doc)
	if historyTestBlocking(r.Issues) {
		t.Fatalf("issues %+v", r.Issues)
	}
	evidence := map[string]bool{}
	for _, i := range r.Issues {
		if i.Code == "explicitly_empty_section" {
			if i.Severity != "warning" {
				t.Errorf("empty declaration severity %s", i.Severity)
			}
			for _, e := range i.Evidence {
				for _, id := range e.WordIDs {
					evidence[id] = true
				}
			}
		}
	}
	for _, c := range r.Coverage {
		if c.State != SectionExplicitlyEmpty {
			t.Errorf("unexpected state %s", c.State)
		}
		for _, id := range c.ConsumedWordIDs {
			if !evidence[id] {
				t.Errorf("empty declaration word %s lacks evidence", id)
			}
		}
	}
}

func TestHistoryEmptyTableKeepsHeaders(t *testing.T) {
	doc := historyTestDoc()
	historyTestBase(&doc)
	// The report can retain the printed table columns above its empty declaration.
	for i, s := range []string{"Registration Dates", "Firm Name", "CRD#", "Branch Location"} {
		historyTestLine(&doc, 1, []float64{25, 200, 420, 520}[i], 90, s)
	}
	historyTestLine(&doc, 1, 20, 160, "No employment history reported.")
	historyTestLine(&doc, 1, 20, 200, "Other Business Activities")
	r := ParseHistory(doc)
	if historyTestBlocking(r.Issues) {
		t.Fatalf("empty printed table rejected: %+v", r.Issues)
	}
	historyTestEvidence(t, doc, r)
	// A real row alongside the same declaration must still be rejected.
	for i, s := range []string{"BD 2020 - 2021", "Firm", "123", "NY"} {
		historyTestLine(&doc, 1, []float64{25, 200, 420, 520}[i], 115, s)
	}
	if !historyTestBlocking(ParseHistory(doc).Issues) {
		t.Fatal("contradictory empty and populated table accepted")
	}
}
func TestHistoryCurrentCityStateOnly(t *testing.T) {
	doc := historyTestDoc()
	historyTestLine(&doc, 1, 20, 40, "Current Registrations")
	for i, s := range []string{"IA MML INVESTORS SERVICES, LLC", "Tampa, FL", "CRD# 10409", "Registered with this firm since: 10/06/2025"} {
		historyTestLine(&doc, 1, 25, 65+float64(i)*15, s)
	}
	historyTestLine(&doc, 1, 20, 150, "Registration History")
	historyTestLine(&doc, 1, 20, 170, "No information reported.")
	historyTestLine(&doc, 1, 20, 200, "Employment History")
	historyTestLine(&doc, 1, 20, 225, "No employment history reported.")
	historyTestLine(&doc, 1, 20, 250, "Other Business Activities")
	r := ParseHistory(doc)
	if historyTestBlocking(r.Issues) {
		t.Fatalf("city/state rejected %+v", r.Issues)
	}
	if len(r.Registrations) != 1 || r.Registrations[0].FirmName != "MML INVESTORS SERVICES, LLC" || r.Registrations[0].Location != "Tampa, FL" || r.Registrations[0].Scope != "IA" {
		t.Fatalf("city/state boundary %+v", r.Registrations)
	}
	historyTestEvidence(t, doc, r)
}

func TestHistorySummaryLocationRetainedAndAmbiguityBlocks(t *testing.T) {
	doc := historyTestDoc()
	for i, s := range []string{"IA EXAMPLE FIRM", "CRD# 123", "Tampa, FL", "01/2020 - 02/2021"} {
		historyTestLine(&doc, 1, 265, 100+float64(i)*15, s)
	}
	rec := Registration{ID: "report-registration-1", Kind: "previous", Scope: "IA", FirmName: "EXAMPLE FIRM", FirmCRD: "123", Location: "New York, NY", Start: historyDate("01/2020"), End: historyDate("02/2021"), Fields: []Field{{Label: "Registration Dates", Value: "01/2020 - 02/2021"}, {Label: "Firm Name", Value: "EXAMPLE FIRM"}, {Label: "CRD#", Value: "123"}, {Label: "Branch Location", Value: "New York, NY"}}}
	result := HistoryResult{Registrations: []Registration{rec}}
	region := historyRegion{doc: doc, coverage: Coverage{Section: "registration_summary"}, result: &result, consumed: map[string]bool{}, ignored: map[string]bool{}}
	region.previousSummaryRecord(historySortedLines(doc.Pages[0]))
	if historyTestBlocking(result.Issues) {
		t.Fatalf("summary match %+v", result.Issues)
	}
	got := result.Registrations[0]
	if got.Location != "New York, NY" || len(got.Fields) != 5 || got.Fields[4].Value != "Tampa, FL" || got.Fields[4].Label != "Firm Location" || !reflect.DeepEqual(got.Fields[4].Path, []string{"Report Summary"}) {
		t.Fatalf("summary overwrote/lost data %+v", got)
	}
	if len(got.Fields[0].Evidence) == 0 || len(got.Fields[1].Evidence) == 0 || len(got.Fields[2].Evidence) == 0 {
		t.Fatal("redundant summary evidence lost")
	}
	result.Registrations = append(result.Registrations, rec)
	region.previousSummaryRecord(historySortedLines(doc.Pages[0]))
	if !historyTestBlocking(result.Issues) {
		t.Fatal("ambiguous summary match accepted")
	}
}
