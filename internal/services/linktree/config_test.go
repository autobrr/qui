// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package linktree

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseBaseDirs(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"/data/a", "/data/b c"}, ParseBaseDirs("  /data/a , /data/b c ,, "))
	require.Empty(t, ParseBaseDirs("   "))
}
