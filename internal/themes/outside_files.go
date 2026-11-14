// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package themes

import (
	"fmt"
	"strings"
)

// OutsideFileError names the first construct that can make a browser load a
// file from another host, and the line it starts on.
type OutsideFileError struct {
	Line   int
	Reason string
}

func (e *OutsideFileError) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.Reason)
}

// CheckNoOutsideFiles refuses CSS that can load a file from another host:
// @import, image-set(), a url() whose target is not a data: address or a
// #fragment, and a backslash outside a quoted string (an escape can spell
// url or @import). Comments are ignored. When the scan cannot decide, for
// example on a line break inside a string, it refuses. It returns an
// *OutsideFileError, or nil.
func CheckNoOutsideFiles(css string) error {
	s := outsideFileScanner{css: css, line: 1}
	return s.scan()
}

type outsideFileScanner struct {
	css  string
	pos  int
	line int
}

func (s *outsideFileScanner) refuse(line int, reason string) error {
	return &OutsideFileError{Line: line, Reason: reason}
}

func (s *outsideFileScanner) advance() byte {
	c := s.css[s.pos]
	s.pos++
	if c == '\n' {
		s.line++
	}
	return c
}

func (s *outsideFileScanner) hasPrefixFold(prefix string) bool {
	return len(s.css)-s.pos >= len(prefix) && strings.EqualFold(s.css[s.pos:s.pos+len(prefix)], prefix)
}

func (s *outsideFileScanner) scan() error {
	for s.pos < len(s.css) {
		switch c := s.css[s.pos]; {
		case c == '/' && s.hasPrefixFold("/*"):
			s.skipComment()
		case c == '"' || c == '\'':
			if _, err := s.readString(); err != nil {
				return err
			}
		case c == '\\':
			return s.refuse(s.line, "a backslash escape outside a quoted string is not allowed")
		case c == '@' && s.hasPrefixFold("@import"):
			return s.refuse(s.line, "@import loads a file from another website")
		case s.hasPrefixFold("image-set("):
			return s.refuse(s.line, "image-set() can load a file from another website; use url() with a data: address")
		case s.hasPrefixFold("url("):
			if err := s.checkURL(); err != nil {
				return err
			}
		default:
			s.advance()
		}
	}
	return nil
}

func (s *outsideFileScanner) skipComment() {
	s.pos += len("/*")
	for s.pos < len(s.css) && !s.hasPrefixFold("*/") {
		s.advance()
	}
	s.pos = min(s.pos+len("*/"), len(s.css))
}

// readString consumes a quoted string, escapes included, and returns its
// contents. A line break ends a CSS string early, so the rest of the line
// would parse as CSS that this scan did not see.
func (s *outsideFileScanner) readString() (string, error) {
	start := s.line
	quote := s.advance()
	var out strings.Builder
	for s.pos < len(s.css) {
		c := s.advance()
		switch c {
		case quote:
			return out.String(), nil
		case '\n', '\r', '\f':
			return "", s.refuse(start, "a quoted string has a line break")
		case '\\':
			if s.pos < len(s.css) {
				out.WriteByte(c)
				out.WriteByte(s.advance())
			}
		default:
			out.WriteByte(c)
		}
	}
	return "", s.refuse(start, "a quoted string is not closed")
}

func (s *outsideFileScanner) checkURL() error {
	line := s.line
	s.pos += len("url(")
	for s.pos < len(s.css) && isCSSSpace(s.css[s.pos]) {
		s.advance()
	}

	var target string
	if s.pos < len(s.css) && (s.css[s.pos] == '"' || s.css[s.pos] == '\'') {
		str, err := s.readString()
		if err != nil {
			return err
		}
		target = str
	} else {
		end := strings.IndexByte(s.css[s.pos:], ')')
		if end < 0 {
			return s.refuse(line, "url() is not closed")
		}
		target = s.css[s.pos : s.pos+end]
		if strings.ContainsAny(target, "\\\"'(") {
			return s.refuse(line, "url() has a character that this check cannot read")
		}
		for range end {
			s.advance()
		}
	}

	lower := strings.ToLower(strings.TrimSpace(target))
	if strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "#") {
		return nil
	}
	return s.refuse(line, "url() can only use a data: address or a #fragment")
}

func isCSSSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}
