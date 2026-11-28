// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package logbudget

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Lines map[Line]int

type Line struct {
	Level   string
	Message string
}

func (l Lines) Message(msg string) int {
	total := 0
	for line, n := range l {
		if line.Message == msg {
			total += n
		}
	}
	return total
}

func Count(t testing.TB, fn func()) Lines {
	t.Helper()
	return CountWhere(t, func(string) bool { return true }, fn)
}

func CountWhere(t testing.TB, keep func(text string) bool, fn func()) Lines {
	t.Helper()
	lines := make(Lines)
	for _, entry := range record(t, fn) {
		if keep(entry.text) {
			lines[Line{Level: entry.Level, Message: entry.Message}]++
		}
	}
	return lines
}

type entry struct {
	Level   string `json:"level"`
	Message string `json:"message"`
	text    string
}

var output = &switchWriter{w: os.Stderr}

func init() {
	log.Logger = zerolog.New(output).With().Timestamp().Logger()
}

func record(t testing.TB, fn func()) []entry {
	t.Helper()
	var out bytes.Buffer
	previousLevel := zerolog.GlobalLevel()
	previous := output.swap(&out)
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	func() {
		defer func() {
			output.swap(previous)
			zerolog.SetGlobalLevel(previousLevel)
		}()
		fn()
	}()

	var entries []entry
	scanner := bufio.NewScanner(&out)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := entry{text: scanner.Text()}
		if err := json.Unmarshal(scanner.Bytes(), &line, jsontext.AllowDuplicateNames(true)); err != nil {
			t.Fatalf("logbudget: read log line %q: %v", scanner.Text(), err)
		}
		entries = append(entries, line)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("logbudget: read log: %v", err)
	}
	return entries
}

func AssertSilent(t testing.TB, fn func(), words ...string) {
	t.Helper()
	for _, line := range record(t, fn) {
		level, err := zerolog.ParseLevel(line.Level)
		if err != nil || level < zerolog.DebugLevel {
			continue
		}
		text := strings.ToLower(line.text)
		named := func(word string) bool { return strings.Contains(text, strings.ToLower(word)) }
		if len(words) == 0 || slices.ContainsFunc(words, named) {
			t.Errorf("logbudget: a line at %s that the path must not write: %s", line.Level, line.text)
		}
	}
}

func AssertFlat(t testing.TB, small, large Lines) {
	t.Helper()
	var grown []Line
	for line, n := range large {
		level, err := zerolog.ParseLevel(line.Level)
		if err != nil || level < zerolog.DebugLevel {
			continue
		}
		if n > small[line] {
			grown = append(grown, line)
		}
	}
	slices.SortFunc(grown, func(a, b Line) int { return cmp.Compare(a.Message, b.Message) })
	for _, line := range grown {
		t.Errorf("logbudget: %s %q grows with the library: %d lines on the small one, %d on the large one",
			line.Level, line.Message, small[line], large[line])
	}
}

type switchWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *switchWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

func (s *switchWriter) swap(w io.Writer) io.Writer {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.w
	s.w = w
	return previous
}
