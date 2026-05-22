package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var errBrokerNotFound = errors.New("broker not found")

type enrichConfig struct {
	Out     string
	Workers int
	Resume  bool
	BaseURL string // e.g. "https://api.brokercheck.finra.org/search/individual"
	CRDs    []string
	Limit   int
}

func runEnrich() {
	rps := *flagRPS
	if rps <= 0 {
		rps = 2.0 // enrich default
	}
	workers := *flagWorkers
	if workers < 1 {
		workers = 4 // enrich default
	}

	crds, err := loadCRDsFromUnique(filepath.Join(*flagOutDir, *flagInput))
	if err != nil {
		log.Fatalf("load CRDs: %v", err)
	}
	log.Printf("loaded %d CRDs from %s", len(crds), *flagInput)

	cfg := enrichConfig{
		Out:     *flagOutDir,
		Workers: workers,
		Resume:  *flagResume,
		BaseURL: apiURL,
		CRDs:    crds,
		Limit:   *flagLimit,
	}
	limiter := newLimiter(rps, 0.25)
	client := newClient(limiter, *flagMaxRetries)

	log.Printf("enrichment starting (rps=%.2f workers=%d)", rps, workers)
	if err := runEnrichWith(context.Background(), client, cfg); err != nil {
		log.Fatalf("enrich failed: %v", err)
	}

	log.Println("enrichment complete; running finalize step...")
	if err := finalizeEnrich(cfg.Out); err != nil {
		log.Fatalf("finalize: %v", err)
	}
	log.Println("done")
}

// runEnrichWith is the testable core: pure inputs/outputs, no flag globals.
func runEnrichWith(ctx context.Context, c *Client, cfg enrichConfig) error {
	if err := os.MkdirAll(cfg.Out, 0755); err != nil {
		return fmt.Errorf("mkdir out: %w", err)
	}

	completed, err := loadEnrichProgress(cfg.Out)
	if err != nil {
		return fmt.Errorf("load progress: %w", err)
	}
	if cfg.Resume && len(completed) > 0 {
		log.Printf("enrich resume: %d CRDs already completed", len(completed))
	}

	var todo []string
	for _, crd := range cfg.CRDs {
		if cfg.Resume && completed[crd] {
			continue
		}
		todo = append(todo, crd)
	}
	if cfg.Limit > 0 && len(todo) > cfg.Limit {
		todo = todo[:cfg.Limit]
	}
	log.Printf("enrich: %d CRDs to process", len(todo))

	ew, err := newEnrichWriter(cfg.Out)
	if err != nil {
		return err
	}
	defer ew.close()

	jobs := make(chan string, cfg.Workers*2)
	var wg sync.WaitGroup
	var counter struct {
		mu   sync.Mutex
		done int
	}
	startTs := time.Now()

	for w := 0; w < cfg.Workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for crd := range jobs {
				if err := fetchAndEmit(ctx, c, cfg.BaseURL, crd, ew); err != nil {
					log.Printf("worker %d crd=%s: %v", id, crd, err)
				}
				counter.mu.Lock()
				counter.done++
				if counter.done%100 == 0 || counter.done == len(todo) {
					elapsed := time.Since(startTs)
					rate := float64(counter.done) / elapsed.Seconds()
					eta := time.Duration(float64(len(todo)-counter.done)/rate) * time.Second
					log.Printf("[%d/%d] rate=%.2f/s ETA=%s", counter.done, len(todo), rate, eta.Round(time.Second))
				}
				counter.mu.Unlock()
			}
		}(w)
	}
	for _, crd := range todo {
		jobs <- crd
	}
	close(jobs)
	wg.Wait()
	return nil
}

func fetchAndEmit(ctx context.Context, c *Client, baseURL, crd string, ew *enrichWriter) error {
	url := baseURL + "/" + crd
	body, err := c.Get(ctx, url)
	if err != nil {
		return err // do not mark complete; transient
	}
	d, err := parseDetail(crd, body)
	if err != nil {
		if errors.Is(err, errBrokerNotFound) {
			ew.markError(crd, "not_found")
			return nil
		}
		ew.markError(crd, fmt.Sprintf("parse: %v", err))
		return nil
	}
	d.FetchedAt = time.Now().UTC().Format(time.RFC3339)
	// emit and markDone are not atomic; a crash between them produces a
	// duplicate detail line on resume. finalizeEnrich deduplicates by CRD.
	ew.emit(d)
	ew.markDone(crd)
	return nil
}

// --- Persistence ---

type enrichWriter struct {
	mu       sync.Mutex
	out      *os.File
	progress *os.File
	errs     *os.File
}

func newEnrichWriter(outDir string) (*enrichWriter, error) {
	out, err := os.OpenFile(filepath.Join(outDir, "brokers_detail.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	prog, err := os.OpenFile(filepath.Join(outDir, "enrich_progress.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		out.Close()
		return nil, err
	}
	errs, err := os.OpenFile(filepath.Join(outDir, "enrich_errors.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		out.Close()
		prog.Close()
		return nil, err
	}
	return &enrichWriter{out: out, progress: prog, errs: errs}, nil
}

func (e *enrichWriter) emit(d *BrokerDetail) {
	e.mu.Lock()
	defer e.mu.Unlock()
	enc, err := json.Marshal(d)
	if err != nil {
		return
	}
	e.out.Write(enc)
	e.out.Write([]byte("\n"))
}

func (e *enrichWriter) markDone(crd string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec := map[string]string{"crd": crd, "ts": time.Now().UTC().Format(time.RFC3339)}
	enc, _ := json.Marshal(rec)
	e.progress.Write(enc)
	e.progress.Write([]byte("\n"))
}

func (e *enrichWriter) markError(crd, reason string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	ts := time.Now().UTC().Format(time.RFC3339)
	errRec, _ := json.Marshal(map[string]string{"crd": crd, "reason": reason, "ts": ts})
	e.errs.Write(errRec)
	e.errs.Write([]byte("\n"))
	// Errored CRDs also count as 'done' so resume skips them. Write a
	// schema-clean progress record (no reason field) so progress.jsonl
	// stays uniform; the diagnostic detail lives in enrich_errors.jsonl.
	progRec, _ := json.Marshal(map[string]string{"crd": crd, "ts": ts})
	e.progress.Write(progRec)
	e.progress.Write([]byte("\n"))
}

func (e *enrichWriter) close() {
	e.out.Close()
	e.progress.Close()
	e.errs.Close()
}

func loadEnrichProgress(outDir string) (map[string]bool, error) {
	done := map[string]bool{}
	f, err := os.Open(filepath.Join(outDir, "enrich_progress.jsonl"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return done, nil
		}
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var rec struct {
			CRD string `json:"crd"`
		}
		if err := json.Unmarshal(sc.Bytes(), &rec); err == nil && rec.CRD != "" {
			done[rec.CRD] = true
		}
	}
	return done, sc.Err()
}

// loadCRDsFromUnique reads brokers_unique.json (the search-stage output)
// and extracts the CRD list.
func loadCRDsFromUnique(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w (run `./brokercheck-scraper search` first)", err)
	}
	defer f.Close()
	var bs []BrokerSource
	if err := json.NewDecoder(f).Decode(&bs); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		if b.CRD != "" {
			out = append(out, b.CRD)
		}
	}
	return out, nil
}

func finalizeEnrich(outDir string) error {
	jsonlPath := filepath.Join(outDir, "brokers_detail.jsonl")
	f, err := os.Open(jsonlPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", jsonlPath, err)
	}
	defer f.Close()

	uniq := map[string]*BrokerDetail{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		var d BrokerDetail
		if err := json.Unmarshal(sc.Bytes(), &d); err != nil {
			continue
		}
		if d.CRD != "" {
			uniq[d.CRD] = &d
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}

	list := make([]*BrokerDetail, 0, len(uniq))
	for _, d := range uniq {
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CRD < list[j].CRD })

	// Pretty JSON
	jp := filepath.Join(outDir, "brokers_detail.json")
	jf, err := os.Create(jp)
	if err != nil {
		return err
	}
	defer jf.Close()
	enc := json.NewEncoder(jf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(list); err != nil {
		return err
	}

	// Flat CSV
	cp := filepath.Join(outDir, "brokers_detail.csv")
	cf, err := os.Create(cp)
	if err != nil {
		return err
	}
	defer cf.Close()
	w := csv.NewWriter(cf)
	defer w.Flush()
	w.Write([]string{
		"CRD", "FirstName", "MiddleName", "LastName", "OtherNamesCount",
		"BCScope", "IAScope", "DaysInIndustryStart", "CurrentFirmName",
		"CurrentBranchStreet", "CurrentBranchCity", "CurrentBranchState",
		"CurrentBranchZip", "CurrentEmploymentCount", "PreviousEmploymentCount",
		"ExamCategoriesList", "RegisteredStatesCount", "RegisteredStatesList",
		"RegisteredSROsList", "DisclosureCount", "HasDisclosure", "HasBCComments", "FetchedAt",
	})
	for _, d := range list {
		w.Write(detailToCSVRow(d))
	}
	return nil
}

func detailToCSVRow(d *BrokerDetail) []string {
	bi := d.BasicInfo
	var firmName, street, city, state, zip string
	if len(d.CurrentEmployments) > 0 {
		ce := d.CurrentEmployments[0]
		firmName = ce.FirmName
		if len(ce.BranchOfficeLocations) > 0 {
			b := ce.BranchOfficeLocations[0]
			street, city, state, zip = b.Street1, b.City, b.State, b.ZipCode
		} else {
			city, state = ce.City, ce.State
		}
	}
	exams := []string{}
	for _, e := range d.StateExams {
		exams = append(exams, e.ExamCategory)
	}
	for _, e := range d.PrincipalExams {
		exams = append(exams, e.ExamCategory)
	}
	for _, e := range d.ProductExams {
		exams = append(exams, e.ExamCategory)
	}
	states := []string{}
	for _, s := range d.RegisteredStates {
		states = append(states, s.State)
	}
	sros := []string{}
	for _, s := range d.RegisteredSROs {
		sros = append(sros, s.SRO)
	}
	hasDisc := "N"
	if len(d.Disclosures) > 0 {
		hasDisc = "Y"
	}
	return []string{
		d.CRD,
		bi.FirstName, bi.MiddleName, bi.LastName,
		fmt.Sprintf("%d", len(bi.OtherNames)),
		bi.BCScope, bi.IAScope, bi.DaysInIndustryCalculatedDate,
		firmName, street, city, state, zip,
		fmt.Sprintf("%d", len(d.CurrentEmployments)),
		fmt.Sprintf("%d", len(d.PreviousEmployments)),
		strings.Join(exams, ";"),
		fmt.Sprintf("%d", len(states)),
		strings.Join(states, ";"),
		strings.Join(sros, ";"),
		fmt.Sprintf("%d", len(d.Disclosures)),
		hasDisc,
		d.BrokerDetails.HasBCComments,
		d.FetchedAt,
	}
}

// parseDetail (from Task 6) is unchanged.
func parseDetail(crd string, body []byte) (*BrokerDetail, error) {
	var env DetailEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("envelope: %w", err)
	}
	if env.Hits.Total == 0 || len(env.Hits.Hits) == 0 {
		return nil, errBrokerNotFound
	}
	contentStr := env.Hits.Hits[0].Source.Content
	if contentStr == "" {
		return nil, fmt.Errorf("empty content field")
	}
	var d BrokerDetail
	if err := json.Unmarshal([]byte(contentStr), &d); err != nil {
		return nil, fmt.Errorf("content: %w", err)
	}
	d.CRD = crd
	return &d, nil
}
