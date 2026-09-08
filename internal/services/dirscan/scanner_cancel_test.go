// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"testing"

	"github.com/autobrr/qui/internal/fsops"
)

type cancelingScannerBackend struct {
	fsops.Backend
	cancel context.CancelFunc
}

func (b *cancelingScannerBackend) ReadDir(context.Context, string) ([]fsops.DirEntry, error) {
	return []fsops.DirEntry{{Name: "release", IsDir: true}}, nil
}

func (b *cancelingScannerBackend) WalkDir(context.Context, string, fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	ch := make(chan fsops.WalkEntry, 1)
	ch <- fsops.WalkEntry{Path: "/scan/release/movie.mkv"}
	b.cancel()
	close(ch)
	return ch, nil
}

func TestScanDirectory_ReturnsCancellationAfterFinalWalkCloses(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	scanner := NewScanner(&cancelingScannerBackend{cancel: cancel})

	_, err := scanner.ScanDirectory(ctx, "/scan")

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ScanDirectory error = %v, want context.Canceled", err)
	}
}
