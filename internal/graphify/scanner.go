package graphify

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Options struct {
	Resolve      func(session string) (name, root string)
	CommitsSince func(root string, since time.Time) int
}

type Scanner struct {
	dir  string
	opts Options
	mu   sync.Mutex
	seen map[string]*fileState
}

func NewScanner(projectsDir string, opts Options) *Scanner {
	return &Scanner{dir: projectsDir, opts: opts, seen: map[string]*fileState{}}
}

func (s *Scanner) Report(from, to time.Time) (Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sessions, err := s.scan(from)
	if err != nil {
		return Report{}, err
	}
	return build(sessions, from, to, s.opts), nil
}

type session struct {
	id      string
	explore int
	calls   []rawCall
}

func (s *Scanner) scan(from time.Time) (map[string]*session, error) {
	out := map[string]*session{}
	live := map[string]bool{}
	ids := map[string]bool{}
	err := filepath.WalkDir(s.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(from) {
			return nil
		}
		st := s.refresh(path, info)
		live[path] = true
		id := sessionID(path)
		sess := out[id]
		if sess == nil {
			sess = &session{id: id}
			out[id] = sess
		}
		sess.explore += st.explore
		for _, c := range st.calls {
			if ids[c.id] {
				continue
			}
			ids[c.id] = true
			sess.calls = append(sess.calls, c)
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for p := range s.seen {
		if !live[p] {
			delete(s.seen, p)
		}
	}
	return out, nil
}

func (s *Scanner) refresh(path string, info fs.FileInfo) *fileState {
	st := s.seen[path]
	if st != nil && st.size == info.Size() && st.mtime.Equal(info.ModTime()) {
		return st
	}
	if st == nil || info.Size() < st.offset {
		st = newFileState()
	}
	s.seen[path] = st
	st.size, st.mtime = info.Size(), info.ModTime()
	readFrom(path, st)
	return st
}

func readFrom(path string, st *fileState) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(st.offset, io.SeekStart); err != nil {
		return
	}
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		st.feed(line)
		st.offset += int64(len(line))
	}
}

func sessionID(path string) string {
	dir := filepath.Dir(path)
	if filepath.Base(dir) == "subagents" {
		return filepath.Base(filepath.Dir(dir))
	}
	return strings.TrimSuffix(filepath.Base(path), ".jsonl")
}
