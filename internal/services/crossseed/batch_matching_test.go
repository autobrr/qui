// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/stringutils"
)

func TestIsTorrentViewSeeding(t *testing.T) {
	t.Parallel()

	mkView := func(progress float64, state qbt.TorrentState) *qbittorrent.CrossInstanceTorrentView {
		return &qbittorrent.CrossInstanceTorrentView{
			TorrentView: &qbittorrent.TorrentView{
				Torrent: &qbt.Torrent{Progress: progress, State: state},
			},
		}
	}

	tests := []struct {
		name     string
		view     *qbittorrent.CrossInstanceTorrentView
		expected bool
	}{
		{name: "nil view", view: nil, expected: false},
		{name: "nil torrent view", view: &qbittorrent.CrossInstanceTorrentView{}, expected: false},
		{name: "uploading with full progress", view: mkView(1.0, qbt.TorrentStateUploading), expected: true},
		{name: "stalledUP with full progress", view: mkView(1.0, qbt.TorrentStateStalledUp), expected: true},
		{name: "queuedUP with full progress", view: mkView(1.0, qbt.TorrentStateQueuedUp), expected: true},
		{name: "checkingUP with full progress", view: mkView(1.0, qbt.TorrentStateCheckingUp), expected: true},
		{name: "forcedUP with full progress", view: mkView(1.0, qbt.TorrentStateForcedUp), expected: true},
		{name: "pausedUP - not seeding", view: mkView(1.0, qbt.TorrentStatePausedUp), expected: false},
		{name: "stoppedUP - not seeding", view: mkView(1.0, qbt.TorrentStateStoppedUp), expected: false},
		{name: "downloading - not seeding", view: mkView(0.5, qbt.TorrentStateDownloading), expected: false},
		{name: "uploading but incomplete progress", view: mkView(0.99, qbt.TorrentStateUploading), expected: false},
		{name: "error state with full progress", view: mkView(1.0, qbt.TorrentStateError), expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isTorrentViewSeeding(tt.view)
			if got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func newTestService() *Service {
	return &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
}

func makeView(hash, name, contentPath, savePath string, progress float64, state qbt.TorrentState) qbittorrent.CrossInstanceTorrentView {
	return qbittorrent.CrossInstanceTorrentView{
		TorrentView: &qbittorrent.TorrentView{
			Torrent: &qbt.Torrent{
				Hash:        hash,
				Name:        name,
				ContentPath: contentPath,
				SavePath:    savePath,
				Progress:    progress,
				State:       state,
			},
		},
	}
}

// crossMatchFixture is one torrent in a BuildCrossMatchSets test: its view on
// an instance and its file list.
type crossMatchFixture struct {
	view  qbittorrent.CrossInstanceTorrentView
	files qbt.TorrentFiles
}

func crossMatchTorrent(instanceID int, hash, name, savePath, contentPath string, files qbt.TorrentFiles) crossMatchFixture {
	view := makeView(hash, name, contentPath, savePath, 1.0, qbt.TorrentStateUploading)
	view.InstanceID = instanceID
	return crossMatchFixture{view: view, files: files}
}

// buildCrossMatch runs BuildCrossMatchSets for instance 1 over the fixtures,
// which can sit on instances 1 and 2.
func buildCrossMatch(t *testing.T, needs CrossMatchNeeds, torrents ...crossMatchFixture) *CrossMatchResult {
	t.Helper()
	sm := &tierSyncManager{
		files:      make(map[string]qbt.TorrentFiles),
		candidates: make(map[int][]qbittorrent.CrossInstanceTorrentView),
	}
	for _, torrent := range torrents {
		sm.files[normalizeHash(torrent.view.Hash)] = torrent.files
		sm.candidates[torrent.view.InstanceID] = append(sm.candidates[torrent.view.InstanceID], torrent.view)
	}
	svc := newTestService()
	svc.syncManager = sm
	svc.instanceStore = newOrderedInstanceStore(
		&models.Instance{ID: 1, Name: "one", IsActive: true},
		&models.Instance{ID: 2, Name: "two", IsActive: true},
	)
	return svc.BuildCrossMatchSets(t.Context(), 1, needs)
}

const (
	cmEpisode      = "Harbor.Watch.S02E03.1080p.WEB.h264-GRP"
	cmEpisodeFile  = cmEpisode + ".mkv"
	cmAudioDescrip = "Harbor.Watch.S02E03.Audio.Description.1080p.WEB.h264-GRP"
)

func cmFile(name string, size int64) qbt.TorrentFiles {
	return qbt.TorrentFiles{{Name: name, Size: size}}
}

func TestBuildCrossMatchSets_SameInstance(t *testing.T) {
	sameNeeds := CrossMatchNeeds{SameExists: true, SameContent: true}

	t.Run("audio-description remux with a different mkv is not a cross-seed", func(t *testing.T) {
		result := buildCrossMatch(t, sameNeeds,
			crossMatchTorrent(1, "aaa", cmEpisode, "/data/tv", "/data/tv/"+cmEpisodeFile, cmFile(cmEpisodeFile, 1000)),
			crossMatchTorrent(1, "bbb", cmAudioDescrip, "/data/ad", "/data/ad/"+cmAudioDescrip+".mkv", cmFile(cmAudioDescrip+".mkv", 1100)),
		)
		require.NotContains(t, result.SameInstanceExists, "aaa")
		require.NotContains(t, result.SameInstanceExists, "bbb")
	})

	t.Run("audio-description name with the same file sizes is a Release copy", func(t *testing.T) {
		result := buildCrossMatch(t, sameNeeds,
			crossMatchTorrent(1, "aaa", cmEpisode, "/data/tv", "/data/tv/"+cmEpisodeFile, cmFile(cmEpisodeFile, 1000)),
			crossMatchTorrent(1, "bbb", cmAudioDescrip, "/data/ad", "/data/ad/"+cmAudioDescrip+".mkv", cmFile(cmAudioDescrip+".mkv", 1000)),
		)
		require.Contains(t, result.SameInstanceExists, "aaa")
		require.Empty(t, result.SameInstanceContentByHash, "a separate copy shares no file")
	})

	t.Run("folder with an nfo next to the single mkv shares the file", func(t *testing.T) {
		result := buildCrossMatch(t, sameNeeds,
			crossMatchTorrent(1, "aaa", cmEpisode, "/data/tv", "/data/tv/"+cmEpisodeFile, cmFile(cmEpisodeFile, 1000)),
			crossMatchTorrent(1, "bbb", "Harbor Watch S02E03 Pack", "/data/tv", "/data/tv",
				qbt.TorrentFiles{{Name: cmEpisodeFile, Size: 1000}, {Name: "info.nfo", Size: 2}}),
		)
		require.Contains(t, result.SameInstanceExists, "aaa")
		require.Contains(t, result.SameInstanceExists, "bbb")
		require.Equal(t, map[string][]string{"aaa": {"bbb"}, "bbb": {"aaa"}}, result.SameInstanceContentByHash)
	})

	t.Run("unrelated torrents in one flat folder are not cross-seeds", func(t *testing.T) {
		result := buildCrossMatch(t, sameNeeds,
			crossMatchTorrent(1, "aaa", cmEpisode, "/data/flat", "/data/flat",
				qbt.TorrentFiles{{Name: cmEpisodeFile, Size: 1000}, {Name: "a.nfo", Size: 2}}),
			crossMatchTorrent(1, "bbb", "Quiet.Coast.2020.720p.BluRay.x264-OTHER", "/data/flat", "/data/flat",
				qbt.TorrentFiles{{Name: "Quiet.Coast.mkv", Size: 500}, {Name: "b.nfo", Size: 2}}),
		)
		require.Empty(t, result.SameInstanceExists)
	})

	t.Run("an unreadable candidate counts as sharing files for grouping only", func(t *testing.T) {
		result := buildCrossMatch(t, sameNeeds,
			crossMatchTorrent(1, "aaa", cmEpisode, "/data/tv", "/data/tv/"+cmEpisodeFile, cmFile(cmEpisodeFile, 1000)),
			crossMatchTorrent(1, "bbb", cmEpisode, "/data/other", "/data/other/"+cmEpisodeFile, nil),
		)
		require.Empty(t, result.SameInstanceExists, "an unread candidate is not a confirmed cross-seed")
		require.Equal(t, []string{"bbb"}, result.SameInstanceContentByHash["aaa"])
	})

	t.Run("a torrent alone does not match itself", func(t *testing.T) {
		result := buildCrossMatch(t, sameNeeds,
			crossMatchTorrent(1, "aaa", cmEpisode, "/data/tv", "/data/tv/"+cmEpisodeFile, cmFile(cmEpisodeFile, 1000)),
		)
		require.Empty(t, result.SameInstanceExists)
	})
}

func TestBuildCrossMatchSets_SeedingAndTags(t *testing.T) {
	paused := crossMatchTorrent(1, "bbb", cmEpisode, "/data/b", "/data/b/"+cmEpisodeFile, cmFile(cmEpisodeFile, 1000))
	paused.view.State = qbt.TorrentStatePausedUp
	paused.view.Tags = "paused-tag"
	seeding := crossMatchTorrent(1, "ccc", cmEpisode, "/data/c", "/data/c/"+cmEpisodeFile, cmFile(cmEpisodeFile, 1000))
	seeding.view.Tags = "seed-tag"
	source := crossMatchTorrent(1, "aaa", cmEpisode, "/data/a", "/data/a/"+cmEpisodeFile, cmFile(cmEpisodeFile, 1000))
	source.view.State = qbt.TorrentStatePausedUp

	result := buildCrossMatch(t, CrossMatchNeeds{SameExists: true, SameSeeding: true, SameTags: true}, source, paused, seeding)

	require.Contains(t, result.SameInstanceSeeding, "aaa")
	require.Contains(t, result.SameInstanceSeeding, "bbb")
	require.NotContains(t, result.SameInstanceSeeding, "ccc", "both of its matches are paused")
	require.Equal(t, []string{"paused-tag", "seed-tag"}, result.SameInstanceTagsByHash["aaa"])
}

func TestBuildCrossMatchSets_OtherInstance(t *testing.T) {
	otherNeeds := CrossMatchNeeds{OtherExists: true}

	t.Run("same release with paired sizes on another instance", func(t *testing.T) {
		result := buildCrossMatch(t, otherNeeds,
			crossMatchTorrent(1, "aaa", cmEpisode, "/data/tv", "/data/tv/"+cmEpisodeFile, cmFile(cmEpisodeFile, 1000)),
			crossMatchTorrent(2, "bbb", cmEpisode, "/data/tv", "/data/tv/"+cmEpisodeFile,
				qbt.TorrentFiles{{Name: cmEpisodeFile, Size: 1000}, {Name: "English.srt", Size: 9}}),
		)
		require.Contains(t, result.OtherInstanceExists, "aaa")
	})

	t.Run("same path on another instance with a different size is not a cross-seed", func(t *testing.T) {
		result := buildCrossMatch(t, otherNeeds,
			crossMatchTorrent(1, "aaa", cmEpisode, "/data/tv", "/data/tv/"+cmEpisodeFile, cmFile(cmEpisodeFile, 1000)),
			crossMatchTorrent(2, "bbb", cmEpisode, "/data/tv", "/data/tv/"+cmEpisodeFile, cmFile(cmEpisodeFile, 1001)),
		)
		require.Empty(t, result.OtherInstanceExists)
	})
}

func TestBuildCrossMatchSets_ReleaseMetadataTraceLogsRejection(t *testing.T) {
	previousLogger := log.Logger
	previousLevel := zerolog.GlobalLevel()
	var buf bytes.Buffer
	log.Logger = zerolog.New(&buf).Level(zerolog.TraceLevel)
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() {
		log.Logger = previousLogger
		zerolog.SetGlobalLevel(previousLevel)
	})

	result := buildCrossMatch(t, CrossMatchNeeds{SameExists: true},
		crossMatchTorrent(1, "aaa", "Movie.2024.1080p.BluRay.x264-GROUP", "/data/a", "/data/a/movie.mkv", cmFile("movie.mkv", 1000)),
		crossMatchTorrent(1, "bbb", "Movie.2024.1080p.BluRay.x264-OTHER", "/data/b", "/data/b/movie.mkv", cmFile("movie.mkv", 1000)),
	)
	require.Empty(t, result.SameInstanceExists, "a group mismatch rejects the release metadata candidate")

	logOutput := buf.String()
	require.Contains(t, logOutput, `"level":"trace"`)
	require.Contains(t, logOutput, `"message":"crossseed: release metadata candidate evaluated"`)
	require.Contains(t, logOutput, `"reason":"group mismatch"`)
}

// A hardlinked cross-seed with an extra file that search does not ignore fails
// the size pairing, but its inode makes it a cross-seed, as in the
// Cross-Seed tab. An independent copy with the same extra file stays out.
func TestBuildCrossMatchSets_HardlinkWithExtraFile(t *testing.T) {
	for _, linked := range []bool{true, false} {
		t.Run(map[bool]string{true: "hardlink", false: "independent copy"}[linked], func(t *testing.T) {
			sourceDir, candidateDir := writeHardlinkFixture(t, cmEpisodeFile, linked)
			sm := &tierSyncManager{
				files: map[string]qbt.TorrentFiles{
					"aaa": cmFile(cmEpisodeFile, 4),
					"bbb": {{Name: cmEpisodeFile, Size: 4}, {Name: "Menu.m2ts", Size: 9}},
				},
				candidates: map[int][]qbittorrent.CrossInstanceTorrentView{1: {
					crossMatchTorrent(1, "aaa", cmEpisode, sourceDir, filepath.Join(sourceDir, cmEpisodeFile), nil).view,
					crossMatchTorrent(1, "bbb", cmEpisode, candidateDir, candidateDir, nil).view,
				}},
			}
			svc := hardlinkTestService(nil)
			svc.syncManager = sm
			svc.instanceStore = newOrderedInstanceStore(&models.Instance{ID: 1, Name: "local", IsActive: true, HasLocalFilesystemAccess: true})

			result := svc.BuildCrossMatchSets(t.Context(), 1, CrossMatchNeeds{SameExists: true})

			if linked {
				require.Contains(t, result.SameInstanceExists, "aaa")
				require.Contains(t, result.SameInstanceExists, "bbb")
			} else {
				require.Empty(t, result.SameInstanceExists)
			}
		})
	}
}
