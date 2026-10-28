package graphify

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Kind string

const (
	Query   Kind = "query"
	Path    Kind = "path"
	Explain Kind = "explain"
	Update  Kind = "update"
	Skill   Kind = "skill"
	Other   Kind = "other"
)

func (k Kind) Lookup() bool { return k == Query || k == Path || k == Explain }

const (
	maxText    = 160
	charsPerTk = 4
)

var (
	exploreTools = [][]byte{[]byte(`"name":"Read"`), []byte(`"name":"Grep"`), []byte(`"name":"Glob"`)}
	graphifyWord = []byte("graphify")
	toolUseKey   = []byte(`"tool_use"`)
	toolResKey   = []byte(`"tool_result"`)

	cliCall     = regexp.MustCompile(`(?:^|[;&|(\n])[ \t]*(?:[\w./~-]*/)?graphify[ \t]+([a-z][\w-]*)([^|;&\n]*)`)
	quotes      = strings.NewReplacer(`"`, "", "'", "")
	redirect    = regexp.MustCompile(`\s+\d*>.*$`)
	nodesFound  = regexp.MustCompile(`(\d+) nodes found`)
	showing     = regexp.MustCompile(`showing (\d+) of (\d+) nodes`)
	graphNodes  = regexp.MustCompile(`\(([\d,]+) nodes\)`)
	sourceField = regexp.MustCompile(`src=([^\s\]]+)`)
)

type rawCall struct {
	At         time.Time `json:"at"`
	Kind       Kind      `json:"kind"`
	Text       string    `json:"text"`
	Chars      int       `json:"chars"`
	Nodes      int       `json:"nodes"`
	Shown      int       `json:"shown"`
	GraphNodes int       `json:"graph_nodes"`
	Truncated  bool      `json:"truncated"`
	Failed     bool      `json:"failed"`
	Sources    []string  `json:"sources"`
	id         string
}

type transcriptLine struct {
	Timestamp time.Time `json:"timestamp"`
	Message   struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type block struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

type fileState struct {
	size    int64
	mtime   time.Time
	offset  int64
	explore int
	calls   []rawCall
	pending map[string]int
}

func newFileState() *fileState { return &fileState{pending: map[string]int{}} }

func (f *fileState) feed(line []byte) {
	if bytes.Contains(line, toolUseKey) {
		for _, name := range exploreTools {
			f.explore += bytes.Count(line, name)
		}
	}
	switch {
	case bytes.Contains(line, graphifyWord) && bytes.Contains(line, toolUseKey):
		f.feedUses(line)
	case len(f.pending) > 0 && bytes.Contains(line, toolResKey):
		f.feedResults(line)
	}
}

func (f *fileState) feedUses(line []byte) {
	blocks, at := decode(line)
	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		call, ok := classify(b)
		if !ok {
			continue
		}
		call.At = at
		call.id = b.ID
		f.calls = append(f.calls, call)
		f.pending[b.ID] = len(f.calls) - 1
	}
}

func (f *fileState) feedResults(line []byte) {
	blocks, _ := decode(line)
	for _, b := range blocks {
		if b.Type != "tool_result" {
			continue
		}
		i, ok := f.pending[b.ToolUseID]
		if !ok {
			continue
		}
		delete(f.pending, b.ToolUseID)
		applyResult(&f.calls[i], resultText(b.Content), b.IsError)
	}
}

func decode(line []byte) ([]block, time.Time) {
	var l transcriptLine
	if json.Unmarshal(line, &l) != nil {
		return nil, time.Time{}
	}
	var blocks []block
	if json.Unmarshal(l.Message.Content, &blocks) != nil {
		return nil, l.Timestamp
	}
	return blocks, l.Timestamp
}

func classify(b block) (rawCall, bool) {
	switch {
	case b.Name == "Bash":
		var in struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(b.Input, &in) != nil {
			return rawCall{}, false
		}
		return classifyCommand(in.Command)
	case b.Name == "Skill":
		var in struct {
			Skill string `json:"skill"`
		}
		if json.Unmarshal(b.Input, &in) != nil || in.Skill != "graphify" {
			return rawCall{}, false
		}
		return rawCall{Kind: Skill, Text: "/graphify"}, true
	case strings.HasPrefix(b.Name, "mcp__graphify"):
		return rawCall{Kind: mcpKind(b.Name), Text: clip(firstString(b.Input))}, true
	}
	return rawCall{}, false
}

func classifyCommand(cmd string) (rawCall, bool) {
	m := cliCall.FindStringSubmatch(cmd)
	if m == nil {
		return rawCall{}, false
	}
	kind := Other
	switch m[1] {
	case "query":
		kind = Query
	case "path":
		kind = Path
	case "explain":
		kind = Explain
	case "update", "extract":
		kind = Update
	}
	text := strings.TrimSpace(redirect.ReplaceAllString(m[2], ""))
	return rawCall{Kind: kind, Text: clip(quotes.Replace(text))}, true
}

func mcpKind(name string) Kind {
	switch {
	case strings.Contains(name, "path"):
		return Path
	case strings.Contains(name, "node"), strings.Contains(name, "explain"):
		return Explain
	}
	return Query
}

func firstString(raw json.RawMessage) string {
	var in map[string]any
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}
	for _, v := range in {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func clip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= maxText {
		return s
	}
	return string([]rune(s)[:maxText]) + "…"
}

func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var sb strings.Builder
	for _, p := range parts {
		sb.WriteString(p.Text)
	}
	return sb.String()
}

func applyResult(c *rawCall, text string, isError bool) {
	c.Chars = len(text)
	c.Failed = isError
	if !c.Kind.Lookup() {
		return
	}
	c.Truncated = strings.Contains(text, "TRUNCATED")
	if m := nodesFound.FindStringSubmatch(text); m != nil {
		c.Nodes, _ = strconv.Atoi(m[1])
	}
	if m := showing.FindStringSubmatch(text); m != nil {
		c.Shown, _ = strconv.Atoi(m[1])
		c.Nodes, _ = strconv.Atoi(m[2])
	}
	if m := graphNodes.FindStringSubmatch(text); m != nil {
		c.GraphNodes, _ = strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
	}
	seen := map[string]bool{}
	for _, m := range sourceField.FindAllStringSubmatch(text, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			c.Sources = append(c.Sources, m[1])
		}
	}
}
