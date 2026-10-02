// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package remote

import (
	"fmt"
	"path"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
)

// writeWideTree writes dirs sibling directories, one file each, under root.
func writeWideTree(t *testing.T, root string, dirs int) {
	t.Helper()
	for i := range dirs {
		writeFile(t, remotePath(root, fmt.Sprintf("d%03d", i), "f"), "x")
	}
}

// Each directory costs opendir, readdir, the readdir that answers EOF and
// close, every one a round trip, so a serial walk of n directories cannot
// beat about 3n round trips. Walking siblings concurrently has to.
func TestWalkDir_SiblingDirectoriesOverlapRoundTrips(t *testing.T) {
	t.Parallel()

	const dirs, latency = 48, 25 * time.Millisecond
	b, server := newBackend(t)
	server.SetLatency(latency)
	dir := t.TempDir()
	writeWideTree(t, dir, dirs)

	start := time.Now()
	ch, err := b.WalkDir(t.Context(), remotePath(dir), fsops.WalkOptions{})
	require.NoError(t, err)
	n := 0
	for entry := range ch {
		require.NoError(t, entry.Err)
		n++
	}
	elapsed := time.Since(start)
	serialFloor := time.Duration(dirs) * 3 * latency
	t.Logf("%d directories at %v latency: walked %d entries in %v (serial floor %v)", dirs, latency, n, elapsed.Round(time.Millisecond), serialFloor)

	assert.Equal(t, 1+2*dirs, n)
	assert.Less(t, elapsed, serialFloor/2, "siblings must be walked concurrently")
}

// The contract that survives concurrency: entries within one directory are
// lexical, and a directory's own entry precedes anything beneath it.
func TestWalkDir_OrderWithinDirectoryAndParentFirst(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	for _, p := range []string{"b/z", "b/a/q", "a/y", "a/x", "c"} {
		writeFile(t, remotePath(dir, p), "x")
	}

	ch, err := b.WalkDir(t.Context(), remotePath(dir), fsops.WalkOptions{})
	require.NoError(t, err)
	seen := map[string]int{}
	byDir := map[string][]string{}
	i := 0
	for entry := range ch {
		require.NoError(t, entry.Err)
		seen[entry.RelPath] = i
		if entry.RelPath != "." {
			parent := path.Dir(entry.RelPath)
			byDir[parent] = append(byDir[parent], path.Base(entry.RelPath))
			_, ok := seen[parent]
			assert.True(t, ok, "%s emitted before its parent %s", entry.RelPath, parent)
		}
		i++
	}
	assert.Equal(t, []string{"a", "b", "c"}, byDir["."])
	assert.Equal(t, []string{"x", "y"}, byDir["a"])
	assert.Equal(t, []string{"a", "z"}, byDir["b"])
}
