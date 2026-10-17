// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
)

func newTestBackend() fsops.Backend {
	return local.NewBackend()
}

// fakeWalkBackend serves canned WalkDir entries so tests can simulate the same
// file appearing at two paths with nlink=1 (a bind-mount/mergerfs alias),
// which cannot be constructed on a real test filesystem.
type fakeWalkBackend struct {
	fsops.Backend
	entries []fsops.WalkEntry
}

func (b *fakeWalkBackend) WalkDir(_ context.Context, _ string, _ fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	ch := make(chan fsops.WalkEntry, len(b.entries))
	for _, e := range b.entries {
		ch <- e
	}
	close(ch)
	return ch, nil
}
