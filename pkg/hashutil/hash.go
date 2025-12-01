// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package hashutil provides utility functions for normalizing and working
// with torrent info hashes consistently across the codebase.
package hashutil

import (
	"strings"

	"github.com/autobrr/qui/pkg/stringutils"
)

// Normalize canonicalizes a torrent hash by trimming whitespace and converting to lowercase.
// Returns an empty string if the input is blank.
// The returned string is interned using Go's unique package for memory efficiency,
// as torrent hashes are frequently compared and stored.
func Normalize(hash string) string {
	return stringutils.InternNormalized(hash)
}

// NormalizeUpper canonicalizes a torrent hash by trimming whitespace and converting to uppercase.
// Returns an empty string if the input is blank.
// The returned string is interned using Go's unique package for memory efficiency.
func NormalizeUpper(hash string) string {
	return stringutils.InternNormalizedUpper(hash)
}

// NormalizeAll normalizes a slice of hashes to lowercase, removing empty entries and duplicates.
// Preserves the order of first occurrence.
func NormalizeAll(hashes []string) []string {
	if len(hashes) == 0 {
		return nil
	}

	result := make([]string, 0, len(hashes))
	seen := make(map[string]struct{}, len(hashes))

	for _, hash := range hashes {
		normalized := Normalize(hash)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}

	return result
}

// NormalizeAllUpper normalizes a slice of hashes to uppercase, removing empty entries and duplicates.
// Preserves the order of first occurrence.
func NormalizeAllUpper(hashes []string) []string {
	if len(hashes) == 0 {
		return nil
	}

	result := make([]string, 0, len(hashes))
	seen := make(map[string]struct{}, len(hashes))

	for _, hash := range hashes {
		normalized := NormalizeUpper(hash)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}

	return result
}

// NormalizedSet holds normalized hashes along with lookup helpers.
type NormalizedSet struct {
	// Canonical contains the normalized (lowercase) hashes in order of first occurrence.
	Canonical []string

	// CanonicalSet provides O(1) lookups for normalized hashes.
	CanonicalSet map[string]struct{}

	// CanonicalToPreferred maps normalized hashes to their original (preferred) form.
	CanonicalToPreferred map[string]string

	// Lookup contains all case variants for flexible matching (lowercase, uppercase, original).
	Lookup []string
}

// NewNormalizedSet creates a NormalizedSet from a slice of hashes.
// This is useful when you need to match hashes case-insensitively and also
// track the original preferred form for API calls.
func NewNormalizedSet(hashes []string) NormalizedSet {
	result := NormalizedSet{
		Canonical:            make([]string, 0, len(hashes)),
		CanonicalSet:         make(map[string]struct{}, len(hashes)),
		CanonicalToPreferred: make(map[string]string, len(hashes)),
		Lookup:               make([]string, 0, len(hashes)),
	}

	seenLookup := make(map[string]struct{}, len(hashes)*2)

	for _, hash := range hashes {
		trimmed := strings.TrimSpace(hash)
		canonical := Normalize(trimmed)
		if canonical == "" {
			continue
		}

		if _, exists := result.CanonicalSet[canonical]; !exists {
			result.CanonicalSet[canonical] = struct{}{}
			result.Canonical = append(result.Canonical, canonical)
			result.CanonicalToPreferred[canonical] = trimmed
		}

		// Add all case variants for flexible matching
		for _, variant := range []string{trimmed, canonical, strings.ToUpper(trimmed)} {
			if variant == "" {
				continue
			}
			if _, ok := seenLookup[variant]; ok {
				continue
			}
			seenLookup[variant] = struct{}{}
			result.Lookup = append(result.Lookup, variant)
		}
	}

	return result
}

// Contains checks if the set contains the given hash (case-insensitive).
func (s *NormalizedSet) Contains(hash string) bool {
	if len(s.CanonicalSet) == 0 {
		return false
	}
	_, ok := s.CanonicalSet[Normalize(hash)]
	return ok
}

// PreferredForm returns the original (preferred) form of the hash, or the normalized form if not found.
func (s *NormalizedSet) PreferredForm(hash string) string {
	canonical := Normalize(hash)
	if preferred, ok := s.CanonicalToPreferred[canonical]; ok {
		return preferred
	}
	return canonical
}

// Difference returns hashes in 'all' that are not in 'subset' (case-insensitive comparison).
// The returned hashes preserve their original form from 'all'.
func Difference(all, subset []string) []string {
	if len(subset) == 0 {
		// Return a copy to avoid aliasing
		result := make([]string, len(all))
		copy(result, all)
		return result
	}

	// Build a set of normalized subset hashes with counts (for handling duplicates)
	subsetCounts := make(map[string]int, len(subset))
	for _, hash := range subset {
		normalized := Normalize(hash)
		if normalized != "" {
			subsetCounts[normalized]++
		}
	}

	remaining := make([]string, 0, len(all))
	for _, hash := range all {
		normalized := Normalize(hash)
		if count, ok := subsetCounts[normalized]; ok && count > 0 {
			subsetCounts[normalized] = count - 1
			continue
		}
		remaining = append(remaining, hash)
	}

	return remaining
}
