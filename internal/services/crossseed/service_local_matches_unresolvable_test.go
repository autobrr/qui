// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

// findLocalMatchesForFiles runs FindLocalMatches over one source and one candidate
// torrent that share the given file list, with local filesystem access on both.
func findLocalMatchesForFiles(t *testing.T, files qbt.TorrentFiles, strict bool) (*LocalMatchesResponse, error) {
	t.Helper()

	sourceDir, candidateDir := writeIndependentLocalMatchFiles(t, files, files)
	source := qbt.Torrent{
		Hash:        hlSourceHash,
		Name:        "Movie.2023.1080p.WEB-GROUP",
		SavePath:    sourceDir,
		ContentPath: sourceDir,
	}
	candidate := *hardlinkTestCandidate(candidateDir)
	candidate.ContentPath = candidateDir
	syncManager := &reflinkFindLocalMatchesSyncManager{
		files: map[string]qbt.TorrentFiles{
			normalizeHash(hlSourceHash):    files,
			normalizeHash(hlCandidateHash): files,
		},
		source:    source,
		candidate: candidate,
	}
	service := &Service{
		instanceStore: newOrderedInstanceStore(&models.Instance{ID: 1, Name: "local", IsActive: true, HasLocalFilesystemAccess: true}),
		syncManager:   syncManager,
		releaseCache:  NewReleaseCache(),
	}
	return service.FindLocalMatches(context.Background(), 1, source.Hash, strict)
}

// A backslash is a legal filename byte on Unix, so such a name cannot be mapped to a
// local path. That is missing evidence, not evidence of absence: strict mode must fail
// instead of reporting "no cross-seeds found" and inviting a delete.
func TestFindLocalMatches_UnresolvableFileName_FailsClosedInStrictMode(t *testing.T) {
	files := qbt.TorrentFiles{{Name: `AC\DC - Back In Black.mkv`, Size: 4}}

	response, err := findLocalMatchesForFiles(t, files, true)
	require.Error(t, err)
	require.Nil(t, response)
	require.Contains(t, err.Error(), "failed to verify local file relationship")
	require.Contains(t, err.Error(), strconv.Quote(files[0].Name))

	// Best-effort mode still returns what it found.
	response, err = findLocalMatchesForFiles(t, files, false)
	require.NoError(t, err)
	require.Len(t, response.Matches, 1)
}

// The candidate's names can be the unresolvable ones while the source's are fine.
// The source here has a hardlinked file (so the candidate FileID pass actually runs),
// and the candidate torrent's list holds only a backslash name: without the recording
// in localLinkedMatchType the check would read "not hardlinked" and strict mode would
// fail open on exactly the torrent that might be the cross-seed.
func TestFindLocalMatches_UnresolvableCandidateName_FailsClosedInStrictMode(t *testing.T) {
	fileName := "Movie.2023.1080p.WEB.mkv"
	sourceDir, candidateDir := writeHardlinkFixture(t, fileName, true)

	candidateName := `AC\DC - Back In Black.mkv`
	source := qbt.Torrent{
		Hash:        hlSourceHash,
		Name:        "Movie.2023.1080p.WEB-GROUP",
		SavePath:    sourceDir,
		ContentPath: sourceDir,
	}
	candidate := *hardlinkTestCandidate(candidateDir)
	syncManager := &reflinkFindLocalMatchesSyncManager{
		files: map[string]qbt.TorrentFiles{
			normalizeHash(hlSourceHash):    {{Name: fileName, Size: 4}},
			normalizeHash(hlCandidateHash): {{Name: candidateName, Size: 4}},
		},
		source:    source,
		candidate: candidate,
	}
	service := &Service{
		instanceStore: newOrderedInstanceStore(&models.Instance{ID: 1, Name: "local", IsActive: true, HasLocalFilesystemAccess: true}),
		syncManager:   syncManager,
		releaseCache:  NewReleaseCache(),
	}

	_, err := service.FindLocalMatches(context.Background(), 1, source.Hash, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to verify local file relationship")
	require.Contains(t, err.Error(), strconv.Quote(candidateName))
}

// A resolved name whose file is not on disk is normal for an incomplete torrent and
// must stay a best-effort skip, or every partial download would trip strict mode.
func TestFindLocalMatches_MissingLocalFile_StaysBestEffort(t *testing.T) {
	// writeIndependentLocalMatchFiles creates the save paths but no file for this name.
	files := qbt.TorrentFiles{{Name: "not-downloaded-yet.mkv", Size: 4}}

	response, err := findLocalMatchesForFiles(t, files, true)
	require.NoError(t, err)
	require.Len(t, response.Matches, 1)
	require.Equal(t, matchTypeName, response.Matches[0].MatchType)
}

func TestResolveLocalTorrentFile(t *testing.T) {
	base := filepath.Join(t.TempDir(), "save")

	tests := []struct {
		name     string
		fileName string
		wantRel  string
	}{
		{name: "plain name", fileName: "Movie.2023.1080p.WEB.mkv", wantRel: "Movie.2023.1080p.WEB.mkv"},
		{name: "nested name", fileName: "Season 01/Episode 01.mkv", wantRel: filepath.Join("Season 01", "Episode 01.mkv")},
		{name: "dot segments inside base", fileName: "sub/../Movie.mkv", wantRel: "Movie.mkv"},
		{name: "empty name"},
		{name: "backslash in name", fileName: `AC\DC - Back In Black.mkv`},
		{name: "windows traversal", fileName: `..\outside.bin`},
		{name: "posix traversal", fileName: "../outside.bin"},
		{name: "nested posix traversal", fileName: "sub/../../outside.bin"},
		{name: "posix rooted", fileName: "/etc/passwd"},
		{name: "windows rooted", fileName: `\Windows\System32\config`},
		{name: "windows drive", fileName: `C:\Windows\System32\config`},
		{name: "unc path", fileName: `\\host\share\file.mkv`},
		{name: "parent only", fileName: ".."},
		{name: "current dir", fileName: "."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := resolveLocalTorrentFile(base, tt.fileName)
			if tt.wantRel == "" {
				require.False(t, ok)
				require.Empty(t, got)
				return
			}
			require.True(t, ok)
			require.Equal(t, filepath.Join(base, tt.wantRel), got)
		})
	}
}
