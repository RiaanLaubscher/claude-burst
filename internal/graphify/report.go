package graphify

import (
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	recentLimit = 40
	citedCap    = 40000
)

type Call struct {
	At           time.Time `json:"at"`
	Session      string    `json:"session"`
	Repo         string    `json:"repo"`
	Kind         Kind      `json:"kind"`
	Text         string    `json:"text"`
	ResultTokens int64     `json:"result_tokens"`
	CitedTokens  int64     `json:"cited_tokens"`
	Nodes        int       `json:"nodes"`
	Shown        int       `json:"shown"`
	Truncated    bool      `json:"truncated"`
	Failed       bool      `json:"failed"`
}

type Totals struct {
	Sessions      int   `json:"sessions"`
	GraphSessions int   `json:"graph_sessions"`
	SessionsUsing int   `json:"sessions_using"`
	Lookups       int   `json:"lookups"`
	Queries       int   `json:"queries"`
	Paths         int   `json:"paths"`
	Explains      int   `json:"explains"`
	Updates       int   `json:"updates"`
	Skills        int   `json:"skills"`
	Others        int   `json:"others"`
	Truncated     int   `json:"truncated"`
	Failed        int   `json:"failed"`
	ResultTokens  int64 `json:"result_tokens"`
	CitedTokens   int64 `json:"cited_tokens"`
}

type Day struct {
	Date    string `json:"date"`
	Lookups int    `json:"lookups"`
	Updates int    `json:"updates"`
}

type RepoUse struct {
	Repo          string    `json:"repo"`
	Path          string    `json:"path,omitempty"`
	HasGraph      bool      `json:"has_graph"`
	GraphBuilt    time.Time `json:"graph_built,omitzero"`
	CommitsSince  int       `json:"commits_since"`
	GraphNodes    int       `json:"graph_nodes"`
	Sessions      int       `json:"sessions"`
	SessionsUsing int       `json:"sessions_using"`
	Lookups       int       `json:"lookups"`
	Updates       int       `json:"updates"`
	Truncated     int       `json:"truncated"`
	ResultTokens  int64     `json:"result_tokens"`
	CitedTokens   int64     `json:"cited_tokens"`
	Last          time.Time `json:"last,omitzero"`
}

type Compare struct {
	UsingSessions int     `json:"using_sessions"`
	OtherSessions int     `json:"other_sessions"`
	UsingExplore  float64 `json:"using_explore"`
	OtherExplore  float64 `json:"other_explore"`
}

type Report struct {
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
	Totals  Totals    `json:"totals"`
	Daily   []Day     `json:"daily"`
	Repos   []RepoUse `json:"repos"`
	Recent  []Call    `json:"recent"`
	Compare Compare   `json:"compare"`
}

type graphInfo struct {
	ok    bool
	built time.Time
}

func build(sessions map[string]*session, from, to time.Time, opts Options) Report {
	rep := Report{From: from, To: to, Daily: days(from, to)}
	dayAt := map[string]*Day{}
	for i := range rep.Daily {
		dayAt[rep.Daily[i].Date] = &rep.Daily[i]
	}
	graphs := map[string]graphInfo{}
	repos := map[string]*RepoUse{}
	var recent []Call
	var usingExplore, otherExplore int

	for _, sess := range sessions {
		name, root := opts.Resolve(sess.id)
		g, seen := graphs[root]
		if !seen {
			g = graphAt(root)
			graphs[root] = g
		}
		ru := repos[name]
		if ru == nil {
			ru = &RepoUse{Repo: name, Path: root, HasGraph: g.ok, GraphBuilt: g.built}
			repos[name] = ru
		}
		ru.Sessions++
		rep.Totals.Sessions++
		if g.ok {
			rep.Totals.GraphSessions++
		}

		lookups := 0
		for _, rc := range sess.calls {
			if rc.At.Before(from) || rc.At.After(to) {
				continue
			}
			c := toCall(rc, sess.id, name, root)
			rep.Totals.add(c)
			ru.add(c, rc)
			d := dayAt[c.At.In(time.Local).Format(time.DateOnly)]
			switch {
			case c.Kind.Lookup():
				lookups++
				if d != nil {
					d.Lookups++
				}
			case c.Kind == Update:
				if d != nil {
					d.Updates++
				}
			}
			if c.Kind != Other {
				recent = append(recent, c)
			}
		}
		if lookups > 0 {
			ru.SessionsUsing++
			rep.Totals.SessionsUsing++
		}
		if g.ok {
			if lookups > 0 {
				rep.Compare.UsingSessions++
				usingExplore += sess.explore
			} else {
				rep.Compare.OtherSessions++
				otherExplore += sess.explore
			}
		}
	}

	rep.Compare.UsingExplore = mean(usingExplore, rep.Compare.UsingSessions)
	rep.Compare.OtherExplore = mean(otherExplore, rep.Compare.OtherSessions)
	rep.Repos = rankRepos(repos, opts)
	sort.Slice(recent, func(i, j int) bool { return recent[i].At.After(recent[j].At) })
	rep.Recent = recent[:min(len(recent), recentLimit)]
	return rep
}

func (t *Totals) add(c Call) {
	switch c.Kind {
	case Query:
		t.Queries++
	case Path:
		t.Paths++
	case Explain:
		t.Explains++
	case Update:
		t.Updates++
	case Skill:
		t.Skills++
	case Other:
		t.Others++
	}
	if c.Kind.Lookup() {
		t.Lookups++
		t.ResultTokens += c.ResultTokens
		t.CitedTokens += c.CitedTokens
		if c.Truncated {
			t.Truncated++
		}
		if c.Failed {
			t.Failed++
		}
	}
}

func (r *RepoUse) add(c Call, rc rawCall) {
	if c.At.After(r.Last) {
		r.Last = c.At
	}
	if rc.GraphNodes > 0 {
		r.GraphNodes = rc.GraphNodes
	}
	switch {
	case c.Kind.Lookup():
		r.Lookups++
		r.ResultTokens += c.ResultTokens
		r.CitedTokens += c.CitedTokens
		if c.Truncated {
			r.Truncated++
		}
	case c.Kind == Update:
		r.Updates++
	}
}

func toCall(rc rawCall, session, repo, root string) Call {
	c := Call{
		At: rc.At, Session: session, Repo: repo, Kind: rc.Kind, Text: rc.Text,
		ResultTokens: int64(rc.Chars / charsPerTk), Nodes: rc.Nodes, Shown: rc.Shown,
		Truncated: rc.Truncated, Failed: rc.Failed,
	}
	if root != "" && rc.Kind.Lookup() {
		var bytes int64
		for _, src := range rc.Sources {
			if info, err := os.Stat(filepath.Join(root, src)); err == nil && info.Mode().IsRegular() {
				bytes += min(info.Size(), citedCap)
			}
		}
		c.CitedTokens = bytes / charsPerTk
	}
	return c
}

func graphAt(root string) graphInfo {
	if root == "" {
		return graphInfo{}
	}
	info, err := os.Stat(filepath.Join(root, "graphify-out", "graph.json"))
	if err != nil {
		return graphInfo{}
	}
	return graphInfo{ok: true, built: info.ModTime()}
}

func rankRepos(repos map[string]*RepoUse, opts Options) []RepoUse {
	out := make([]RepoUse, 0, len(repos))
	for _, r := range repos {
		if r.HasGraph {
			r.CommitsSince = opts.CommitsSince(r.Path, r.GraphBuilt)
		}
		if r.HasGraph || r.Lookups+r.Updates > 0 {
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Lookups != out[j].Lookups {
			return out[i].Lookups > out[j].Lookups
		}
		return out[i].Repo < out[j].Repo
	})
	return out
}

func days(from, to time.Time) []Day {
	var out []Day
	d := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.Local)
	for ; !d.After(to) && len(out) < 400; d = d.AddDate(0, 0, 1) {
		out = append(out, Day{Date: d.Format(time.DateOnly)})
	}
	return out
}

func mean(sum, n int) float64 {
	if n == 0 {
		return 0
	}
	return float64(sum) / float64(n)
}
