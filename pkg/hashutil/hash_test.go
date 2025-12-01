// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package hashutil

import (
	"reflect"
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ABC123", "abc123"},
		{"  abc123  ", "abc123"},
		{"", ""},
		{"   ", ""},
		{"AbC123DeF", "abc123def"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := Normalize(tt.input)
			if result != tt.expected {
				t.Errorf("Normalize(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeUpper(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"abc123", "ABC123"},
		{"  ABC123  ", "ABC123"},
		{"", ""},
		{"   ", ""},
		{"AbC123DeF", "ABC123DEF"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := NormalizeUpper(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeUpper(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeAll(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty slice",
			input:    []string{},
			expected: nil,
		},
		{
			name:     "nil slice",
			input:    nil,
			expected: nil,
		},
		{
			name:     "single hash",
			input:    []string{"ABC123"},
			expected: []string{"abc123"},
		},
		{
			name:     "multiple unique hashes",
			input:    []string{"ABC", "DEF", "GHI"},
			expected: []string{"abc", "def", "ghi"},
		},
		{
			name:     "duplicate removal",
			input:    []string{"ABC", "abc", "ABC"},
			expected: []string{"abc"},
		},
		{
			name:     "empty strings removed",
			input:    []string{"ABC", "", "DEF", "   "},
			expected: []string{"abc", "def"},
		},
		{
			name:     "preserves order",
			input:    []string{"ZZZ", "AAA", "MMM"},
			expected: []string{"zzz", "aaa", "mmm"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeAll(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("NormalizeAll(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeAllUpper(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty slice",
			input:    []string{},
			expected: nil,
		},
		{
			name:     "single hash",
			input:    []string{"abc123"},
			expected: []string{"ABC123"},
		},
		{
			name:     "duplicate removal",
			input:    []string{"ABC", "abc", "ABC"},
			expected: []string{"ABC"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeAllUpper(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("NormalizeAllUpper(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNewNormalizedSet(t *testing.T) {
	hashes := []string{"ABC123", "def456", "ABC123", "  ghi789  "}
	set := NewNormalizedSet(hashes)

	// Check canonical list (lowercase, deduped, ordered)
	expectedCanonical := []string{"abc123", "def456", "ghi789"}
	if !reflect.DeepEqual(set.Canonical, expectedCanonical) {
		t.Errorf("Canonical = %v, want %v", set.Canonical, expectedCanonical)
	}

	// Check Contains
	if !set.Contains("ABC123") {
		t.Error("set.Contains(ABC123) should be true")
	}
	if !set.Contains("abc123") {
		t.Error("set.Contains(abc123) should be true")
	}
	if set.Contains("xyz999") {
		t.Error("set.Contains(xyz999) should be false")
	}

	// Check PreferredForm returns original form
	if pref := set.PreferredForm("abc123"); pref != "ABC123" {
		t.Errorf("PreferredForm(abc123) = %q, want %q", pref, "ABC123")
	}
	if pref := set.PreferredForm("ghi789"); pref != "ghi789" {
		t.Errorf("PreferredForm(ghi789) = %q, want %q (trimmed)", pref, "ghi789")
	}
}

func TestDifference(t *testing.T) {
	tests := []struct {
		name     string
		all      []string
		subset   []string
		expected []string
	}{
		{
			name:     "empty subset",
			all:      []string{"a", "b", "c"},
			subset:   []string{},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "full overlap",
			all:      []string{"a", "b", "c"},
			subset:   []string{"A", "B", "C"},
			expected: []string{},
		},
		{
			name:     "partial overlap",
			all:      []string{"a", "b", "c", "d"},
			subset:   []string{"B", "D"},
			expected: []string{"a", "c"},
		},
		{
			name:     "case insensitive",
			all:      []string{"ABC", "def"},
			subset:   []string{"abc", "DEF"},
			expected: []string{},
		},
		{
			name:     "preserves original form",
			all:      []string{"ABC123", "def456"},
			subset:   []string{"abc123"},
			expected: []string{"def456"},
		},
		{
			name:     "handles duplicates in subset",
			all:      []string{"a", "a", "b"},
			subset:   []string{"a"},
			expected: []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Difference(tt.all, tt.subset)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("Difference(%v, %v) = %v, want %v", tt.all, tt.subset, result, tt.expected)
			}
		})
	}
}
