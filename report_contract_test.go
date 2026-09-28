package main

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReportContractNulls(t *testing.T) {
	b, e := json.Marshal(RawDate{Raw: "Present", Precision: "present"})
	if e != nil || string(b) != `{"raw":"Present","iso":null,"precision":"present"}` {
		t.Fatalf("%s %v", b, e)
	}
	for _, s := range []string{"0", "-1", "../1", "1.0", " 1"} {
		if _, e := normalizeCRD(s); e == nil {
			t.Fatal(s)
		}
	}
}
func TestPDFFetchRetryTruncation(t *testing.T) {
	n := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if r.Header.Get("Accept") != "application/pdf" {
			t.Error("accept")
		}
		if n == 1 {
			w.Header().Set("Content-Length", "100")
			w.Write([]byte("partial"))
			return
		}
		w.Write([]byte("%PDF-final"))
	}))
	defer s.Close()
	c := newClient(newLimiter(10000, 1), 2)
	c.backoffBase = time.Millisecond
	c.breakerPause = time.Millisecond
	p := filepath.Join(t.TempDir(), "tmp")
	if _, e := c.FetchPDF(context.Background(), s.URL, p); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "%PDF-final" || n != 2 {
		t.Fatalf("%s %d", b, n)
	}
}
func TestPDFRetryAndLimiter(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	if pdfRetryAfter(now.Add(time.Minute).Format(http.TimeFormat), now) != time.Minute {
		t.Fatal("HTTP date")
	}
	if pdfRetryAfter("5", now) != 5*time.Second {
		t.Fatal("seconds")
	}
	l := newLimiter(.2, 1)
	l.onError()
	if l.currentRate() > .2 || math.IsNaN(l.currentRate()) {
		t.Fatal("floor exceeds ceiling")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if pdfWait(ctx, time.Hour) == nil {
		t.Fatal("cancellation")
	}
}
