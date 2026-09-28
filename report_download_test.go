package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func downloadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "pdf", name))
	if err != nil {
		t.Fatal(err)
	}
	// Backend tests deliberately fail if Poppler is unavailable in acceptance.
	if _, err := ProbePDF(context.Background(), filepath.Join("testdata", "pdf", name)); err != nil {
		t.Fatal(err)
	}
	return b
}
func downloadTestClient(attempts int) *Client {
	c := newClient(newLimiter(10000, 10000), attempts)
	c.breakerPause = time.Millisecond
	c.backoffBase = time.Millisecond
	return c
}
func downloadServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/individual_1691670.pdf" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Accept") != "application/pdf" {
			t.Errorf("PDF Accept header missing")
		}
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}
func assertDownloadFiles(t *testing.T, dir string, count int) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "reports", "pdf", "1691670", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != count {
		t.Fatalf("retained files = %v, want %d", files, count)
	}
}
func assertDownloadHash(t *testing.T, out string, ref ReportRef, expected []byte) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, ref.PDFPath))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, expected) || ref.SHA256 != fmt.Sprintf("%x", sha256.Sum256(b)) || ref.Bytes != int64(len(b)) {
		t.Fatalf("retained PDF/ref/hash mismatch: %+v", ref)
	}
	if ref.CRD != "1691670" || ref.PageCount < 1 || filepath.IsAbs(ref.PDFPath) {
		t.Fatalf("invalid ref: %+v", ref)
	}
	if _, err := time.Parse(time.RFC3339Nano, ref.FetchedAt); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadReportRealRetentionRefreshAndRecovery(t *testing.T) {
	body := downloadFixture(t, "1691670.pdf")
	srv := downloadServer(t, body)
	out := t.TempDir()
	cfg := DownloadConfig{OutDir: out, BaseURL: srv.URL}
	c := downloadTestClient(2)
	first, err := DownloadReport(context.Background(), c, cfg, "0001691670")
	if err != nil {
		t.Fatal(err)
	}
	assertDownloadHash(t, out, first, body)
	if first.SourceURL != srv.URL+"/individual_1691670.pdf" {
		t.Fatalf("wrong source URL: %s", first.SourceURL)
	}
	p := filepath.Join(out, first.PDFPath)
	old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	refreshed, err := DownloadReport(context.Background(), c, cfg, "1691670")
	if err != nil {
		t.Fatal(err)
	}
	if first.PDFPath != refreshed.PDFPath || first.FetchedAt == refreshed.FetchedAt {
		t.Fatal("identical refresh must reuse bytes with new retrieval metadata")
	}
	st, err := os.Stat(p)
	if err != nil || !st.ModTime().Equal(old) {
		t.Fatalf("refresh rewrote immutable PDF: %v %v", st, err)
	}
	assertDownloadFiles(t, out, 1)
	// A missing/corrupt hash-addressed file is recoverable by reacquisition.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	recovered, err := DownloadReport(context.Background(), c, cfg, "1691670")
	if err != nil {
		t.Fatal(err)
	}
	assertDownloadHash(t, out, recovered, body)
	if err := os.WriteFile(p, []byte("corrupted"), 0600); err != nil {
		t.Fatal(err)
	}
	recovered, err = DownloadReport(context.Background(), c, cfg, "1691670")
	if err != nil {
		t.Fatal(err)
	}
	assertDownloadHash(t, out, recovered, body)
	// A distinct valid snapshot has its own address, retaining the first file.
	other := downloadFixture(t, "synthetic-no-disclosures.pdf")
	secondServer := downloadServer(t, other)
	cfg.BaseURL = secondServer.URL
	second, err := DownloadReport(context.Background(), c, cfg, "1691670")
	if err != nil {
		t.Fatal(err)
	}
	if second.SHA256 == first.SHA256 {
		t.Fatal("different PDFs share hash")
	}
	assertDownloadHash(t, out, first, body)
	assertDownloadHash(t, out, second, other)
	assertDownloadFiles(t, out, 2)
}

func TestDownloadReportRejectsInvalidBodies(t *testing.T) {
	body := downloadFixture(t, "synthetic-no-disclosures.pdf")
	for _, tc := range []struct {
		name, contentType string
		body              []byte
	}{
		{"HTML content type", "text/html", body},
		{"HTML body", "application/pdf", []byte("<html>error</html>")},
		{"empty", "application/pdf", nil},
		{"truncated PDF", "application/pdf", body[:len(body)/2]},
		{"fake PDF", "application/pdf", []byte("%PDF-1.7\nnot a document\n%%EOF\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				_, _ = w.Write(tc.body)
			}))
			defer srv.Close()
			out := t.TempDir()
			ref, err := DownloadReport(context.Background(), downloadTestClient(1), DownloadConfig{out, srv.URL}, "1691670")
			var invalid *InvalidPDFError
			if !errors.As(err, &invalid) || ref != (ReportRef{}) {
				t.Fatalf("expected typed invalid PDF with no reference, got %+v %v", ref, err)
			}
			assertDownloadFiles(t, out, 0)
		})
	}
}
func TestDownloadReportHTTPOutcomes(t *testing.T) {
	body := downloadFixture(t, "synthetic-no-disclosures.pdf")
	for _, code := range []int{403, 404, 410, 429, 500, 503} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(code); _, _ = w.Write(body) }))
			defer srv.Close()
			out := t.TempDir()
			_, err := DownloadReport(context.Background(), downloadTestClient(2), DownloadConfig{out, srv.URL}, "1691670")
			var status *HTTPStatusError
			if !errors.As(err, &status) || status.StatusCode != code {
				t.Fatalf("expected typed HTTP %d, got %v", code, err)
			}
			wantCalls := int32(1)
			if code == 429 || code >= 500 {
				wantCalls = 2
			}
			if calls.Load() != wantCalls {
				t.Fatalf("calls=%d want=%d", calls.Load(), wantCalls)
			}
			assertDownloadFiles(t, out, 0)
		})
	}
	for _, code := range []int{429, 503} {
		t.Run(fmt.Sprintf("recover-%d", code), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(code)
					return
				}
				w.Header().Set("Content-Type", "application/pdf")
				_, _ = w.Write(body)
			}))
			defer srv.Close()
			out := t.TempDir()
			ref, err := DownloadReport(context.Background(), downloadTestClient(2), DownloadConfig{out, srv.URL}, "1691670")
			if err != nil {
				t.Fatal(err)
			}
			assertDownloadHash(t, out, ref, body)
			if calls.Load() != 2 {
				t.Fatal("retry did not recover")
			}
		})
	}
}
func TestDownloadReportPartialBodyRetriesTruncate(t *testing.T) {
	body := downloadFixture(t, "synthetic-no-disclosures.pdf")
	for _, recover := range []bool{false, true} {
		t.Run(strconv.FormatBool(recover), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				w.Header().Set("Content-Type", "application/pdf")
				if n == 1 || !recover {
					w.Header().Set("Content-Length", strconv.Itoa(len(body)*3))
					_, _ = w.Write(append(body, body...))
					return
				}
				_, _ = w.Write(body)
			}))
			defer srv.Close()
			out := t.TempDir()
			ref, err := DownloadReport(context.Background(), downloadTestClient(2), DownloadConfig{out, srv.URL}, "1691670")
			if recover {
				if err != nil {
					t.Fatal(err)
				}
				assertDownloadHash(t, out, ref, body)
				assertDownloadFiles(t, out, 1)
			} else {
				if !errors.Is(err, io.ErrUnexpectedEOF) || ref != (ReportRef{}) {
					t.Fatalf("partial response accepted: %+v %v", ref, err)
				}
				assertDownloadFiles(t, out, 0)
			}
			if calls.Load() != 2 {
				t.Fatalf("calls=%d", calls.Load())
			}
		})
	}
}
func TestDownloadReportCancellationAndPublicationBoundaries(t *testing.T) {
	body := downloadFixture(t, "synthetic-no-disclosures.pdf")
	srv := downloadServer(t, body)
	for _, boundary := range []string{"start", "downloaded", "sync", "close", "rename", "directory_sync"} {
		t.Run(boundary, func(t *testing.T) {
			out := t.TempDir()
			injected := errors.New("injected publication interruption")
			ref, err := downloadReportWithFault(context.Background(), downloadTestClient(1), DownloadConfig{out, srv.URL}, "1691670", func(b string) error {
				if b == boundary {
					return injected
				}
				return nil
			})
			if !errors.Is(err, injected) || ref != (ReportRef{}) {
				t.Fatalf("publication error lost: %+v %v", ref, err)
			}
			// After rename, durable valid bytes may exist, but no completion is returned.
			wantFiles := 0
			if boundary == "directory_sync" {
				wantFiles = 1
			}
			assertDownloadFiles(t, out, wantFiles)
			recovered, err := DownloadReport(context.Background(), downloadTestClient(1), DownloadConfig{out, srv.URL}, "1691670")
			if err != nil {
				t.Fatal(err)
			}
			assertDownloadHash(t, out, recovered, body)
			assertDownloadFiles(t, out, 1)
		})
	}
	t.Run("canceled in flight", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write(body[:32])
			w.(http.Flusher).Flush()
			cancel()
			<-r.Context().Done()
		}))
		defer server.Close()
		out := t.TempDir()
		ref, err := DownloadReport(ctx, downloadTestClient(3), DownloadConfig{out, server.URL}, "1691670")
		if !errors.Is(err, context.Canceled) || ref != (ReportRef{}) {
			t.Fatalf("cancellation lost: %+v %v", ref, err)
		}
		assertDownloadFiles(t, out, 0)
	})
	t.Run("canceled before publish", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		out := t.TempDir()
		ref, err := downloadReportWithFault(ctx, downloadTestClient(1), DownloadConfig{out, srv.URL}, "1691670", func(b string) error {
			if b == "downloaded" {
				cancel()
			}
			return nil
		})
		if !errors.Is(err, context.Canceled) || ref != (ReportRef{}) {
			t.Fatalf("cancellation lost: %+v %v", ref, err)
		}
		assertDownloadFiles(t, out, 0)
	})
}
func TestDownloadReportUnsafePathsAndInfrastructureErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); t.Error("invalid request reached network") }))
	defer srv.Close()
	for _, crd := range []string{"", "0", "00", "../1691670", "1/2", "-1", "+1", " 1", "1?x=y"} {
		if _, err := DownloadReport(context.Background(), downloadTestClient(1), DownloadConfig{t.TempDir(), srv.URL}, crd); err == nil {
			t.Fatalf("unsafe CRD accepted: %q", crd)
		}
	}
	for _, base := range []string{"file:///tmp", "/relative", srv.URL + "?q=x", srv.URL + "#fragment"} {
		if _, err := DownloadReport(context.Background(), downloadTestClient(1), DownloadConfig{t.TempDir(), base}, "1691670"); err == nil {
			t.Fatalf("invalid base accepted: %s", base)
		}
	}
	t.Run("output blocked by file", func(t *testing.T) {
		out := t.TempDir()
		if err := os.WriteFile(filepath.Join(out, "reports"), []byte("existing user file"), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := DownloadReport(context.Background(), downloadTestClient(1), DownloadConfig{out, srv.URL}, "1691670")
		if err == nil {
			t.Fatal("output obstruction ignored")
		}
	})
	t.Run("symlink directory", func(t *testing.T) {
		out, external := t.TempDir(), t.TempDir()
		if err := os.Symlink(external, filepath.Join(out, "reports")); err != nil {
			t.Fatal(err)
		}
		_, err := DownloadReport(context.Background(), downloadTestClient(1), DownloadConfig{out, srv.URL}, "1691670")
		if err == nil {
			t.Fatal("symlink directory accepted")
		}
	})
	if calls.Load() != 0 {
		t.Fatalf("invalid configurations made %d requests", calls.Load())
	}
}
func TestDownloadReportMissingBackendIsInfrastructure(t *testing.T) {
	body := downloadFixture(t, "synthetic-no-disclosures.pdf")
	srv := downloadServer(t, body)
	t.Setenv("PDFINFO", filepath.Join(t.TempDir(), "missing-pdfinfo"))
	out := t.TempDir()
	_, err := DownloadReport(context.Background(), downloadTestClient(1), DownloadConfig{out, srv.URL}, "1691670")
	var invalid *InvalidPDFError
	if err == nil || errors.As(err, &invalid) || !strings.Contains(err.Error(), "pdfinfo") {
		t.Fatalf("backend failure misclassified: %v", err)
	}
	assertDownloadFiles(t, out, 0)
}

func TestDownloadReportExistingArtifactObstructions(t *testing.T) {
	body := downloadFixture(t, "synthetic-no-disclosures.pdf")
	srv := downloadServer(t, body)
	for _, kind := range []string{"symlink", "directory"} {
		t.Run(kind, func(t *testing.T) {
			out := t.TempDir()
			dir := filepath.Join(out, "reports", "pdf", "1691670")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(dir, fmt.Sprintf("%x.pdf", sha256.Sum256(body)))
			outside := filepath.Join(t.TempDir(), "user-file")
			if err := os.WriteFile(outside, []byte("preserve"), 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "symlink" {
				if err := os.Symlink(outside, destination); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Mkdir(destination, 0755); err != nil {
				t.Fatal(err)
			}
			ref, err := DownloadReport(context.Background(), downloadTestClient(1), DownloadConfig{out, srv.URL}, "1691670")
			if err == nil || ref != (ReportRef{}) {
				t.Fatalf("obstruction accepted: %+v %v", ref, err)
			}
			b, err := os.ReadFile(outside)
			if err != nil || string(b) != "preserve" {
				t.Fatal("existing user bytes modified")
			}
			assertDownloadFiles(t, out, 1)
		})
	}
}

func TestDownloadReportCancellationDuringRetryWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		close(started)
	}))
	defer srv.Close()
	go func() { <-started; time.Sleep(20 * time.Millisecond); cancel() }()
	out := t.TempDir()
	begin := time.Now()
	ref, err := DownloadReport(ctx, downloadTestClient(5), DownloadConfig{out, srv.URL}, "1691670")
	if !errors.Is(err, context.Canceled) || ref != (ReportRef{}) {
		t.Fatalf("retry wait cancellation lost: %+v %v", ref, err)
	}
	if calls.Load() != 1 || time.Since(begin) > time.Second {
		t.Fatalf("retry wait ignored cancellation: calls=%d elapsed=%s", calls.Load(), time.Since(begin))
	}
	assertDownloadFiles(t, out, 0)
}
