package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// InvalidPDFError means a successful HTTP response could not be retained as a
// valid PDF. Transport, cancellation, dependency, and filesystem errors retain
// their original types instead so callers can distinguish their outcomes.
type InvalidPDFError struct{ Err error }

func (e *InvalidPDFError) Error() string { return "invalid PDF: " + e.Err.Error() }
func (e *InvalidPDFError) Unwrap() error { return e.Err }

func DownloadReport(ctx context.Context, c *Client, cfg DownloadConfig, crd string) (ReportRef, error) {
	return downloadReportWithFault(ctx, c, cfg, crd, nil)
}

// The private, per-call hook exercises publication failures without global test
// state or replacing the shared HTTP client and PDF backend.
func downloadReportWithFault(ctx context.Context, c *Client, cfg DownloadConfig, crd string, fault func(string) error) (ref ReportRef, err error) {
	check := func(boundary string) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if fault != nil {
			return fault(boundary)
		}
		return nil
	}
	crd, err = normalizeCRD(crd)
	if err != nil {
		return ReportRef{}, err
	}
	if c == nil {
		return ReportRef{}, fmt.Errorf("missing PDF client")
	}
	base := cfg.BaseURL
	if base == "" {
		base = DefaultReportBaseURL
	}
	u, err := url.Parse(base)
	if err != nil {
		return ReportRef{}, fmt.Errorf("invalid report base URL: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return ReportRef{}, fmt.Errorf("invalid report base URL")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/individual_" + crd + ".pdf"
	u.RawPath = ""
	if err = check("start"); err != nil {
		return ReportRef{}, err
	}
	out := cfg.OutDir
	if out == "" {
		out = "."
	}
	dir, err := downloadReportDirectory(out, crd)
	if err != nil {
		return ReportRef{}, err
	}
	f, err := os.CreateTemp(dir, ".download-*.pdf")
	if err != nil {
		return ReportRef{}, err
	}
	temporaryPath := f.Name()
	defer func() {
		if e := os.Remove(temporaryPath); e != nil && !os.IsNotExist(e) {
			err = errors.Join(err, e)
		}
		if err != nil {
			ref = ReportRef{}
		}
	}()
	if err = f.Close(); err != nil {
		return ReportRef{}, err
	}
	meta, err := c.FetchPDF(ctx, u.String(), temporaryPath)
	if err != nil {
		return ReportRef{}, err
	}
	if err = check("downloaded"); err != nil {
		return ReportRef{}, err
	}
	if meta.StatusCode != http.StatusOK {
		return ReportRef{}, &HTTPStatusError{StatusCode: meta.StatusCode, URL: u.String()}
	}
	if meta.ContentType != "" {
		mediaType, _, e := mime.ParseMediaType(meta.ContentType)
		if e != nil || (mediaType != "application/pdf" && mediaType != "application/octet-stream") {
			return ReportRef{}, &InvalidPDFError{fmt.Errorf("unexpected content type %q", meta.ContentType)}
		}
	}
	if err = downloadPDFEnvelope(temporaryPath); err != nil {
		return ReportRef{}, err
	}
	info, err := ProbePDF(ctx, temporaryPath)
	if err != nil {
		if e := ctx.Err(); e != nil {
			return ReportRef{}, e
		}
		var launch *exec.Error
		var pathErr *os.PathError
		if errors.As(err, &launch) || errors.As(err, &pathErr) || errors.Is(err, exec.ErrNotFound) {
			return ReportRef{}, err
		}
		return ReportRef{}, &InvalidPDFError{err}
	}
	if info.PageCount < 1 {
		return ReportRef{}, &InvalidPDFError{fmt.Errorf("report has no pages")}
	}
	hash, size, err := downloadPDFHash(temporaryPath)
	if err != nil {
		return ReportRef{}, err
	}
	rel := filepath.Join("reports", "pdf", crd, hash+".pdf")
	destination := filepath.Join(out, rel)
	fetched, err := time.Parse(time.RFC3339Nano, meta.RetrievedAt)
	if err != nil {
		return ReportRef{}, fmt.Errorf("invalid retrieval timestamp: %w", err)
	}
	ref = ReportRef{CRD: crd, SHA256: hash, SourceURL: u.String(), PDFPath: rel, FetchedAt: fetched.UTC().Format(time.RFC3339Nano), Bytes: size, PageCount: info.PageCount}
	if st, e := os.Lstat(destination); e == nil {
		if !st.Mode().IsRegular() {
			return ReportRef{}, fmt.Errorf("PDF artifact is not a regular file: %s", destination)
		}
		existingHash, existingSize, e := downloadPDFHash(destination)
		if e != nil {
			return ReportRef{}, e
		}
		if existingHash == hash && existingSize == size {
			if err = check("reuse"); err != nil {
				return ReportRef{}, err
			}
			// A previous attempt may have been interrupted after rename and before
			// directory sync; make that artifact durable before reporting reuse.
			if err = downloadSyncDirectory(dir); err != nil {
				return ReportRef{}, err
			}
			return ref, nil
		}
		// Corrupt bytes at the expected hash are repaired with independently checked
		// bytes. Other hashes (historical snapshots) are never touched.
	} else if !os.IsNotExist(e) {
		return ReportRef{}, e
	}
	if err = check("sync"); err != nil {
		return ReportRef{}, err
	}
	f, err = os.OpenFile(temporaryPath, os.O_RDWR, 0)
	if err != nil {
		return ReportRef{}, err
	}
	syncErr := f.Sync()
	closeErr := f.Close()
	if err = errors.Join(syncErr, closeErr); err != nil {
		return ReportRef{}, err
	}
	if err = check("close"); err != nil {
		return ReportRef{}, err
	}
	if err = check("rename"); err != nil {
		return ReportRef{}, err
	}
	if err = os.Rename(temporaryPath, destination); err != nil {
		return ReportRef{}, err
	}
	if err = check("directory_sync"); err != nil {
		return ReportRef{}, err
	}
	if err = downloadSyncDirectory(dir); err != nil {
		return ReportRef{}, err
	}
	return ref, nil
}

func downloadSyncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

func downloadReportDirectory(out, crd string) (string, error) {
	if err := os.MkdirAll(out, 0755); err != nil {
		return "", err
	}
	current := out
	for _, part := range []string{"reports", "pdf", crd} {
		next := filepath.Join(current, part)
		err := os.Mkdir(next, 0755)
		if err != nil && !os.IsExist(err) {
			return "", err
		}
		st, e := os.Lstat(next)
		if e != nil {
			return "", e
		}
		if !st.IsDir() {
			return "", fmt.Errorf("report directory is not a directory: %s", next)
		}
		if err == nil {
			if e := downloadSyncDirectory(current); e != nil {
				return "", e
			}
		}
		current = next
	}
	return current, nil
}

func downloadPDFHash(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	size, readErr := io.Copy(h, f)
	if err = errors.Join(readErr, f.Close()); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

func downloadPDFEnvelope(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	var header [5]byte
	_, readErr := io.ReadFull(f, header[:])
	if readErr != nil {
		ce := f.Close()
		if ce != nil {
			return ce
		}
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
			return &InvalidPDFError{fmt.Errorf("missing PDF header")}
		}
		return readErr
	}
	if string(header[:]) != "%PDF-" {
		return errors.Join(&InvalidPDFError{fmt.Errorf("missing PDF header")}, f.Close())
	}
	st, statErr := f.Stat()
	if statErr != nil {
		return errors.Join(statErr, f.Close())
	}
	tailSize := min(st.Size(), int64(1024))
	tail := make([]byte, tailSize)
	_, readErr = f.ReadAt(tail, st.Size()-tailSize)
	if err = errors.Join(readErr, f.Close()); err != nil {
		return err
	}
	if !bytes.Contains(tail, []byte("%%EOF")) {
		return &InvalidPDFError{fmt.Errorf("missing PDF end marker")}
	}
	return nil
}
