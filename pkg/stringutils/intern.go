// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package stringutils provides string utility functions including interning
// via Go 1.23's unique package for memory-efficient deduplication of commonly
// used strings like tracker domains, category names, and torrent hashes.
package stringutils

import (
	"strings"
	"unique"
)

// Intern returns a canonical representation of the string using Go's unique package.
// Identical strings will share the same underlying memory, reducing allocations
// and enabling fast equality comparisons.
//
// Use this for strings that are frequently repeated across the application:
//   - Tracker domain names
//   - Category names
//   - Tag names
//   - Error types and messages
//   - Status strings
//
// The returned string is semantically identical to the input.
func Intern(s string) string {
	if s == "" {
		return ""
	}
	return unique.Make(s).Value()
}

// InternLower interns a lowercase version of the string.
// Combines strings.ToLower with interning for consistent case-insensitive storage.
func InternLower(s string) string {
	if s == "" {
		return ""
	}
	return unique.Make(strings.ToLower(s)).Value()
}

// InternTrimmed interns a trimmed version of the string.
// Combines strings.TrimSpace with interning.
func InternTrimmed(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	return unique.Make(trimmed).Value()
}

// InternNormalized interns a trimmed and lowercased version of the string.
// This is the canonical form for case-insensitive string matching.
func InternNormalized(s string) string {
	normalized := strings.ToLower(strings.TrimSpace(s))
	if normalized == "" {
		return ""
	}
	return unique.Make(normalized).Value()
}

// InternNormalizedUpper interns a trimmed and uppercased version of the string.
// This is useful for case-insensitive matching where uppercase is preferred.
func InternNormalizedUpper(s string) string {
	normalized := strings.ToUpper(strings.TrimSpace(s))
	if normalized == "" {
		return ""
	}
	return unique.Make(normalized).Value()
}

// Handle wraps unique.Handle for type-safe interned string references.
// This provides a lighter-weight comparison mechanism when you need
// to frequently compare strings for equality.
type Handle = unique.Handle[string]

// MakeHandle creates a Handle for the given string.
// Handles can be compared with == for fast equality checks.
func MakeHandle(s string) Handle {
	return unique.Make(s)
}

// InternAll interns all strings in a slice, returning a new slice with interned values.
// Empty strings are preserved as empty strings (not interned).
func InternAll(strings []string) []string {
	if len(strings) == 0 {
		return strings
	}
	result := make([]string, len(strings))
	for i, s := range strings {
		result[i] = Intern(s)
	}
	return result
}

// InternAllNormalized interns all strings in a slice after normalizing (trim + lowercase).
// Empty strings after normalization are preserved as empty strings.
func InternAllNormalized(strings []string) []string {
	if len(strings) == 0 {
		return strings
	}
	result := make([]string, len(strings))
	for i, s := range strings {
		result[i] = InternNormalized(s)
	}
	return result
}

// InternMap interns all string values in a map, returning a new map.
// Keys are not interned; only values are interned.
// For maps with string keys, use InternStringMap to intern both keys and values.
func InternMap[K comparable](m map[K]string) map[K]string {
	if len(m) == 0 {
		return m
	}
	result := make(map[K]string, len(m))
	for k, v := range m {
		result[k] = Intern(v)
	}
	return result
}

// InternStringMap interns both keys and values of a string-to-string map.
// This is useful for attribute maps, headers, and other string-keyed data
// where both keys and values may be repeated across many instances.
func InternStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return m
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		result[Intern(k)] = Intern(v)
	}
	return result
}
