package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"
)

type ReportRunSummary struct {
	RunID               string             `json:"run_id"`
	InputPath           string             `json:"input_path"`
	InputSHA256         string             `json:"input_sha256"`
	SelectedInputSHA256 string             `json:"selected_input_sha256"`
	Input               int                `json:"input"`
	Selected            int                `json:"selected"`
	Accepted            int                `json:"accepted"`
	SkippedAccepted     int                `json:"skipped_accepted"`
	Unresolved          int                `json:"unresolved"`
	SkippedUnresolved   int                `json:"skipped_unresolved"`
	SchemaVersion       string             `json:"schema_version"`
	ParserVersion       string             `json:"parser_version"`
	ExtractorVersion    string             `json:"extractor_version"`
	ElapsedSeconds      float64            `json:"elapsed_seconds"`
	ReportsPerSecond    float64            `json:"reports_per_second"`
	PDFBytes            int64              `json:"pdf_bytes"`
	BytesPerReport      float64            `json:"bytes_per_report"`
	Failures            []ReportTransition `json:"failures"`
	ExportGeneration    string             `json:"export_generation"`
}

func parseReportsConfig(args []string, w io.Writer) (ReportsConfig, error) {
	c := ReportsConfig{BaseURL: DefaultReportBaseURL}
	f := flag.NewFlagSet("reports", flag.ContinueOnError)
	f.SetOutput(w)
	f.StringVar(&c.OutDir, "out", ".", "Output directory")
	f.StringVar(&c.Input, "input", "brokers_unique.json", "Input JSON, relative to --out")
	f.IntVar(&c.Workers, "workers", 2, "Concurrent report workers")
	f.Float64Var(&c.RPS, "rps", .2, "Shared request ceiling per second")
	f.IntVar(&c.Retries, "retries", 5, "Maximum download attempts")
	f.IntVar(&c.Limit, "limit", 0, "First N eligible CRDs, numeric order; zero means all")
	f.BoolVar(&c.Resume, "resume", true, "Resume current accepted results")
	f.BoolVar(&c.Reparse, "reparse", false, "Reparse retained bytes without network")
	f.BoolVar(&c.Refresh, "refresh", false, "Fetch a new snapshot")
	f.BoolVar(&c.RetryFailed, "retry-failed", false, "Retry unresolved results")
	if e := f.Parse(args); e != nil {
		return c, e
	}
	if f.NArg() != 0 {
		return c, fmt.Errorf("unexpected arguments: %v", f.Args())
	}
	if c.Workers <= 0 || c.RPS <= 0 || math.IsNaN(c.RPS) || math.IsInf(c.RPS, 0) || c.Retries <= 0 || c.Limit < 0 {
		return c, fmt.Errorf("workers, rps and attempts must be positive; limit nonnegative")
	}
	m := 0
	for _, b := range []bool{c.Reparse, c.Refresh, c.RetryFailed} {
		if b {
			m++
		}
	}
	if m > 1 || m > 0 && !c.Resume {
		return c, fmt.Errorf("modes are mutually exclusive and require --resume=true")
	}
	return c, nil
}
func readReportInput(c ReportsConfig) ([]string, string, string, error) {
	p := c.Input
	if !filepath.IsAbs(p) {
		p = filepath.Join(c.OutDir, p)
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return nil, p, "", e
	}
	var rows []struct {
		CRD json.RawMessage `json:"ind_source_id"`
	}
	if e = json.Unmarshal(b, &rows); e != nil {
		return nil, p, "", e
	}
	seen := map[string]bool{}
	for i, row := range rows {
		var s string
		if e = json.Unmarshal(row.CRD, &s); e != nil {
			return nil, p, "", fmt.Errorf("row %d requires a decimal string ind_source_id", i+1)
		}
		id, e := normalizeCRD(s)
		if e != nil {
			return nil, p, "", e
		}
		seen[id] = true
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if len(ids[i]) != len(ids[j]) {
			return len(ids[i]) < len(ids[j])
		}
		return ids[i] < ids[j]
	})
	h := sha256.Sum256(b)
	return ids, p, hex.EncodeToString(h[:]), nil
}
func reportsMain(args []string) int {
	c, e := parseReportsConfig(args, os.Stderr)
	if e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, e)
		return 1
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	code, summary, e := RunReports(ctx, c)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
	}
	if summary.RunID != "" {
		fmt.Fprintf(os.Stderr, "reports: input=%d selected=%d accepted=%d skipped-accepted=%d unresolved=%d skipped-unresolved=%d exports=%s\n", summary.Input, summary.Selected, summary.Accepted, summary.SkippedAccepted, summary.Unresolved, summary.SkippedUnresolved, summary.ExportGeneration)
	}
	return code
}

type reportWork struct {
	crd   string
	prior ReportTransition
}
type reportWorkResult struct {
	transition ReportTransition
	doc        Document
	parsed     ParsedReport
	validation ValidationResult
	hasParsed  bool
	err        error
	ack        chan error
}

func RunReports(ctx context.Context, c ReportsConfig) (code int, summary ReportRunSummary, err error) {
	start := time.Now()
	ids, inputPath, inputHash, e := readReportInput(c)
	if e != nil {
		return 1, summary, e
	}
	s, e := OpenReportStore(c.OutDir)
	if e != nil {
		return 1, summary, e
	}
	defer func() {
		if e := s.Close(); e != nil {
			code = 1
			err = errors.Join(err, e)
		}
	}()
	// Dependency qualification is explicit even when every item would be skipped.
	for _, backend := range []string{"pdfinfo", "pdftotext"} {
		if _, e := popplerBinary(backend); e != nil {
			return 1, summary, e
		}
	}

	runID := time.Now().UTC().Format("20060102T150405.000000000") + fmt.Sprintf("-%d", os.Getpid())
	summary = ReportRunSummary{RunID: runID, InputPath: inputPath, InputSHA256: inputHash, Input: len(ids), SchemaVersion: SchemaVersion, ParserVersion: ParserVersion, ExtractorVersion: ExtractorVersion}
	selected := []string{}
	for _, id := range ids {
		prior, exists := s.Latest[id]
		if c.RetryFailed && (!exists || isCurrentAccepted(s, prior)) {
			continue
		}
		selected = append(selected, id)
		if c.Limit > 0 && len(selected) == c.Limit {
			break
		}
	}
	summary.Selected = len(selected)
	b, _ := json.Marshal(selected)
	h := sha256.Sum256(b)
	summary.SelectedInputSHA256 = hex.EncodeToString(h[:])
	jobs := []reportWork{}
	for _, id := range selected {
		prior := s.Latest[id]
		if c.Resume && !c.Reparse && !c.Refresh && !c.RetryFailed {
			if _, ok := currentAccepted(s, prior); ok {
				summary.SkippedAccepted++
				continue
			}
			switch prior.Outcome {
			case "needs_review", "unavailable", "access_failure", "invalid_pdf":
				summary.SkippedUnresolved++
				continue
			}
		}
		jobs = append(jobs, reportWork{id, prior})
	}
	// Persist intent before any worker starts; an interrupted refresh cannot leave
	// the previous accepted artifact looking like the latest completed attempt.
	for _, job := range jobs {
		pending := job.prior
		pending.CRD = job.crd
		pending.RunID = runID
		pending.AttemptID = runID + "-" + job.crd
		pending.Outcome = "pending"
		pending.ExtractionState = "pending"
		if c.Refresh || pending.Report == nil {
			pending.DownloadState = "pending"
		}
		if e := s.Append(pending); e != nil {
			return 1, summary, e
		}
	}
	client := newClient(newLimiter(c.RPS, math.Min(.05, c.RPS)), c.Retries)
	work := make(chan reportWork)
	results := make(chan reportWorkResult)
	var wg sync.WaitGroup
	for i := 0; i < c.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range work {
				results <- processReport(ctx, c, client, runID, job, func(t ReportTransition) error {
					ack := make(chan error, 1)
					results <- reportWorkResult{transition: t, ack: ack}
					return <-ack
				})
			}
		}()
	}
	go func() {
		defer close(work)
		for _, job := range jobs {
			work <- job
		}
	}()
	go func() { wg.Wait(); close(results) }()
	var infra error
	for r := range results {
		if r.ack != nil {
			if infra == nil {
				infra = s.Append(r.transition)
			}
			r.ack <- infra
			continue
		}
		if infra != nil {
			continue
		}
		if r.err != nil {
			infra = r.err
			continue
		}
		t := r.transition
		if r.hasParsed {
			dp, pp, e := s.Publish(r.doc, r.parsed, r.validation)
			if e != nil {
				infra = e
				continue
			}
			t.DocumentPath = dp
			t.ParsedPath = pp
			t.DocumentSHA256, e = reportArtifactHash(s.OutDir, dp)
			if e != nil {
				infra = e
				continue
			}
			t.ParsedSHA256, e = reportArtifactHash(s.OutDir, pp)
			if e != nil {
				infra = e
				continue
			}

			if r.validation.Accepted {
				t.LastAcceptedHash = t.Report.SHA256
				t.LastAcceptedPath = pp
			}
		}
		if e = s.Append(t); e != nil {
			infra = e
		}
	}
	if infra != nil {
		return 1, summary, infra
	}
	retainedCount := 0
	for _, id := range selected {
		t := s.Latest[id]
		if _, ok := currentAccepted(s, t); ok {
			summary.Accepted++
		} else {
			summary.Unresolved++
			summary.Failures = append(summary.Failures, t)
		}
		if t.Report != nil {
			summary.PDFBytes += t.Report.Bytes
			retainedCount++
		}
	}
	summary.ElapsedSeconds = time.Since(start).Seconds()
	if summary.ElapsedSeconds > 0 {
		summary.ReportsPerSecond = float64(len(jobs)) / summary.ElapsedSeconds
	}
	if retainedCount > 0 {
		summary.BytesPerReport = float64(summary.PDFBytes) / float64(retainedCount)
	}
	generation, e := PublishReportExports(s, runID, selected)
	if e != nil {
		return 1, summary, e
	}
	summary.ExportGeneration = generation
	if e = writeReportJSON(filepath.Join(c.OutDir, "reports", "run_summary.json"), summary); e != nil {
		return 1, summary, e
	}
	if ctx.Err() != nil {
		return 2, summary, ctx.Err()
	}
	if summary.Unresolved > 0 {
		return 2, summary, nil
	}
	return 0, summary, nil
}
func processReport(ctx context.Context, c ReportsConfig, client *Client, runID string, job reportWork, checkpoint func(ReportTransition) error) reportWorkResult {
	t := job.prior
	t.RunID = runID
	t.AttemptID = runID + "-" + job.crd
	t.CRD = job.crd
	t.Error = ""
	t.ErrorCode = ""
	t.ParsedPath = ""
	t.DocumentPath = ""
	t.DocumentSHA256 = ""
	t.ParsedSHA256 = ""
	fail := func(outcome, code string, e error) reportWorkResult {
		t.Outcome = outcome
		t.ErrorCode = code
		t.Error = e.Error()
		return reportWorkResult{transition: t}
	}
	if ctx.Err() != nil {
		t.ExtractionState = "pending"
		return fail("transient_failure", "cancelled", ctx.Err())
	}
	verified := t.Report != nil && verifyReportRef(c.OutDir, *t.Report) == nil
	fetch := c.Refresh || !verified || job.prior.DownloadState == "pending" || job.prior.DownloadState == "transient_failure"
	if c.RetryFailed && (job.prior.DownloadState == "unavailable" || job.prior.DownloadState == "access_failure" || job.prior.DownloadState == "transient_failure" || job.prior.DownloadState == "invalid_pdf") {
		fetch = true
	}
	if c.Reparse {
		fetch = false
		if !verified {
			t.ExtractionState = "parse_failure"
			return fail("parse_failure", "missing_or_corrupt_pdf", fmt.Errorf("no verified retained PDF for %s", job.crd))
		}
	}
	if fetch {
		ref, e := DownloadReport(ctx, client, DownloadConfig{OutDir: c.OutDir, BaseURL: c.BaseURL}, job.crd)
		if e != nil {
			outcome := "transient_failure"
			var status *HTTPStatusError
			if errors.As(e, &status) {
				switch status.StatusCode {
				case 404, 410:
					outcome = "unavailable"
				case 401, 403:
					outcome = "access_failure"
				}
			}
			var invalidPDF *InvalidPDFError
			if errors.As(e, &invalidPDF) {
				outcome = "invalid_pdf"
			}
			var pe *os.PathError
			if errors.As(e, &pe) {
				return reportWorkResult{err: e}
			}
			t.DownloadState = outcome
			t.ExtractionState = "pending"
			return fail(outcome, outcome, e)
		}
		t.Report = &ref
		t.DownloadState = "downloaded"
		t.ExtractionState = "pending"
		t.Outcome = "downloaded"
		if e := checkpoint(t); e != nil {
			return reportWorkResult{err: e}
		}
	}
	t.DownloadState = "downloaded"
	ref := *t.Report
	abs, e := safeReportPath(c.OutDir, ref.PDFPath)
	if e != nil {
		return reportWorkResult{err: e}
	}
	extractRef := ref
	extractRef.PDFPath = abs
	doc, e := ExtractDocument(ctx, extractRef)
	if e != nil {
		t.ExtractionState = "parse_failure"
		return fail("parse_failure", "extractor_failure", e)
	}
	doc.Report = ref
	p := ParsedReport{Report: ref, SchemaVersion: SchemaVersion, ParserVersion: ParserVersion, ExtractorVersion: doc.ExtractorVersion, History: ParseHistory(doc), Disclosures: ParseDisclosures(doc)}
	v := ValidateReport(doc, p)
	t.ExtractionState = "needs_review"
	t.Outcome = "needs_review"
	if v.Accepted {
		t.ExtractionState = "accepted"
		t.Outcome = "accepted"
	}
	return reportWorkResult{transition: t, doc: doc, parsed: p, validation: v, hasParsed: true}
}
