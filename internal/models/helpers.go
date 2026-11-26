// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"database/sql"
	"encoding/json"
	"strings"
)

func BoolToSQLite(v bool) int {
	if v {
		return 1
	}
	return 0
}

func SQLiteIntToBool(v int) bool {
	return v != 0
}

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

func SanitizeCommaSeparatedStringSlice(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(values))
	var result []string
	for _, value := range values {
		for part := range strings.SplitSeq(value, ",") {
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
