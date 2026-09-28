package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

var historyRange = regexp.MustCompile(`^(?:(BD|B|IA|other|unknown)\s+)?(\d{2}/\d{2}/\d{4}|\d{2}/\d{4}|\d{4})\s+-\s+(\d{2}/\d{2}/\d{4}|\d{2}/\d{4}|\d{4}|Present)$`)
var historyAddress = regexp.MustCompile(`^(\d+\s|P\.?O\.? BOX\s|POST OFFICE BOX\s)`)
var historyCityState = regexp.MustCompile(`^[A-Za-z .'-]+, (?:AL|AK|AZ|AR|CA|CO|CT|DE|DC|FL|GA|HI|ID|IL|IN|IA|KS|KY|LA|ME|MD|MA|MI|MN|MS|MO|MT|NE|NV|NH|NJ|NM|NY|NC|ND|OH|OK|OR|PA|RI|SC|SD|TN|TX|UT|VT|VA|WA|WV|WI|WY|PR|VI|GU)(?: \d{5}(?:-\d{4})?)?$`)

func historyDate(raw string) RawDate {
	d := RawDate{Raw: raw, Precision: "unknown"}
	if strings.EqualFold(raw, "Present") {
		d.Precision = "present"
		return d
	}
	for _, f := range []struct{ layout, out, precision string }{{"01/02/2006", "2006-01-02", "day"}, {"01/2006", "2006-01", "month"}, {"2006", "2006", "year"}} {
		if t, e := time.Parse(f.layout, raw); e == nil {
			s := t.Format(f.out)
			d.ISO = &s
			d.Precision = f.precision
			return d
		}
	}
	return d
}

// ParseHistory reads only independently detected regions. Unknown text remains
// unconsumed and blocking; record evidence never substitutes for ordered fields.
func ParseHistory(doc Document) HistoryResult {
	r := HistoryResult{Registrations: []Registration{}, Employments: []EmploymentRecord{}, Coverage: []Coverage{}, Issues: []ParseIssue{}}
	for _, c := range DetectReportSections(doc) {
		if c.Section != "current_registration" && c.Section != "registration_history" && c.Section != "employment_history" {
			continue
		}
		p := historyRegion{doc: doc, coverage: c, result: &r, consumed: map[string]bool{}, ignored: map[string]bool{}}
		p.lines = historyRegionLines(doc, c.RegionWordIDs)
		before := len(r.Registrations) + len(r.Employments)
		if c.State == SectionAbsent {
			p.issue("missing_history_section", "Required history section is absent", nil)
		} else if c.Section == "current_registration" {
			p.current()
		} else {
			p.table()
		}
		if p.coverage.State == SectionExplicitlyEmpty && len(r.Registrations)+len(r.Employments) > before {
			p.issue("contradictory_history_section", "An explicitly empty history section also contains records", historyEvidence(doc, c.RegionWordIDs))
		}
		if p.coverage.State == SectionPresent && len(r.Registrations)+len(r.Employments) == before {
			p.issue("empty_unexplained_history_section", "History section has no readable records or explicit empty declaration", historyEvidence(doc, c.RegionWordIDs))
		}
		for _, id := range c.RegionWordIDs {
			if p.consumed[id] {
				p.coverage.ConsumedWordIDs = append(p.coverage.ConsumedWordIDs, id)
			} else if !p.ignored[id] {
				p.issue("unmatched_history_content", "Required history content has no extracted field or identified structural role", historyEvidence(doc, []string{id}))
			}
		}
		r.Coverage = append(r.Coverage, p.coverage)
	}
	// The summary may provide a firm location different from the detailed branch.
	for _, c := range DetectReportSections(doc) {
		if c.Section != "registration_summary" || c.State == SectionAbsent {
			continue
		}
		p := historyRegion{doc: doc, coverage: c, result: &r, consumed: map[string]bool{}, ignored: map[string]bool{}}
		p.lines = historyRegionLines(doc, c.RegionWordIDs)
		p.previousSummary()
		for _, id := range c.RegionWordIDs {
			if p.consumed[id] {
				p.coverage.ConsumedWordIDs = append(p.coverage.ConsumedWordIDs, id)
			} else if !p.ignored[id] {
				p.issue("unmatched_history_content", "Prior-registration summary content is not represented by a matched field", historyEvidence(doc, []string{id}))
			}
		}
		r.Coverage = append(r.Coverage, p.coverage)
	}
	// This adjacent optional section is kept separately, including page continuations.
	active := false
	for _, page := range doc.Pages {
		for _, l := range historySortedLines(page) {
			if strings.HasPrefix(l.Text, "Other Business Activities") {
				active = true
				continue
			}
			if l.Text == "Disclosure Events" || l.Text == "Disclosure Event Details" || l.Text == "End of Report" {
				active = false
			}
			if active && l.Y0 >= 25 && l.Y0 < page.Height-30 && l.Text != "Registration and Employment History" {
				if r.OtherBusinessText != "" {
					r.OtherBusinessText += "\n"
				}
				r.OtherBusinessText += l.Text
			}
		}
	}
	return r
}

type historyRegion struct {
	doc               Document
	lines             []reportTextLine
	coverage          Coverage
	result            *HistoryResult
	consumed, ignored map[string]bool
}

func (p *historyRegion) issue(code, message string, e []Evidence) {
	p.result.Issues = append(p.result.Issues, ParseIssue{Code: code, Severity: "blocking", Section: p.coverage.Section, Message: message, Evidence: e})
}
func historySortedLines(page Page) []reportTextLine {
	ls := reportLines(page)
	sort.SliceStable(ls, func(i, j int) bool {
		if ls[i].Y0 != ls[j].Y0 {
			return ls[i].Y0 < ls[j].Y0
		}
		return ls[i].X0 < ls[j].X0
	})
	return ls
}
func historyRegionLines(doc Document, ids []string) []reportTextLine {
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	var ls []reportTextLine
	for _, page := range doc.Pages {
		part := Page{Number: page.Number}
		for _, w := range page.Words {
			if wanted[w.ID] {
				part.Words = append(part.Words, w)
			}
		}
		ls = append(ls, historySortedLines(part)...)
	}
	return ls
}
func historyIDs(lines []reportTextLine) []string {
	ids := []string{}
	for _, l := range lines {
		for _, w := range l.Words {
			ids = append(ids, w.ID)
		}
	}
	return ids
}
func historyEvidence(doc Document, ids []string) []Evidence {
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	es := []Evidence{}
	for _, page := range doc.Pages {
		filtered := Page{Number: page.Number}
		for _, w := range page.Words {
			if wanted[w.ID] {
				filtered.Words = append(filtered.Words, w)
			}
		}
		if len(filtered.Words) == 0 {
			continue
		}
		ls := historySortedLines(filtered)
		s := []string{}
		for _, l := range ls {
			s = append(s, l.Text)
		}
		es = append(es, Evidence{Page: page.Number, WordIDs: historyIDs(ls), RawText: strings.Join(s, "\n")})
	}
	return es
}
func (p *historyRegion) ignore(l reportTextLine, why string) {
	ids := historyIDs([]reportTextLine{l})
	p.coverage.Ignored = append(p.coverage.Ignored, IgnoredSpan{WordIDs: ids, Reason: why})
	for _, id := range ids {
		p.ignored[id] = true
	}
}
func (p *historyRegion) consume(ids []string) {
	for _, id := range ids {
		p.consumed[id] = true
	}
}
func (p *historyRegion) field(parent, label, value string, ids []string, ordinal, occurrence int) Field {
	p.consume(ids)
	return Field{ID: fmt.Sprintf("%s-field-%d", parent, ordinal), Path: []string{}, Label: label, Value: value, Occurrence: occurrence, Evidence: historyEvidence(p.doc, ids)}
}
func historyStructural(text string) string {
	switch strings.TrimSuffix(text, ", continued") {
	case "Current Registrations", "Registration History", "Employment History", "Registration and Employment History":
		return "section heading"
	}
	switch text {
	case "Currently employed by and registered with the", "following Firm(s):", "The broker previously was registered with the following firms:",
		"This broker was previously registered with the", "following securities firm(s):",
		"This section provides up to 10 years of an individual broker's employment history as reported by the individual broker on the most recently filed",
		"Form U4.", "Please note that the broker is required to provide this information only while registered with FINRA or a national securities exchange",
		"and the information is not updated via Form U4 after the broker ceases to be registered. Therefore, an employment end date of",
		`"Present" may not reflect the broker's current employment status.`:
		return "identified guidance text"
	}
	return ""
}
func (p *historyRegion) clean() []reportTextLine {
	var ls []reportTextLine
	for _, l := range p.lines {
		if why := historyStructural(l.Text); why != "" {
			p.ignore(l, why)
			continue
		}
		if l.Text == "This broker is not currently registered." || l.Text == "No information reported." || l.Text == "No employment history reported." {
			p.coverage.State = SectionExplicitlyEmpty
			ids := historyIDs([]reportTextLine{l})
			p.result.Issues = append(p.result.Issues, ParseIssue{Code: "explicitly_empty_section", Severity: "warning", Section: p.coverage.Section, Message: "Report explicitly declares this history section empty", Evidence: historyEvidence(p.doc, ids)})
			p.consume(ids)
			continue
		}
		if l.Text == "www.finra.org/brokercheck" || l.Text == "User Guidance" {
			p.ignore(l, "repeated header/footer")
			continue
		}
		ls = append(ls, l)
	}
	return ls
}

func (p *historyRegion) current() {
	ls := p.clean()
	if len(ls) == 0 {
		return
	}
	var group []reportTextLine
	flush := func() {
		if len(group) > 0 {
			p.currentRecord(group)
			group = nil
		}
	}
	for _, l := range ls {
		if len(l.Words) > 1 && (l.Words[0].Text == "IA" || l.Words[0].Text == "B" || l.Words[0].Text == "BD") {
			flush()
			group = append(group, l)
		} else if len(group) > 0 {
			group = append(group, l)
		}
	}
	flush()
}
func (p *historyRegion) currentRecord(ls []reportTextLine) {
	first := ls[0].Words[0]
	scope := first.Text
	if scope == "B" {
		scope = "BD"
	}
	// Name/address boundaries are qualified by a recognizable street or PO box.
	// Unsupported address formats cannot be accepted by assigning leftover text.
	addr, crd, date := -1, -1, -1
	for i, l := range ls {
		if i > 0 && addr < 0 && historyAddress.MatchString(l.Text) {
			addr = i
		}
		if strings.HasPrefix(l.Text, "CRD# ") {
			crd = i
		}
		if strings.HasPrefix(l.Text, "Registered with this firm since:") {
			date = i
		}
	}
	// Some current IA summaries report only a city and state, with no street.
	// Accept this bounded variant only immediately before the labeled firm CRD.
	if addr < 0 && crd >= 2 && historyCityState.MatchString(ls[crd-1].Text) {
		addr = crd - 1
	}
	if addr < 1 || crd <= addr || date != crd+1 || date != len(ls)-1 {
		p.issue("unknown_current_registration_layout", "Current registration requires distinct firm, address, CRD, and registration date boundaries", historyEvidence(p.doc, historyIDs(ls)))
		return
	}
	id := fmt.Sprintf("%s-registration-%d", p.doc.Report.SHA256, len(p.result.Registrations)+1)
	nameLines := append([]reportTextLine{}, ls[:addr]...)
	nameLines[0].Words = append([]Word{}, nameLines[0].Words[1:]...)
	nameLines[0].Text = strings.TrimSpace(strings.TrimPrefix(nameLines[0].Text, first.Text))
	join := func(lines []reportTextLine) string {
		ss := []string{}
		for _, l := range lines {
			ss = append(ss, l.Text)
		}
		return strings.Join(ss, "\n")
	}
	name, loc := join(nameLines), join(ls[addr:crd])
	firmCRD := strings.TrimSpace(strings.TrimPrefix(ls[crd].Text, "CRD#"))
	rawDate := strings.TrimSpace(strings.TrimPrefix(ls[date].Text, "Registered with this firm since:"))
	if _, e := normalizeCRD(firmCRD); e != nil || historyDate(rawDate).ISO == nil {
		p.issue("invalid_history_value", "Invalid firm CRD or current registration date", historyEvidence(p.doc, historyIDs(ls)))
		return
	}
	reg := Registration{ID: id, Kind: "current", Scope: scope, FirmName: name, FirmCRD: firmCRD, Location: loc, Start: historyDate(rawDate), End: historyDate(""), Evidence: historyEvidence(p.doc, historyIDs(ls))}
	reg.Fields = []Field{p.field(id, "Firm Name", name, historyIDs(nameLines), 1, 1), p.field(id, "CRD#", firmCRD, historyIDs(ls[crd:crd+1]), 2, 1), p.field(id, "Branch Location", loc, historyIDs(ls[addr:crd]), 3, 1), p.field(id, "Registered with this firm since", rawDate, historyIDs(ls[date:date+1]), 4, 1)}
	p.consume([]string{first.ID})
	p.result.Registrations = append(p.result.Registrations, reg)
}

type historyColumn struct {
	label string
	x     float64
	ids   []string
}
type historyCell struct{ lines []reportTextLine }

func (c historyCell) value() string {
	s := []string{}
	for _, l := range c.lines {
		s = append(s, l.Text)
	}
	return strings.Join(s, "\n")
}
func historyHeaderColumns(lines []reportTextLine, employment bool) ([]historyColumn, bool) {
	labels := []string{"Registration Dates", "Firm Name", "CRD#", "Branch Location"}
	if employment {
		labels = []string{"Employment", "Employer Name", "Position", "Investment Related", "Employer Location"}
	}
	var cols []historyColumn
	for _, l := range lines {
		ws := l.Words
		for len(ws) > 0 {
			matched := false
			for _, label := range labels {
				parts := strings.Fields(label)
				if len(ws) < len(parts) {
					continue
				}
				ok := true
				for i, s := range parts {
					if ws[i].Text != s {
						ok = false
						break
					}
				}
				if ok {
					ids := []string{}
					for _, w := range ws[:len(parts)] {
						ids = append(ids, w.ID)
					}
					cols = append(cols, historyColumn{label: label, x: ws[0].X0, ids: ids})
					ws = ws[len(parts):]
					matched = true
					break
				}
			}
			if !matched {
				text := []string{}
				ids := []string{}
				x := ws[0].X0
				for _, w := range ws {
					text = append(text, w.Text)
					ids = append(ids, w.ID)
				}
				cols = append(cols, historyColumn{label: strings.Join(text, " "), x: x, ids: ids})
				break
			}
		}
	}
	sort.SliceStable(cols, func(i, j int) bool { return cols[i].x < cols[j].x })
	for _, label := range labels {
		n := 0
		for _, c := range cols {
			if c.label == label {
				n++
			}
		}
		if n != 1 {
			return nil, false
		}
	}
	for i := 1; i < len(cols); i++ {
		if cols[i].x-cols[i-1].x < 5 {
			return nil, false
		}
	}
	return cols, true
}
func (p *historyRegion) table() {
	ls := p.clean()
	if len(ls) == 0 {
		return
	}
	employment := p.coverage.Section == "employment_history"
	headerName := "Registration Dates"
	if employment {
		headerName = "Employment"
	}
	var cols []historyColumn
	var cells []historyCell
	var rowIDs []string
	flush := func() {
		if len(rowIDs) > 0 {
			p.tableRecord(cols, cells, rowIDs, employment)
			cells = nil
			rowIDs = nil
		}
	}
	for i := 0; i < len(ls); {
		l := ls[i]
		if l.Text == headerName {
			j := i + 1
			for j < len(ls) && ls[j].Page == l.Page && ls[j].Y0-l.Y0 < 3 {
				j++
			}
			next, ok := historyHeaderColumns(ls[i:j], employment)
			if !ok {
				p.issue("unknown_history_columns", "Required history table columns are absent or ambiguous", historyEvidence(p.doc, historyIDs(ls[i:j])))
				i = j
				continue
			}
			if len(cols) > 0 {
				same := len(cols) == len(next)
				if same {
					for k := range cols {
						if cols[k].label != next[k].label || cols[k].x != next[k].x {
							same = false
						}
					}
				}
				if !same {
					flush()
					p.issue("changed_history_columns", "Continuation table columns changed", historyEvidence(p.doc, historyIDs(ls[i:j])))
				}
			}
			cols = next
			for _, h := range ls[i:j] {
				p.ignore(h, "column header")
			}
			i = j
			continue
		}
		if len(cols) == 0 {
			i++
			continue
		}
		// Cluster same visual baseline despite minor font-dependent y offsets.
		j := i + 1
		for j < len(ls) && ls[j].Page == l.Page && ls[j].Y0-l.Y0 < 2 {
			j++
		}
		group := ls[i:j]
		parts := make([][]Word, len(cols))
		for _, line := range group {
			for _, w := range line.Words {
				k := sort.Search(len(cols), func(k int) bool { return cols[k].x > w.X0+2 }) - 1
				if k < 0 {
					k = 0
				}
				parts[k] = append(parts[k], w)
			}
		}
		for k := range parts {
			sort.SliceStable(parts[k], func(a, b int) bool { return parts[k][a].X0 < parts[k][b].X0 })
		}
		dateText := []string{}
		for _, w := range parts[0] {
			dateText = append(dateText, w.Text)
		}
		if historyRange.MatchString(strings.Join(dateText, " ")) {
			flush()
			cells = make([]historyCell, len(cols))
		}
		if len(cells) == 0 {
			i = j
			continue
		}
		for k, words := range parts {
			if len(words) == 0 {
				continue
			}
			sort.SliceStable(words, func(a, b int) bool { return words[a].X0 < words[b].X0 })
			texts := []string{}
			for _, w := range words {
				texts = append(texts, w.Text)
				rowIDs = append(rowIDs, w.ID)
			}
			cells[k].lines = append(cells[k].lines, reportTextLine{Text: strings.Join(texts, " "), Words: words, Page: l.Page, Y0: l.Y0})
		}
		i = j
	}
	flush()
}
func (p *historyRegion) tableRecord(cols []historyColumn, cells []historyCell, ids []string, employment bool) {
	vals := map[string]string{}
	for i, c := range cols {
		if _, exists := vals[c.label]; !exists {
			vals[c.label] = cells[i].value()
		}
	}
	dateLabel := "Registration Dates"
	kind := "registration"
	ordinal := len(p.result.Registrations) + 1
	if employment {
		dateLabel = "Employment"
		kind = "employment"
		ordinal = len(p.result.Employments) + 1
	}
	m := historyRange.FindStringSubmatch(vals[dateLabel])
	if m == nil {
		p.issue("invalid_history_date_range", "A history row has an invalid or ambiguous date range", historyEvidence(p.doc, ids))
		return
	}
	start, end := historyDate(m[2]), historyDate(m[3])
	if start.ISO == nil || (end.ISO == nil && end.Precision != "present") {
		p.issue("invalid_history_date", "Invalid calendar date in history row", historyEvidence(p.doc, ids))
		return
	}
	id := fmt.Sprintf("%s-%s-%d", p.doc.Report.SHA256, kind, ordinal)
	fields := []Field{}
	occ := map[string]int{}
	for i, c := range cols {
		value := cells[i].value()
		if c.label == dateLabel {
			value = m[2] + " - " + m[3]
		}
		evIDs := historyIDs(cells[i].lines)
		if len(evIDs) == 0 {
			evIDs = c.ids
		}
		occ[c.label]++
		fields = append(fields, p.field(id, c.label, value, evIDs, i+1, occ[c.label]))
	}
	if employment {
		raw := vals["Investment Related"]
		var related *bool
		switch strings.ToLower(raw) {
		case "y", "yes":
			v := true
			related = &v
		case "n", "no":
			v := false
			related = &v
		case "", "unknown", "n/a":
		default:
			p.issue("unknown_investment_related_value", "Unrecognized investment-related indicator", historyEvidence(p.doc, ids))
		}
		if vals["Employer Name"] == "" {
			p.issue("empty_history_employer", "Employment row has no employer", historyEvidence(p.doc, ids))
		}
		p.result.Employments = append(p.result.Employments, EmploymentRecord{ID: id, Employer: vals["Employer Name"], Position: vals["Position"], InvestmentRelated: related, Location: vals["Employer Location"], Start: start, End: end, Fields: fields, Evidence: historyEvidence(p.doc, ids)})
	} else {
		scope := m[1]
		if scope == "B" {
			scope = "BD"
		}
		if scope == "" {
			scope = "unknown"
			p.issue("unknown_registration_scope", "Registration table row has no identified BD/IA scope", historyEvidence(p.doc, ids))
		}
		if _, e := normalizeCRD(vals["CRD#"]); e != nil {
			p.issue("invalid_firm_crd", "Registration row has invalid firm CRD", historyEvidence(p.doc, ids))
		}
		if vals["Firm Name"] == "" {
			p.issue("empty_history_firm", "Registration row has no firm name", historyEvidence(p.doc, ids))
		}
		p.result.Registrations = append(p.result.Registrations, Registration{ID: id, Kind: "previous", Scope: scope, FirmName: vals["Firm Name"], FirmCRD: vals["CRD#"], Location: vals["Branch Location"], Start: start, End: end, Fields: fields, Evidence: historyEvidence(p.doc, ids)})
	}
	p.consume(ids)
}

// previousSummary reconciles the displayed subset with full prior rows; summary
// locations describe the firm and must not overwrite a detailed branch location.
func (p *historyRegion) previousSummary() {
	lines := p.clean()
	if p.coverage.State == SectionExplicitlyEmpty {
		for _, r := range p.result.Registrations {
			if r.Kind == "previous" {
				p.issue("contradictory_history_section", "Summary declares no prior registrations but detailed rows exist", historyEvidence(p.doc, p.coverage.RegionWordIDs))
				break
			}
		}
	}
	var group []reportTextLine
	flush := func() {
		if len(group) > 0 {
			p.previousSummaryRecord(group)
			group = nil
		}
	}
	for _, l := range lines {
		if len(l.Words) > 1 && (l.Words[0].Text == "B" || l.Words[0].Text == "BD" || l.Words[0].Text == "IA") {
			flush()
			group = append(group, l)
		} else if len(group) > 0 {
			group = append(group, l)
		}
	}
	flush()
}
func (p *historyRegion) previousSummaryRecord(lines []reportTextLine) {
	scope := lines[0].Words[0].Text
	if scope == "B" {
		scope = "BD"
	}
	crdLine := -1
	for i, l := range lines {
		if strings.HasPrefix(l.Text, "CRD# ") {
			if crdLine >= 0 {
				p.issue("ambiguous_summary_registration", "Multiple firm CRDs in prior-summary entry", historyEvidence(p.doc, historyIDs(lines)))
				return
			}
			crdLine = i
		}
	}
	if crdLine < 1 || crdLine >= len(lines)-1 {
		p.issue("unknown_summary_registration_layout", "Prior summary needs firm name, CRD, and date range", historyEvidence(p.doc, historyIDs(lines)))
		return
	}
	dates := historyRange.FindStringSubmatch(lines[len(lines)-1].Text)
	if dates == nil {
		p.issue("unknown_summary_registration_layout", "Prior summary lacks a complete date range", historyEvidence(p.doc, historyIDs(lines)))
		return
	}
	crd := strings.TrimSpace(strings.TrimPrefix(lines[crdLine].Text, "CRD#"))
	match := -1
	for i, r := range p.result.Registrations {
		if r.Kind == "previous" && r.Scope == scope && r.FirmCRD == crd && r.Start.Raw == dates[2] && r.End.Raw == dates[3] {
			if match >= 0 {
				p.issue("ambiguous_summary_registration", "Prior summary matches multiple detailed rows", historyEvidence(p.doc, historyIDs(lines)))
				return
			}
			match = i
		}
	}
	if match < 0 {
		p.issue("unmatched_summary_registration", "Prior summary does not match a detailed registration by scope, CRD, and dates", historyEvidence(p.doc, historyIDs(lines)))
		return
	}
	rec := &p.result.Registrations[match]
	names := []string{strings.TrimSpace(strings.TrimPrefix(lines[0].Text, lines[0].Words[0].Text))}
	for _, l := range lines[1:crdLine] {
		names = append(names, l.Text)
	}
	locs := []string{}
	for _, l := range lines[crdLine+1 : len(lines)-1] {
		locs = append(locs, l.Text)
	}
	add := func(baseLabel, summaryLabel, value string, ls []reportTextLine) {
		ids := historyIDs(ls)
		ev := historyEvidence(p.doc, ids)
		for i := range rec.Fields {
			if rec.Fields[i].Label == baseLabel && len(rec.Fields[i].Path) == 0 {
				if strings.Join(strings.Fields(rec.Fields[i].Value), " ") == strings.Join(strings.Fields(value), " ") {
					rec.Fields[i].Evidence = append(rec.Fields[i].Evidence, ev...)
					p.consume(ids)
					return
				}
				break
			}
		}
		f := p.field(rec.ID, summaryLabel, value, ids, len(rec.Fields)+1, 1)
		f.Path = []string{"Report Summary"}
		rec.Fields = append(rec.Fields, f)
	}
	add("Firm Name", "Firm Name", strings.Join(names, "\n"), lines[:crdLine])
	add("CRD#", "CRD#", crd, lines[crdLine:crdLine+1])
	if len(locs) > 0 {
		add("Branch Location", "Firm Location", strings.Join(locs, "\n"), lines[crdLine+1:len(lines)-1])
	}
	add("Registration Dates", "Registration Dates", dates[2]+" - "+dates[3], lines[len(lines)-1:])
}
