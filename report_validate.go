package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

func blocking(code, section, message string) ParseIssue {
	return ParseIssue{Code: code, Severity: "blocking", Section: section, Message: message}
}
func ValidateReport(doc Document, p ParsedReport) ValidationResult {
	v := ValidationResult{ExtractedEventCount: len(p.Disclosures.Events)}
	v.Issues = append(v.Issues, doc.Issues...)
	v.Issues = append(v.Issues, p.Issues...)
	v.Issues = append(v.Issues, p.History.Issues...)
	v.Issues = append(v.Issues, p.Disclosures.Issues...)
	add := func(c, s, m string) { v.Issues = append(v.Issues, blocking(c, s, m)) }
	if p.Report.CRD != doc.Report.CRD || p.Report.SHA256 != doc.Report.SHA256 {
		add("report_reference_mismatch", "report", "Parsed report differs from extracted document")
	}
	if p.SchemaVersion != SchemaVersion || p.ParserVersion != ParserVersion || p.ExtractorVersion != doc.ExtractorVersion || doc.ExtractorVersion != ExtractorVersion {
		add("version_mismatch", "report", "Unexpected schema/parser/extractor version")
	}
	// Reconstruct semantic records from the saved positioned document. This detects
	// corrupted/deleted child records in archives; completeness still depends on the
	// independent regions below and the separately reviewed fixture annotations.
	consistent := func(a, b any) bool { aa, _ := json.Marshal(a); bb, _ := json.Marshal(b); return bytes.Equal(aa, bb) }
	if !consistent(p.History, ParseHistory(doc)) {
		add("history_artifact_inconsistent", "history", "Stored history differs from current deterministic extraction")
	}
	if !consistent(p.Disclosures, ParseDisclosures(doc)) {
		add("disclosure_artifact_inconsistent", "disclosures", "Stored disclosures differ from current deterministic extraction")
	}
	words := map[string]int{}
	for _, page := range doc.Pages {
		if page.Number <= 0 || len(page.Words) == 0 {
			add("unreadable_page", "report", fmt.Sprintf("Physical page %d has no readable words", page.Number))
		}
		for _, w := range page.Words {
			if _, ok := words[w.ID]; ok {
				add("duplicate_word_id", "report", w.ID)
			}
			words[w.ID] = page.Number
		}
	}
	ids := map[string]bool{}
	evidenceWords := map[string]bool{}
	checkID := func(id string) {
		if id == "" || ids[id] {
			add("duplicate_record_id", "report", id)
		}
		ids[id] = true
	}
	checkEvidence := func(ev []Evidence) {
		for _, e := range ev {
			for _, id := range e.WordIDs {
				page, ok := words[id]
				if !ok || page != e.Page {
					add("invalid_evidence_reference", "report", id)
				} else {
					evidenceWords[id] = true
				}
			}
		}
	}
	for _, issue := range p.History.Issues {
		if issue.Code == "explicitly_empty_section" {
			checkEvidence(issue.Evidence)
		}
	}
	checkFields := func(fields []Field) {
		occ := map[string]int{}
		for _, f := range fields {
			checkID(f.ID)
			key := strings.Join(f.Path, "\x00") + "\x01" + f.Label
			occ[key]++
			if f.Occurrence != occ[key] || f.Label == "" {
				add("invalid_field", "report", f.ID)
			}
			if len(f.Evidence) == 0 {
				add("missing_field_evidence", "report", f.ID)
			}
			checkEvidence(f.Evidence)
		}
	}
	for _, r := range p.History.Registrations {
		checkID(r.ID)
		checkEvidence(r.Evidence)
		checkFields(r.Fields)
	}
	for _, r := range p.History.Employments {
		checkID(r.ID)
		checkEvidence(r.Evidence)
		checkFields(r.Fields)
	}
	for _, e := range p.Disclosures.Events {
		checkID(e.ID)
		checkEvidence(e.Evidence)
		checkFields(e.Fields)
		for _, s := range e.Sources {
			checkID(s.ID)
			checkEvidence(s.Evidence)
			checkFields(s.Fields)
		}
	}
	comparableCounts := 0
	for _, c := range p.Disclosures.Counts {
		checkEvidence(c.Evidence)
		if c.Count == nil {
			continue
		}
		comparableCounts++
		if len(c.Evidence) == 0 {
			add("count_without_evidence", "disclosures", c.Category)
		}
		actual := 0
		for _, e := range p.Disclosures.Events {
			if (c.Category == "all" || reportCategoryMatches(c.Category, e.Type)) && (c.Status == "" || c.Status == "all" || reportStatusMatches(c.Status, e)) {
				actual++
			}
		}
		if *c.Count < 0 || actual != *c.Count {
			add("count_mismatch", "disclosures", fmt.Sprintf("%s/%s reported %d extracted %d", c.Category, c.Status, *c.Count, actual))
		}
		if c.Category == "all" && (c.Status == "" || c.Status == "all") {
			v.ReportedEventCount = c.Count
		}
	}
	if v.ReportedEventCount == nil && RecognizedReportVariant(doc) != "finra-active-reports-category-summary-v1" {
		add("unqualified_missing_total", "disclosures", "No fixture-qualified report variant allowing absent total")
	}
	if comparableCounts == 0 {
		add("missing_comparable_counts", "disclosures", "No reported numeric count has been extracted")
	}
	for _, event := range p.Disclosures.Events {
		found := false
		for _, c := range p.Disclosures.Counts {
			if c.Count != nil && (c.Category == "all" || reportCategoryMatches(c.Category, event.Type)) && (c.Status == "all" || c.Status == "") {
				found = true
			}
		}
		if !found {
			add("missing_event_category_count", "disclosures", event.Type)
		}
	}
	cover := map[string]Coverage{}
	for _, c := range append(append([]Coverage{}, p.History.Coverage...), p.Disclosures.Coverage...) {
		if _, ok := cover[c.Section]; ok {
			add("duplicate_section_coverage", c.Section, "Duplicate coverage")
		}
		cover[c.Section] = c
	}
	total, assigned := 0, 0
	for _, region := range DetectReportSections(doc) {
		c, ok := cover[region.Section]
		if !ok {
			add("missing_section_coverage", region.Section, "No parser coverage")
			continue
		}
		if region.State == SectionAbsent || region.State == SectionUnreadable || c.State == SectionAbsent || c.State == SectionUnreadable {
			add("missing_required_section", region.Section, string(region.State))
		}
		if c.State == SectionNotApplicable && region.State != SectionNotApplicable {
			add("unsupported_not_applicable", region.Section, "Section variant lacks independent evidence")
		}
		consumed := map[string]bool{}
		ignored := map[string]bool{}
		for _, id := range c.ConsumedWordIDs {
			if !evidenceWords[id] {
				add("coverage_without_evidence", c.Section, id)
			}
			consumed[id] = true
		}
		for _, span := range c.Ignored {
			switch span.Reason {
			case "repeated header/footer", "column header", "section heading", "identified guidance text":
			default:
				add("invalid_ignore_reason", c.Section, span.Reason)
			}
			for _, id := range span.WordIDs {
				ignored[id] = true
			}
		}
		for _, id := range region.RegionWordIDs {
			total++
			if consumed[id] || ignored[id] {
				assigned++
			} else {
				add("unmatched_required_content", region.Section, id)
			}
		}
	}
	if total == 0 {
		add("empty_required_regions", "report", "No independently detected required words")
	} else {
		v.RequiredWordCoverage = float64(assigned) / float64(total)
	}
	v.Accepted = true
	for _, i := range v.Issues {
		if i.Severity == "blocking" {
			v.Accepted = false
		}
	}
	return v
}

// Only dispositions qualified by fixtures map to the matrix's broader Final status.
func reportStatusMatches(reported string, event DisclosureEvent) bool {
	if strings.EqualFold(reported, event.Status) {
		return true
	}
	if strings.EqualFold(reported, "Final") {
		// The qualified employment-separation detail has no status heading;
		// its report matrix explicitly classifies the event as Final.
		if event.Type == "Employment Separation After Allegations" && event.Status == "" {
			return true
		}
		switch event.Status {
		case "Closed-No Action / Withdrawn / Dismissed / Denied", "Closed/No Action", "Settled", "Final", "Final Disposition":
			return true
		}
	}
	return false
}

// FINRA's matrix and detail headings differ for this verified category.
func reportCategoryMatches(reported, event string) bool {
	return strings.EqualFold(reported, event) ||
		(reported == "Regulatory Event" && event == "Regulatory") ||
		(reported == "Termination" && event == "Employment Separation After Allegations")
}
