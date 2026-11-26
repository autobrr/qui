// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package themes

import (
	"bytes"
	"fmt"
	"strings"

	tdparse "github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
)

type OutsideFileKind string

const (
	KindOutsideFile OutsideFileKind = "outside-file"
	KindUnreadable  OutsideFileKind = "unreadable"
)

type OutsideFileError struct {
	Line   int
	Kind   OutsideFileKind
	Reason string
}

func (e *OutsideFileError) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.Reason)
}

const cssSpace = " \t\n\r\f"

func CheckNoOutsideFiles(content string) error {
	lexer := css.NewLexer(tdparse.NewInputString(content))
	line := 1
	for {
		tt, data := lexer.Next()
		if tt == css.ErrorToken {
			return nil
		}
		if kind, reason := outsideFileReason(tt, data); reason != "" {
			return &OutsideFileError{Line: line, Kind: kind, Reason: reason}
		}
		line += bytes.Count(data, []byte{'\n'})
	}
}

func outsideFileReason(tt css.TokenType, data []byte) (OutsideFileKind, string) {
	switch tt {
	case css.CommentToken, css.StringToken:
		return "", ""
	case css.BadStringToken:
		return KindUnreadable, "a quoted string has a line break"
	case css.BadURLToken:
		return KindUnreadable, "url() has a character that this check cannot read"
	case css.URLToken:
		return KindOutsideFile, urlTargetReason(data)
	default:
		return KindOutsideFile, tokenReason(tt, data)
	}
}

func tokenReason(tt css.TokenType, data []byte) string {
	if bytes.IndexByte(data, '\\') >= 0 {
		return "a backslash escape outside a quoted string is not allowed"
	}
	name := strings.ToLower(string(data))
	switch {
	case tt == css.AtKeywordToken && name == "@import":
		return "@import loads a file from another website"
	case tt == css.FunctionToken && strings.HasSuffix(name, "image-set("):
		return "image-set() can load a file from another website; use url() with a data: address"
	}
	return ""
}

func urlTargetReason(data []byte) string {
	_, after, _ := bytes.Cut(data, []byte{'('})
	inner := strings.TrimSuffix(strings.Trim(string(after), cssSpace), ")")
	inner = strings.Trim(inner, cssSpace)
	if inner != "" && (inner[0] == '"' || inner[0] == '\'') {
		inner = strings.TrimSuffix(inner[1:], inner[:1])
	} else if strings.IndexByte(inner, '\\') >= 0 {
		return "a backslash escape outside a quoted string is not allowed"
	}
	target := strings.ToLower(strings.Trim(inner, cssSpace))
	if strings.HasPrefix(target, "data:") || strings.HasPrefix(target, "#") {
		return ""
	}
	return "url() can only use a data: address or a #fragment"
}
