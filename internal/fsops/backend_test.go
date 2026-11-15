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

// perPath answers Stat and Lstat for the paths it holds, and records each
// call. The nil embedded Backend makes any other call panic.
type perPath struct {
	Backend
	files map[string]bool
	calls []string
}

func (b *perPath) Lstat(_ context.Context, p string) (*LstatInfo, error) {
	return b.answer("lstat", p)
}

func (b *perPath) Stat(_ context.Context, p string) (*LstatInfo, error) {
	return b.answer("stat", p)
}

func (b *perPath) answer(op, p string) (*LstatInfo, error) {
	b.calls = append(b.calls, op+" "+p)
	if !b.files[p] {
		return nil, &fs.PathError{Op: op, Path: p, Err: fs.ErrNotExist}
	}
	return &LstatInfo{Path: p}, nil
}

func TestAllWithoutBatchesAsksEachPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		op  string
		all func(context.Context, Backend, []string) ([]*LstatInfo, []error)
	}{
		{op: "lstat", all: LstatAll},
		{op: "stat", all: StatAll},
	}
	for _, tt := range tests {
		t.Run(tt.op, func(t *testing.T) {
			t.Parallel()

			b := &perPath{files: map[string]bool{"/a": true, "/c": true}}
			infos, errs := tt.all(t.Context(), b, []string{"/a", "/b", "/c"})

			assert.Equal(t, []string{tt.op + " /a", tt.op + " /b", tt.op + " /c"}, b.calls)
			require.NoError(t, errs[0])
			require.ErrorIs(t, errs[1], fs.ErrNotExist)
			require.NoError(t, errs[2])
			assert.Equal(t, "/a", infos[0].Path)
			assert.Nil(t, infos[1])
			assert.Equal(t, "/c", infos[2].Path)
		})
	}
}
