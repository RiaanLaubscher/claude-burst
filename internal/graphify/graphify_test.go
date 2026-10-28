package graphify_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andrewbakercloudscale/claude-burst/internal/graphify"
)

var base = time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)

func use(t *testing.T, at time.Time, id, name string, input map[string]any) string {
	t.Helper()
	return entry(t, "assistant", at, map[string]any{"type": "tool_use", "id": id, "name": name, "input": input})
}

func result(t *testing.T, at time.Time, id, text string) string {
	t.Helper()
	return entry(t, "user", at, map[string]any{"type": "tool_result", "tool_use_id": id, "content": text})
}

func entry(t *testing.T, kind string, at time.Time, b map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"type": kind, "timestamp": at.UTC().Format(time.RFC3339Nano),
		"message": map[string]any{"content": []any{b}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw) + "\n"
}

func bash(t *testing.T, at time.Time, id, cmd string) string {
	t.Helper()
	return use(t, at, id, "Bash", map[string]any{"command": cmd})
}

func write(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "")), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendTo(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

type fixture struct {
	projects string
	repo     string
	scanner  *graphify.Scanner
}

func newFixture(t *testing.T, withGraph bool) fixture {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "proj")
	if err := os.MkdirAll(filepath.Join(repo, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "api", "a.go"), []byte(strings.Repeat("x", 4000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if withGraph {
		if err := os.MkdirAll(filepath.Join(repo, "graphify-out"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, "graphify-out", "graph.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	projects := filepath.Join(root, "projects")
	return fixture{
		projects: projects,
		repo:     repo,
		scanner: graphify.NewScanner(projects, graphify.Options{
			Resolve:      func(string) (string, string) { return "proj", repo },
			CommitsSince: func(string, time.Time) int { return 3 },
		}),
	}
}

const queryOut = "Graph: graphify-out/graph.json (14,085 nodes) | Traversal: BFS depth=2\n\n" +
	"[!] TRUNCATED: showing 54 of 2215 nodes (~2000-token budget).\n\n" +
	"NODE Aliases [src=api/a.go loc=L19 community=R]\nNODE Alias [src=api/a.go loc=L40]\n"

func TestReportCountsWhatClaudeDid(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	tr := filepath.Join(f.projects, "-p", "s1.jsonl")
	write(t, tr,
		bash(t, base, "q1", `graphify query "alias mismatch" 2>/dev/null | head -60`),
		result(t, base.Add(time.Second), "q1", queryOut),
		use(t, base.Add(2*time.Second), "r1", "Read", map[string]any{"file_path": "/x"}),
		use(t, base.Add(3*time.Second), "g1", "Grep", map[string]any{"pattern": "x"}),
		bash(t, base.Add(4*time.Second), "u1", "git add -A && graphify update . 2>&1 | tail -5"),
		result(t, base.Add(5*time.Second), "u1", "ok"),
		use(t, base.Add(6*time.Second), "sk", "Skill", map[string]any{"skill": "graphify"}),
	)
	write(t, filepath.Join(f.projects, "-p", "s2.jsonl"),
		use(t, base, "r2", "Read", map[string]any{"file_path": "/x"}),
		use(t, base, "r3", "Read", map[string]any{"file_path": "/y"}),
		use(t, base, "r4", "Glob", map[string]any{"pattern": "*"}),
	)

	rep, err := f.scanner.Report(base.Add(-time.Hour), base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	tot := rep.Totals
	if tot.Sessions != 2 || tot.GraphSessions != 2 || tot.SessionsUsing != 1 {
		t.Errorf("sessions = %+v", tot)
	}
	if tot.Queries != 1 || tot.Updates != 1 || tot.Skills != 1 || tot.Lookups != 1 {
		t.Errorf("calls = %+v", tot)
	}
	if tot.Truncated != 1 {
		t.Errorf("Truncated = %d, want 1", tot.Truncated)
	}
	if want := int64(len(queryOut) / 4); tot.ResultTokens != want {
		t.Errorf("ResultTokens = %d, want %d", tot.ResultTokens, want)
	}
	if tot.CitedTokens != 1000 {
		t.Errorf("CitedTokens = %d, want 1000", tot.CitedTokens)
	}
	if rep.Compare.UsingSessions != 1 || rep.Compare.UsingExplore != 2 || rep.Compare.OtherExplore != 3 {
		t.Errorf("Compare = %+v", rep.Compare)
	}
	if len(rep.Repos) != 1 || rep.Repos[0].CommitsSince != 3 || rep.Repos[0].GraphNodes != 14085 || !rep.Repos[0].HasGraph {
		t.Errorf("Repos = %+v", rep.Repos)
	}
	if len(rep.Recent) != 3 || rep.Recent[2].Text != "alias mismatch" {
		t.Errorf("Recent = %+v", rep.Recent)
	}
}

func TestCommandForms(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		cmd  string
		kind graphify.Kind
		text string
		want bool
	}{
		"plain query":        {`graphify query "how does auth work"`, graphify.Query, "how does auth work", true},
		"after cd":           {`cd /tmp/x && graphify query foo --budget 900`, graphify.Query, "foo --budget 900", true},
		"explain piped":      {`graphify explain "isPrivateRoute" 2>&1 | head -20; echo ---`, graphify.Explain, "isPrivateRoute", true},
		"path":               {`graphify path "A" "B"`, graphify.Path, "A B", true},
		"update":             {`git add x && graphify update . 2>&1 | tail -5`, graphify.Update, ".", true},
		"absolute binary":    {`~/.local/bin/graphify query x`, graphify.Query, "x", true},
		"hook install":       {`graphify hook install`, graphify.Other, "install", true},
		"output dir only":    {`git add graphify-out/ && ls`, "", "", false},
		"mentioned in echo":  {`echo "run graphify query later"`, "", "", false},
		"grep for the word":  {`grep -r graphify query.txt`, "", "", false},
		"python import only": {`python3 -c "from graphify.build import build_from_json"`, "", "", false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, false)
			write(t, filepath.Join(f.projects, "-p", "s.jsonl"), bash(t, base, "c", tc.cmd))
			rep, err := f.scanner.Report(base.Add(-time.Hour), base.Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			var got *graphify.Call
			if len(rep.Recent) > 0 {
				got = &rep.Recent[0]
			}
			if tc.kind == graphify.Other {
				if rep.Totals.Others != 1 {
					t.Errorf("Others = %d, want 1", rep.Totals.Others)
				}
				return
			}
			if !tc.want {
				if rep.Totals.Lookups+rep.Totals.Updates+rep.Totals.Others != 0 {
					t.Errorf("counted %+v for %q", rep.Totals, tc.cmd)
				}
				return
			}
			if got == nil || got.Kind != tc.kind || got.Text != tc.text {
				t.Errorf("got %+v, want kind %q text %q", got, tc.kind, tc.text)
			}
		})
	}
}

func TestScanResumesAppendedTranscript(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	tr := filepath.Join(f.projects, "-p", "s.jsonl")
	write(t, tr, bash(t, base, "q1", "graphify query one"), result(t, base, "q1", queryOut))
	from, to := base.Add(-time.Hour), base.Add(time.Hour)

	rep, err := f.scanner.Report(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Totals.Queries != 1 {
		t.Fatalf("first scan: Queries = %d, want 1", rep.Totals.Queries)
	}

	second := bash(t, base, "q2", "graphify query two")
	appendTo(t, tr, second[:len(second)/2])
	if rep, err = f.scanner.Report(from, to); err != nil {
		t.Fatal(err)
	}
	if rep.Totals.Queries != 1 {
		t.Errorf("half a line was counted: Queries = %d, want 1", rep.Totals.Queries)
	}

	appendTo(t, tr, second[len(second)/2:]+result(t, base, "q2", queryOut))
	if rep, err = f.scanner.Report(from, to); err != nil {
		t.Fatal(err)
	}
	if rep.Totals.Queries != 2 || rep.Totals.ResultTokens != 2*int64(len(queryOut)/4) {
		t.Errorf("after append: %+v, want 2 queries with their results", rep.Totals)
	}
}

func TestReportWindow(t *testing.T) {
	t.Parallel()
	f := newFixture(t, false)
	old := base.AddDate(0, 0, -10)
	write(t, filepath.Join(f.projects, "-p", "s.jsonl"),
		bash(t, old, "a", "graphify query old"),
		bash(t, base, "b", "graphify query new"),
	)
	rep, err := f.scanner.Report(base.AddDate(0, 0, -2), base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Totals.Queries != 1 || len(rep.Recent) != 1 || rep.Recent[0].Text != "new" {
		t.Errorf("window leaked: %+v", rep.Totals)
	}
	var days, lookups int
	for _, d := range rep.Daily {
		days++
		lookups += d.Lookups
	}
	if days != 3 || lookups != 1 {
		t.Errorf("Daily = %d days with %d lookups, want 3 and 1", days, lookups)
	}
}

func TestSubagentCountsTowardItsSession(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	write(t, filepath.Join(f.projects, "-p", "s1.jsonl"), use(t, base, "r", "Read", map[string]any{"file_path": "/x"}))
	write(t, filepath.Join(f.projects, "-p", "s1", "subagents", "agent-a.jsonl"),
		bash(t, base, "q", "graphify query sub"), result(t, base, "q", queryOut))
	rep, err := f.scanner.Report(base.Add(-time.Hour), base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Totals.Sessions != 1 || rep.Totals.SessionsUsing != 1 || rep.Compare.UsingExplore != 1 {
		t.Errorf("subagent split from its session: %+v %+v", rep.Totals, rep.Compare)
	}
}

func TestMissingProjectsDirIsEmpty(t *testing.T) {
	t.Parallel()
	s := graphify.NewScanner(filepath.Join(t.TempDir(), "nope"), graphify.Options{
		Resolve:      func(string) (string, string) { return "", "" },
		CommitsSince: func(string, time.Time) int { return 0 },
	})
	rep, err := s.Report(base, base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Totals != (graphify.Totals{}) {
		t.Errorf("Totals = %+v, want zero", rep.Totals)
	}
}

func TestResumedTranscriptDoesNotCountACallTwice(t *testing.T) {
	t.Parallel()
	f := newFixture(t, true)
	lines := []string{bash(t, base, "q1", "graphify query one"), result(t, base, "q1", queryOut)}
	write(t, filepath.Join(f.projects, "-p", "s1.jsonl"), lines...)
	write(t, filepath.Join(f.projects, "-p", "s2.jsonl"), append(lines, bash(t, base, "q2", "graphify query two"))...)
	rep, err := f.scanner.Report(base.Add(-time.Hour), base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Totals.Queries != 2 {
		t.Errorf("Queries = %d, want 2", rep.Totals.Queries)
	}
}
