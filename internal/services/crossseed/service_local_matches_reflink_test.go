// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/reflinktree"
	"github.com/autobrr/qui/pkg/sharedextents"
)

func TestMatchTorrentsInInstance_ReflinkCandidates(t *testing.T) {
	tests := []struct {
		name          string
		sourceName    string
		candidateName string
		wantInitial   string
	}{
		{
			name:          "name match",
			sourceName:    "Movie.2023.1080p.WEB-GROUP",
			candidateName: "Movie.2023.1080p.WEB-GROUP",
			wantInitial:   matchTypeName,
		},
		{
			name:          "release match",
			sourceName:    "Show.S01E05.1080p.WEB-DL-GROUP",
			candidateName: "Show S01E05 1080p WEB-DL-GROUP",
			wantInitial:   matchTypeRelease,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fileName := "shared.mkv"
			sourceDir, candidateDir := writeIndependentLocalMatchFiles(
				t,
				qbt.TorrentFiles{{Name: fileName, Size: 4}},
				qbt.TorrentFiles{{Name: fileName, Size: 4}},
			)
			service := hardlinkTestService(map[string]qbt.TorrentFiles{
				normalizeHash(hlSourceHash):    {{Name: fileName, Size: 4}},
				normalizeHash(hlCandidateHash): {{Name: fileName, Size: 4}},
			})
			var queries int
			service.filesShareAllocation = func(sourcePath, candidatePath string) (bool, error) {
				queries++
				require.Equal(t, filepath.Join(sourceDir, fileName), sourcePath)
				require.Equal(t, filepath.Join(candidateDir, fileName), candidatePath)
				return true, nil
			}

			source := &qbt.Torrent{
				Hash:        hlSourceHash,
				Name:        tt.sourceName,
				SavePath:    sourceDir,
				ContentPath: filepath.Join(sourceDir, fileName),
			}
			candidate := hardlinkTestCandidate(candidateDir)
			candidate.Name = tt.candidateName
			initial := service.determineLocalMatchType(
				source,
				service.releaseCache.Parse(source.Name),
				candidate,
				normalizePathForComparison(source.ContentPath),
				hardlinkTestMatchCtx(service, sourceDir),
			)
			require.Equal(t, tt.wantInitial, initial)

			matches := service.matchTorrentsInInstance(
				context.Background(),
				nil,
				&models.Instance{ID: 1, Name: "local", HasLocalFilesystemAccess: true},
				[]qbittorrent.CrossInstanceTorrentView{*candidate},
				1,
				normalizeHash(source.Hash),
				source,
				service.releaseCache.Parse(source.Name),
				normalizePathForComparison(source.ContentPath),
				hardlinkTestMatchCtx(service, sourceDir),
			)

			require.Len(t, matches, 1)
			require.Equal(t, matchTypeReflink, matches[0].MatchType)
			require.Equal(t, 1, queries)
		})
	}
}

func TestLocalLinkedMatchType_HardlinkPrecedesReflink(t *testing.T) {
	fileName := "shared.mkv"
	sourceDir, candidateDir := writeHardlinkFixture(t, fileName, true)
	files := qbt.TorrentFiles{{Name: fileName, Size: 4}}
	service := hardlinkTestService(map[string]qbt.TorrentFiles{
		normalizeHash(hlSourceHash):    files,
		normalizeHash(hlCandidateHash): files,
	})
	var queries int
	service.filesShareAllocation = func(string, string) (bool, error) {
		queries++
		return true, nil
	}

	matchType := service.localLinkedMatchType(
		hardlinkTestMatchCtx(service, sourceDir),
		&models.Instance{ID: 1, HasLocalFilesystemAccess: true},
		hardlinkTestCandidate(candidateDir),
	)
	require.Equal(t, matchTypeHardlink, matchType)
	require.Zero(t, queries)
}

func TestMatchTorrentsInInstance_MetadataLessSkipsLinkedVerification(t *testing.T) {
	tests := []struct {
		name                 string
		sourceHasContentPath bool
		candidateHasContent  bool
	}{
		{
			name:                "source",
			candidateHasContent: true,
		},
		{
			name:                 "candidate",
			sourceHasContentPath: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fileName := "shared.mkv"
			files := qbt.TorrentFiles{{Name: fileName, Size: 4}}
			sourceDir, candidateDir := writeIndependentLocalMatchFiles(t, files, files)
			service := hardlinkTestService(map[string]qbt.TorrentFiles{
				normalizeHash(hlSourceHash):    files,
				normalizeHash(hlCandidateHash): files,
			})
			var queries int
			service.filesShareAllocation = func(string, string) (bool, error) {
				queries++
				return true, nil
			}

			source := &qbt.Torrent{
				Hash:     hlSourceHash,
				Name:     "Movie.2023.1080p.WEB-GROUP",
				SavePath: sourceDir,
			}
			if tt.sourceHasContentPath {
				source.ContentPath = filepath.Join(sourceDir, fileName)
			}
			candidate := hardlinkTestCandidate(candidateDir)
			candidate.Name = source.Name
			if !tt.candidateHasContent {
				candidate.ContentPath = ""
			}
			matchCtx := hardlinkTestMatchCtx(service, sourceDir)

			matches := service.matchTorrentsInInstance(
				context.Background(),
				nil,
				&models.Instance{ID: 1, Name: "local", HasLocalFilesystemAccess: true},
				[]qbittorrent.CrossInstanceTorrentView{*candidate},
				1,
				normalizeHash(source.Hash),
				source,
				service.releaseCache.Parse(source.Name),
				normalizePathForComparison(source.ContentPath),
				matchCtx,
			)

			require.Len(t, matches, 1)
			require.Equal(t, matchTypeName, matches[0].MatchType)
			require.Zero(t, queries)
			require.NoError(t, matchCtx.sourceFilesErr)
			require.NoError(t, matchCtx.candidateFilesErr)
			require.NoError(t, matchCtx.verificationErr)
		})
	}
}

func TestLocalLinkedMatchType_ConservativePairing(t *testing.T) {
	tests := []struct {
		name           string
		sourceFiles    qbt.TorrentFiles
		candidateFiles qbt.TorrentFiles
		wantQueries    int
		wantMatch      bool
	}{
		{
			name:           "exact path and size",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie/file.mkv", Size: 4}},
			candidateFiles: qbt.TorrentFiles{{Name: "movie/FILE.mkv", Size: 4}},
			wantQueries:    1,
			wantMatch:      true,
		},
		{
			name:           "common root layout difference",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie/file.mkv", Size: 4}},
			candidateFiles: qbt.TorrentFiles{{Name: "file.mkv", Size: 4}},
			wantQueries:    1,
			wantMatch:      true,
		},
		{
			name:           "same basename different size",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie/file.mkv", Size: 4}},
			candidateFiles: qbt.TorrentFiles{{Name: "file.mkv", Size: 5}},
		},
		{
			name:           "slightly different basename",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie/file.mkv", Size: 4}},
			candidateFiles: qbt.TorrentFiles{{Name: "file-remux.mkv", Size: 4}},
		},
		{
			name: "duplicate basename ambiguity",
			sourceFiles: qbt.TorrentFiles{
				{Name: "A/file.mkv", Size: 4},
				{Name: "B/file.mkv", Size: 4},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "C/file.mkv", Size: 4},
				{Name: "D/file.mkv", Size: 4},
			},
		},
		{
			name: "exact path wins despite duplicate basename",
			sourceFiles: qbt.TorrentFiles{
				{Name: "A/file.mkv", Size: 4},
				{Name: "B/file.mkv", Size: 4},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "A/file.mkv", Size: 4},
				{Name: "C/file.mkv", Size: 4},
			},
			wantQueries: 1,
			wantMatch:   true,
		},
		{
			name:           "zero length skipped",
			sourceFiles:    qbt.TorrentFiles{{Name: "file.mkv", Size: 0}},
			candidateFiles: qbt.TorrentFiles{{Name: "file.mkv", Size: 0}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceDir, candidateDir := writeIndependentLocalMatchFiles(t, tt.sourceFiles, tt.candidateFiles)
			service := hardlinkTestService(map[string]qbt.TorrentFiles{
				normalizeHash(hlSourceHash):    tt.sourceFiles,
				normalizeHash(hlCandidateHash): tt.candidateFiles,
			})
			var queries int
			service.filesShareAllocation = func(string, string) (bool, error) {
				queries++
				return true, nil
			}

			matchType := service.localLinkedMatchType(
				hardlinkTestMatchCtx(service, sourceDir),
				&models.Instance{ID: 1, HasLocalFilesystemAccess: true},
				hardlinkTestCandidate(candidateDir),
			)
			require.Equal(t, tt.wantQueries, queries)
			if tt.wantMatch {
				require.Equal(t, matchTypeReflink, matchType)
			} else {
				require.Empty(t, matchType)
			}
		})
	}
}

func TestLocalLinkedMatchType_SkipsSamePhysicalFile(t *testing.T) {
	root := t.TempDir()
	releaseDir := filepath.Join(root, "Release")
	require.NoError(t, os.Mkdir(releaseDir, 0o700))
	fileName := "Release.mkv"
	require.NoError(t, os.WriteFile(filepath.Join(releaseDir, fileName), []byte("data"), 0o600))

	sourceFiles := qbt.TorrentFiles{{Name: path.Join("Release", fileName), Size: 4}}
	candidateFiles := qbt.TorrentFiles{{Name: fileName, Size: 4}}
	service := hardlinkTestService(map[string]qbt.TorrentFiles{
		normalizeHash(hlSourceHash):    sourceFiles,
		normalizeHash(hlCandidateHash): candidateFiles,
	})
	var queries int
	service.filesShareAllocation = func(string, string) (bool, error) {
		queries++
		return true, nil
	}

	matchType := service.localLinkedMatchType(
		hardlinkTestMatchCtx(service, root),
		&models.Instance{ID: 1, HasLocalFilesystemAccess: true},
		hardlinkTestCandidate(releaseDir),
	)
	require.Empty(t, matchType)
	require.Zero(t, queries)
}

func TestLocalLinkedMatchType_SkipsUnsafePaths(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "source", "save")
	candidateDir := filepath.Join(root, "candidate", "save")
	require.NoError(t, os.MkdirAll(sourceDir, 0o700))
	require.NoError(t, os.MkdirAll(candidateDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "source", "outside.mkv"), []byte("data"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "candidate", "outside.mkv"), []byte("data"), 0o600))

	for _, unsafeName := range []string{
		"../outside.mkv",
		`..\outside.mkv`,
	} {
		t.Run(strings.ReplaceAll(unsafeName, "/", "_"), func(t *testing.T) {
			sourceFiles := qbt.TorrentFiles{{Name: unsafeName, Size: 4}}
			candidateFiles := qbt.TorrentFiles{{Name: unsafeName, Size: 4}}
			service := hardlinkTestService(map[string]qbt.TorrentFiles{
				normalizeHash(hlSourceHash):    sourceFiles,
				normalizeHash(hlCandidateHash): candidateFiles,
			})
			var queries int
			service.filesShareAllocation = func(string, string) (bool, error) {
				queries++
				return true, nil
			}

			matchType := service.localLinkedMatchType(
				hardlinkTestMatchCtx(service, sourceDir),
				&models.Instance{ID: 1, HasLocalFilesystemAccess: true},
				hardlinkTestCandidate(candidateDir),
			)
			require.Empty(t, matchType)
			require.Zero(t, queries)
		})
	}
}

func TestLocalLinkedMatchType_GatesAndErrors(t *testing.T) {
	fileName := "shared.mkv"
	files := qbt.TorrentFiles{{Name: fileName, Size: 4}}
	sourceDir, candidateDir := writeIndependentLocalMatchFiles(t, files, files)

	t.Run("no filesystem access", func(t *testing.T) {
		service := hardlinkTestService(map[string]qbt.TorrentFiles{
			normalizeHash(hlSourceHash):    files,
			normalizeHash(hlCandidateHash): files,
		})
		var queries int
		service.filesShareAllocation = func(string, string) (bool, error) {
			queries++
			return true, nil
		}
		matchType := service.localLinkedMatchType(
			hardlinkTestMatchCtx(service, sourceDir),
			&models.Instance{ID: 1},
			hardlinkTestCandidate(candidateDir),
		)
		require.Empty(t, matchType)
		require.Zero(t, queries)
	})

	t.Run("unsupported filesystem", func(t *testing.T) {
		files := qbt.TorrentFiles{
			{Name: "first.mkv", Size: 4},
			{Name: "second.mkv", Size: 4},
		}
		sourceDir, candidateDir := writeIndependentLocalMatchFiles(t, files, files)
		service := hardlinkTestService(map[string]qbt.TorrentFiles{
			normalizeHash(hlSourceHash):    files,
			normalizeHash(hlCandidateHash): files,
		})
		var queries int
		service.filesShareAllocation = func(string, string) (bool, error) {
			queries++
			if queries == 1 {
				return false, sharedextents.ErrUnsupported
			}
			return true, nil
		}
		matchCtx := hardlinkTestMatchCtx(service, sourceDir)
		matchType := service.localLinkedMatchType(
			matchCtx,
			&models.Instance{ID: 1, HasLocalFilesystemAccess: true},
			hardlinkTestCandidate(candidateDir),
		)
		require.Equal(t, matchTypeReflink, matchType)
		require.Equal(t, 2, queries)
		require.NoError(t, matchCtx.verificationErr)
	})

	t.Run("different volume result", func(t *testing.T) {
		service := hardlinkTestService(map[string]qbt.TorrentFiles{
			normalizeHash(hlSourceHash):    files,
			normalizeHash(hlCandidateHash): files,
		})
		service.filesShareAllocation = func(string, string) (bool, error) {
			return false, nil
		}
		matchCtx := hardlinkTestMatchCtx(service, sourceDir)
		matchType := service.localLinkedMatchType(
			matchCtx,
			&models.Instance{ID: 1, HasLocalFilesystemAccess: true},
			hardlinkTestCandidate(candidateDir),
		)
		require.Empty(t, matchType)
		require.NoError(t, matchCtx.verificationErr)
	})

	t.Run("unexpected verification failure", func(t *testing.T) {
		queryErr := errors.New("retrieval pointer failure")
		service := hardlinkTestService(map[string]qbt.TorrentFiles{
			normalizeHash(hlSourceHash):    files,
			normalizeHash(hlCandidateHash): files,
		})
		service.filesShareAllocation = func(string, string) (bool, error) {
			return false, queryErr
		}
		matchCtx := hardlinkTestMatchCtx(service, sourceDir)
		matchType := service.localLinkedMatchType(
			matchCtx,
			&models.Instance{ID: 1, HasLocalFilesystemAccess: true},
			hardlinkTestCandidate(candidateDir),
		)
		require.Empty(t, matchType)
		require.ErrorIs(t, matchCtx.verificationErr, queryErr)
	})
}

type reflinkFindLocalMatchesSyncManager struct {
	localMatchSyncManager
	source    qbt.Torrent
	candidate qbittorrent.CrossInstanceTorrentView
}

//nolint:gocritic // Interface requires value type for TorrentFilterOptions.
func (m *reflinkFindLocalMatchesSyncManager) GetTorrents(
	_ context.Context,
	_ int,
	_ qbt.TorrentFilterOptions,
) ([]qbt.Torrent, error) {
	return []qbt.Torrent{m.source}, nil
}

func (m *reflinkFindLocalMatchesSyncManager) GetCachedInstanceTorrents(
	_ context.Context,
	_ int,
) ([]qbittorrent.CrossInstanceTorrentView, error) {
	return []qbittorrent.CrossInstanceTorrentView{m.candidate}, nil
}

func TestFindLocalMatches_ReflinkVerificationErrorPolicy(t *testing.T) {
	fileName := "shared.mkv"
	files := qbt.TorrentFiles{{Name: fileName, Size: 4}}
	sourceDir, candidateDir := writeIndependentLocalMatchFiles(t, files, files)
	source := qbt.Torrent{
		Hash:        hlSourceHash,
		Name:        "Movie.2023.1080p.WEB-GROUP",
		SavePath:    sourceDir,
		ContentPath: filepath.Join(sourceDir, fileName),
	}
	candidate := *hardlinkTestCandidate(candidateDir)
	syncManager := &reflinkFindLocalMatchesSyncManager{
		files: map[string]qbt.TorrentFiles{
			normalizeHash(hlSourceHash):    files,
			normalizeHash(hlCandidateHash): files,
		},
		source:    source,
		candidate: candidate,
	}
	queryErr := errors.New("retrieval pointer failure")
	service := &Service{
		instanceStore: newOrderedInstanceStore(&models.Instance{ID: 1, Name: "local", IsActive: true, HasLocalFilesystemAccess: true}),
		syncManager:   syncManager,
		releaseCache:  NewReleaseCache(),
		filesShareAllocation: func(string, string) (bool, error) {
			return false, queryErr
		},
	}
	service.SetBackendPool(fsops.NewPool(service.instanceStore, local.NewBackend()))

	response, err := service.FindLocalMatches(context.Background(), 1, source.Hash, false)
	require.NoError(t, err)
	require.Len(t, response.Matches, 1)
	require.Equal(t, matchTypeName, response.Matches[0].MatchType)

	response, err = service.FindLocalMatches(context.Background(), 1, source.Hash, true)
	require.Nil(t, response)
	require.ErrorIs(t, err, queryErr)
}

func TestLocalLinkedMatchTypeReFS(t *testing.T) {
	root := os.Getenv("QUI_REFS_TEST_DIR")
	if root == "" {
		t.Skip("QUI_REFS_TEST_DIR is not set")
	}

	cleanRoot, err := filepath.Abs(root)
	require.NoError(t, err)
	testRoot, err := os.MkdirTemp(cleanRoot, "qui-crossseed-reflink-")
	require.NoError(t, err)
	testRoot, err = filepath.Abs(testRoot)
	require.NoError(t, err)
	relativeTestRoot, err := filepath.Rel(cleanRoot, testRoot)
	require.NoError(t, err)
	require.NotEqual(t, "..", relativeTestRoot)
	require.False(t, strings.HasPrefix(relativeTestRoot, ".."+string(filepath.Separator)))
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(testRoot))
	})

	fileName := "shared.mkv"
	sourceDir := filepath.Join(testRoot, "source")
	cloneDir := filepath.Join(testRoot, "clone")
	copyDir := filepath.Join(testRoot, "copy")
	hardlinkDir := filepath.Join(testRoot, "hardlink")
	for _, dir := range []string{sourceDir, cloneDir, copyDir, hardlinkDir} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}

	data := make([]byte, 3*64*1024)
	_, err = rand.Read(data)
	require.NoError(t, err)
	sourcePath := filepath.Join(sourceDir, fileName)
	clonePath := filepath.Join(cloneDir, fileName)
	copyPath := filepath.Join(copyDir, fileName)
	hardlinkPath := filepath.Join(hardlinkDir, fileName)
	require.NoError(t, os.WriteFile(sourcePath, data, 0o600))
	require.NoError(t, os.WriteFile(copyPath, data, 0o600))
	require.NoError(t, os.Link(sourcePath, hardlinkPath))
	_, err = reflinktree.Create(&hardlinktree.TreePlan{
		RootDir: cloneDir,
		Files: []hardlinktree.FilePlan{{
			SourcePath: sourcePath,
			TargetPath: clonePath,
		}},
	})
	require.NoError(t, err)

	files := qbt.TorrentFiles{{Name: fileName, Size: int64(len(data))}}
	service := hardlinkTestService(map[string]qbt.TorrentFiles{
		normalizeHash(hlSourceHash):    files,
		normalizeHash(hlCandidateHash): files,
	})
	instance := &models.Instance{ID: 1, HasLocalFilesystemAccess: true}

	require.Equal(t, matchTypeReflink, service.localLinkedMatchType(
		hardlinkTestMatchCtx(service, sourceDir),
		instance,
		hardlinkTestCandidate(cloneDir),
	))
	require.Empty(t, service.localLinkedMatchType(
		hardlinkTestMatchCtx(service, sourceDir),
		instance,
		hardlinkTestCandidate(copyDir),
	))
	require.Equal(t, matchTypeHardlink, service.localLinkedMatchType(
		hardlinkTestMatchCtx(service, sourceDir),
		instance,
		hardlinkTestCandidate(hardlinkDir),
	))

	cloneFile, err := os.OpenFile(clonePath, os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = cloneFile.WriteAt([]byte{0xff}, 0)
	require.NoError(t, err)
	require.NoError(t, cloneFile.Sync())
	require.NoError(t, cloneFile.Close())
	require.Equal(t, matchTypeReflink, service.localLinkedMatchType(
		hardlinkTestMatchCtx(service, sourceDir),
		instance,
		hardlinkTestCandidate(cloneDir),
	))

	replacement := make([]byte, len(data))
	_, err = rand.Read(replacement)
	require.NoError(t, err)
	cloneFile, err = os.OpenFile(clonePath, os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = cloneFile.WriteAt(replacement, 0)
	require.NoError(t, err)
	require.NoError(t, cloneFile.Sync())
	require.NoError(t, cloneFile.Close())
	require.Empty(t, service.localLinkedMatchType(
		hardlinkTestMatchCtx(service, sourceDir),
		instance,
		hardlinkTestCandidate(cloneDir),
	))
}

func writeIndependentLocalMatchFiles(
	t *testing.T,
	sourceFiles qbt.TorrentFiles,
	candidateFiles qbt.TorrentFiles,
) (string, string) {
	t.Helper()

	root := t.TempDir()
	sourceDir := filepath.Join(root, "source")
	candidateDir := filepath.Join(root, "candidate")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(candidateDir, 0o755))
	writeLocalMatchFiles(t, sourceDir, sourceFiles)
	writeLocalMatchFiles(t, candidateDir, candidateFiles)
	return sourceDir, candidateDir
}

func writeLocalMatchFiles(t *testing.T, base string, files qbt.TorrentFiles) {
	t.Helper()

	for _, file := range files {
		if _, ok := resolveLocalTorrentFile(base, file.Name); !ok {
			continue
		}
		fullPath := filepath.Join(base, filepath.FromSlash(strings.ReplaceAll(file.Name, `\`, "/")))
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
		require.NoError(t, os.WriteFile(fullPath, []byte("data"), 0o600))
	}
}
