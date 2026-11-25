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
)

func findCrossSeedsForFiles(t *testing.T, files, onDisk qbt.TorrentFiles) (*LocalMatchesResponse, error) {
	t.Helper()
	return localMatchesServiceForFiles(t, files, onDisk).FindCrossSeeds(t.Context(), 1, hlSourceHash)
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
	return pairService(map[string]qbt.TorrentFiles{
		normalizeHash(hlSourceHash):    files,
		normalizeHash(hlCandidateHash): files,
	}, source, candidate)
}

func TestFindCrossSeeds_UnresolvableFileName_FailsClosed(t *testing.T) {
	files := qbt.TorrentFiles{{Name: `AC\DC - Back In Black.mkv`, Size: 4}}

	response, err := findCrossSeedsForFiles(t, files, files)
	require.Error(t, err)
	require.Nil(t, response)
	require.Contains(t, err.Error(), "check the links of torrent")
	require.Contains(t, err.Error(), normalizeHash(hlSourceHash))
	require.Contains(t, err.Error(), strconv.Quote(files[0].Name))
}

func TestFindCrossSeeds_UnresolvableCandidateName_FailsClosed(t *testing.T) {
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
			service := pairService(map[string]qbt.TorrentFiles{
				normalizeHash(hlSourceHash):    {{Name: fileName, Size: 4}},
				normalizeHash(hlCandidateHash): {{Name: candidateName, Size: 4}},
			}, source, candidate)
			if tc.reflink {
				service.filesShareAllocation = func(string, string) (bool, error) { return false, nil }
			}

			_, err := service.FindCrossSeeds(t.Context(), 1, source.Hash)
			require.Error(t, err)
			require.Contains(t, err.Error(), "check the links of torrent")
			require.Contains(t, err.Error(), normalizeHash(hlCandidateHash))
			require.Contains(t, err.Error(), strconv.Quote(candidateName))
		})
	}
}

func TestFindCrossSeeds_MissingLocalFile_IsSkipped(t *testing.T) {
	files := qbt.TorrentFiles{{Name: "not-downloaded-yet.mkv", Size: 4}}

	response, err := findCrossSeedsForFiles(t, files, nil)
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

func TestFindCrossSeeds_LstatError(t *testing.T) {
	files := qbt.TorrentFiles{{Name: "Movie.2023.1080p.WEB.mkv", Size: 4}}
	for _, tc := range []struct {
		name    string
		err     error
		wantErr bool
	}{
		{name: "permission denied fails closed", err: syscall.EACCES, wantErr: true},
		{name: "not a directory is skipped", err: syscall.ENOTDIR},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := localMatchesServiceForFiles(t, files, nil)
			service.SetBackendPool(fsops.NewPool(service.instanceStore, lstatErrorBackend{Backend: local.NewBackend(), err: tc.err}))

			response, err := service.FindCrossSeeds(t.Context(), 1, hlSourceHash)
			if tc.wantErr {
				require.ErrorIs(t, err, fs.ErrPermission)
				require.Contains(t, err.Error(), "check the links of torrent")
				return
			}
			require.NoError(t, err)
			require.Len(t, response.Matches, 1)
			require.Equal(t, matchTypeName, response.Matches[0].MatchType)
		})
	}
}
