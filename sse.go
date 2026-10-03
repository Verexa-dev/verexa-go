package verexa

import (
	"bytes"
	"io"
	"sort"
	"strings"
)

// guardedStream passes an SSE body through byte for byte while accumulating
// the reply on the side, then runs the output check when the stream signals
// its end. Clients such as openai-go stop reading at the terminal event and
// never reach EOF, so the check runs there and, on a block, withholds that
// event and returns the error in its place.
type guardedStream struct {
	body     io.ReadCloser
	event    func(data []byte) streamEvent
	finish   func(texts []string) *BlockedError
	line     []byte
	replies  map[int]*strings.Builder
	finished bool
	err      error
}

type streamEvent struct {
	index int
	text  string
	done  bool
}

func (s *guardedStream) Read(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	if s.finished {
		return s.body.Read(p)
	}
	n, err := s.body.Read(p)
	cut, done := s.scan(p[:n])
	if !done && err == io.EOF {
		s.scanLine(s.line)
	}
	if done || err == io.EOF {
		s.finished = true
		if blocked := s.finish(s.texts()); blocked != nil {
			s.err = blocked
			if done {
				n = cut
			}
			if n > 0 {
				return n, nil
			}
			return 0, blocked
		}
	}
	return n, err
}

func (s *guardedStream) Close() error {
	return s.body.Close()
}

// scan feeds complete lines to scanLine and reports where in chunk the
// terminal line starts, if one was seen.
func (s *guardedStream) scan(chunk []byte) (int, bool) {
	start := 0
	for start < len(chunk) {
		i := bytes.IndexByte(chunk[start:], '\n')
		if i < 0 {
			s.line = append(s.line, chunk[start:]...)
			return 0, false
		}
		done := s.scanLine(append(s.line, chunk[start:start+i]...))
		s.line = s.line[:0]
		if done {
			return start, true
		}
		start += i + 1
	}
	return 0, false
}

func (s *guardedStream) scanLine(line []byte) bool {
	line = bytes.TrimRight(line, "\r")
	data, ok := bytes.CutPrefix(line, []byte("data:"))
	if !ok {
		return false
	}
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("[DONE]")) {
		return true
	}
	if len(data) == 0 {
		return false
	}
	ev := s.event(data)
	if ev.text != "" {
		if s.replies == nil {
			s.replies = map[int]*strings.Builder{}
		}
		b, ok := s.replies[ev.index]
		if !ok {
			b = &strings.Builder{}
			s.replies[ev.index] = b
		}
		b.WriteString(ev.text)
	}
	return ev.done
}

func (s *guardedStream) texts() []string {
	indexes := make([]int, 0, len(s.replies))
	for i := range s.replies {
		indexes = append(indexes, i)
	}
	sort.Ints(indexes)
	texts := make([]string, len(indexes))
	for k, i := range indexes {
		texts[k] = s.replies[i].String()
	}
	return texts
}
