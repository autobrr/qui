// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBoolToSQLite(t *testing.T) {
	tests := []struct {
		name     string
		input    bool
		expected int
	}{
		{
			name:     "true returns 1",
			input:    true,
			expected: 1,
		},
		{
			name:     "false returns 0",
			input:    false,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BoolToSQLite(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeStringSlice(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "nil slice returns empty slice",
			input:    nil,
			expected: []string{},
		},
		{
			name:     "empty slice returns empty slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "trims whitespace",
			input:    []string{"  foo  ", "  bar  "},
			expected: []string{"foo", "bar"},
		},
		{
			name:     "removes empty strings",
			input:    []string{"foo", "", "bar", "   "},
			expected: []string{"foo", "bar"},
		},
		{
			name:     "deduplicates case-insensitively preserving first occurrence",
			input:    []string{"Foo", "foo", "FOO", "bar"},
			expected: []string{"Foo", "bar"},
		},
		{
			name:     "handles mixed case deduplication",
			input:    []string{"Movies", "movies", "TV", "tv", "TV"},
			expected: []string{"Movies", "TV"},
		},
		{
			name:     "preserves order of first occurrences",
			input:    []string{"c", "B", "a", "C", "b", "A"},
			expected: []string{"c", "B", "a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeStringSlice(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeCommaSeparatedStringSlice(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "nil slice returns empty slice",
			input:    nil,
			expected: []string{},
		},
		{
			name:     "empty slice returns empty slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "splits and trims",
			input:    []string{"  foo  , bar", "baz"},
			expected: []string{"foo", "bar", "baz"},
		},
		{
			name:     "drops empties and lowercases",
			input:    []string{"Foo,,  ,BAR", "bAz"},
			expected: []string{"foo", "bar", "baz"},
		},
		{
			name:     "deduplicates across inputs",
			input:    []string{"a,b", "B", "c", "A"},
			expected: []string{"a", "b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeCommaSeparatedStringSlice(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEncodeStringSliceJSON(t *testing.T) {
	tests := []struct {
		name        string
		input       []string
		expected    string
		expectError bool
	}{
		{
			name:     "nil slice returns empty array",
			input:    nil,
			expected: "[]",
		},
		{
			name:     "empty slice returns empty array",
			input:    []string{},
			expected: "[]",
		},
		{
			name:     "single item",
			input:    []string{"foo"},
			expected: `["foo"]`,
		},
		{
			name:     "multiple items",
			input:    []string{"foo", "bar", "baz"},
			expected: `["foo","bar","baz"]`,
		},
		{
			name:     "items with special characters",
			input:    []string{"foo bar", "hello\nworld"},
			expected: `["foo bar","hello\nworld"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := EncodeStringSliceJSON(tt.input)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDecodeStringSliceJSON(t *testing.T) {
	tests := []struct {
		name        string
		input       sql.NullString
		expected    []string
		expectError bool
	}{
		{
			name:     "invalid null string returns empty slice",
			input:    sql.NullString{Valid: false},
			expected: []string{},
		},
		{
			name:     "empty string returns empty slice",
			input:    sql.NullString{Valid: true, String: ""},
			expected: []string{},
		},
		{
			name:     "whitespace only returns empty slice",
			input:    sql.NullString{Valid: true, String: "   "},
			expected: []string{},
		},
		{
			name:     "empty JSON array",
			input:    sql.NullString{Valid: true, String: "[]"},
			expected: []string{},
		},
		{
			name:     "single item",
			input:    sql.NullString{Valid: true, String: `["foo"]`},
			expected: []string{"foo"},
		},
		{
			name:     "multiple items",
			input:    sql.NullString{Valid: true, String: `["foo","bar","baz"]`},
			expected: []string{"foo", "bar", "baz"},
		},
		{
			name:     "sanitizes decoded values",
			input:    sql.NullString{Valid: true, String: `["  foo  ", "bar", "Foo"]`},
			expected: []string{"foo", "bar"},
		},
		{
			name:        "invalid JSON returns error",
			input:       sql.NullString{Valid: true, String: "not json"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := DecodeStringSliceJSON(tt.input)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
