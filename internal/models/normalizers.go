// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import "github.com/autobrr/qui/pkg/stringutils"

var lowerTrimNormalizer = stringutils.NewDefaultNormalizer()

func normalizeLowerTrim(value string) string {
	return lowerTrimNormalizer.Normalize(value)
}
