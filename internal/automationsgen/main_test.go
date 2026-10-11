// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReferenceIsCurrent also fails when a rule struct field has no doc tag, because generate returns an error.
func TestReferenceIsCurrent(t *testing.T) {
	root := filepath.Join("..", "..")
	want, err := generate(root)
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(referencePath)))
	require.NoError(t, err)
	require.Equal(t, string(want), string(got), "%s is stale, run `make generate`", referencePath)
}
