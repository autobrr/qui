// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

type alreadyRenamedSyncManager struct {
	*alignmentFailureSyncManager
	files         qbt.TorrentFiles
	fileResponses []qbt.TorrentFiles
	fileCalls     int
	filesErr      error
	renameErr     error
	renameCalls   int
}

func (m *alreadyRenamedSyncManager) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	if m.filesErr != nil {
		return nil, m.filesErr
	}

	files := m.files
	if len(m.fileResponses) > 0 {
		idx := m.fileCalls
		if idx >= len(m.fileResponses) {
			idx = len(m.fileResponses) - 1
		}
		files = m.fileResponses[idx]
		m.fileCalls++
	}

	result := make(map[string]qbt.TorrentFiles, len(hashes))
	for _, hash := range hashes {
		cp := make(qbt.TorrentFiles, len(files))
		copy(cp, files)
		result[normalizeHash(hash)] = cp
	}
	return result, nil
}

func (m *alreadyRenamedSyncManager) RenameTorrentFile(context.Context, int, string, string, string) error {
	m.renameCalls++
	return m.renameErr
}

type rootlessFolderAlignmentSyncManager struct {
	*alignmentFailureSyncManager
	files   qbt.TorrentFiles
	renames []fileRenameInstruction
}

func (m *rootlessFolderAlignmentSyncManager) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	result := make(map[string]qbt.TorrentFiles, len(hashes))
	for _, hash := range hashes {
		cp := make(qbt.TorrentFiles, len(m.files))
		copy(cp, m.files)
		result[normalizeHash(hash)] = cp
	}
	return result, nil
}

func (m *rootlessFolderAlignmentSyncManager) RenameTorrentFile(_ context.Context, _ int, _, oldPath, newPath string) error {
	m.renames = append(m.renames, fileRenameInstruction{oldPath: oldPath, newPath: newPath})
	for i := range m.files {
		if m.files[i].Name == oldPath {
			m.files[i].Name = newPath
		}
	}
	return nil
}

func TestRenameFileWithVerificationTreatsAlreadyRenamedAPIErrorAsSuccess(t *testing.T) {
	t.Parallel()

	const (
		hash    = "201984dc31d8f7f719a14087b8f97cf52267ce8a"
		oldPath = "Scene.Movie.2017.1080p.BluRay.x264-GROUP.mkv"
		newPath = "scene.movie.2017.limited.1080p.bluray.x264-group.mkv"
	)

	sync := &alreadyRenamedSyncManager{
		alignmentFailureSyncManager: &alignmentFailureSyncManager{},
		files:                       qbt.TorrentFiles{{Name: newPath, Size: 7038266663}},
		renameErr: errors.New(
			"failed to rename file: oldPath: " + oldPath + " | newPath: " + newPath +
				": invalid newPath or oldPath, or newPath already in use",
		),
	}
	service := &Service{syncManager: sync}

	require.True(t, service.renameFileWithVerification(context.Background(), 1, hash, oldPath, newPath))
	require.Equal(t, 1, sync.renameCalls)
}

func TestRenameFileWithVerificationDoesNotTreatUnverifiableAPIErrorAsSuccess(t *testing.T) {
	t.Parallel()

	const (
		hash    = "201984dc31d8f7f719a14087b8f97cf52267ce8a"
		oldPath = "Scene.Movie.2017.1080p.BluRay.x264-GROUP.mkv"
		newPath = "scene.movie.2017.limited.1080p.bluray.x264-group.mkv"
	)

	sync := &alreadyRenamedSyncManager{
		alignmentFailureSyncManager: &alignmentFailureSyncManager{},
		filesErr:                    errors.New("file list unavailable"),
		renameErr: errors.New(
			"failed to rename file: oldPath: " + oldPath + " | newPath: " + newPath +
				": invalid newPath or oldPath, or newPath already in use",
		),
	}
	service := &Service{syncManager: sync}

	require.False(t, service.renameFileWithVerification(context.Background(), 1, hash, oldPath, newPath))
	require.Equal(t, 3, sync.renameCalls)
}

func TestRenameFileWithVerificationPollsUntilAsyncRenameAppears(t *testing.T) {
	t.Parallel()

	const (
		hash    = "201984dc31d8f7f719a14087b8f97cf52267ce8a"
		oldPath = "Scene.Movie.2017.1080p.BluRay.x264-GROUP.mkv"
		newPath = "scene.movie.2017.limited.1080p.bluray.x264-group.mkv"
	)

	sync := &alreadyRenamedSyncManager{
		alignmentFailureSyncManager: &alignmentFailureSyncManager{},
		fileResponses: []qbt.TorrentFiles{
			{{Name: oldPath, Size: 7038266663}},
			{{Name: newPath, Size: 7038266663}},
		},
	}
	service := &Service{syncManager: sync}

	require.True(t, service.renameFileWithVerification(context.Background(), 1, hash, oldPath, newPath))
	require.Equal(t, 1, sync.renameCalls)
	require.GreaterOrEqual(t, sync.fileCalls, 2)
}

func TestAlignCrossSeedContentPaths_RootlessSourceIntoMatchedFolderRenamesBasenameOnly(t *testing.T) {
	t.Parallel()

	const (
		hash         = "newhash"
		sourceName   = "Movie.2015.1080p.BluRay.x264-GROUP.mkv"
		sourcePath   = "Movie.2015.1080p.BluRay.x264-GROUP.mkv"
		candidateDir = "Movie.2015.LIMITED.1080p.BluRay.x264-GROUP"
		targetName   = "Movie.2015.LIMITED.1080p.BluRay.x264-GROUP.mkv"
	)

	sync := &rootlessFolderAlignmentSyncManager{
		alignmentFailureSyncManager: &alignmentFailureSyncManager{},
		files:                       qbt.TorrentFiles{{Name: sourcePath, Size: 4096}},
	}
	service := &Service{
		syncManager:      sync,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	sourceFiles := qbt.TorrentFiles{{Name: sourcePath, Size: 4096}}
	candidateFiles := qbt.TorrentFiles{{Name: candidateDir + "/" + targetName, Size: 4096}}
	matchedTorrent := qbt.Torrent{Hash: "matchedhash", Name: candidateDir}
	plan := buildCrossSeedAddPlan(
		matchedTorrent,
		candidateFiles,
		"single-file",
		service.releaseCache.Parse(sourceName),
		service.releaseCache.Parse(candidateDir),
		sourceFiles,
	)
	ok, activeHash := service.alignCrossSeedContentPaths(
		context.Background(),
		1,
		hash,
		"",
		sourceName,
		&plan,
	)

	require.True(t, ok)
	require.Equal(t, hash, activeHash)
	require.Equal(t, []fileRenameInstruction{{oldPath: sourcePath, newPath: targetName}}, sync.renames)
}
