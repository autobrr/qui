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

// OutsideFileError names the first construct that can make a browser load a
// file from another host, and the line it starts on.
type OutsideFileError struct {
	Line   int
	Reason string
}

func (e *OutsideFileError) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.Reason)
}

const cssSpace = " \t\n\r\f"

// CheckNoOutsideFiles refuses CSS that can load a file from another host:
// @import, image-set(), a url() whose target is not a data: address or a
// #fragment, and a backslash outside a quoted string (an escape can spell
// url or @import). Comments are ignored. A token that the browser would
// drop, such as a string with a line break, is refused too. It returns an
// *OutsideFileError, or nil.
func CheckNoOutsideFiles(content string) error {
	lexer := css.NewLexer(tdparse.NewInputString(content))
	line := 1
	for {
		tt, data := lexer.Next()
		if tt == css.ErrorToken {
			return nil
		}
		if reason := outsideFileReason(tt, data); reason != "" {
			return &OutsideFileError{Line: line, Reason: reason}
		}
		line += bytes.Count(data, []byte{'\n'})
	}
}

func outsideFileReason(tt css.TokenType, data []byte) string {
	switch tt {
	case css.CommentToken, css.StringToken:
		return ""
	case css.BadStringToken:
		return "a quoted string has a line break"
	case css.BadURLToken:
		return "url() has a character that this check cannot read"
	case css.URLToken:
		return urlTargetReason(data)
	default:
		return tokenReason(tt, data)
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

// urlTargetReason reads the lexer's url() token, which holds both the
// unquoted form and the quoted form. Escapes stay unresolved, so an escape
// in the data: prefix is refused.
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
