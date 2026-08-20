// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestSizeMismatchLogLevel(t *testing.T) {
	t.Parallel()

	s := &Service{}

	require.Equal(t, zerolog.WarnLevel, s.sizeMismatchLogLevel("src-a", "match-1"), "first rejection of a pair warns")
	require.Equal(t, zerolog.TraceLevel, s.sizeMismatchLogLevel("src-a", "match-1"), "repeat of the same pair drops to trace")
	require.Equal(t, zerolog.TraceLevel, s.sizeMismatchLogLevel("src-a", "match-1"), "further repeats stay trace")

	require.Equal(t, zerolog.WarnLevel, s.sizeMismatchLogLevel("src-a", "match-2"), "same source against another local copy warns")
	require.Equal(t, zerolog.WarnLevel, s.sizeMismatchLogLevel("src-b", "match-1"), "different source against the same local copy warns")
}
