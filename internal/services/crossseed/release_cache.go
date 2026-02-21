// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import "github.com/autobrr/qui/pkg/releases"

// ReleaseCache is preserved for backwards compatibility within the crossseed package.
// It is an alias to the shared releases.Parser.
type ReleaseCache = releases.Parser

// NewReleaseCache creates a cached parser for release metadata.
func NewReleaseCache() *ReleaseCache {
	return releases.NewDefaultParser()
}
