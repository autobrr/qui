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
		out            strings.Builder
		param          int
		inSingleQuote  bool
		inDoubleQuote  bool
		inLineComment  bool
		inBlockComment bool
		dollarQuoteTag string
	)
	out.Grow(len(query) + 16)

	for i := 0; i < len(query); i++ {
		ch := query[i]

		if dollarQuoteTag != "" {
			if strings.HasPrefix(query[i:], dollarQuoteTag) {
				out.WriteString(dollarQuoteTag)
				i += len(dollarQuoteTag) - 1
				dollarQuoteTag = ""
				continue
			}
			out.WriteByte(ch)
			continue
		}

		if inLineComment {
			out.WriteByte(ch)
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}

		if inBlockComment {
			out.WriteByte(ch)
			if ch == '*' && i+1 < len(query) && query[i+1] == '/' {
				out.WriteByte('/')
				i++
				inBlockComment = false
			}
			continue
		}

		if inSingleQuote {
			out.WriteByte(ch)
			if ch == '\'' {
				// Escaped single quote in SQL string literal.
				if i+1 < len(query) && query[i+1] == '\'' {
					out.WriteByte('\'')
					i++
				} else {
					inSingleQuote = false
				}
			}
			continue
		}

		if inDoubleQuote {
			out.WriteByte(ch)
			if ch == '"' {
				if i+1 < len(query) && query[i+1] == '"' {
					out.WriteByte('"')
					i++
				} else {
					inDoubleQuote = false
				}
			}
			continue
		}

		if ch == '\'' {
			inSingleQuote = true
			out.WriteByte(ch)
			continue
		}
		if ch == '"' {
			inDoubleQuote = true
			out.WriteByte(ch)
			continue
		}
		if ch == '-' && i+1 < len(query) && query[i+1] == '-' {
			inLineComment = true
			out.WriteString("--")
			i++
			continue
		}
		if ch == '/' && i+1 < len(query) && query[i+1] == '*' {
			inBlockComment = true
			out.WriteString("/*")
			i++
			continue
		}
		if ch == '$' {
			tag := parseDollarQuoteTag(query[i:])
			if tag != "" {
				dollarQuoteTag = tag
				out.WriteString(tag)
				i += len(tag) - 1
				continue
			}
		}
		if ch == '?' {
			param++
			out.WriteByte('$')
			out.WriteString(strconv.Itoa(param))
			continue
		}
		out.WriteByte(ch)
	}

	return out.String()
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
