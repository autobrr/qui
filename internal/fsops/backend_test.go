// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"context"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lstatOnly answers Lstat for the paths it holds. The nil embedded Backend
// makes any other call panic.
type lstatOnly struct {
	Backend
	files map[string]bool
	calls []string
}

func (b *lstatOnly) Lstat(_ context.Context, p string) (*LstatInfo, error) {
	b.calls = append(b.calls, p)
	if !b.files[p] {
		return nil, &fs.PathError{Op: "lstat", Path: p, Err: fs.ErrNotExist}
	}
	return &LstatInfo{Path: p}, nil
}

func TestLstatAllWithoutBatchesLstatsEachPath(t *testing.T) {
	t.Parallel()

	b := &lstatOnly{files: map[string]bool{"/a": true, "/c": true}}
	paths := []string{"/a", "/b", "/c"}
	infos, errs := LstatAll(t.Context(), b, paths)

	assert.Equal(t, paths, b.calls)
	require.NoError(t, errs[0])
	require.ErrorIs(t, errs[1], fs.ErrNotExist)
	require.NoError(t, errs[2])
	assert.Equal(t, "/a", infos[0].Path)
	assert.Nil(t, infos[1])
	assert.Equal(t, "/c", infos[2].Path)
}
