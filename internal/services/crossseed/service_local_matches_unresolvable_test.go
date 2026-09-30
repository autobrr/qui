// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"io/fs"
	"strconv"
	"syscall"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
)

// findLocalMatchesForFiles runs FindLocalMatches over one source and one candidate
// torrent that share the given file list, with local filesystem access on both.
// Only the onDisk files are written to either save path.
func findLocalMatchesForFiles(t *testing.T, files, onDisk qbt.TorrentFiles, strict bool) (*LocalMatchesResponse, error) {
	t.Helper()
	return localMatchesServiceForFiles(t, files, onDisk).FindLocalMatches(t.Context(), 1, hlSourceHash, strict)
}

func localMatchesServiceForFiles(t *testing.T, files, onDisk qbt.TorrentFiles) *Service {
	t.Helper()

	sourceDir, candidateDir := writeIndependentLocalMatchFiles(t, onDisk, onDisk)
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
	service.SetBackendPool(fsops.NewPool(service.instanceStore, local.NewBackend()))
	return service
}

// A backslash is a legal filename byte on Unix, so such a name cannot be mapped to a
// local path. That is missing evidence, not evidence of absence: strict mode must fail
// instead of reporting "no cross-seeds found" and inviting a delete.
func TestFindLocalMatches_UnresolvableFileName_FailsClosedInStrictMode(t *testing.T) {
	files := qbt.TorrentFiles{{Name: `AC\DC - Back In Black.mkv`, Size: 4}}

	response, err := findLocalMatchesForFiles(t, files, files, true)
	require.Error(t, err)
	require.Nil(t, response)
	require.Contains(t, err.Error(), "failed to verify local file relationship")
	require.Contains(t, err.Error(), normalizeHash(hlSourceHash))
	require.Contains(t, err.Error(), strconv.Quote(files[0].Name))

	// Best-effort mode still returns what it found.
	response, err = findLocalMatchesForFiles(t, files, files, false)
	require.NoError(t, err)
	require.Len(t, response.Matches, 1)
}

// The candidate's names can be the unresolvable ones while the source's are fine.
// The candidate torrent's list holds only a backslash name: without the recording in
// localLinkedMatchType the check would read "not linked" and strict mode would fail
// open on exactly the torrent that might be the cross-seed. The reflink row has no
// source FileIDs, and its pairing pass ignores refusals, so it holds that the FileID
// pass still runs over the candidate first.
func TestFindLocalMatches_UnresolvableCandidateName_FailsClosedInStrictMode(t *testing.T) {
	for _, tc := range []struct {
		name    string
		reflink bool
	}{
		{name: "hardlinked source"},
		{name: "reflink pairing", reflink: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fileName := "Movie.2023.1080p.WEB.mkv"
			sourceDir, candidateDir := writeHardlinkFixture(t, fileName, !tc.reflink)

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
			service.SetBackendPool(fsops.NewPool(service.instanceStore, local.NewBackend()))
			if tc.reflink {
				service.filesShareAllocation = func(string, string) (bool, error) { return false, nil }
			}

			_, err := service.FindLocalMatches(t.Context(), 1, source.Hash, true)
			require.Error(t, err)
			require.Contains(t, err.Error(), "failed to verify local file relationship")
			require.Contains(t, err.Error(), normalizeHash(hlCandidateHash))
			require.Contains(t, err.Error(), strconv.Quote(candidateName))
		})
	}
}

// A resolved name whose file is not on disk is normal for an incomplete torrent and
// must stay a best-effort skip, or every partial download would trip strict mode.
func TestFindLocalMatches_MissingLocalFile_StaysBestEffort(t *testing.T) {
	files := qbt.TorrentFiles{{Name: "not-downloaded-yet.mkv", Size: 4}}

	response, err := findLocalMatchesForFiles(t, files, nil, true)
	require.NoError(t, err)
	require.Len(t, response.Matches, 1)
	require.Equal(t, matchTypeName, response.Matches[0].MatchType)
}

type lstatErrorBackend struct {
	*local.Backend
	err error
}

func (b lstatErrorBackend) Lstat(_ context.Context, path string) (*fsops.LstatInfo, error) {
	return nil, &fs.PathError{Op: "lstat", Path: path, Err: b.err}
}

// A permission error (a PUID mismatch, say) hides link evidence just like an
// unresolvable name, so strict mode must fail. ENOTDIR means a parent of the path is
// a file: the file is missing, which stays a best-effort skip.
func TestFindLocalMatches_LstatError(t *testing.T) {
	files := qbt.TorrentFiles{{Name: "Movie.2023.1080p.WEB.mkv", Size: 4}}
	for _, tc := range []struct {
		name    string
		err     error
		wantErr bool
	}{
		{name: "permission denied fails closed", err: syscall.EACCES, wantErr: true},
		{name: "not a directory stays best-effort", err: syscall.ENOTDIR},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := localMatchesServiceForFiles(t, files, nil)
			service.SetBackendPool(fsops.NewPool(service.instanceStore, lstatErrorBackend{Backend: local.NewBackend(), err: tc.err}))

			response, err := service.FindLocalMatches(t.Context(), 1, hlSourceHash, true)
			if tc.wantErr {
				require.ErrorIs(t, err, fs.ErrPermission)
				require.Contains(t, err.Error(), "failed to verify local file relationship")
				return
			}
			require.NoError(t, err)
			require.Len(t, response.Matches, 1)
			require.Equal(t, matchTypeName, response.Matches[0].MatchType)
		})
	}
}
