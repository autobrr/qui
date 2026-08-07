// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package crossseed

import (
	"errors"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestLocalLinkedMatchType_UnsupportedPlatformSkipsCandidateFetchWithoutHardlinks(t *testing.T) {
	fileName := "Movie.2023.1080p.WEB.mkv"
	sourceDir, candidateDir := writeHardlinkFixture(t, fileName, false)
	sourceFiles := qbt.TorrentFiles{{Name: fileName, Size: 4}}
	service := hardlinkTestService(map[string]qbt.TorrentFiles{
		normalizeHash(hlSourceHash): sourceFiles,
	})
	matchCtx := hardlinkTestMatchCtx(service, sourceDir)
	_, _, err := matchCtx.getSourceFiles()
	require.NoError(t, err)

	service.syncManager = &localMatchSyncManager{errorOnFetch: errors.New("unexpected candidate fetch")}
	matchType := service.localLinkedMatchType(
		matchCtx,
		&models.Instance{ID: 1, HasLocalFilesystemAccess: true},
		hardlinkTestCandidate(candidateDir),
	)

	require.Empty(t, matchType)
	require.NoError(t, matchCtx.candidateFilesErr)
}
