// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"testing"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/pkg/hardlink"
)

func newTestBackend() fsops.Backend {
	return local.NewBackend()
}

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

func newTestProtected(t testing.TB, d fsops.PathDialect, ignorePaths []string, tfm *TorrentFileMap) *protectedPaths {
	t.Helper()
	protected, err := newProtectedPaths(d, ignorePaths, tfm)
	if err != nil {
		t.Fatalf("newProtectedPaths: %v", err)
	}
	return protected
}

type aliasWalkBackend struct {
	fsops.Backend
	ids map[string]hardlink.FileID
}

func (b *aliasWalkBackend) WalkDir(ctx context.Context, root string, opts fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	in, err := b.Backend.WalkDir(ctx, root, opts)
	if err != nil {
		return nil, err
	}
	out := make(chan fsops.WalkEntry)
	go func() {
		defer close(out)
		for e := range in {
			if id, ok := b.ids[e.Path]; ok {
				e.FileID, e.Nlinks = id, 1
			}
			select {
			case out <- e:
			case <-ctx.Done():
				for range in {
				}
				return
			}
		}
	}()
	return out, nil
}

func sameFile(paths ...string) map[string]hardlink.FileID {
	ids := make(map[string]hardlink.FileID, len(paths))
	for _, p := range paths {
		ids[p] = hardlink.UnixFileID(1, 1)
	}
	return ids
}
