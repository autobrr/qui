// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"slices"
	"strings"
	"time"

	"github.com/moistari/rls"

	"github.com/autobrr/qui/pkg/stringutils"
)

// variantOverrides lists release tags that must match on both torrents for
// cross-seeding to be considered safe. This lets us plug RLS parsing gaps
// (e.g. IMAX vs HYBRID) without forking the parser.
type variantOverrides struct {
	collection []string
	other      []string
	edition    []string
	cut        []string
}

// strictVariantOverrides contains tags that must ALWAYS match exactly.
// These represent different video masters (IMAX, HYBRID) that cannot be cross-seeded.
//
// nonPackVariantOverrides contains tags that must match for non-pack content.
// Season packs are exempt because a pack might contain a REPACK of just one episode.
var (
	variantNormalizer = stringutils.NewNormalizer(5*time.Minute, transformToUpper)

	// Always strict - these represent different source masters
	strictVariantOverrides = newVariantOverrides(
		[]string{"IMAX"}, // IMAX releases behave like a unique master
		[]string{
			"HYBRID", // HYBRID encodes differ notably from vanilla releases
		},
		nil,
		nil,
	)

	// Non-pack strict - exempt season packs since they may contain partial REPACKs
	nonPackVariantOverrides = newVariantOverrides(
		nil,
		[]string{
			"REPACK",   // Re-release to fix issues with original
			"REPACK2",  // Second re-release
			"REPACK3",  // Third re-release
			"REPACK4",  // Fourth re-release
			"REPACK5",  // Fifth re-release
			"REPACK6",  // Sixth re-release
			"REPACK7",  // Seventh re-release
			"REPACK8",  // Eighth re-release
			"REPACK9",  // Ninth re-release
			"REPACK10", // Tenth re-release
			"PROPER",   // Correction to a flawed release
		},
		nil,
		nil,
	)
)

func transformToUpper(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

func newVariantOverrides(collection, other, edition, cut []string) variantOverrides {
	return variantOverrides{
		collection: normalizeVariantSlice(collection),
		other:      normalizeVariantSlice(other),
		edition:    normalizeVariantSlice(edition),
		cut:        normalizeVariantSlice(cut),
	}
}

func normalizeVariantSlice(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, v := range values {
		if nv := normalizeVariant(v); nv != "" {
			normalized = append(normalized, nv)
		}
	}
	return normalized
}

func normalizeVariant(value string) string {
	return variantNormalizer.Normalize(value)
}

func (o variantOverrides) releaseVariants(r *rls.Release) map[string]struct{} {
	variants := make(map[string]struct{})

	addVariant := func(name string) {
		if name == "" {
			return
		}
		variants[name] = struct{}{}
	}

	for _, candidate := range o.collection {
		if normalizeVariant(r.Collection) == candidate {
			addVariant(candidate)
		}
	}

	addListVariants := func(values []string, strictValues []string) {
		if len(values) == 0 || len(strictValues) == 0 {
			return
		}
		for _, entry := range values {
			nv := normalizeVariant(entry)
			if nv == "" {
				continue
			}
			for _, candidate := range strictValues {
				if variantValueMatches(nv, candidate) {
					addVariant(candidate)
				}
			}
		}
	}

	addListVariants(r.Other, o.other)
	addListVariants(r.Edition, o.edition)
	addListVariants(r.Cut, o.cut)

	return variants
}

func variantValueMatches(value, target string) bool {
	if value == "" || target == "" {
		return false
	}
	if value == target {
		return true
	}
	tokens := variantTokens(value)
	return slices.Contains(tokens, target)
}

func variantTokens(value string) []string {
	split := func(r rune) bool {
		switch r {
		case '.', '-', '_', ' ', '/', '+', '[', ']', '(', ')':
			return true
		default:
			return false
		}
	}
	tokens := strings.FieldsFunc(value, split)
	if len(tokens) == 0 && value != "" {
		return []string{value}
	}
	return tokens
}

func (o variantOverrides) variantsCompatible(source, candidate *rls.Release) bool {
	sourceVariants := o.releaseVariants(source)
	if len(sourceVariants) == 0 {
		return true
	}
	candidateVariants := o.releaseVariants(candidate)
	for key := range sourceVariants {
		if _, ok := candidateVariants[key]; !ok {
			return false
		}
	}
	return true
}

// findMismatch returns the first variant in source that is missing from candidate.
// Returns empty string if all variants match.
func (o variantOverrides) findMismatch(source, candidate *rls.Release) string {
	sourceVariants := o.releaseVariants(source)
	if len(sourceVariants) == 0 {
		return ""
	}
	candidateVariants := o.releaseVariants(candidate)
	for key := range sourceVariants {
		if _, ok := candidateVariants[key]; !ok {
			return key
		}
	}
	return ""
}

// isSeasonPack returns true if the release is a season pack (has series but no episode).
func isSeasonPack(r *rls.Release) bool {
	return r.Series > 0 && r.Episode == 0
}

// checkVariantsCompatible validates variant compatibility between source and candidate.
// For always-strict variants (IMAX, HYBRID), mismatches are never allowed.
// For non-pack variants (REPACK, PROPER), mismatches are allowed if either release is a season pack.
// Returns (compatible, mismatchReason) where mismatchReason is empty if compatible.
func checkVariantsCompatible(source, candidate *rls.Release) (bool, string) {
	// Always-strict variants must match regardless of content type
	if mismatch := strictVariantOverrides.findMismatch(source, candidate); mismatch != "" {
		return false, mismatch
	}
	if mismatch := strictVariantOverrides.findMismatch(candidate, source); mismatch != "" {
		return false, mismatch
	}

	// Non-pack variants are skipped for season packs
	// A season pack might contain a REPACK of just one episode
	if isSeasonPack(source) || isSeasonPack(candidate) {
		return true, ""
	}

	// For non-pack content, REPACK/PROPER must match
	if mismatch := nonPackVariantOverrides.findMismatch(source, candidate); mismatch != "" {
		return false, mismatch
	}
	if mismatch := nonPackVariantOverrides.findMismatch(candidate, source); mismatch != "" {
		return false, mismatch
	}

	return true, ""
}
