// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package experimental owns the Experimental features: features that are merged
// before they are finished and stay off unless the user names them in
// experimentalFeatures. See docs/standards/experimental-features.md.
package experimental

import (
	"net/http"
	"slices"

	"github.com/rs/zerolog/log"
)

// Feature is the name of an Experimental feature, as the user writes it in experimentalFeatures.
type Feature string

// known lists every Experimental feature. Each name carries a comment that
// says when it goes away.
var known = []Feature{
	ProblemReports, // goes away when the maintainer decides, with no ticket
	HeldTitles,     // goes away in #4458
}

// ProblemReports is Report a problem: Problem reports that qui's server sends to brrbin.
const ProblemReports Feature = "problemReports"

const HeldTitles Feature = "heldTitles"

// Features holds the Experimental features that are on. qui builds it once at start.
type Features struct {
	on []Feature
}

// New turns on each known feature that configured names. Names match exactly.
// It logs a warning for each unknown name and one info line that lists the
// features that are on.
func New(configured []string) *Features {
	return NewWithKnown(known, configured)
}

// NewWithKnown is New with its own list of known features, for tests that use test-only names.
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

// Require guards a route group: while the Experimental feature is off, each
// route in the group answers 404, as a route that qui does not have.
func (f *Features) Require(feature Feature) func(http.Handler) http.Handler {
	enabled := f.Enabled(feature)
	return func(next http.Handler) http.Handler {
		if enabled {
			return next
		}
		return http.NotFoundHandler()
	}
}
