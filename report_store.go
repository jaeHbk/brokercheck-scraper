package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var reportLocks sync.Map

type StoredReport struct {
	Parsed     ParsedReport     `json:"parsed"`
	Validation ValidationResult `json:"validation"`
}
type ReportTransition struct {
	ParsedSHA256     string     `json:"parsed_sha256,omitempty"`
	DocumentSHA256   string     `json:"document_sha256,omitempty"`
	RunID            string     `json:"run_id"`
	AttemptID        string     `json:"attempt_id"`
	CRD              string     `json:"crd"`
	Timestamp        string     `json:"timestamp"`
	Outcome          string     `json:"outcome"`
	DownloadState    string     `json:"download_state"`
	ExtractionState  string     `json:"extraction_state"`
	ErrorCode        string     `json:"error_code,omitempty"`
	Error            string     `json:"error,omitempty"`
	Report           *ReportRef `json:"report,omitempty"`
	ParsedPath       string     `json:"parsed_path,omitempty"`
	DocumentPath     string     `json:"document_path,omitempty"`
	LastAcceptedPath string     `json:"last_accepted_path,omitempty"`
	LastAcceptedHash string     `json:"last_accepted_hash,omitempty"`
	SchemaVersion    string     `json:"schema_version"`
	ParserVersion    string     `json:"parser_version"`
	ExtractorVersion string     `json:"extractor_version"`
}
type ReportStore struct {
	OutDir   string
	manifest *os.File
	lockPath string
	Latest   map[string]ReportTransition
}

// Fault hook allows tests to exercise publication boundaries without unreliable disk exhaustion.
var reportPublicationFault func(string) error

func reportFault(boundary string) error {
	if reportPublicationFault != nil {
		return reportPublicationFault(boundary)
	}
	return nil
}
func atomicReportFile(path string, write func(io.Writer) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = reportFault("write"); err == nil {
		err = write(f)
	}
	if err == nil {
		err = reportFault("sync")
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err == nil {
		err = reportFault("close")
	}
	if err != nil {
		return err
	}
	if err = reportFault("rename"); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	err = d.Sync()
	ce = d.Close()
	if err == nil {
		err = ce
	}
	return err
}
func writeReportJSON(path string, v any) error {
	return atomicReportFile(path, func(w io.Writer) error { return json.NewEncoder(w).Encode(v) })
}
func OpenReportStore(out string) (*ReportStore, error) {
	dir := filepath.Join(out, "reports")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	lock := filepath.Join(dir, ".lock")
	// Advisory flock is released by the kernel on interruption; PID is diagnostic.
	lf, err := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lf.Close()
		return nil, fmt.Errorf("output directory already locked: %w", err)
	}
	// Retain the lock descriptor until Close; never unlink a lock inode while another process can open it.
	s := &ReportStore{OutDir: out, lockPath: lock, Latest: map[string]ReportTransition{}}
	reportLocks.Store(s, lf)
	fail := func(e error) (*ReportStore, error) { s.Close(); return nil, e }
	if err = lf.Truncate(0); err != nil {
		return fail(err)
	}
	if _, err = lf.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0); err != nil {
		return fail(err)
	}
	if err = lf.Sync(); err != nil {
		return fail(err)
	}
	path := filepath.Join(dir, "manifest.jsonl")
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fail(err)
	}
	validEnd := len(b)
	if len(b) > 0 && b[len(b)-1] != '\n' {
		i := bytes.LastIndexByte(b, '\n')
		validEnd = i + 1
	}
	scan := bufio.NewScanner(bytes.NewReader(b[:validEnd]))
	scan.Buffer(make([]byte, 4096), 16*1024*1024)
	line := 0
	for scan.Scan() {
		line++
		var tr ReportTransition
		if err = json.Unmarshal(scan.Bytes(), &tr); err != nil {
			return fail(fmt.Errorf("manifest interior line %d: %w", line, err))
		}
		if tr.CRD == "" || tr.AttemptID == "" {
			return fail(fmt.Errorf("invalid manifest line %d", line))
		}
		s.Latest[tr.CRD] = tr
	}
	if err = scan.Err(); err != nil {
		return fail(err)
	}
	s.manifest, err = os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return fail(err)
	}
	if err = s.manifest.Truncate(int64(validEnd)); err != nil {
		return fail(err)
	}
	return s, nil
}
func (s *ReportStore) Close() error {
	var errs []error
	if s.manifest != nil {
		errs = append(errs, s.manifest.Close())
		s.manifest = nil
	}
	if v, ok := reportLocks.LoadAndDelete(s); ok {
		f := v.(*os.File)
		errs = append(errs, syscall.Flock(int(f.Fd()), syscall.LOCK_UN), f.Close())
	}
	return errors.Join(errs...)
}
func (s *ReportStore) Append(t ReportTransition) error {
	t.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	t.SchemaVersion = SchemaVersion
	t.ParserVersion = ParserVersion
	t.ExtractorVersion = ExtractorVersion
	b, e := json.Marshal(t)
	if e != nil {
		return e
	}
	if e = reportFault("manifest"); e != nil {
		return e
	}
	n, e := s.manifest.Write(append(b, '\n'))
	if e != nil {
		return e
	}
	if n != len(b)+1 {
		return io.ErrShortWrite
	}
	if e = s.manifest.Sync(); e != nil {
		return e
	}
	s.Latest[t.CRD] = t
	return nil
}
func safeReportPath(out, rel string) (string, error) {
	if filepath.IsAbs(rel) || rel == "" {
		return "", fmt.Errorf("invalid relative artifact path")
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("artifact escapes output")
	}
	return filepath.Join(out, clean), nil
}
func verifyReportRef(out string, r ReportRef) error {
	p, e := safeReportPath(out, r.PDFPath)
	if e != nil {
		return e
	}
	f, e := os.Open(p)
	if e != nil {
		return e
	}
	h := sha256.New()
	n, e := io.Copy(h, f)
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if hex.EncodeToString(h.Sum(nil)) != r.SHA256 || n != r.Bytes {
		return fmt.Errorf("PDF hash/size mismatch")
	}
	return nil
}
func (s *ReportStore) ReadParsed(t ReportTransition) (StoredReport, error) {
	var result StoredReport
	if t.Report == nil {
		return result, fmt.Errorf("missing report")
	}
	if e := verifyReportRef(s.OutDir, *t.Report); e != nil {
		return result, e
	}
	p, e := safeReportPath(s.OutDir, t.ParsedPath)
	if e != nil {
		return result, e
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return result, e
	}
	e = json.Unmarshal(b, &result)
	if e == nil && (result.Parsed.Report.SHA256 != t.Report.SHA256 || result.Parsed.Report.CRD != t.CRD) {
		e = fmt.Errorf("parsed artifact reference mismatch")
	}
	return result, e
}

// An extraction is immutable once referenced by a manifest transition. A
// same-version refresh with changed retrieval metadata gets a content revision.
func (s *ReportStore) Publish(doc Document, p ParsedReport, v ValidationResult) (string, string, error) {
	r := p.Report
	dp := filepath.Join("reports", "text", r.SHA256, ExtractorVersion+".json")
	pp := filepath.Join("reports", "parsed", r.CRD, r.SHA256, reportVersionKey()+".json")
	dp, e := s.publishImmutableJSON(dp, doc)
	if e != nil {
		return "", "", e
	}
	pp, e = s.publishImmutableJSON(pp, StoredReport{p, v})
	if e != nil {
		return "", "", e
	}
	return dp, pp, nil
}
func (s *ReportStore) publishImmutableJSON(rel string, v any) (string, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return "", e
	}
	b = append(b, '\n')
	path, e := safeReportPath(s.OutDir, rel)
	if e != nil {
		return "", e
	}
	old, e := os.ReadFile(path)
	if e == nil {
		if bytes.Equal(old, b) {
			return rel, nil
		}
		h := sha256.Sum256(b)
		rel = strings.TrimSuffix(rel, ".json") + "-" + hex.EncodeToString(h[:]) + ".json"
		path, e = safeReportPath(s.OutDir, rel)
		if e != nil {
			return "", e
		}
		if existing, e := os.ReadFile(path); e == nil {
			if bytes.Equal(existing, b) {
				return rel, nil
			}
			// Repair corrupted bytes at a content-derived path from the verified
			// source document; no valid historical artifact is replaced.
			e = atomicReportFile(path, func(w io.Writer) error {
				n, e := w.Write(b)
				if e == nil && n != len(b) {
					e = io.ErrShortWrite
				}
				return e
			})
			return rel, e
		} else if !os.IsNotExist(e) {
			return "", e
		}
	} else if !os.IsNotExist(e) {
		return "", e
	}
	e = atomicReportFile(path, func(w io.Writer) error {
		n, e := w.Write(b)
		if e == nil && n != len(b) {
			e = io.ErrShortWrite
		}
		return e
	})
	return rel, e
}

func reportArtifactHash(out, rel string) (string, error) {
	p, e := safeReportPath(out, rel)
	if e != nil {
		return "", e
	}
	f, e := os.Open(p)
	if e != nil {
		return "", e
	}
	h := sha256.New()
	_, e = io.Copy(h, f)
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
