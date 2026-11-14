// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
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
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		syncManager:      &localMatchSyncManager{files: files},
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	svc.SetBackendPool(fsops.NewPool(&mockInstanceStore{
		instances: map[int]*models.Instance{
			1: {ID: 1, HasLocalFilesystemAccess: true},
		},
	}, local.NewBackend()))
	return svc
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
		ctx:                   context.Background(),
		svc:                   svc,
		sourceInstanceID:      1,
		sourceHash:            hlSourceHash,
		sourceSavePath:        sourceDir,
		sourceContentPath:     sourceDir,
		sourceHasFileIdentity: true,
		sourceLocal:           true,
	}
}

func TestFindCrossSeeds_HardlinkedCandidate_UpgradedToHardlink(t *testing.T) {
	fileName := "Movie.2023.1080p.WEB.mkv"
	sourceDir, candidateDir := writeHardlinkFixture(t, fileName, true)

	torrentFiles := qbt.TorrentFiles{{Name: fileName, Size: 4}}
	source := qbt.Torrent{
		Hash:        hlSourceHash,
		Name:        "Movie.2023.1080p.WEB-GROUP",
		SavePath:    sourceDir,
		ContentPath: filepath.Join(sourceDir, fileName),
	}
	svc := pairService(map[string]qbt.TorrentFiles{
		normalizeHash(hlSourceHash):    torrentFiles,
		normalizeHash(hlCandidateHash): torrentFiles,
	}, source, *hardlinkTestCandidate(candidateDir))

	response, err := svc.FindCrossSeeds(t.Context(), 1, hlSourceHash)
	require.NoError(t, err)
	require.Len(t, response.Matches, 1)
	require.Equal(t, matchTypeHardlink, response.Matches[0].MatchType)
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

func TestLocalLinkedMatchType_LinkedSubtitleOnly_NoUpgrade(t *testing.T) {
	// The videos differ. Only the subtitle is a hardlink, and subtitles do not count.
	sourceDir, candidateDir := writeHardlinkFixture(t, "Movie.2023.1080p.WEB.srt", true)
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "Movie.2023.1080p.WEB.mkv"), []byte("data"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(candidateDir, "Movie.2023.1080p.WEB.mkv"), []byte("other!"), 0o600))

	svc := hardlinkTestService(map[string]qbt.TorrentFiles{
		normalizeHash(hlSourceHash):    {{Name: "Movie.2023.1080p.WEB.mkv", Size: 4}, {Name: "Movie.2023.1080p.WEB.srt", Size: 4}},
		normalizeHash(hlCandidateHash): {{Name: "Movie.2023.1080p.WEB.mkv", Size: 6}, {Name: "Movie.2023.1080p.WEB.srt", Size: 4}},
	})
	instance := &models.Instance{ID: 1, HasLocalFilesystemAccess: true}
	matchCtx := hardlinkTestMatchCtx(svc, sourceDir)

	require.Empty(t, svc.localLinkedMatchType(matchCtx, instance, hardlinkTestCandidate(candidateDir)))
	require.NoError(t, matchCtx.verificationErr)
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

	// Source instance lacks trusted file identity.
	matchCtx = hardlinkTestMatchCtx(svc, sourceDir)
	matchCtx.sourceHasFileIdentity = false
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

func TestFindCrossSeeds_MagnetSource_NoError(t *testing.T) {
	// A metadata-less magnet source (empty content path, empty file list) holds no
	// data, so it has no cross-seeds. Its empty file list must not be read as a
	// failed file list, which would fail the tab.
	fileName := "Movie.2023.1080p.WEB.mkv"
	sourceDir, candidateDir := writeHardlinkFixture(t, fileName, true)

	source := qbt.Torrent{
		Hash:     hlSourceHash,
		Name:     "Movie.2023.1080p.WEB-GROUP",
		SavePath: sourceDir,
	}
	svc := pairService(map[string]qbt.TorrentFiles{
		normalizeHash(hlSourceHash):    {},
		normalizeHash(hlCandidateHash): {{Name: fileName, Size: 4}},
	}, source, *hardlinkTestCandidate(candidateDir))

	response, err := svc.FindCrossSeeds(t.Context(), 1, hlSourceHash)
	require.NoError(t, err)
	require.Empty(t, response.Matches)
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

// countingInstanceStore serves the first failAfter Get calls from its map and
// fails every one after that, so a test can pick which backend resolution in a
// call chain fails. localLinkedMatchType resolves backends sequentially in one
// goroutine, so the counter needs no mutex.
type countingInstanceStore struct {
	instances map[int]*models.Instance
	failAfter int
	err       error
	calls     int
}

func (c *countingInstanceStore) Get(_ context.Context, id int) (*models.Instance, error) {
	c.calls++
	if c.calls > c.failAfter {
		return nil, c.err
	}
	instance, ok := c.instances[id]
	if !ok {
		return nil, models.ErrInstanceNotFound
	}
	return instance, nil
}

func TestGetSourceFileIDs_BackendResolutionFailure_RecordedForStrictMode(t *testing.T) {
	// A backend outage discards ALL hardlink evidence for the source instance,
	// so it must be recorded rather than read as "no shared files".
	fileName := "Movie.2023.1080p.WEB.mkv"
	sourceDir, _ := writeHardlinkFixture(t, fileName, true)

	files := qbt.TorrentFiles{{Name: fileName, Size: 4}}
	svc := hardlinkTestService(map[string]qbt.TorrentFiles{normalizeHash(hlSourceHash): files})
	// An empty store cannot resolve instance 1.
	svc.SetBackendPool(fsops.NewPool(&mockInstanceStore{instances: map[int]*models.Instance{}}, local.NewBackend()))

	matchCtx := hardlinkTestMatchCtx(svc, sourceDir)

	require.Nil(t, matchCtx.getSourceFileIDs())
	require.ErrorIs(t, matchCtx.verificationErr, models.ErrInstanceNotFound)
	require.ErrorContains(t, matchCtx.verificationErr, "resolve filesystem backend for instance 1")
}

func TestLocalLinkedMatchType_CandidateBackendFailure_RecordedForStrictMode(t *testing.T) {
	// The source resolves, the candidate's instance does not: without the
	// candidate backend there is no evidence either way, so the check must fail
	// closed instead of returning "not hardlinked".
	fileName := "Movie.2023.1080p.WEB.mkv"
	sourceDir, candidateDir := writeHardlinkFixture(t, fileName, true)

	files := qbt.TorrentFiles{{Name: fileName, Size: 4}}
	svc := hardlinkTestService(map[string]qbt.TorrentFiles{
		normalizeHash(hlSourceHash):    files,
		normalizeHash(hlCandidateHash): files,
	})
	matchCtx := hardlinkTestMatchCtx(svc, sourceDir)

	// The candidate lives on instance 2, which the pool's store does not know.
	candidateInstance := &models.Instance{ID: 2, HasLocalFilesystemAccess: true}
	candidate := hardlinkTestCandidate(candidateDir)
	candidate.InstanceID = candidateInstance.ID
	require.Empty(t, svc.localLinkedMatchType(matchCtx, candidateInstance, candidate))
	require.ErrorIs(t, matchCtx.verificationErr, models.ErrInstanceNotFound)
	require.ErrorContains(t, matchCtx.verificationErr, "resolve filesystem backend for instance 2")
}

func TestLocalLinkedMatchType_SourceBackendReresolveFailure_RecordedForStrictMode(t *testing.T) {
	// No shared inode, so the shared-extent probe runs and re-resolves the source
	// backend. That third resolution is its own failure point and must fail closed
	// like the other two.
	fileName := "Movie.2023.1080p.WEB.mkv"
	sourceDir, candidateDir := writeHardlinkFixture(t, fileName, false)

	files := qbt.TorrentFiles{{Name: fileName, Size: 4}}
	svc := hardlinkTestService(map[string]qbt.TorrentFiles{
		normalizeHash(hlSourceHash):    files,
		normalizeHash(hlCandidateHash): files,
	})
	backendErr := errors.New("backend pool offline")
	store := &countingInstanceStore{
		instances: map[int]*models.Instance{1: {ID: 1, HasLocalFilesystemAccess: true}},
		failAfter: 2, // source (getSourceFileIDs) and candidate succeed; the source re-resolve fails
		err:       backendErr,
	}
	svc.SetBackendPool(fsops.NewPool(store, local.NewBackend()))
	svc.filesShareAllocation = func(string, string) (bool, error) { return false, nil }

	matchCtx := hardlinkTestMatchCtx(svc, sourceDir)
	instance := &models.Instance{ID: 1, HasLocalFilesystemAccess: true}

	require.Empty(t, svc.localLinkedMatchType(matchCtx, instance, hardlinkTestCandidate(candidateDir)))
	// Exact count: it is what places the failure at the re-resolve rather than
	// at an earlier lookup. A refactor that resolves the source backend once and
	// threads it would drop this to 2 and delete the site under test, so this
	// assertion is meant to be re-read, not just re-numbered.
	require.Equal(t, 3, store.calls, "expected source, candidate and source re-resolve backend lookups")
	require.ErrorIs(t, matchCtx.verificationErr, backendErr)
	require.ErrorContains(t, matchCtx.verificationErr, "resolve filesystem backend for instance 1")
}

func TestFindCrossSeeds_BackendFailure_FailsTheCall(t *testing.T) {
	// The recorded backend error has to reach the caller, which is the whole
	// reason the fail-closed sites record it.
	fileName := "shared.mkv"
	files := qbt.TorrentFiles{{Name: fileName, Size: 4}}
	sourceDir, candidateDir := writeIndependentLocalMatchFiles(t, files, files)
	source := qbt.Torrent{
		Hash:        hlSourceHash,
		Name:        "Movie.2023.1080p.WEB-GROUP",
		SavePath:    sourceDir,
		ContentPath: filepath.Join(sourceDir, fileName),
	}
	svc := pairService(map[string]qbt.TorrentFiles{
		normalizeHash(hlSourceHash):    files,
		normalizeHash(hlCandidateHash): files,
	}, source, *hardlinkTestCandidate(candidateDir))
	backendErr := errors.New("backend pool offline")
	// The pool's store fails every resolution, so the very first backend lookup
	// (getSourceFileIDs) records the error.
	svc.SetBackendPool(fsops.NewPool(&countingInstanceStore{err: backendErr}, local.NewBackend()))

	response, err := svc.FindCrossSeeds(t.Context(), 1, source.Hash)

	require.Nil(t, response)
	require.ErrorIs(t, err, backendErr)
	require.ErrorContains(t, err, "check the links of torrent "+hlCandidateHash)
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
	err := forEachLocalFileID(context.Background(), local.NewBackend(), sourceDir, files, func(_ hardlink.FileID, _ uint64) bool {
		visited++
		return true
	})
	require.Equal(t, 1, visited, "only the in-base file should be statted")
	require.Error(t, err, "escaping names must be reported, not silently skipped")

	// Relative or empty save paths are refused outright.
	visited = 0
	require.NoError(t, forEachLocalFileID(context.Background(), local.NewBackend(), "relative/path", files, func(_ hardlink.FileID, _ uint64) bool {
		visited++
		return true
	}))
	require.NoError(t, forEachLocalFileID(context.Background(), local.NewBackend(), "", files, func(_ hardlink.FileID, _ uint64) bool {
		visited++
		return true
	}))
	require.Zero(t, visited)
}

// A delete dialog can add linked matches to the delete, so it counts only
// identity that qui's own kernel reports. Browsing still reads a remote
// instance's identity. The remote backend here is the local one, so the
// files really are hardlinks.
func TestLocalLinkedMatchType_DeleteDialogNeedsTrustedIdentity(t *testing.T) {
	fileName := "Movie.2023.1080p.WEB.mkv"
	sourceDir, candidateDir := writeHardlinkFixture(t, fileName, true)

	torrentFiles := qbt.TorrentFiles{{Name: fileName, Size: 4}}
	svc := hardlinkTestService(map[string]qbt.TorrentFiles{
		normalizeHash(hlSourceHash):    torrentFiles,
		normalizeHash(hlCandidateHash): torrentFiles,
	})
	remoteExec := &models.Instance{ID: 2, SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-hostkey", SSHTier: models.SSHTierExec}
	svc.SetBackendPool(fsops.NewPoolWithRemote(&mockInstanceStore{
		instances: map[int]*models.Instance{
			1: {ID: 1, HasLocalFilesystemAccess: true},
			2: remoteExec,
		},
	}, local.NewBackend(), func(*models.Instance) fsops.Backend { return local.NewBackend() }))
	candidate := hardlinkTestCandidate(candidateDir)
	candidate.InstanceID = 2

	matchCtx := hardlinkTestMatchCtx(svc, sourceDir)
	require.Equal(t, matchTypeHardlink, svc.localLinkedMatchType(matchCtx, remoteExec, candidate), "browsing reads remote identity")

	matchCtx = hardlinkTestMatchCtx(svc, sourceDir)
	matchCtx.trustedIdentityOnly = true
	require.Empty(t, svc.localLinkedMatchType(matchCtx, remoteExec, candidate), "a delete dialog never trusts remote identity")
}

// The delete dialog admits instances from the list it loaded at the start. If
// one left local mode since, its backend now answers from the SSH host, so the
// lookup must fail rather than count that answer. The remote backend here is
// the local one, so the files really are hardlinks.
func TestLocalLinkedMatchType_DeleteDialogRefusesAnInstanceThatLeftLocalMode(t *testing.T) {
	remoteExec := &models.Instance{SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-hostkey", SSHTier: models.SSHTierExec}
	for _, tc := range []struct {
		name   string
		leftID int
	}{
		{name: "source", leftID: 1},
		{name: "candidate", leftID: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fileName := "Movie.2023.1080p.WEB.mkv"
			sourceDir, candidateDir := writeHardlinkFixture(t, fileName, true)

			torrentFiles := qbt.TorrentFiles{{Name: fileName, Size: 4}}
			svc := hardlinkTestService(map[string]qbt.TorrentFiles{
				normalizeHash(hlSourceHash):    torrentFiles,
				normalizeHash(hlCandidateHash): torrentFiles,
			})
			rows := map[int]*models.Instance{
				1: {ID: 1, HasLocalFilesystemAccess: true},
				2: {ID: 2, HasLocalFilesystemAccess: true},
			}
			left := *remoteExec
			left.ID = tc.leftID
			rows[tc.leftID] = &left
			svc.SetBackendPool(fsops.NewPoolWithRemote(&mockInstanceStore{instances: rows},
				local.NewBackend(), func(*models.Instance) fsops.Backend { return local.NewBackend() }))
			candidate := hardlinkTestCandidate(candidateDir)
			candidate.InstanceID = 2

			matchCtx := hardlinkTestMatchCtx(svc, sourceDir)
			matchCtx.trustedIdentityOnly = true
			loaded := &models.Instance{ID: 2, HasLocalFilesystemAccess: true}
			require.Empty(t, svc.localLinkedMatchType(matchCtx, loaded, candidate))
			require.Error(t, matchCtx.verificationErr, "the dialog must fail the check, not read no cross-seeds")
		})
	}
}
