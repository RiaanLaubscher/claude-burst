package router

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func gzipped(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestGzipRequestBodyRejectedWhenCorrupt(t *testing.T) {
	called := false
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer primary.Close()

	s, _ := newTestServer(t, primary.URL, "")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/v1/messages", strings.NewReader("not gzip"))
	req.Header.Set("Content-Encoding", "gzip")
	s.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if called {
		t.Error("a corrupt gzip body reached the provider")
	}
}

func TestGzipRequestBodyOverLimitRejected(t *testing.T) {
	called := false
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer primary.Close()

	s, _ := newTestServer(t, primary.URL, "")
	s.cfg.MaxRequestMB = 1
	big := `{"model":"claude-opus-5-5","pad":"` + strings.Repeat("a", 2*1024*1024) + `"}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/v1/messages", bytes.NewReader(gzipped(t, big)))
	req.Header.Set("Content-Encoding", "gzip")
	s.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusRequestEntityTooLarge)
	}
	if called {
		t.Error("an oversized gzip body reached the provider")
	}
}
