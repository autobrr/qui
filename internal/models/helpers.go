// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"database/sql"
	"encoding/json"
	"strings"
)

// BoolToSQLite converts a bool to SQLite integer representation (0 or 1).
func BoolToSQLite(v bool) int {
	if v {
		return 1
	}
	return 0
}

// SanitizeStringSlice trims whitespace, removes empty strings, and deduplicates case-insensitively.
// Original casing is preserved for the first occurrence of each unique value.
func SanitizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(values))
	var result []string
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		lower := normalizeLowerTrim(trimmed)
		if _, exists := seen[lower]; exists {
			continue
		}
		seen[lower] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

// SanitizeCommaSeparatedStringSlice splits values on comma, trims whitespace, removes empty strings,
// lowercases entries, and deduplicates case-insensitively.
func SanitizeCommaSeparatedStringSlice(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(values))
	var result []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			lower := normalizeLowerTrim(trimmed)
			if _, exists := seen[lower]; exists {
				continue
			}
			seen[lower] = struct{}{}
			result = append(result, lower)
		}
	}
	return result
}

// EncodeStringSliceJSON marshals a string slice to JSON. Returns "[]" for empty/nil slices.
func EncodeStringSliceJSON(values []string) (string, error) {
	if len(values) == 0 {
		return "[]", nil
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

// DecodeStringSliceJSON unmarshals a JSON string to a sanitized string slice.
// Returns an empty slice for NULL or empty database values.
func DecodeStringSliceJSON(raw sql.NullString) ([]string, error) {
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return []string{}, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw.String), &values); err != nil {
		return nil, err
	}
	return SanitizeStringSlice(values), nil
}
