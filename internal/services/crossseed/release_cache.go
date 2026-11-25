// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import "github.com/autobrr/qui/pkg/releases"

type ReleaseCache = releases.Parser

func NewReleaseCache() *ReleaseCache {
	return releases.NewDefaultParser() //nolint:forbidigo // the release cache is the one parser that cross-seed code builds (ADR 0008)
}
