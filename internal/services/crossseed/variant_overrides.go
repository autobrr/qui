// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/autobrr/rls"

	"github.com/autobrr/qui/pkg/stringutils"
)

type variantOverrides struct {
	collection []string
	other      []string
	edition    []string
	cut        []string
}

var (
	variantNormalizer = stringutils.NewNormalizer(5*time.Minute, transformToUpper)

	strictVariantOverrides = newVariantOverrides(
		[]string{"IMAX"},
		[]string{
			"HYBRID",
		},
		nil,
		nil,
	)

	nonPackVariantOverrides = newVariantOverrides(
		nil,
		[]string{
			"REPACK",
			"REREPACK",
			"REPACK3",
			"REPACK4",
			"REPACK5",
			"REPACK6",
			"REPACK7",
			"REPACK8",
			"REPACK9",
			"REPACK10",
			"PROPER",
			"RERIP",
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

func (o variantOverrides) findMismatch(source, candidate *rls.Release) string {
	sourceVariants := o.releaseVariants(source)
	if len(sourceVariants) == 0 {
		return ""
	}
	candidateVariants := o.releaseVariants(candidate)
	keys := make([]string, 0, len(sourceVariants))
	for key := range sourceVariants {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, ok := candidateVariants[key]; !ok {
			return key
		}
	}
	return ""
}

func isSeasonPack(r *rls.Release) bool {
	return r.Series > 0 && r.Episode == 0
}

func checkVariantsCompatible(source, candidate *rls.Release) (bool, string) {
	if mismatch := strictVariantOverrides.findMismatch(source, candidate); mismatch != "" {
		return false, mismatch
	}
	if mismatch := strictVariantOverrides.findMismatch(candidate, source); mismatch != "" {
		return false, mismatch
	}

	if isSeasonPack(source) || isSeasonPack(candidate) {
		return true, ""
	}

	if mismatch := nonPackVariantOverrides.findMismatch(source, candidate); mismatch != "" {
		return false, mismatch
	}
	if mismatch := nonPackVariantOverrides.findMismatch(candidate, source); mismatch != "" {
		return false, mismatch
	}

	return true, ""
}
