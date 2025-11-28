// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
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

// strictVariantOverrides contains the current curated set of strict tags.
//
// Edit these slices to add more variants that should match exactly.
var (
	variantNormalizer      = stringutils.NewNormalizer(5*time.Minute, transformToUpper)
	strictVariantOverrides = newVariantOverrides(
		[]string{"IMAX"}, // IMAX releases behave like a unique master
		[]string{
			"HYBRID",   // HYBRID encodes differ notably from vanilla releases
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
	for _, token := range tokens {
		if token == target {
			return true
		}
	}
	return false
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
