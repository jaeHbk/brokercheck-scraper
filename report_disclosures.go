package main

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ParseDisclosures keeps report events separate from reporting-source versions.
// Unknown content is never assigned to an arbitrary field or ignored to make coverage pass.
func ParseDisclosures(doc Document) DisclosureResult {
	r := DisclosureResult{Events: []DisclosureEvent{}, Counts: []DisclosureCount{}, Coverage: []Coverage{}, Issues: []ParseIssue{}}
	for _, c := range DetectReportSections(doc) {
		if c.Section == "disclosure_summary" || c.Section == "disclosures" {
			r.Coverage = append(r.Coverage, c)
		}
	}
	if len(r.Coverage) != 2 {
		return r
	}
	summary, details := &r.Coverage[0], &r.Coverage[1]
	parseDisclosureSummary(doc, summary, &r)
	parseDisclosureDetails(doc, details, &r)
	explicitEmpty := false
	for _, c := range r.Counts {
		if c.Category == "all" && c.Status == "all" && c.Count != nil && *c.Count == 0 {
			explicitEmpty = true
		}
	}
	if explicitEmpty && len(r.Events) == 0 {
		summary.State = SectionExplicitlyEmpty
		if details.State == SectionAbsent {
			details.State = SectionExplicitlyEmpty
		}
	}
	for _, c := range r.Coverage {
		covered := map[string]bool{}
		for _, id := range c.ConsumedWordIDs {
			covered[id] = true
		}
		for _, span := range c.Ignored {
			for _, id := range span.WordIDs {
				covered[id] = true
			}
		}
		missing := []string{}
		for _, id := range c.RegionWordIDs {
			if !covered[id] {
				missing = append(missing, id)
			}
		}
		if len(missing) > 0 {
			r.Issues = append(r.Issues, ParseIssue{Code: "unmatched_disclosure_content", Severity: "blocking", Section: c.Section, Message: fmt.Sprintf("%d required words have no unambiguous structured interpretation", len(missing)), Evidence: disclosureWordsEvidence(doc, missing)})
		}
	}
	return r
}

func disclosureWordsEvidence(doc Document, ids []string) []Evidence {
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	out := []Evidence{}
	for _, p := range doc.Pages {
		e := Evidence{Page: p.Number, WordIDs: []string{}}
		parts := []string{}
		for _, w := range p.Words {
			if wanted[w.ID] {
				e.WordIDs = append(e.WordIDs, w.ID)
				parts = append(parts, w.Text)
			}
		}
		if len(e.WordIDs) > 0 {
			e.RawText = strings.Join(parts, " ")
			out = append(out, e)
		}
	}
	return out
}
func disclosureEvidence(l reportTextLine) Evidence {
	ids := []string{}
	for _, w := range l.Words {
		ids = append(ids, w.ID)
	}
	return Evidence{Page: l.Page, WordIDs: ids, RawText: l.Text}
}
func disclosureConsume(c *Coverage, l reportTextLine) {
	for _, w := range l.Words {
		c.ConsumedWordIDs = append(c.ConsumedWordIDs, w.ID)
	}
}
func disclosureIgnore(c *Coverage, l reportTextLine, reason string) {
	ids := []string{}
	for _, w := range l.Words {
		ids = append(ids, w.ID)
	}
	c.Ignored = append(c.Ignored, IgnoredSpan{WordIDs: ids, Reason: reason})
}
func disclosureLines(doc Document, c Coverage) []reportTextLine {
	region := map[string]bool{}
	for _, id := range c.RegionWordIDs {
		region[id] = true
	}
	out := []reportTextLine{}
	for _, p := range doc.Pages {
		for _, l := range reportLines(p) {
			kept := []Word{}
			for _, w := range l.Words {
				if region[w.ID] {
					kept = append(kept, w)
				}
			}
			if len(kept) == 0 {
				continue
			}
			l.Words = kept
			l.Text = ""
			l.X0 = kept[0].X0
			l.X1 = kept[len(kept)-1].X1
			for _, w := range kept {
				if l.Text != "" {
					l.Text += " "
				}
				l.Text += w.Text
			}
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Page != b.Page {
			return a.Page < b.Page
		}
		if math.Abs(a.Y0-b.Y0) < 1 {
			return a.X0 < b.X0
		}
		return a.Y0 < b.Y0
	})
	return out
}
func disclosureBoilerplate(doc Document, l reportTextLine) bool {
	if l.Page < 1 || l.Page > len(doc.Pages) {
		return false
	}
	p := doc.Pages[l.Page-1]
	if l.Y0 < 45 && (l.Text == "www.finra.org/brokercheck" || l.Text == "User Guidance" || l.Text == "SYNTHETIC DEVELOPMENT FIXTURE - NOT A REAL BROKER REPORT" || l.Text == "CRD# "+doc.Report.CRD) {
		return true
	}
	return l.Y0 > p.Height-30 && (strings.Contains(l.Text, "FINRA. All rights reserved. Report about ") || regexp.MustCompile(`^[0-9]+$`).MatchString(l.Text))
}

// Only exact, fixture-inspected guidance text is eligible for this exclusion.
const disclosureGuidance = `All individuals registered to sell securities or provide investment advice are required to disclose customer complaints and arbitrations, regulatory actions, employment terminations, bankruptcy filings, and criminal or civil judicial proceedings.
The following types of disclosures have been reported:
What you should know about reported disclosure events:
1. All individuals registered to sell securities or provide investment advice are required to disclose customer complaints and arbitrations, regulatory actions, employment terminations, bankruptcy filings, and criminal or civil judicial proceedings.
2. Certain thresholds must be met before an event is reported to CRD, for example:
o A law enforcement agency must file formal charges before a broker is required to disclose a particular criminal event.
o A customer dispute must involve allegations that a broker engaged in activity that violates certain rules or conduct governing the industry and that the activity resulted in damages of at least $5,000.
3. Disclosure events in BrokerCheck reports come from different sources:
o As mentioned at the beginning of this report, information contained in BrokerCheck comes from brokers, brokerage firms and regulators. When more than one of these sources reports information for the same disclosure event, all versions of the event will appear in the BrokerCheck report. The different versions will be separated by a solid line with the reporting source labeled.
4. There are different statuses and dispositions for disclosure events:
o A disclosure event may have a status of pending, on appeal, or final.
§ A "pending" event involves allegations that have not been proven or formally adjudicated.
§ An event that is "on appeal" involves allegations that have been adjudicated but are currently being appealed.
§ A "final" event has been concluded and its resolution is not subject to change.
o A final event generally has a disposition of adjudicated, settled or otherwise resolved.
§ An "adjudicated" matter includes a disposition by (1) a court of law in a criminal or civil matter, or (2) an administrative panel in an action brought by a regulator that is contested by the party charged with some alleged wrongdoing.
§ A "settled" matter generally involves an agreement by the parties to resolve the matter. Please note that brokers and brokerage firms may choose to settle customer disputes or regulatory matters for business or other reasons.
§ A "resolved" matter usually involves no payment to the customer and no finding of wrongdoing on the part of the individual broker. Such matters generally involve customer disputes.
For your convenience, below is a matrix of the number and status of disclosure events involving this broker. Further information regarding these events can be found in the subsequent pages of this report. You also may wish to contact the broker to obtain further information regarding these events.
When evaluating this information, please keep in mind that a discloure event may be pending or involve allegations that are contested and have not been resolved or proven. The matter may, in the end, be withdrawn, dismissed, resolved in favor of the broker, or concluded through a negotiated settlement for certain business reasons (e.g., to maintain customer relationships or to limit the litigation costs associated with disputing the allegations) with no admission or finding of wrongdoing.
This report provides the information exactly as it was reported to CRD and therefore some of the specific data fields contained in the report may be blank if the information was not provided to CRD.
This type of disclosure event involves (1) a consumer-initiated, investment-related arbitration or civil suit containing allegations of sales practice violations against the individual broker that was dismissed, withdrawn, or denied; or (2) a consumer-initiated, investment-related written complaint containing allegations that the broker engaged in sales practice violations resulting in compensatory damages of at least $5,000, forgery, theft, or misappropriation, or conversion of funds or securities, which was closed without action, withdrawn, or denied.
This type of disclosure event may involve (1) a final, formal proceeding initiated by a regulatory authority (e.g., a state securities agency, self- regulatory organization, federal regulatory such as the Securities and Exchange Commission, foreign financial regulatory body) for a violation of investment-related rules or regulations; or (2) a revocation or suspension of a broker's authority to act as an attorney, accountant, or federal contractor.
This type of disclosure event involves a criminal charge against the broker that has resulted in a conviction, acquittal, dismissal, or plea. The criminal matter may pertain to any felony or certain misdemeanor offenses, including bribery, perjury, forgery, counterfeiting, extortion, fraud, and wrongful taking of property.
This type of disclosure event involves a consumer-initiated, investment-related complaint, arbitration proceeding or civil suit containing allegations of sale practice violations against the broker that resulted in a monetary settlement to the customer.
This type of disclosure event involves a situation where the broker voluntarily resigned, was discharged, or was permitted to resign after being accused of (1) violating investment-related statutes, regulations, rules or industry standards of conduct; (2) fraud or the wrongful taking of property; or (3) failure to supervise in connection with investment-related statutes, regulations, rules, or industry standards of conduct.`

func disclosureKnownGuidance(l reportTextLine) bool {
	return l.Text == "o" || l.Text == "contractor." || (len(l.Text) > 15 && strings.Contains(strings.Join(strings.Fields(disclosureGuidance), " "), strings.Join(strings.Fields(l.Text), " ")))
}
func disclosureIssue(r *DisclosureResult, code, section, message string, l reportTextLine) {
	r.Issues = append(r.Issues, ParseIssue{Code: code, Severity: "blocking", Section: section, Message: message, Evidence: []Evidence{disclosureEvidence(l)}})
}

// This advisory is an adjacent summary panel, not a broker disclosure. Match
// the complete verified panel in its original summary column; altered or added
// text remains unconsumed and therefore blocking.
const investmentAdviserPanel = "Investment Adviser Representative Information The information below represents the individual's record as a broker. For details on this individual's record as an investment adviser representative, visit the SEC's Investment Adviser Public Disclosure website at https://www.adviserinfo.sec.gov"

func disclosureAdviserPanelEnd(doc Document, lines []reportTextLine, start int) int {
	first := lines[start]
	page := doc.Pages[first.Page-1]
	if first.Text != "Investment Adviser Representative" || first.X0 < page.Width/2 || !strings.Contains(page.Text, "Report Summary for this Broker") {
		return start
	}
	words := []string{}
	for i := start; i < len(lines) && i < start+12; i++ {
		line := lines[i]
		sameColumn := math.Abs(line.X0-first.X0) <= 5
		// The verified linked URL is either left-aligned or indented in this panel.
		if line.Text == "https://www.adviserinfo.sec.gov" && line.X0 >= first.X0 && line.X1 <= page.Width {
			sameColumn = true
		}
		if line.Page != first.Page || !sameColumn || line.Y0-first.Y0 > 130 {
			break
		}
		words = append(words, strings.Fields(line.Text)...)
		text := strings.Join(words, " ")
		if text == investmentAdviserPanel {
			return i + 1
		}
		if !strings.HasPrefix(investmentAdviserPanel, text) {
			break
		}
	}
	return start
}

func parseDisclosureSummary(doc Document, c *Coverage, r *DisclosureResult) {
	lines := disclosureLines(doc, *c)
	used := map[int]bool{}
	type column struct {
		x      float64
		status string
	}
	cols := []column{}
	page := 0
	for i, l := range lines {
		if used[i] {
			continue
		}
		if end := disclosureAdviserPanelEnd(doc, lines, i); end > i {
			for j := i; j < end; j++ {
				disclosureIgnore(c, lines[j], "identified guidance text")
				used[j] = true
			}
			continue
		}
		if l.Page != page {
			cols = nil
			page = l.Page
		}
		if disclosureBoilerplate(doc, l) {
			disclosureIgnore(c, l, "repeated header/footer")
			continue
		}
		if l.Text == "Disclosure Events" || l.Text == "Disclosure Events, continued" {
			disclosureIgnore(c, l, "section heading")
			continue
		}
		if disclosureKnownGuidance(l) || (l.Text == "reported:" && i > 0 && lines[i-1].Text == "The following types of disclosures have been") {
			disclosureIgnore(c, l, "identified guidance text")
			continue
		}
		if strings.HasPrefix(l.Text, "Are there events disclosed about this broker? ") {
			answer := strings.TrimPrefix(l.Text, "Are there events disclosed about this broker? ")
			if answer == "No" {
				zero := 0
				r.Counts = append(r.Counts, DisclosureCount{Category: "all", Status: "all", Count: &zero, Raw: "No", Evidence: []Evidence{disclosureEvidence(l)}})
				disclosureConsume(c, l)
			} else if answer == "Yes" {
				disclosureIgnore(c, l, "identified guidance text")
			}
			continue
		}
		if l.Text == "No disclosure events reported." {
			disclosureIgnore(c, l, "identified guidance text")
			continue
		}
		if l.Text == "Type" {
			disclosureIgnore(c, l, "column header")
			continue
		}
		if l.Text == "Count" || l.Text == "Pending" || l.Text == "Final" || l.Text == "On Appeal" {
			status := l.Text
			if status == "Count" {
				status = "all"
			}
			cols = append(cols, column{l.X0, status})
			disclosureIgnore(c, l, "column header")
			continue
		}
		if len(cols) == 0 {
			continue
		}
		// A row begins left of every count column. Values must occupy that same physical row.
		if l.X0 >= cols[0].x-15 {
			continue
		}
		category := l.Text
		if category == "Total Disclosures:" {
			category = "all"
		}
		matched := 0
		for k, col := range cols {
			end := math.Inf(1)
			if k+1 < len(cols) {
				end = cols[k+1].x - 20
			}
			candidates := []int{}
			for j := i + 1; j < len(lines); j++ {
				v := lines[j]
				if v.Page != l.Page || v.Y0 > l.Y0+1 {
					break
				}
				if math.Abs(v.Y0-l.Y0) < 1 && v.X0 >= col.x-15 && v.X0 < end {
					candidates = append(candidates, j)
				}
			}
			if len(candidates) != 1 {
				continue
			}
			j := candidates[0]
			v := lines[j]
			var n *int
			if count, err := strconv.Atoi(v.Text); err == nil && count >= 0 {
				n = &count
			} else if v.Text != "N/A" {
				continue
			}
			r.Counts = append(r.Counts, DisclosureCount{Category: category, Status: col.status, Count: n, Raw: v.Text, Evidence: []Evidence{disclosureEvidence(l), disclosureEvidence(v)}})
			disclosureConsume(c, v)
			used[j] = true
			matched++
		}
		if matched > 0 {
			disclosureConsume(c, l)
		}
	}
}

var disclosureBoundary = regexp.MustCompile(`^Disclosure ([0-9]+) of ([0-9]+)$`)

func parseDisclosureDetails(doc Document, c *Coverage, r *DisclosureResult) {
	lines := disclosureLines(doc, *c)
	eventIndex, sourceIndex := -1, -1
	eventType, eventStatus := "", ""
	declaredTotal, groupCount := 0, 0
	path := []string{}
	preamble := true
	var current *Field
	var pending, pendingValues, pendingEvidence []reportTextLine
	used := map[int]bool{}
	// Some FINRA PDFs leave a clipped value at the very bottom of a page,
	// repeating it visibly alongside its label at the next page's first row.
	// Match exact text and both positions; move evidence, never discard words.
	continuedValues := map[int][]reportTextLine{}
	for i, l := range lines {
		if l.X0 < doc.Pages[l.Page-1].Width*.2 || l.Y0 < doc.Pages[l.Page-1].Height-40 {
			continue
		}
		paired := false
		for _, other := range lines {
			if other.Page == l.Page && other.X0 < doc.Pages[l.Page-1].Width*.2 && math.Abs(other.Y0-l.Y0) < 3 {
				paired = true
			}
		}
		if paired {
			continue
		}
		next := -1
		for j := i + 1; j < len(lines); j++ {
			v := lines[j]
			if v.Page > l.Page+1 {
				break
			}
			if v.Page != l.Page+1 || disclosureBoilerplate(doc, v) {
				continue
			}
			next = j
			break
		}
		if next < 0 {
			continue
		}
		label := lines[next]
		if label.X0 >= doc.Pages[label.Page-1].Width*.2 || label.Y0 > 90 || !(strings.HasSuffix(label.Text, ":") || strings.HasSuffix(label.Text, "?")) {
			continue
		}
		for j := next + 1; j < len(lines); j++ {
			v := lines[j]
			if v.Page != label.Page || v.Y0 > label.Y0+3 {
				break
			}
			if v.X0 >= doc.Pages[v.Page-1].Width*.2 && v.Text == l.Text && math.Abs(v.Y0-label.Y0) < 3 {
				continuedValues[next] = append(continuedValues[next], l)
				used[i] = true
				break
			}
		}
	}
	addParent := func(l reportTextLine) {
		if eventIndex < 0 {
			return
		}
		e := &r.Events[eventIndex]
		e.Evidence = append(e.Evidence, disclosureEvidence(l))
		if sourceIndex >= 0 {
			e.Sources[sourceIndex].Evidence = append(e.Sources[sourceIndex].Evidence, disclosureEvidence(l))
		}
	}
	flush := func() {
		if current == nil {
			return
		}
		e := &r.Events[eventIndex]
		fields := &e.Fields
		parent := e.ID
		if sourceIndex >= 0 {
			fields = &e.Sources[sourceIndex].Fields
			parent = e.Sources[sourceIndex].ID
		}
		current.ID = fmt.Sprintf("%s-field-%d", parent, len(*fields)+1)
		current.Occurrence = 1
		for _, f := range *fields {
			if f.Label == current.Label && strings.Join(f.Path, "\x00") == strings.Join(current.Path, "\x00") {
				current.Occurrence++
			}
		}
		*fields = append(*fields, *current)
		current = nil
	}
	rejectPending := func() {
		for _, l := range pending {
			disclosureIssue(r, "ambiguous_disclosure_label", c.Section, "Unterminated or ambiguous field label", l)
		}
		for _, l := range pendingValues {
			disclosureIssue(r, "orphan_disclosure_value", c.Section, "Value has no complete field label", l)
		}
		pending = nil
		pendingValues = nil
		pendingEvidence = nil
	}
	appendValue := func(l reportTextLine) {
		if current.Value != "" {
			current.Value += "\n"
		}
		current.Value += l.Text
		current.Evidence = append(current.Evidence, disclosureEvidence(l))
		addParent(l)
		disclosureConsume(c, l)
	}
	finishLabel := func() {
		// Exact prefix overlap across page breaks is one repeated/continued label.
		// All original words, including clipped duplicates, remain in field evidence.
		label := []string{}
		page := 0
		segment := []string{}
		merge := func() {
			if len(segment) == 0 {
				return
			}
			n := 0
			for n < len(label) && n < len(segment) && label[n] == segment[n] {
				n++
			}
			if n == len(label) {
				label = append([]string{}, segment...)
			} else {
				overlap := 0
				for k := 1; k <= len(label) && k <= len(segment); k++ {
					if strings.Join(label[len(label)-k:], "\n") == strings.Join(segment[:k], "\n") {
						overlap = k
					}
				}
				label = append(label, segment[overlap:]...)
			}
			segment = nil
		}
		for _, l := range pending {
			if l.Page != page {
				merge()
				page = l.Page
			}
			segment = append(segment, l.Text)
		}
		merge()
		current = &Field{Label: strings.Join(label, "\n"), Path: append([]string{}, path...), Evidence: []Evidence{}}
		for _, l := range pendingEvidence {
			current.Evidence = append(current.Evidence, disclosureEvidence(l))
			addParent(l)
			disclosureConsume(c, l)
		}
		pendingEvidence = nil
		for _, l := range pending {
			current.Evidence = append(current.Evidence, disclosureEvidence(l))
			addParent(l)
			disclosureConsume(c, l)
		}
		pending = nil
		for _, l := range pendingValues {
			appendValue(l)
		}
		pendingValues = nil
	}
	checkGroup := func() {
		if groupCount > 0 && declaredTotal != groupCount {
			r.Issues = append(r.Issues, ParseIssue{Code: "disclosure_boundary_count_mismatch", Severity: "blocking", Section: c.Section, Message: fmt.Sprintf("%s - %s boundaries report %d events but %d were extracted", eventType, eventStatus, declaredTotal, groupCount)})
		}
	}
	for i, l := range lines {
		if used[i] {
			continue
		}
		if disclosureBoilerplate(doc, l) {
			disclosureIgnore(c, l, "repeated header/footer")
			continue
		}
		if l.Text == "Disclosure Event Details" || l.Text == "Disclosure Event Details, continued" {
			disclosureIgnore(c, l, "section heading")
			continue
		}
		if preamble && disclosureKnownGuidance(l) {
			disclosureIgnore(c, l, "identified guidance text")
			continue
		}
		left := l.X0 < doc.Pages[l.Page-1].Width*.2
		if left {
			if l.Text == "Employment Separation After Allegations" {
				known := false
				for _, count := range r.Counts {
					if count.Category == "Termination" {
						known = true
					}
				}
				if known {
					flush()
					rejectPending()
					checkGroup()
					declaredTotal, groupCount = 0, 0
					eventType, eventStatus = l.Text, ""
					preamble = true
					disclosureIgnore(c, l, "section heading")
					continue
				}
			}
			if typ, status, ok := strings.Cut(l.Text, " - "); ok && typ != "" && status != "" {
				category := typ
				if category == "Regulatory" {
					category = "Regulatory Event"
				}
				known := false
				for _, count := range r.Counts {
					if count.Category == category {
						known = true
					}
				}
				if known {
					flush()
					rejectPending()
					checkGroup()
					declaredTotal, groupCount = 0, 0
					eventType, eventStatus = typ, status
					preamble = true
					disclosureIgnore(c, l, "section heading")
					continue
				}
			}
			if m := disclosureBoundary.FindStringSubmatch(l.Text); m != nil {
				flush()
				rejectPending()
				n, _ := strconv.Atoi(m[1])
				total, _ := strconv.Atoi(m[2])
				if (declaredTotal != 0 && declaredTotal != total) || n != groupCount+1 || n > total || eventType == "" {
					disclosureIssue(r, "ambiguous_disclosure_event", c.Section, "Disclosure numbering or category boundary is inconsistent", l)
				}
				declaredTotal = total
				groupCount++
				preamble = false
				e := DisclosureEvent{ID: fmt.Sprintf("%s-event-%d", doc.Report.SHA256, len(r.Events)+1), ReportedID: m[1], Type: eventType, Status: eventStatus, Fields: []Field{}, Sources: []DisclosureSource{}, Evidence: []Evidence{disclosureEvidence(l)}}
				r.Events = append(r.Events, e)
				eventIndex = len(r.Events) - 1
				sourceIndex = -1
				path = []string{}
				disclosureConsume(c, l)
				continue
			}
			// Qualified BrokerCheck inter-event divider glyph (not substantive prose).
			if l.Text == "i" && i+1 < len(lines) && (disclosureBoundary.MatchString(lines[i+1].Text) || lines[i+1].Text == "Reporting Source:") {
				disclosureIgnore(c, l, "section heading")
				continue
			}
			if l.Text == "Reporting Source:" {
				flush()
				rejectPending()
				if eventIndex < 0 {
					disclosureIssue(r, "ambiguous_disclosure_source", c.Section, "Reporting source appears before an event", l)
					continue
				}
				found := -1
				for j := i + 1; j < len(lines); j++ {
					v := lines[j]
					if v.Page != l.Page || v.Y0 > l.Y0+3 {
						break
					}
					if math.Abs(v.Y0-l.Y0) < 3 && v.X0 >= doc.Pages[l.Page-1].Width*.2 {
						if found >= 0 {
							found = -1
							break
						}
						found = j
					}
				}
				if found < 0 {
					disclosureIssue(r, "ambiguous_disclosure_source", c.Section, "Reporting source has no unique labeled version", l)
					sourceIndex = -1
					continue
				}
				value := lines[found]
				used[found] = true
				e := &r.Events[eventIndex]
				e.Sources = append(e.Sources, DisclosureSource{ID: fmt.Sprintf("%s-source-%d", e.ID, len(e.Sources)+1), Label: value.Text, Fields: []Field{}, Evidence: []Evidence{}})
				sourceIndex = len(e.Sources) - 1
				path = []string{}
				addParent(l)
				addParent(value)
				disclosureConsume(c, l)
				disclosureConsume(c, value)
				continue
			}
			if l.Text == "Customer Complaint Information" || disclosureSubrecord.MatchString(l.Text) {
				flush()
				rejectPending()
				if eventIndex < 0 {
					continue
				}
				path = []string{l.Text}
				addParent(l)
				disclosureIgnore(c, l, "section heading")
				continue
			}
			if eventIndex < 0 || preamble {
				continue
			}
			terminal := strings.HasSuffix(l.Text, ":") || strings.HasSuffix(l.Text, "?") || strings.HasSuffix(l.Text, "? or") || disclosureStatement.MatchString(l.Text)
			if len(pending) == 0 && current != nil && current.Value == "" && current.Label == l.Text && len(current.Evidence) > 0 {
				prior := current.Evidence[len(current.Evidence)-1]
				if prior.Page+1 == l.Page && l.Y0 < 100 {
					current.Evidence = append(current.Evidence, disclosureEvidence(l))
					addParent(l)
					disclosureConsume(c, l)
					continue
				}
			}
			if len(pending) > 0 && l.Page == pending[len(pending)-1].Page && l.Y0-pending[len(pending)-1].Y0 > 15 {
				rejectPending()
			}
			if len(pending) == 0 {
				flush()
			}
			pending = append(pending, l)
			pendingEvidence = append(pendingEvidence, continuedValues[i]...)
			if terminal {
				finishLabel()
			}
			continue
		}
		if eventIndex < 0 || preamble {
			continue
		}
		if len(pending) > 0 {
			pendingValues = append(pendingValues, l)
			continue
		}
		if current == nil {
			disclosureIssue(r, "orphan_disclosure_value", c.Section, "Value has no unambiguous field", l)
			continue
		}
		appendValue(l)
	}
	flush()
	rejectPending()
	checkGroup()
	for _, e := range r.Events {
		if len(e.Fields) == 0 && len(e.Sources) == 0 {
			r.Issues = append(r.Issues, ParseIssue{Code: "empty_disclosure_event", Severity: "blocking", Section: c.Section, Message: "Event has no structured fields or reporting sources", Evidence: e.Evidence})
		}
		for _, source := range e.Sources {
			if len(source.Fields) == 0 {
				r.Issues = append(r.Issues, ParseIssue{Code: "empty_disclosure_source", Severity: "blocking", Section: c.Section, Message: "Reporting source has no structured fields", Evidence: source.Evidence})
			}
		}
	}
}

var disclosureSubrecord = regexp.MustCompile(`^(Sanction|Charge\(s\)) [0-9]+ of [0-9]+$`)
var disclosureStatement = regexp.MustCompile(`^(Broker|Firm|Regulator) Statement$`)
