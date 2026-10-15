// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

const (
	tierSourceHash    = "1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa"
	tierCandidateHash = "2222bbbb2222bbbb2222bbbb2222bbbb2222bbbb"
	tierSourceName    = "Lantern.Bay.2023.1080p.BluRay.x264-GRP"
	tierMovieFile     = "Lantern.Bay.2023.1080p.BluRay.x264-GRP.mkv"
)

// tierSyncManager serves one source torrent, the candidates of each instance,
// and file lists. A hash in failHashes fails its file fetch.
type tierSyncManager struct {
	localMatchSyncManager
	source      qbt.Torrent
	candidates  map[int][]qbittorrent.CrossInstanceTorrentView
	failHashes  map[string]bool
	snapshotErr error
}

//nolint:gocritic // Interface requires value type for TorrentFilterOptions.
func (m *tierSyncManager) GetTorrents(context.Context, int, qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	return []qbt.Torrent{m.source}, nil
}

func (m *tierSyncManager) GetCachedInstanceTorrents(_ context.Context, instanceID int) ([]qbittorrent.CrossInstanceTorrentView, error) {
	return m.candidates[instanceID], m.snapshotErr
}

func (m *tierSyncManager) GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	for _, h := range hashes {
		if m.failHashes[normalizeHash(h)] {
			return nil, errors.New("qbittorrent unavailable")
		}
	}
	return m.localMatchSyncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
}

func tierCandidate(instanceID int, name, savePath, contentPath string) qbittorrent.CrossInstanceTorrentView {
	return qbittorrent.CrossInstanceTorrentView{
		TorrentView: &qbittorrent.TorrentView{
			Torrent: &qbt.Torrent{
				Hash:        tierCandidateHash,
				Name:        name,
				SavePath:    savePath,
				ContentPath: contentPath,
			},
		},
		InstanceID: instanceID,
	}
}

// tierService builds a service over two remote instances without filesystem
// access, so only the file lists decide the tier.
func tierService(sm *tierSyncManager) *Service {
	return &Service{
		instanceStore: newOrderedInstanceStore(
			&models.Instance{ID: 1, Name: "one", IsActive: true},
			&models.Instance{ID: 2, Name: "two", IsActive: true},
		),
		syncManager:  sm,
		releaseCache: NewReleaseCache(),
	}
}

func tierSingleFileSource() qbt.Torrent {
	return qbt.Torrent{
		Hash:        tierSourceHash,
		Name:        tierSourceName,
		SavePath:    "/data/movies",
		ContentPath: "/data/movies/" + tierMovieFile,
	}
}

func findTier(t *testing.T, sm *tierSyncManager) []LocalMatch {
	t.Helper()
	response, err := tierService(sm).FindLocalMatches(t.Context(), 1, tierSourceHash, true)
	require.NoError(t, err)
	return response.Matches
}

func requireTier(t *testing.T, matches []LocalMatch, want string) {
	t.Helper()
	if want == "" {
		require.Empty(t, matches)
		return
	}
	require.Len(t, matches, 1)
	require.Equal(t, want, matches[0].MatchType)
}

func TestFindLocalMatches_Tiers(t *testing.T) {
	sourceFiles := qbt.TorrentFiles{{Name: tierMovieFile, Size: 1000}}
	withNfo := qbt.TorrentFiles{{Name: tierMovieFile, Size: 1000}, {Name: "Lantern.Bay.2023.1080p.BluRay.x264-GRP.nfo", Size: 3}}
	// The candidate name differs from the source name only in its separators,
	// so it matches on release metadata, not on the name.
	const releaseName = "Lantern Bay 2023 1080p BluRay x264-GRP"

	tests := []struct {
		name           string
		source         qbt.Torrent
		sourceFiles    qbt.TorrentFiles
		candidate      qbittorrent.CrossInstanceTorrentView
		candidateFiles qbt.TorrentFiles
		want           string
	}{
		{
			name:        "same folder with an nfo and no subfolder shares the mkv",
			source:      tierSingleFileSource(),
			sourceFiles: sourceFiles,
			// No subfolder: qBittorrent reports the save path as the content path.
			candidate:      tierCandidate(1, releaseName, "/data/movies", "/data/movies"),
			candidateFiles: withNfo,
			want:           matchTypeContentPath,
		},
		{
			name:           "different name at the same content path shares the mkv",
			source:         tierSingleFileSource(),
			sourceFiles:    sourceFiles,
			candidate:      tierCandidate(1, "Harbor.Lights.2021.720p.WEB.h264-OTHER", "/data/movies", "/data/movies/"+tierMovieFile),
			candidateFiles: sourceFiles,
			want:           matchTypeContentPath,
		},
		{
			name: "subfolder layout shares the mkv",
			source: qbt.Torrent{
				Hash:        tierSourceHash,
				Name:        tierSourceName,
				SavePath:    "/data/movies/Lantern.Bay",
				ContentPath: "/data/movies/Lantern.Bay/" + tierMovieFile,
			},
			sourceFiles: sourceFiles,
			candidate:   tierCandidate(1, releaseName, "/data/movies", "/data/movies/Lantern.Bay"),
			candidateFiles: qbt.TorrentFiles{
				{Name: "Lantern.Bay/" + tierMovieFile, Size: 1000},
				{Name: "Lantern.Bay/Lantern.Bay.nfo", Size: 3},
			},
			want: matchTypeContentPath,
		},
		{
			name: "episode inside the folder of its season pack shares the file",
			source: qbt.Torrent{
				Hash:        tierSourceHash,
				Name:        "Harbor.Watch.S02E01.1080p.WEB.h264-GRP",
				SavePath:    "/data/tv/Harbor.Watch.S02",
				ContentPath: "/data/tv/Harbor.Watch.S02/Harbor.Watch.S02E01.mkv",
			},
			sourceFiles: qbt.TorrentFiles{{Name: "Harbor.Watch.S02E01.mkv", Size: 1000}},
			candidate:   tierCandidate(1, "Harbor.Watch.S02.1080p.WEB.h264-GRP", "/data/tv", "/data/tv/Harbor.Watch.S02"),
			candidateFiles: qbt.TorrentFiles{
				{Name: "Harbor.Watch.S02/Harbor.Watch.S02E01.mkv", Size: 1000},
				{Name: "Harbor.Watch.S02/Harbor.Watch.S02E02.mkv", Size: 1000},
			},
			want: matchTypeContentPath,
		},
		{
			name: "season pack shares the file of an episode inside its folder",
			source: qbt.Torrent{
				Hash:        tierSourceHash,
				Name:        "Harbor.Watch.S02.1080p.WEB.h264-GRP",
				SavePath:    "/data/tv",
				ContentPath: "/data/tv/Harbor.Watch.S02",
			},
			sourceFiles: qbt.TorrentFiles{
				{Name: "Harbor.Watch.S02/Harbor.Watch.S02E01.mkv", Size: 1000},
				{Name: "Harbor.Watch.S02/Harbor.Watch.S02E02.mkv", Size: 1000},
			},
			candidate:      tierCandidate(1, "Harbor.Watch.S02E01.1080p.WEB.h264-GRP", "/data/tv/Harbor.Watch.S02", "/data/tv/Harbor.Watch.S02/Harbor.Watch.S02E01.mkv"),
			candidateFiles: qbt.TorrentFiles{{Name: "Harbor.Watch.S02E01.mkv", Size: 1000}},
			want:           matchTypeContentPath,
		},
		{
			name:           "same paths on another instance are a separate copy",
			source:         tierSingleFileSource(),
			sourceFiles:    sourceFiles,
			candidate:      tierCandidate(2, releaseName, "/data/movies", "/data/movies"),
			candidateFiles: withNfo,
			want:           matchTypeRelease,
		},
		{
			name:           "name match with a different file size is not a cross-seed",
			source:         tierSingleFileSource(),
			sourceFiles:    sourceFiles,
			candidate:      tierCandidate(1, tierSourceName, "/data/other", "/data/other/"+tierMovieFile),
			candidateFiles: qbt.TorrentFiles{{Name: tierMovieFile, Size: 1001}},
			want:           "",
		},
		{
			name:        "name match with an extra srt is a Name copy",
			source:      tierSingleFileSource(),
			sourceFiles: sourceFiles,
			candidate:   tierCandidate(1, tierSourceName, "/data/other", "/data/other/"+tierSourceName),
			candidateFiles: qbt.TorrentFiles{
				{Name: tierSourceName + "/" + tierMovieFile, Size: 1000},
				{Name: tierSourceName + "/English.srt", Size: 50},
			},
			want: matchTypeName,
		},
		{
			name:           "retitled upload with paired sizes is a Release copy",
			source:         tierSingleFileSource(),
			sourceFiles:    sourceFiles,
			candidate:      tierCandidate(1, "cdn-lanternbay.2023.1080p.bluray.x264.mkv", "/data/cross", "/data/cross/cdn-lanternbay.2023.1080p.bluray.x264.mkv"),
			candidateFiles: qbt.TorrentFiles{{Name: "cdn-lanternbay.2023.1080p.bluray.x264.mkv", Size: 1000}},
			want:           matchTypeRelease,
		},
		{
			name:           "retitled upload with a size off by one is not a cross-seed",
			source:         tierSingleFileSource(),
			sourceFiles:    sourceFiles,
			candidate:      tierCandidate(1, "cdn-lanternbay.2023.1080p.bluray.x264.mkv", "/data/cross", "/data/cross/cdn-lanternbay.2023.1080p.bluray.x264.mkv"),
			candidateFiles: qbt.TorrentFiles{{Name: "cdn-lanternbay.2023.1080p.bluray.x264.mkv", Size: 999}},
			want:           "",
		},
		{
			name:        "unrelated torrent in an ambiguous shared folder is not a cross-seed",
			source:      qbt.Torrent{Hash: tierSourceHash, Name: tierSourceName, SavePath: "/data/flat", ContentPath: "/data/flat"},
			sourceFiles: qbt.TorrentFiles{{Name: tierMovieFile, Size: 1000}, {Name: "a.nfo", Size: 1}},
			candidate:   tierCandidate(1, "Harbor.Lights.2021.720p.WEB.h264-OTHER", "/data/flat", "/data/flat"),
			candidateFiles: qbt.TorrentFiles{
				{Name: "Harbor.Lights.2021.720p.WEB.h264-OTHER.mkv", Size: 500},
				{Name: "b.nfo", Size: 1},
			},
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sm := &tierSyncManager{
				files: map[string]qbt.TorrentFiles{
					normalizeHash(tierSourceHash):    tc.sourceFiles,
					normalizeHash(tierCandidateHash): tc.candidateFiles,
				},
				source:     tc.source,
				candidates: map[int][]qbittorrent.CrossInstanceTorrentView{tc.candidate.InstanceID: {tc.candidate}},
			}
			requireTier(t, findTier(t, sm), tc.want)
		})
	}
}

// A hardlinked cross-seed keeps its Hardlink badge even when an extra file
// stops its sizes from pairing with the source.
func TestFindLocalMatches_HardlinkWithExtraFile(t *testing.T) {
	sourceDir, candidateDir := writeHardlinkFixture(t, tierMovieFile, true)
	source := qbt.Torrent{
		Hash:        tierSourceHash,
		Name:        tierSourceName,
		SavePath:    sourceDir,
		ContentPath: filepath.Join(sourceDir, tierMovieFile),
	}
	candidate := tierCandidate(1, tierSourceName, candidateDir, candidateDir)
	sm := &tierSyncManager{
		files: map[string]qbt.TorrentFiles{
			normalizeHash(tierSourceHash): {{Name: tierMovieFile, Size: 4}},
			normalizeHash(tierCandidateHash): {
				{Name: tierMovieFile, Size: 4},
				{Name: "Lantern.Bay.2023.1080p.BluRay.x264-GRP.nfo", Size: 3},
				{Name: "Disc.Menu.m2ts", Size: 9},
			},
		},
		source:     source,
		candidates: map[int][]qbittorrent.CrossInstanceTorrentView{1: {candidate}},
	}
	svc := hardlinkTestService(nil)
	svc.instanceStore = newOrderedInstanceStore(&models.Instance{ID: 1, Name: "local", IsActive: true, HasLocalFilesystemAccess: true})
	svc.syncManager = sm

	response, err := svc.FindLocalMatches(t.Context(), 1, tierSourceHash, true)
	require.NoError(t, err)
	requireTier(t, response.Matches, matchTypeHardlink)
}

func TestFindLocalMatches_CrossInstancePathOnlyFetchFailure(t *testing.T) {
	for _, candidatePath := range []string{"/data/movies", "/data/movies/" + tierMovieFile, "/data/movies/" + tierMovieFile + "/extra.mkv"} {
		t.Run(candidatePath, func(t *testing.T) {
			sm := &tierSyncManager{
				files: map[string]qbt.TorrentFiles{
					normalizeHash(tierSourceHash): {{Name: tierMovieFile, Size: 1000}},
				},
				source: tierSingleFileSource(),
				candidates: map[int][]qbittorrent.CrossInstanceTorrentView{2: {
					tierCandidate(2, "Harbor.Lights.2021.720p.WEB.h264-OTHER", "/data/movies", candidatePath),
				}},
				failHashes: map[string]bool{normalizeHash(tierCandidateHash): true},
			}
			require.Empty(t, findTier(t, sm))
		})
	}
}

// A failed candidate file fetch fails the delete dialog and is skipped in the tab.
func TestFindLocalMatches_CandidateFetchFailure(t *testing.T) {
	sm := &tierSyncManager{
		files: map[string]qbt.TorrentFiles{
			normalizeHash(tierSourceHash): {{Name: tierMovieFile, Size: 1000}},
		},
		source:     tierSingleFileSource(),
		candidates: map[int][]qbittorrent.CrossInstanceTorrentView{1: {tierCandidate(1, tierSourceName, "/data/other", "/data/other/"+tierMovieFile)}},
		failHashes: map[string]bool{normalizeHash(tierCandidateHash): true},
	}

	_, err := tierService(sm).FindLocalMatches(t.Context(), 1, tierSourceHash, true)
	require.ErrorContains(t, err, "failed to verify local file relationship")

	response, err := tierService(sm).FindLocalMatches(t.Context(), 1, tierSourceHash, false)
	require.NoError(t, err)
	require.Empty(t, response.Matches)
}
