// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsPathInsideBase(t *testing.T) {
	// Use OS-specific path separator for test cases
	sep := string(os.PathSeparator)

	tests := []struct {
		name     string
		basePath string
		fullPath string
		expected bool
	}{
		{
			name:     "normal nested path",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + "file.mkv",
			expected: true,
		},
		{
			name:     "nested directory path",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + "subdir" + sep + "file.mkv",
			expected: true,
		},
		{
			name:     "path equals base (edge case)",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents",
			expected: true,
		},
		{
			name:     "parent traversal with ..",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + ".." + sep + "secret.txt",
			expected: false,
		},
		{
			name:     "double parent traversal",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + ".." + sep + ".." + sep + "etc" + sep + "passwd",
			expected: false,
		},
		{
			name:     "path that resolves to parent",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data",
			expected: false,
		},
		{
			name:     "sibling path",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "other",
			expected: false,
		},
		{
			name:     "absolute path outside base",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "etc" + sep + "passwd",
			expected: false,
		},
		{
			name:     "traversal hidden in middle",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + "safe" + sep + ".." + sep + ".." + sep + "secret",
			expected: false,
		},
		{
			name:     "current directory dots are ok",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + "." + sep + "file.mkv",
			expected: true,
		},
		{
			name:     "deeply nested valid path",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + "a" + sep + "b" + sep + "c" + sep + "file.mkv",
			expected: true,
		},
		{
			name:     "path with trailing separator",
			basePath: sep + "data" + sep + "torrents" + sep,
			fullPath: sep + "data" + sep + "torrents" + sep + "file.mkv",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPathInsideBase(tt.basePath, tt.fullPath)
			if result != tt.expected {
				t.Errorf("isPathInsideBase(%q, %q) = %v, want %v",
					tt.basePath, tt.fullPath, result, tt.expected)
			}
		})
	}
}

func TestIsPathInsideBase_RelativeCleanedPaths(t *testing.T) {
	// Test with paths that have various normalization edge cases
	sep := string(os.PathSeparator)

	tests := []struct {
		name     string
		basePath string
		fullPath string
		expected bool
	}{
		{
			name:     "redundant separators in base",
			basePath: sep + "data" + sep + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + "file.mkv",
			expected: true,
		},
		{
			name:     "redundant separators in full",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + sep + "file.mkv",
			expected: true,
		},
		{
			name:     "dot components in both",
			basePath: sep + "data" + sep + "." + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + "." + sep + "file.mkv",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPathInsideBase(tt.basePath, tt.fullPath)
			if result != tt.expected {
				t.Errorf("isPathInsideBase(%q, %q) = %v, want %v",
					tt.basePath, tt.fullPath, result, tt.expected)
			}
		})
	}
}

func TestIsPathInsideBase_OSSpecific(t *testing.T) {
	// Platform-specific tests using actual filepath behavior
	basePath := filepath.Join("data", "torrents")
	fullPath := filepath.Join("data", "torrents", "file.mkv")

	if !isPathInsideBase(basePath, fullPath) {
		t.Errorf("Expected relative path inside base to return true")
	}

	escapingPath := filepath.Join("data", "torrents", "..", "other", "file.txt")
	if isPathInsideBase(basePath, escapingPath) {
		t.Errorf("Expected escaping path to return false")
	}
}
