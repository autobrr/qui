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

type collidingIDBackend struct{ fsops.Backend }

func (b collidingIDBackend) Lstat(ctx context.Context, p string) (*fsops.LstatInfo, error) {
	info, err := b.Backend.Lstat(ctx, p)
	if err != nil {
		return nil, err
	}
	info.FileID, info.FileIDErr = hardlink.UnixFileID(7, 42), nil
	return info, nil
}

func TestMetadataIgnoreRootsNeedsTrustedIdentityToUnignore(t *testing.T) {
	base := t.TempDir()
	scanRoot := filepath.Join(base, "Incoming")
	metadataRoot := filepath.Join(base, "incoming")
	require.NoError(t, os.MkdirAll(scanRoot, 0o755))
	require.NoError(t, os.MkdirAll(metadataRoot, 0o755))
	backend := collidingIDBackend{local.NewBackend()}

	remote := &models.Instance{ID: 1, SSHHost: "box.example.invalid", SSHKeyEncrypted: "k", SSHHostKeyEncrypted: "hk", SSHTier: models.SSHTierExec}
	localInst := &models.Instance{ID: 1, HasLocalFilesystemAccess: true}

	assert.Equal(t, []string{filepath.Clean(metadataRoot)},
		metadataIgnoreRoots(t.Context(), []string{base, scanRoot}, []string{metadataRoot}, backend, remote),
		"peer identity keeps the root ignored")
	assert.Empty(t,
		metadataIgnoreRoots(t.Context(), []string{base, scanRoot}, []string{metadataRoot}, backend, localInst),
		"kernel identity may un-ignore it")
}
