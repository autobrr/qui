// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGeneratedFilesAreCurrent also fails when a rule struct field has no doc tag, because generate returns an error.
func TestGeneratedFilesAreCurrent(t *testing.T) {
	root := filepath.Join("..", "..")
	reference, err := generate(root)
	require.NoError(t, err)

	for path, want := range map[string][]byte{
		referencePath: reference,
		fieldsPath:    generateFields(),
	} {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		require.NoError(t, err)
		require.Equal(t, string(want), string(got), "%s is stale, run `make generate`", path)
	}
}
