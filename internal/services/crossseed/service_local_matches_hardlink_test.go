// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/hardlink"
	"github.com/autobrr/qui/pkg/stringutils"
)

const (
	hlSourceHash    = "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111"
	hlCandidateHash = "bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222"
)

// writeHardlinkFixture creates a source dir with one file and a candidate dir whose
// file is either a hardlink of the source file or an independent copy.
func writeHardlinkFixture(t *testing.T, fileName string, link bool) (sourceDir, candidateDir string) {
	t.Helper()
	root := t.TempDir()
	sourceDir = filepath.Join(root, "source")
	candidateDir = filepath.Join(root, "candidate")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(candidateDir, 0o755))

	srcFile := filepath.Join(sourceDir, fileName)
	require.NoError(t, os.WriteFile(srcFile, []byte("data"), 0o600))

	candFile := filepath.Join(candidateDir, fileName)
	if link {
		require.NoError(t, os.Link(srcFile, candFile))
	} else {
		require.NoError(t, os.WriteFile(candFile, []byte("data"), 0o600))
	}
	return sourceDir, candidateDir
}

func hardlinkTestService(files map[string]qbt.TorrentFiles) *Service {
	return &Service{
		releaseCache:     NewReleaseCache(),
		syncManager:      &localMatchSyncManager{files: files},
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
}

func hardlinkTestCandidate(candidateDir string) *qbittorrent.CrossInstanceTorrentView {
	return &qbittorrent.CrossInstanceTorrentView{
		TorrentView: &qbittorrent.TorrentView{
			Torrent: &qbt.Torrent{
				Hash:        hlCandidateHash,
				Name:        "Movie.2023.1080p.WEB-GROUP",
				SavePath:    candidateDir,
				ContentPath: filepath.Join(candidateDir, "Movie.2023.1080p.WEB.mkv"),
			},
		},
		InstanceID: 1,
	}
}

func hardlinkTestMatchCtx(svc *Service, sourceDir string) *localMatchContext {
	return &localMatchContext{
		ctx:               context.Background(),
		svc:               svc,
		sourceInstanceID:  1,
		sourceHash:        hlSourceHash,
		sourceSavePath:    sourceDir,
		sourceHasFSAccess: true,
	}
}

func TestMatchTorrentsInInstance_HardlinkedCandidate_UpgradedToHardlink(t *testing.T) {
	fileName := "Movie.2023.1080p.WEB.mkv"
	sourceDir, candidateDir := writeHardlinkFixture(t, fileName, true)

	torrentFiles := qbt.TorrentFiles{{Name: fileName, Size: 4}}
	svc := hardlinkTestService(map[string]qbt.TorrentFiles{
		normalizeHash(hlSourceHash):    torrentFiles,
		normalizeHash(hlCandidateHash): torrentFiles,
	})

	source := &qbt.Torrent{
		Hash:        hlSourceHash,
		Name:        "Movie.2023.1080p.WEB-GROUP",
		SavePath:    sourceDir,
		ContentPath: filepath.Join(sourceDir, fileName),
	}
	instance := &models.Instance{ID: 1, Name: "local", HasLocalFilesystemAccess: true}
	matchCtx := hardlinkTestMatchCtx(svc, sourceDir)

	matches := svc.matchTorrentsInInstance(
		context.Background(), nil, instance,
		[]qbittorrent.CrossInstanceTorrentView{*hardlinkTestCandidate(candidateDir)},
		1, normalizeHash(hlSourceHash),
		source, svc.releaseCache.Parse(source.Name),
		strings.ToLower(normalizePath(source.ContentPath)), matchCtx,
	)

	require.Len(t, matches, 1)
	require.Equal(t, matchTypeHardlink, matches[0].MatchType)
}

func TestLocalLinkedMatchType_SeparateCopy_NoUpgrade(t *testing.T) {
	// Same name and size but an independent copy on disk: no shared inode, no upgrade.
	fileName := "Movie.2023.1080p.WEB.mkv"
	sourceDir, candidateDir := writeHardlinkFixture(t, fileName, false)

	torrentFiles := qbt.TorrentFiles{{Name: fileName, Size: 4}}
	svc := hardlinkTestService(map[string]qbt.TorrentFiles{
		normalizeHash(hlSourceHash):    torrentFiles,
		normalizeHash(hlCandidateHash): torrentFiles,
	})
	instance := &models.Instance{ID: 1, HasLocalFilesystemAccess: true}
	matchCtx := hardlinkTestMatchCtx(svc, sourceDir)

	require.Empty(t, svc.localLinkedMatchType(matchCtx, instance, hardlinkTestCandidate(candidateDir)))
}

func TestLocalLinkedMatchType_NoFilesystemAccess(t *testing.T) {
	fileName := "Movie.2023.1080p.WEB.mkv"
	sourceDir, candidateDir := writeHardlinkFixture(t, fileName, true)

	torrentFiles := qbt.TorrentFiles{{Name: fileName, Size: 4}}
	svc := hardlinkTestService(map[string]qbt.TorrentFiles{
		normalizeHash(hlSourceHash):    torrentFiles,
		normalizeHash(hlCandidateHash): torrentFiles,
	})
	candidate := hardlinkTestCandidate(candidateDir)

	// Candidate instance lacks filesystem access.
	matchCtx := hardlinkTestMatchCtx(svc, sourceDir)
	require.Empty(t, svc.localLinkedMatchType(matchCtx, &models.Instance{ID: 1}, candidate))

	// Source instance lacks filesystem access.
	matchCtx = hardlinkTestMatchCtx(svc, sourceDir)
	matchCtx.sourceHasFSAccess = false
	require.Empty(t, svc.localLinkedMatchType(matchCtx, &models.Instance{ID: 1, HasLocalFilesystemAccess: true}, candidate))
}

func TestLocalLinkedMatchType_CandidateFetchError_RecordedForStrictMode(t *testing.T) {
	fileName := "Movie.2023.1080p.WEB.mkv"
	sourceDir, candidateDir := writeHardlinkFixture(t, fileName, true)

	torrentFiles := qbt.TorrentFiles{{Name: fileName, Size: 4}}
	svc := hardlinkTestService(map[string]qbt.TorrentFiles{
		normalizeHash(hlSourceHash): torrentFiles,
	})
	instance := &models.Instance{ID: 1, HasLocalFilesystemAccess: true}
	matchCtx := hardlinkTestMatchCtx(svc, sourceDir)

	// Prime the source file IDs, then make candidate fetches fail.
	require.NotEmpty(t, matchCtx.getSourceFileIDs())
	fetchErr := errors.New("qbittorrent unavailable")
	svc.syncManager = &localMatchSyncManager{errorOnFetch: fetchErr}

	require.Empty(t, svc.localLinkedMatchType(matchCtx, instance, hardlinkTestCandidate(candidateDir)))
	require.ErrorIs(t, matchCtx.candidateFilesErr, fetchErr)
}

func TestLocalLinkedMatchType_EmptyCandidateFileList_RecordedForStrictMode(t *testing.T) {
	// An empty candidate file list is not evidence of "no hardlinks" - it must trip
	// the strict-mode fail-safe like candidateSharesSourceFiles does.
	fileName := "Movie.2023.1080p.WEB.mkv"
	sourceDir, candidateDir := writeHardlinkFixture(t, fileName, true)

	svc := hardlinkTestService(map[string]qbt.TorrentFiles{
		normalizeHash(hlSourceHash): {{Name: fileName, Size: 4}},
		// Candidate present but with an empty file list.
		normalizeHash(hlCandidateHash): {},
	})
	instance := &models.Instance{ID: 1, HasLocalFilesystemAccess: true}
	matchCtx := hardlinkTestMatchCtx(svc, sourceDir)

	require.Empty(t, svc.localLinkedMatchType(matchCtx, instance, hardlinkTestCandidate(candidateDir)))
	require.ErrorContains(t, matchCtx.candidateFilesErr, "empty file list")
}

func TestMatchTorrentsInInstance_MagnetSource_NoStrictError(t *testing.T) {
	// A metadata-less magnet source (empty content path, empty file list) matching a
	// candidate by name must not attempt the hardlink check: it has no files on disk,
	// and stamping its empty file list as sourceFilesErr would fail strict delete dialogs.
	fileName := "Movie.2023.1080p.WEB.mkv"
	sourceDir, candidateDir := writeHardlinkFixture(t, fileName, true)

	svc := hardlinkTestService(map[string]qbt.TorrentFiles{
		normalizeHash(hlSourceHash):    {},
		normalizeHash(hlCandidateHash): {{Name: fileName, Size: 4}},
	})
	source := &qbt.Torrent{
		Hash:     hlSourceHash,
		Name:     "Movie.2023.1080p.WEB-GROUP",
		SavePath: sourceDir,
	}
	instance := &models.Instance{ID: 1, Name: "local", HasLocalFilesystemAccess: true}
	matchCtx := hardlinkTestMatchCtx(svc, sourceDir)

	matches := svc.matchTorrentsInInstance(
		context.Background(), nil, instance,
		[]qbittorrent.CrossInstanceTorrentView{*hardlinkTestCandidate(candidateDir)},
		1, normalizeHash(hlSourceHash),
		source, svc.releaseCache.Parse(source.Name),
		"", matchCtx,
	)

	require.Len(t, matches, 1)
	require.Equal(t, matchTypeName, matches[0].MatchType)
	require.NoError(t, matchCtx.sourceFilesErr)
	require.NoError(t, matchCtx.candidateFilesErr)
}

func TestLocalLinkedMatchType_MagnetCandidate_SkippedSilently(t *testing.T) {
	// A metadata-less magnet candidate (empty content path) has no files on disk:
	// skip it without recording a strict-mode error for its empty file list.
	fileName := "Movie.2023.1080p.WEB.mkv"
	sourceDir, _ := writeHardlinkFixture(t, fileName, true)

	svc := hardlinkTestService(map[string]qbt.TorrentFiles{
		normalizeHash(hlSourceHash):    {{Name: fileName, Size: 4}},
		normalizeHash(hlCandidateHash): {},
	})
	instance := &models.Instance{ID: 1, HasLocalFilesystemAccess: true}
	matchCtx := hardlinkTestMatchCtx(svc, sourceDir)

	candidate := &qbittorrent.CrossInstanceTorrentView{
		TorrentView: &qbittorrent.TorrentView{
			Torrent: &qbt.Torrent{Hash: hlCandidateHash, Name: "Movie.2023.1080p.WEB-GROUP"},
		},
		InstanceID: 1,
	}

	require.Empty(t, svc.localLinkedMatchType(matchCtx, instance, candidate))
	require.NoError(t, matchCtx.candidateFilesErr)
}

func TestForEachLocalFileID_SkipsUnsafePaths(t *testing.T) {
	fileName := "Movie.2023.1080p.WEB.mkv"
	sourceDir, _ := writeHardlinkFixture(t, fileName, true)

	// Plant a file outside the save path that a traversal name would reach.
	outside := filepath.Join(filepath.Dir(sourceDir), "outside.bin")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o600))

	files := qbt.TorrentFiles{
		{Name: "../outside.bin", Size: 1},
		{Name: "..\\outside.bin", Size: 1},
		{Name: "/etc/passwd", Size: 1},
		{Name: "C:\\windows\\system32\\config", Size: 1},
		{Name: fileName, Size: 4},
	}

	var visited int
	forEachLocalFileID(sourceDir, files, func(_ hardlink.FileID, _ uint64) bool {
		visited++
		return true
	})
	require.Equal(t, 1, visited, "only the in-base file should be statted")

	// Relative or empty save paths are refused outright.
	visited = 0
	forEachLocalFileID("relative/path", files, func(_ hardlink.FileID, _ uint64) bool {
		visited++
		return true
	})
	forEachLocalFileID("", files, func(_ hardlink.FileID, _ uint64) bool {
		visited++
		return true
	})
	require.Zero(t, visited)
}
