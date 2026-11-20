// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package gocomments

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"os/exec"
	"regexp"
	"slices"
	"strings"
)

var directive = regexp.MustCompile(`^//(go:|nolint\b| nolint:|exhaustive:| ?#nosec\b| Deprecated: | Copyright \(c\) | SPDX-License-Identifier: | regression: #\d+)`)

var ErrCodeChanged = errors.New("the code tokens changed")

type Block struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	End      int    `json:"end"`
	Trailing bool   `json:"trailing"`
	Text     string `json:"text"`

	comments []comment
}

type comment struct {
	start, end    int
	line, endLine int
	text          string
}

type span struct {
	start, end int
	wholeLines bool
}

func Tracked() ([]string, error) {
	out, err := exec.CommandContext(context.Background(), "git", "ls-files", "-z", "--", "*.go").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	return strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00"), nil
}

func Count(path string, src []byte) (int, error) {
	comments, err := banned(path, src)
	return len(comments), err
}

func Blocks(path string, src []byte) ([]Block, error) {
	comments, err := banned(path, src)
	if err != nil {
		return nil, err
	}
	var blocks []Block
	for _, c := range comments {
		alone := cut(src, c).wholeLines
		if n := len(blocks); n > 0 && alone && !blocks[n-1].Trailing && blocks[n-1].End+1 == c.line {
			blocks[n-1].End = c.endLine
			blocks[n-1].Text += "\n" + c.text
			blocks[n-1].comments = append(blocks[n-1].comments, c)
			continue
		}
		blocks = append(blocks, Block{File: path, Line: c.line, End: c.endLine, Trailing: !alone, Text: c.text, comments: []comment{c}})
	}
	return blocks, nil
}

func Strip(path string, src []byte, doomed []Block) (out []byte, missed []Block, err error) {
	blocks, err := Blocks(path, src)
	if err != nil {
		return nil, nil, err
	}
	var spans []span
	for _, d := range doomed {
		i := slices.IndexFunc(blocks, func(b Block) bool { return b.Line == d.Line && b.Text == d.Text })
		if i < 0 {
			missed = append(missed, d)
			continue
		}
		for _, c := range blocks[i].comments {
			spans = append(spans, cut(src, c))
		}
		blocks = slices.Delete(blocks, i, i+1)
	}
	if len(spans) == 0 {
		return src, missed, nil
	}
	slices.SortFunc(spans, func(a, b span) int { return a.start - b.start })
	out, err = format.Source(remove(src, spans))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	if !SameCode(src, out) {
		return nil, nil, fmt.Errorf("%s: %w", path, ErrCodeChanged)
	}
	return out, missed, nil
}

func SameCode(a, b []byte) bool {
	return slices.Equal(tokens(a), tokens(b))
}

func banned(path string, src []byte) ([]comment, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	if ast.IsGenerated(f) {
		return nil, nil
	}
	var comments []comment
	for _, group := range f.Comments {
		for _, c := range group.List {
			if directive.MatchString(c.Text) {
				continue
			}
			start, end := fset.Position(c.Pos()), fset.Position(c.End())
			comments = append(comments, comment{start: start.Offset, end: end.Offset, line: start.Line, endLine: end.Line, text: c.Text})
		}
	}
	return comments, nil
}

func cut(src []byte, c comment) span {
	lineStart := bytes.LastIndexByte(src[:c.start], '\n') + 1
	lineEnd := len(src)
	if i := bytes.IndexByte(src[c.end:], '\n'); i >= 0 {
		lineEnd = c.end + i + 1
	}
	before, after := src[lineStart:c.start], src[c.end:lineEnd]
	switch {
	case len(bytes.TrimSpace(before)) == 0 && len(bytes.TrimSpace(after)) == 0:
		return span{start: lineStart, end: lineEnd, wholeLines: true}
	case len(bytes.TrimSpace(after)) == 0:
		return span{start: lineStart + len(bytes.TrimRight(before, " \t")), end: c.end}
	default:
		return span{start: c.start, end: c.end + len(after) - len(bytes.TrimLeft(after, " \t"))}
	}
}

func remove(src []byte, spans []span) []byte {
	var out bytes.Buffer
	pos := 0
	for _, s := range spans {
		out.Write(src[pos:max(pos, s.start)])
		pos = max(pos, s.end)
		if s.wholeLines {
			pos = closeGap(&out, src, pos)
		}
	}
	out.Write(src[pos:])
	return out.Bytes()
}

func closeGap(out *bytes.Buffer, src []byte, pos int) int {
	prev := lastLine(out.Bytes())
	next, nextEnd := lineAt(src, pos)
	after, _ := lineAt(src, nextEnd)
	switch {
	case next == "" && (prev == "" || opensBlock(prev) || closesBlock(after)):
		return nextEnd
	case prev == "" && closesBlock(next):
		kept := out.Bytes()
		out.Truncate(bytes.LastIndexByte(kept[:len(kept)-1], '\n') + 1)
	}
	return pos
}

func lastLine(out []byte) string {
	if len(out) == 0 {
		return "{"
	}
	out = out[:len(out)-1]
	return strings.TrimSpace(string(out[bytes.LastIndexByte(out, '\n')+1:]))
}

func lineAt(src []byte, pos int) (line string, end int) {
	if pos >= len(src) {
		return "}", pos
	}
	if i := bytes.IndexByte(src[pos:], '\n'); i >= 0 {
		return strings.TrimSpace(string(src[pos : pos+i])), pos + i + 1
	}
	return strings.TrimSpace(string(src[pos:])), len(src)
}

func opensBlock(line string) bool {
	return line != "" && strings.ContainsRune("{([:", rune(line[len(line)-1]))
}

func closesBlock(line string) bool {
	return line != "" && strings.ContainsRune("})]", rune(line[0]))
}

func tokens(src []byte) []string {
	var s scanner.Scanner
	fset := token.NewFileSet()
	s.Init(fset.AddFile("", fset.Base(), len(src)), src, nil, 0)
	var out []string
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			return out
		}
		if tok == token.SEMICOLON {
			lit = ""
		}
		out = append(out, tok.String(), lit)
	}
}
