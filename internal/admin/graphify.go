package admin

import (
	"context"
	_ "embed"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andrewbakercloudscale/claude-burst/internal/graphify"
)

//go:embed graphify.js
var graphifyJS []byte

const (
	graphifyDefaultDays = 30
	graphifyMaxDays     = 90
	gitTimeout          = 3 * time.Second
)

type graphifyScan struct {
	once    sync.Once
	scanner *graphify.Scanner
}

type graphifySetup struct {
	Binary        string `json:"binary"`
	HookInstalled bool   `json:"hook_installed"`
}

type graphifyResponse struct {
	graphify.Report
	Days  int           `json:"days"`
	Setup graphifySetup `json:"setup"`
}

func graphifyFrom(now time.Time, days int) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, -(days - 1))
}

func handleGraphifyJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(graphifyJS)
}

func (s *Server) handleGraphify(w http.ResponseWriter, r *http.Request) {
	days := graphifyDefaultDays
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > graphifyMaxDays {
			http.Error(w, "days must be 1 to "+strconv.Itoa(graphifyMaxDays), http.StatusBadRequest)
			return
		}
		days = n
	}
	now := time.Now()
	rep, err := s.graphifyScanner().Report(graphifyFrom(now, days), now)
	if err != nil {
		slog.ErrorContext(r.Context(), "graphify report", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, graphifyResponse{Report: rep, Days: days, Setup: graphifyInstall()})
}

func (s *Server) WarmGraphify() {
	now := time.Now()
	_, err := s.graphifyScanner().Report(graphifyFrom(now, graphifyDefaultDays), now)
	if err != nil {
		slog.Error("graphify warm-up", "err", err)
	}
}

func (s *Server) graphifyScanner() *graphify.Scanner {
	s.graphify.once.Do(func() {
		s.graphify.scanner = graphify.NewScanner(claudeProjectsDir(), graphify.Options{
			Resolve:      s.repos.resolve,
			CommitsSince: commitsSince,
		})
	})
	return s.graphify.scanner
}

func commitsSince(root string, since time.Time) int {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", root, "rev-list", "--count",
		"--since="+since.Format(time.RFC3339), "HEAD").Output()
	if err != nil {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return -1
	}
	return n
}

func graphifyInstall() graphifySetup {
	bin, _ := exec.LookPath("graphify")
	settings, _ := os.ReadFile(filepath.Join(filepath.Dir(claudeProjectsDir()), "settings.json"))
	return graphifySetup{Binary: bin, HookInstalled: strings.Contains(string(settings), "graphify")}
}
