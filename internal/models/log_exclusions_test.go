// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseLogExclusionPatternsNormalizesNullToEmptySlice(t *testing.T) {
	patterns := parseLogExclusionPatterns("null")
	require.NotNil(t, patterns)
	require.Empty(t, patterns)
}
