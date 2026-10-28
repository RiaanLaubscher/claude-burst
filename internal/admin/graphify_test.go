package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andrewbakercloudscale/claude-burst/internal/repo"
)

func graphifyFixture(t *testing.T) *Server {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	s := newTestServer(t)
	s.repos = &repoResolver{repo.NewWith(filepath.Join(cfg, "projects"), []string{"/nonexistent/"})}
	proj := filepath.Join(t.TempDir(), "proj")
	for _, d := range []string{filepath.Join(proj, ".git"), filepath.Join(proj, "graphify-out")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(proj, "graphify-out", "graph.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Format(time.RFC3339Nano)
	lines := []string{
		`{"type":"user","cwd":"` + proj + `","timestamp":"` + at + `","message":{"content":"hi"}}`,
		`{"type":"assistant","timestamp":"` + at + `","message":{"content":[{"type":"tool_use","id":"q1","name":"Bash","input":{"command":"graphify query \"alias\""}}]}}`,
		`{"type":"user","timestamp":"` + at + `","message":{"content":[{"type":"tool_result","tool_use_id":"q1","content":"NODE A [src=a.go loc=L1]"}]}}`,
	}
	dir := filepath.Join(cfg, "projects", "-p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sess1.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGraphifyEndpointReportsCalls(t *testing.T) {
	s := graphifyFixture(t)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, localRequest(http.MethodGet, "http://127.0.0.1/api/graphify?days=7", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Days   int `json:"days"`
		Totals struct {
			Sessions      int `json:"sessions"`
			GraphSessions int `json:"graph_sessions"`
			SessionsUsing int `json:"sessions_using"`
			Queries       int `json:"queries"`
		} `json:"totals"`
		Repos []struct {
			Repo     string `json:"repo"`
			HasGraph bool   `json:"has_graph"`
		} `json:"repos"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Days != 7 || got.Totals.Sessions != 1 || got.Totals.GraphSessions != 1 || got.Totals.SessionsUsing != 1 || got.Totals.Queries != 1 {
		t.Errorf("response = %+v", got)
	}
	if len(got.Repos) != 1 || got.Repos[0].Repo != "proj" || !got.Repos[0].HasGraph {
		t.Errorf("repos = %+v", got.Repos)
	}
}

func TestGraphifyEndpointRejectsBadWindow(t *testing.T) {
	s := graphifyFixture(t)
	for _, q := range []string{"0", "91", "x", "-3"} {
		rr := httptest.NewRecorder()
		s.Handler().ServeHTTP(rr, localRequest(http.MethodGet, "http://127.0.0.1/api/graphify?days="+q, nil))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("days=%s: status %d, want 400", q, rr.Code)
		}
	}
}

func TestGraphifyPageWiring(t *testing.T) {
	s := newTestServer(t)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, localRequest(http.MethodGet, "http://127.0.0.1/graphify.js", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "BurstGraphify") {
		t.Fatalf("graphify.js: %d", rr.Code)
	}
	page := string(indexHTML)
	for _, want := range []string{`data-target="sec-graphify"`, `<section id="sec-graphify"`, `src="/graphify.js"`} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %s", want)
		}
	}
}

func TestFindGraphifyLooksBeyondTheGatewaysPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	if got := findGraphify(home); got != "" {
		t.Fatalf("found %q with nothing installed", got)
	}
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(bin, "graphify")
	if err := os.WriteFile(want, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := findGraphify(home); got != want {
		t.Errorf("findGraphify = %q, want %q", got, want)
	}
}
