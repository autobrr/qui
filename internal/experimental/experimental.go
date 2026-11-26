// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package experimental

import (
	"net/http"
	"slices"

	"github.com/rs/zerolog/log"
)

type Feature string

var known = []Feature{
	ProblemReports, // goes away when the maintainer decides, with no ticket
	HeldTitles,     // goes away in #4458
}

const ProblemReports Feature = "problemReports"

const HeldTitles Feature = "heldTitles"

type Features struct {
	on []Feature
}

func New(configured []string) *Features {
	return NewWithKnown(known, configured)
}

func NewWithKnown(knownFeatures []Feature, configured []string) *Features {
	features := &Features{on: []Feature{}}
	for _, feature := range knownFeatures {
		if slices.Contains(configured, string(feature)) {
			features.on = append(features.on, feature)
		}
	}

	var unknown []string
	for _, name := range configured {
		if slices.Contains(knownFeatures, Feature(name)) || slices.Contains(unknown, name) {
			continue
		}
		unknown = append(unknown, name)
		log.Warn().Str("name", name).Msg("experimentalFeatures names an unknown Experimental feature, ignoring it")
	}

	if len(features.on) > 0 {
		log.Info().Strs("experimentalFeatures", features.Names()).Msg("Experimental features are on")
	}
	return features
}

func (f *Features) Enabled(feature Feature) bool {
	return slices.Contains(f.on, feature)
}

func (f *Features) Names() []string {
	names := make([]string, 0, len(f.on))
	for _, feature := range f.on {
		names = append(names, string(feature))
	}
	return names
}

func (f *Features) Require(feature Feature) func(http.Handler) http.Handler {
	enabled := f.Enabled(feature)
	return func(next http.Handler) http.Handler {
		if enabled {
			return next
		}
		return http.NotFoundHandler()
	}
}
