// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlink"
)

// collidingIDBackend reports one identity for every directory, the way a
// remote host could when it lies or collapses case.
type collidingIDBackend struct{ fsops.Backend }

func (b collidingIDBackend) Lstat(ctx context.Context, p string) (*fsops.LstatInfo, error) {
	info, err := b.Backend.Lstat(ctx, p)
	if err != nil {
		return nil, err
	}
	info.FileID, info.FileIDErr = hardlink.UnixFileID(7, 42), nil
	return info, nil
}

// Identity from a peer host may narrow a scan but never widen a delete: a
// remote instance whose identity says a metadata root is a scan root keeps
// that root ignored, where a local instance's kernel-attested identity
// takes it out of the ignore list.
func TestMetadataIgnoreRootsNeedsTrustedIdentityToUnignore(t *testing.T) {
	base := t.TempDir()
	scanRoot := filepath.Join(base, "Incoming")
	metadataRoot := filepath.Join(base, "incoming")
	require.NoError(t, os.MkdirAll(scanRoot, 0o755))
	require.NoError(t, os.MkdirAll(metadataRoot, 0o755))
	backend := collidingIDBackend{local.NewBackend()}

	remote := &models.Instance{ID: 1, SSHHost: "box.example.invalid", SSHKeyEncrypted: "k", SSHHostKeyEncrypted: "hk", SSHTier: models.SSHTierExec}
	localInst := &models.Instance{ID: 1, HasLocalFilesystemAccess: true}

	// The roots differ only by case, so they normalize alike and only the
	// identity decides whether the metadata root is the scan root.
	assert.Equal(t, []string{filepath.Clean(metadataRoot)},
		metadataIgnoreRoots(t.Context(), []string{base, scanRoot}, []string{metadataRoot}, backend, remote),
		"peer identity keeps the root ignored")
	assert.Empty(t,
		metadataIgnoreRoots(t.Context(), []string{base, scanRoot}, []string{metadataRoot}, backend, localInst),
		"kernel identity may un-ignore it")
}
