// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package externalprograms

import (
	"sort"
	"strings"

	"github.com/autobrr/qui/internal/models"
)

func SplitArgs(s string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	for _, r := range s {
		switch {
		case r == '"' || r == '\'':
			switch {
			case !inQuote:
				inQuote = true
				quoteChar = r
			case r == quoteChar:
				inQuote = false
				quoteChar = 0
			default:
				current.WriteRune(r)
			}
		case r == ' ' && !inQuote:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args
}

func BuildArguments(template string, torrentData map[string]string) []string {
	if template == "" {
		return []string{}
	}

	args := SplitArgs(template)
	for i := range args {
		for key, value := range torrentData {
			placeholder := "{" + key + "}"
			args[i] = strings.ReplaceAll(args[i], placeholder, value)
		}
	}

	return args
}

func ApplyPathMappings(path string, mappings []models.PathMapping) string {
	if len(mappings) == 0 {
		return path
	}

	sortedMappings := make([]models.PathMapping, len(mappings))
	copy(sortedMappings, mappings)
	sort.Slice(sortedMappings, func(i, j int) bool {
		return len(sortedMappings[i].From) > len(sortedMappings[j].From)
	})

	for _, mapping := range sortedMappings {
		if mapping.From == "" || mapping.To == "" {
			continue
		}
		if strings.HasPrefix(path, mapping.From) {
			remainder := path[len(mapping.From):]
			fromEndsWithSep := strings.HasSuffix(mapping.From, "/") || strings.HasSuffix(mapping.From, "\\")
			if remainder == "" || fromEndsWithSep || remainder[0] == '/' || remainder[0] == '\\' {
				return mapping.To + remainder
			}
		}
	}

	return path
}
