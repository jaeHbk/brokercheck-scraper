package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Poppler subprocesses never use a shell. Overrides permit reproducible pinned installations.
func popplerBinary(name string) (string, error) {
	if p := os.Getenv(strings.ToUpper(name)); p != "" {
		return p, nil
	}
	p, e := exec.LookPath(name)
	if e == nil {
		return p, nil
	}
	// The desktop runtime bundles pdftotext without exposing it in its override PATH.
	if name == "pdftotext" {
		if info, err := exec.LookPath("pdfinfo"); err == nil {
			resolved, _ := filepath.EvalSymlinks(info)
			candidates := []string{filepath.Join(filepath.Dir(resolved), name), filepath.Join(filepath.Dir(info), "../../native/poppler/poppler/bin", name)}
			for _, candidate := range candidates {
				if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
					return candidate, nil
				}
			}
		}
	}
	return "", fmt.Errorf("required Poppler executable %s is unavailable: %w", name, e)
}

func ProbePDF(ctx context.Context, path string) (PDFInfo, error) {
	bin, err := popplerBinary("pdfinfo")
	if err != nil {
		return PDFInfo{}, err
	}
	out, err := exec.CommandContext(ctx, bin, path).CombinedOutput()
	if err != nil {
		return PDFInfo{}, fmt.Errorf("pdfinfo failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	info := PDFInfo{Backend: "Poppler", Metadata: map[string]string{}}
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if ok {
			info.Metadata[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	info.PageCount, err = strconv.Atoi(info.Metadata["Pages"])
	if err != nil || info.PageCount < 1 {
		return PDFInfo{}, fmt.Errorf("pdfinfo returned invalid page count")
	}
	version, err := exec.CommandContext(ctx, bin, "-v").CombinedOutput()
	if err != nil {
		return PDFInfo{}, fmt.Errorf("pdfinfo version: %w", err)
	}
	info.BackendVersion = strings.TrimSpace(strings.SplitN(string(version), "\n", 2)[0])
	return info, nil
}

type bboxWord struct {
	Text string  `xml:",chardata"`
	X0   float64 `xml:"xMin,attr"`
	Y0   float64 `xml:"yMin,attr"`
	X1   float64 `xml:"xMax,attr"`
	Y1   float64 `xml:"yMax,attr"`
}

func ExtractDocument(ctx context.Context, ref ReportRef) (Document, error) {
	info, err := ProbePDF(ctx, ref.PDFPath)
	if err != nil {
		return Document{}, err
	}
	bin, err := popplerBinary("pdftotext")
	if err != nil {
		return Document{}, err
	}
	cmd := exec.CommandContext(ctx, bin, "-bbox-layout", "-enc", "UTF-8", ref.PDFPath, "-")
	out, err := cmd.Output()
	if err != nil {
		return Document{}, fmt.Errorf("pdftotext failed: %w", err)
	}
	doc, err := decodeBBox(strings.NewReader(string(out)), ref)
	if err != nil {
		return Document{}, err
	}
	if len(doc.Pages) != info.PageCount {
		return Document{}, fmt.Errorf("page count mismatch: pdfinfo=%d positioned=%d", info.PageCount, len(doc.Pages))
	}
	doc.Report.PageCount = info.PageCount
	inspectDocument(&doc)
	return doc, nil
}

func decodeBBox(r io.Reader, ref ReportRef) (Document, error) {
	doc := Document{Report: ref, ExtractorVersion: ExtractorVersion, Pages: []Page{}, Issues: []ParseIssue{}}
	dec := xml.NewDecoder(r)
	var page *Page
	var line int
	var lineWords []string
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Document{}, fmt.Errorf("invalid Poppler positioned output: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "page":
				p := Page{Number: len(doc.Pages) + 1, Words: []Word{}}
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "width":
						p.Width, _ = strconv.ParseFloat(a.Value, 64)
					case "height":
						p.Height, _ = strconv.ParseFloat(a.Value, 64)
					}
				}
				if p.Width <= 0 || p.Height <= 0 {
					return Document{}, fmt.Errorf("invalid PDF page dimensions")
				}
				doc.Pages = append(doc.Pages, p)
				page = &doc.Pages[len(doc.Pages)-1]
				line = 0
			case "line":
				line++
				lineWords = nil
			case "word":
				if page == nil {
					return Document{}, fmt.Errorf("word outside page")
				}
				var b bboxWord
				if err := dec.DecodeElement(&b, &t); err != nil {
					return Document{}, err
				}
				w := Word{ID: fmt.Sprintf("p%d-w%d", page.Number, len(page.Words)+1), LineID: fmt.Sprintf("p%d-l%d", page.Number, line), Text: b.Text, X0: b.X0, Y0: b.Y0, X1: b.X1, Y1: b.Y1}
				if math.IsNaN(w.X0) || math.IsNaN(w.Y0) || math.IsNaN(w.X1) || math.IsNaN(w.Y1) || w.X1 < w.X0 || w.Y1 < w.Y0 {
					return Document{}, fmt.Errorf("invalid positioned word coordinates")
				}
				page.Words = append(page.Words, w)
				lineWords = append(lineWords, w.Text)
			}
		case xml.EndElement:
			if t.Name.Local == "line" && page != nil {
				if page.Text != "" {
					page.Text += "\n"
				}
				page.Text += strings.Join(lineWords, " ")
			}
			if t.Name.Local == "page" && page != nil {
				for _, w := range page.Words {
					if w.Y0 > page.Height-35 && w.X0 > page.Width*0.85 {
						if _, err := strconv.Atoi(w.Text); err == nil {
							page.PrintedLabel = w.Text
						}
					}
				}
				page = nil
			}
		}
	}
	if len(doc.Pages) == 0 {
		return Document{}, fmt.Errorf("Poppler returned no pages")
	}
	return doc, nil
}

var reportCRDPattern = regexp.MustCompile(`CRD\s*#\s*([0-9]+)`)

func inspectDocument(doc *Document) {
	// Identity is anchored to the cover/summary; firm CRDs in later sections are unrelated.
	found := false
	for _, p := range doc.Pages {
		if p.Number > 3 {
			break
		}
		m := reportCRDPattern.FindStringSubmatch(p.Text)
		if len(m) > 1 {
			found = true
			if m[1] != doc.Report.CRD {
				doc.Issues = append(doc.Issues, ParseIssue{Code: "crd_mismatch", Severity: "blocking", Section: "identity", Message: "PDF broker CRD " + m[1] + " differs from requested " + doc.Report.CRD, Evidence: []Evidence{{Page: p.Number, RawText: m[0]}}})
			}
			break
		}
	}
	if !found {
		doc.Issues = append(doc.Issues, ParseIssue{Code: "crd_missing", Severity: "blocking", Section: "identity", Message: "No broker CRD found on report cover or summary"})
	}
	for _, p := range doc.Pages {
		if len(p.Words) == 0 {
			doc.Issues = append(doc.Issues, ParseIssue{Code: "unreadable_page", Severity: "blocking", Section: "document", Message: fmt.Sprintf("Physical PDF page %d has no readable words; OCR is unavailable", p.Number), Evidence: []Evidence{{Page: p.Number}}})
		}
	}
	// The table of contents is independent of parser-recognized rows. Verify promised sections.
	if len(doc.Pages) > 0 && strings.Contains(doc.Pages[0].Text, "Section Title") {
		all := ""
		for _, p := range doc.Pages[1:] {
			all += "\n" + p.Text
		}
		for _, heading := range []string{"Registration and Employment History", "Disclosure Events"} {
			if strings.Contains(doc.Pages[0].Text, heading) && !strings.Contains(all, heading) {
				doc.Issues = append(doc.Issues, ParseIssue{Code: "outline_section_missing", Severity: "blocking", Section: "document", Message: "Table of contents promises missing section: " + heading, Evidence: []Evidence{{Page: 1, RawText: heading}}})
			}
		}
	}
}

type reportTextLine struct {
	Text           string
	Words          []Word
	X0, Y0, X1, Y1 float64
	Page           int
}

func reportLines(p Page) []reportTextLine {
	var result []reportTextLine
	indices := map[string]int{}
	for _, w := range p.Words {
		i, ok := indices[w.LineID]
		if !ok {
			i = len(result)
			indices[w.LineID] = i
			result = append(result, reportTextLine{X0: w.X0, Y0: w.Y0, X1: w.X1, Y1: w.Y1, Page: p.Number})
		}
		l := &result[i]
		l.Words = append(l.Words, w)
		if l.Text != "" {
			l.Text += " "
		}
		l.Text += w.Text
		l.X0 = math.Min(l.X0, w.X0)
		l.Y0 = math.Min(l.Y0, w.Y0)
		l.X1 = math.Max(l.X1, w.X1)
		l.Y1 = math.Max(l.Y1, w.Y1)
	}
	return result
}

// DetectReportSections derives complete spatial regions from headings, never recognized rows.
// Parsers must account for every returned word with field evidence or a permitted ignored span.
func DetectReportSections(doc Document) []Coverage {
	names := []string{"current_registration", "registration_history", "employment_history", "disclosure_summary", "disclosures", "registration_summary"}
	result := make([]Coverage, len(names))
	index := map[string]int{}
	seen := map[string]map[string]bool{}
	for i, n := range names {
		result[i] = Coverage{Section: n, State: SectionAbsent, RegionWordIDs: []string{}, ConsumedWordIDs: []string{}, Ignored: []IgnoredSpan{}}
		index[n] = i
		seen[n] = map[string]bool{}
	}
	add := func(name string, p Page, x0, y0, x1, y1 float64) {
		c := &result[index[name]]
		c.State = SectionPresent
		for _, w := range p.Words {
			if w.X0 >= x0-1 && w.X0 < x1 && w.Y0 >= y0-1 && w.Y0 < y1 {
				if !seen[name][w.ID] {
					c.RegionWordIDs = append(c.RegionWordIDs, w.ID)
					seen[name][w.ID] = true
				}
			}
		}
	}
	active := ""
	for _, p := range doc.Pages {
		lines := reportLines(p)
		summary := strings.Contains(p.Text, "Report Summary for this Broker")
		if summary {
			// Poppler can merge a long broker name with the summary title in one
			// line. Locate the title's words instead of assuming a separate line.
			right := p.Width/3 - 4
			for _, l := range lines {
				for i, w := range l.Words {
					if w.Text == "Report" && i+1 < len(l.Words) && l.Words[i+1].Text == "Summary" {
						right = w.X0 - 1
					}
				}
			}
			summaryEnd := p.Width * 2 / 3
			for _, l := range lines {
				if l.Text == "Disclosure Events" {
					summaryEnd = l.X0 - 1
				}
			}
			for _, l := range lines {
				if l.Text == "Registration History" && l.X0 >= right {
					add("registration_summary", p, l.X0, l.Y0, summaryEnd, p.Height-35)
				}
			}
			for _, l := range lines {
				if l.X0 < right && (strings.HasPrefix(l.Text, "Currently employed by and registered with") || strings.HasPrefix(l.Text, "This broker is not currently registered")) {
					add("current_registration", p, 0, l.Y0, right, p.Height-35)
				}
				if l.Text == "Disclosure Events" {
					add("disclosure_summary", p, l.X0, l.Y0, p.Width, p.Height-35)
				}
			}
			continue
		}

		// Cover contains headings but is not report content.
		if strings.Contains(p.Text, "Section Title") {
			continue
		}
		type boundary struct {
			name string
			y    float64
		}
		var bs []boundary
		for _, l := range lines {
			switch strings.TrimSuffix(l.Text, ", continued") {
			case "Registration History":
				if l.X0 < p.Width/3 {
					bs = append(bs, boundary{"registration_history", l.Y0})
				}
			case "Employment History":
				bs = append(bs, boundary{"employment_history", l.Y0})
			case "Other Business Activities":
				bs = append(bs, boundary{"", l.Y0})
			case "Disclosure Events":
				bs = append(bs, boundary{"disclosure_summary", l.Y0})
			case "Disclosure Event Details":
				bs = append(bs, boundary{"disclosures", l.Y0})
			case "End of Report", "Broker Qualifications":
				bs = append(bs, boundary{"", l.Y0})
			case "Current Registrations":
				bs = append(bs, boundary{"current_registration", l.Y0})
			}
		}
		// bbox flow ordering can differ from geometric order.
		for i := 1; i < len(bs); i++ {
			for j := i; j > 0 && bs[j].y < bs[j-1].y; j-- {
				bs[j], bs[j-1] = bs[j-1], bs[j]
			}
		}
		start := float64(25)
		for _, b := range bs {
			if active != "" && b.y > start {
				add(active, p, 0, start, p.Width, b.y)
			}
			active = b.name
			start = b.y
		}
		if active != "" {
			add(active, p, 0, start, p.Width, p.Height-30)
		}
	}
	// An absent details heading is explicitly empty only when a detected summary
	// region contains the fixture-tested negative disclosure declaration.
	if result[index["disclosures"]].State == SectionAbsent {
		summaryIDs := seen["disclosure_summary"]
		for _, p := range doc.Pages {
			for _, l := range reportLines(p) {
				if l.Text == "Are there events disclosed about this broker? No" {
					inside := len(l.Words) > 0
					for _, w := range l.Words {
						inside = inside && summaryIDs[w.ID]
					}
					if inside {
						result[index["disclosures"]].State = SectionExplicitlyEmpty
					}
				}
			}
		}
	}
	if result[index["registration_summary"]].State == SectionAbsent {
		result = result[:len(result)-1]
	}
	return result
}

// RecognizedReportVariant identifies only fixture-qualified layout signatures.
// It does not establish acceptance: independent field coverage and category reconciliation remain required.
func RecognizedReportVariant(doc Document) string {
	if len(doc.Pages) == 0 {
		return ""
	}
	all := ""
	for _, p := range doc.Pages {
		all += "\n" + p.Text
	}
	if strings.Contains(doc.Pages[0].Text, "BrokerCheck Report") && strings.Contains(doc.Pages[0].Text, "Section Title") && strings.Contains(all, "Report Summary for this Broker") && strings.Contains(all, "Registration and Employment History") && strings.Contains(all, "For your convenience, below is a matrix") && strings.Contains(all, "Disclosure Event Details") {
		return "finra-active-reports-category-summary-v1"
	}
	return ""
}
