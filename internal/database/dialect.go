// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
)

func (d Dialect) String() string {
	return string(d)
}

func parseDialect(raw string) (Dialect, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case "", string(DialectSQLite):
		return DialectSQLite, nil
	case string(DialectPostgres), "postgresql":
		return DialectPostgres, nil
	default:
		return "", fmt.Errorf("unsupported database engine %q", raw)
	}
}

func (db *DB) Dialect() string {
	if db == nil {
		return string(DialectSQLite)
	}
	if db.dialect == "" {
		return string(DialectSQLite)
	}
	return db.dialect.String()
}

func (t *Tx) Dialect() string {
	if t == nil || t.db == nil {
		return string(DialectSQLite)
	}
	return t.db.Dialect()
}

func (t *Tx) DeferForeignKeyChecks(ctx context.Context) error {
	if t == nil || t.db == nil || t.db.dialect != DialectSQLite {
		return nil
	}
	_, err := t.ExecContext(ctx, "PRAGMA defer_foreign_keys = ON;")
	return err
}

func (db *DB) bindQuery(query string) string {
	if db == nil || db.dialect != DialectPostgres {
		return query
	}
	return rebindQuestionToDollar(query)
}

func rebindQuestionToDollar(query string) string {
	if query == "" || !strings.Contains(query, "?") {
		return query
	}

	var (
		out   strings.Builder
		param int
	)
	out.Grow(len(query) + 16)

	for i := 0; i < len(query); {
		if end := skipQuotedOrComment(query, i); end > i {
			out.WriteString(query[i:end])
			i = end
			continue
		}
		if query[i] == '?' {
			param++
			out.WriteByte('$')
			out.WriteString(strconv.Itoa(param))
		} else {
			out.WriteByte(query[i])
		}
		i++
	}

	return out.String()
}

func skipQuotedOrComment(query string, i int) int {
	rest := query[i:]
	switch {
	case rest[0] == '\'', rest[0] == '"':
		return quotedEnd(query, i+1, rest[0])
	case strings.HasPrefix(rest, "--"):
		return indexEnd(query, i+2, "\n")
	case strings.HasPrefix(rest, "/*"):
		return indexEnd(query, i+2, "*/")
	case rest[0] == '$':
		if tag := parseDollarQuoteTag(rest); tag != "" {
			return indexEnd(query, i+len(tag), tag)
		}
	}
	return i
}

func quotedEnd(query string, start int, quote byte) int {
	for i := start; i < len(query); i++ {
		if query[i] != quote {
			continue
		}
		// A doubled quote is an escaped quote in SQL.
		if i+1 < len(query) && query[i+1] == quote {
			i++
			continue
		}
		return i + 1
	}
	return len(query)
}

func indexEnd(query string, start int, terminator string) int {
	idx := strings.Index(query[start:], terminator)
	if idx < 0 {
		return len(query)
	}
	return start + idx + len(terminator)
}

func parseDollarQuoteTag(s string) string {
	if len(s) < 2 || s[0] != '$' {
		return ""
	}
	for i := 1; i < len(s); i++ {
		ch := s[i]
		if ch == '$' {
			return s[:i+1]
		}
		if (ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') ||
			ch == '_' {
			continue
		}
		return ""
	}
	return ""
}
