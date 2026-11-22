// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package logbudget counts the log lines of a call, so a test can show that
// the lines at the default level do not grow with the size of the library.
// A test that uses it swaps the global logger, so it cannot run in parallel.
package logbudget

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Lines holds the number of log lines for each level and message.
type Lines map[Line]int

// Line is one log message at one level.
type Line struct {
	Level   string
	Message string
}

// Message returns the number of lines with msg at every level.
func (l Lines) Message(msg string) int {
	total := 0
	for line, n := range l {
		if line.Message == msg {
			total += n
		}
	}
	return total
}

// Count runs fn with the global logger at TRACE and returns the lines that fn
// wrote, also from goroutines that end before fn returns.
func Count(t testing.TB, fn func()) Lines {
	t.Helper()
	lines := make(Lines)
	for _, entry := range record(t, fn) {
		lines[Line{Level: entry.Level, Message: entry.Message}]++
	}
	return lines
}

type entry struct {
	Level   string `json:"level"`
	Message string `json:"message"`
	text    string
}

func record(t testing.TB, fn func()) []entry {
	t.Helper()
	previousLogger, previousLevel := log.Logger, zerolog.GlobalLevel()
	defer func() {
		log.Logger = previousLogger
		zerolog.SetGlobalLevel(previousLevel)
	}()

	var out lockedBuffer
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	log.Logger = zerolog.New(&out).Level(zerolog.TraceLevel)
	fn()

	out.mu.Lock()
	defer out.mu.Unlock()
	var entries []entry
	scanner := bufio.NewScanner(&out.buf)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := entry{text: scanner.Text()}
		// zerolog repeats a key when an event sets a field of its logger again.
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

// AssertFlat fails for each message at DEBUG or above that large writes more
// often than small. Run the same work on a small and a large library: a line
// that grows with the library belongs at TRACE. Make the small library bigger
// than each cap on DEBUG lines, or a capped line looks like growth. AssertFlat
// compares message text, so put a count in a field, not in the message.
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

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
