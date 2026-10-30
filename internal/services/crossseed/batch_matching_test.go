// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
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

// withTotalSize sets the total size that qBittorrent reports from the files.
func withTotalSize(torrent crossMatchFixture) crossMatchFixture {
	torrent.view.Size = sourceFilesTotalSize(torrent.files)
	return torrent
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

	t.Run("a torrent without a subfolder inside another torrent's folder shares the file", func(t *testing.T) {
		result := buildCrossMatch(t, sameNeeds,
			crossMatchTorrent(1, "aaa", "Harbor.Watch.S02.1080p.WEB.h264-GRP", "/data", "/data/Season2",
				cmFile("Season2/"+cmEpisodeFile, 1000)),
			crossMatchTorrent(1, "bbb", "Quiet.Coast.2020.720p.BluRay.x264-OTHER", "/data/Season2", "/data/Season2",
				cmFile(cmEpisodeFile, 1000)),
		)
		require.Equal(t, map[string][]string{"aaa": {"bbb"}, "bbb": {"aaa"}}, result.SameInstanceContentByHash)
	})

	t.Run("an episode inside the folder of its season pack shares the file", func(t *testing.T) {
		result := buildCrossMatch(t, sameNeeds,
			crossMatchTorrent(1, "aaa", "Harbor.Watch.S02.1080p.WEB.h264-GRP", "/data/tv", "/data/tv/Season2",
				qbt.TorrentFiles{{Name: "Season2/" + cmEpisodeFile, Size: 1000}, {Name: "Season2/other.mkv", Size: 1000}}),
			crossMatchTorrent(1, "bbb", cmEpisode, "/data/tv/Season2", "/data/tv/Season2/"+cmEpisodeFile, cmFile(cmEpisodeFile, 1000)),
		)
		require.Equal(t, map[string][]string{"aaa": {"bbb"}, "bbb": {"aaa"}}, result.SameInstanceContentByHash)
	})

	t.Run("a shared subtitle joins safety groups without making unrelated payloads cross-seeds", func(t *testing.T) {
		result := buildCrossMatch(t, sameNeeds,
			crossMatchTorrent(1, "aaa", cmEpisode, "/data/flat", "/data/flat",
				qbt.TorrentFiles{{Name: cmEpisodeFile, Size: 1000}, {Name: "English.srt", Size: 10}}),
			crossMatchTorrent(1, "bbb", "Quiet.Coast.2020.720p.BluRay.x264-OTHER", "/data/flat", "/data/flat",
				qbt.TorrentFiles{{Name: "other.mkv", Size: 2000}, {Name: "English.srt", Size: 10}}),
		)
		require.Empty(t, result.SameInstanceExists)
		require.Empty(t, result.SameInstanceUnknown)
		require.Equal(t, map[string][]string{"aaa": {"bbb"}, "bbb": {"aaa"}}, result.SameInstanceContentByHash)
	})

	t.Run("an unreadable torrent in a flat folder counts as sharing files", func(t *testing.T) {
		result := buildCrossMatch(t, sameNeeds,
			crossMatchTorrent(1, "aaa", cmEpisode, "/data/flat", "/data/flat/"+cmEpisodeFile, cmFile(cmEpisodeFile, 1000)),
			crossMatchTorrent(1, "bbb", "Quiet.Coast.2020.720p.BluRay.x264-OTHER", "/data/flat", "/data/flat", nil),
		)
		require.Empty(t, result.SameInstanceExists)
		require.Equal(t, map[string][]string{"aaa": {"bbb"}, "bbb": {"aaa"}}, result.SameInstanceContentByHash)
	})

	const proof = "Proof.2005.1080p.BluRay.x264-GRP"
	t.Run("a torrent whose every file is on the ignore list compares all its files", func(t *testing.T) {
		result := buildCrossMatch(t, sameNeeds,
			crossMatchTorrent(1, "aaa", proof, "/data/movies", "/data/movies/"+proof+".mkv", cmFile(proof+".mkv", 1000)),
			crossMatchTorrent(1, "bbb", "Proof 2005 rehost", "/data/movies", "/data/movies",
				qbt.TorrentFiles{{Name: proof + ".mkv", Size: 1000}, {Name: proof + ".nfo", Size: 2}}),
			crossMatchTorrent(1, "ccc", proof, "/data/other", "/data/other/"+proof+".mkv", cmFile(proof+".mkv", 1100)),
		)
		require.Equal(t, map[string][]string{"aaa": {"bbb"}, "bbb": {"aaa"}}, result.SameInstanceContentByHash)
		require.NotContains(t, result.SameInstanceExists, "ccc", "a copy with other file sizes is not a cross-seed")
	})

	t.Run("a copy with an ignored word in its title leaves its sidecars out", func(t *testing.T) {
		result := buildCrossMatch(t, sameNeeds,
			crossMatchTorrent(1, "aaa", proof, "/data/movies", "/data/movies/"+proof+".mkv", cmFile(proof+".mkv", 1000)),
			crossMatchTorrent(1, "bbb", proof, "/data/copy", "/data/copy/"+proof,
				qbt.TorrentFiles{{Name: proof + "/" + proof + ".mkv", Size: 1000}, {Name: proof + "/" + proof + ".nfo", Size: 2}}),
		)
		require.Contains(t, result.SameInstanceExists, "aaa")
		require.Contains(t, result.SameInstanceExists, "bbb")
		require.Empty(t, result.SameInstanceContentByHash, "separate copies share no file")
	})

	t.Run("a copy with an ignored word in its title leaves its sample out", func(t *testing.T) {
		result := buildCrossMatch(t, sameNeeds,
			crossMatchTorrent(1, "aaa", proof, "/data/movies", "/data/movies/"+proof+".mkv", cmFile(proof+".mkv", 1000)),
			crossMatchTorrent(1, "bbb", proof, "/data/copy", "/data/copy/"+proof,
				qbt.TorrentFiles{{Name: proof + "/" + proof + ".mkv", Size: 1000}, {Name: proof + "/Sample/grp-proof-sample.mkv", Size: 10}}),
		)
		require.Contains(t, result.SameInstanceExists, "aaa")
		require.Contains(t, result.SameInstanceExists, "bbb")
	})

	t.Run("an unreadable candidate counts as sharing files for grouping only", func(t *testing.T) {
		result := buildCrossMatch(t, sameNeeds,
			crossMatchTorrent(1, "aaa", cmEpisode, "/data/tv", "/data/tv/"+cmEpisodeFile, cmFile(cmEpisodeFile, 1000)),
			crossMatchTorrent(1, "bbb", cmEpisode, "/data/other", "/data/other/"+cmEpisodeFile, nil),
		)
		require.Empty(t, result.SameInstanceExists, "an unread candidate is not a confirmed cross-seed")
		require.Contains(t, result.SameInstanceUnknown, "aaa", "nor a confirmed miss")
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

func TestBuildCrossMatchSets_SharedPayloadKeepsAllSeedingAndTagMatches(t *testing.T) {
	a := crossMatchTorrent(1, "aaa", cmEpisode, "/data/flat", "/data/flat", cmFile(cmEpisodeFile, 1000))
	b := crossMatchTorrent(1, "bbb", cmEpisode, "/data/flat", "/data/flat", cmFile(cmEpisodeFile, 1000))
	c := crossMatchTorrent(1, "ccc", cmEpisode, "/data/flat", "/data/flat", cmFile(cmEpisodeFile, 1000))
	a.view.State, b.view.State = qbt.TorrentStateStoppedUp, qbt.TorrentStateStoppedUp
	c.view.Tags = "third-copy"
	result := buildCrossMatch(t, CrossMatchNeeds{SameExists: true, SameSeeding: true, SameTags: true, SameContent: true}, a, b, c)
	require.Contains(t, result.SameInstanceSeeding, "aaa")
	require.Contains(t, result.SameInstanceSeeding, "bbb")
	require.Equal(t, []string{"third-copy"}, result.SameInstanceTagsByHash["aaa"])
	require.Len(t, result.SameInstanceContentByHash, 3)
	for hash, peers := range result.SameInstanceContentByHash {
		require.NotEmpty(t, peers)
		require.NotContains(t, peers, hash)
	}
}

func TestBuildCrossMatchSets_SharedSidecarsStayOutOfMatchConditions(t *testing.T) {
	for _, collectContent := range []bool{false, true} {
		t.Run(map[bool]string{false: "conditions only", true: "with safety groups"}[collectContent], func(t *testing.T) {
			result := buildCrossMatch(t, CrossMatchNeeds{SameExists: true, SameSeeding: true, SameTags: true, SameContent: collectContent},
				crossMatchTorrent(1, "aaa", cmEpisode, "/data/flat", "/data/flat", qbt.TorrentFiles{
					{Name: "first.mkv", Size: 1000}, {Name: "English.srt", Size: 10}, {Name: "notes.txt", Size: 10},
				}),
				crossMatchTorrent(1, "bbb", "Quiet.Coast.2020.720p.BluRay.x264-OTHER", "/data/flat", "/data/flat", qbt.TorrentFiles{
					{Name: "second.mkv", Size: 2000}, {Name: "English.srt", Size: 10}, {Name: "info.nfo", Size: 10},
				}),
				crossMatchTorrent(1, "ccc", "Silver.Lantern.2019.2160p.WEB.h265-THIRD", "/data/flat", "/data/flat", qbt.TorrentFiles{
					{Name: "third.mkv", Size: 3000}, {Name: "notes.txt", Size: 10}, {Name: "info.nfo", Size: 10},
				}),
			)
			require.Empty(t, result.SameInstanceExists)
			require.Empty(t, result.SameInstanceSeeding)
			require.Empty(t, result.SameInstanceTagsByHash)
			if collectContent {
				require.Len(t, result.SameInstanceContentByHash, 3)
				for hash, peers := range result.SameInstanceContentByHash {
					require.NotEmpty(t, peers)
					require.NotContains(t, peers, hash)
				}
			} else {
				require.Nil(t, result.SameInstanceContentByHash)
			}
		})
	}
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

// The size group selects a copy whose name shares no Title key or episode
// with the source.
// regression: #3587
func TestBuildCrossMatchSets_SizeGroup(t *testing.T) {
	const (
		retitledSource = "Quiet.Coast.2020.1080p.BluRay.x264-GRP"
		renumbered     = "Harbor Watch - 25 1080p WEB h264-GRP"
		seasoned       = "Harbor.Watch.S02E01.1080p.WEB.h264-GRP"
	)
	tests := []struct {
		name            string
		source, copy    string
		wantCrossSeeded bool
	}{
		{name: "retitled copy", source: retitledSource, copy: "Silent Shore 2020 1080p", wantCrossSeeded: true},
		{name: "copy that numbers its episodes in another way", source: renumbered, copy: seasoned, wantCrossSeeded: true},
		{name: "other release with the same total size", source: retitledSource, copy: "Silent.Shore.2020.720p.BluRay.x264-GRP"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildCrossMatch(t, CrossMatchNeeds{SameExists: true},
				withTotalSize(crossMatchTorrent(1, "aaa", tt.source, "/data/a", "/data/a/"+tt.source+".mkv", cmFile(tt.source+".mkv", 1000))),
				withTotalSize(crossMatchTorrent(1, "bbb", tt.copy, "/data/b", "/data/b/"+tt.copy+".mkv", cmFile(tt.copy+".mkv", 1000))),
			)
			if tt.wantCrossSeeded {
				require.Contains(t, result.SameInstanceExists, "aaa")
				require.Contains(t, result.SameInstanceExists, "bbb")
			} else {
				require.Empty(t, result.SameInstanceExists)
			}
		})
	}
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

// The episodes of a daily show share a Title key and episode 0. Only a pair
// with the same date, or a pair where one side has no full date, meets the
// matcher, so a large daily archive does not compare every date.
func TestBuildCrossMatchSets_DailyShowMeetsOnlyItsDate(t *testing.T) {
	previousLogger := log.Logger
	previousLevel := zerolog.GlobalLevel()
	var buf bytes.Buffer
	log.Logger = zerolog.New(&buf).Level(zerolog.TraceLevel)
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() {
		log.Logger = previousLogger
		zerolog.SetGlobalLevel(previousLevel)
	})

	const (
		jan15     = "Harbor.Watch.2024.01.15.1080p.WEB.h264-GRP"
		jan15Copy = "Harbor Watch 2024 01 15 1080p WEB h264-GRP"
		jan16     = "Harbor.Watch.2024.01.16.1080p.WEB.h264-GRP"
		undated   = "Harbor.Watch.1080p.WEB.h264-GRP"
	)
	result := buildCrossMatch(t, CrossMatchNeeds{SameExists: true},
		crossMatchTorrent(1, "aaa", jan15, "/data/a", "/data/a/ep.mkv", cmFile("ep.mkv", 1000)),
		crossMatchTorrent(1, "bbb", jan15Copy, "/data/b", "/data/b/ep.mkv", cmFile("ep.mkv", 1000)),
		crossMatchTorrent(1, "ccc", jan16, "/data/c", "/data/c/ep.mkv", cmFile("ep.mkv", 1000)),
		crossMatchTorrent(1, "ddd", undated, "/data/d", "/data/d/ep.mkv", cmFile("ep.mkv", 1000)),
	)
	require.Contains(t, result.SameInstanceExists, "aaa")
	require.Contains(t, result.SameInstanceExists, "bbb")
	require.NotContains(t, result.SameInstanceExists, "ccc")

	met := make(map[[2]string]bool)
	for line := range bytes.Lines(buf.Bytes()) {
		var entry struct {
			SourceTitle    string `json:"sourceTitle"`
			CandidateTitle string `json:"candidateTitle"`
		}
		if json.Unmarshal(line, &entry) == nil && entry.SourceTitle != "" {
			met[[2]string{entry.SourceTitle, entry.CandidateTitle}] = true
		}
	}
	require.True(t, met[[2]string{jan15, jan15Copy}], "the same date meets the matcher")
	require.False(t, met[[2]string{jan15, jan16}], "another date does not meet the matcher")
	require.True(t, met[[2]string{jan15, undated}], "a dated source meets an undated candidate")
	require.True(t, met[[2]string{undated, jan16}], "an undated source meets every dated candidate")
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

func TestBuildCrossMatchSets_LinkVerificationFailureIsUnknown(t *testing.T) {
	for _, backendFailure := range []bool{false, true} {
		t.Run(map[bool]string{true: "backend unavailable", false: "file path cannot be verified"}[backendFailure], func(t *testing.T) {
			sourceDir, candidateDir := writeHardlinkFixture(t, cmEpisodeFile, true)
			candidateFile := cmEpisodeFile
			if !backendFailure {
				candidateFile = "../outside.mkv"
			}
			sm := &tierSyncManager{
				files: map[string]qbt.TorrentFiles{
					"aaa": cmFile(cmEpisodeFile, 4),
					"bbb": {{Name: candidateFile, Size: 4}, {Name: "Menu.m2ts", Size: 9}},
				},
				candidates: map[int][]qbittorrent.CrossInstanceTorrentView{1: {
					crossMatchTorrent(1, "aaa", cmEpisode, sourceDir, filepath.Join(sourceDir, cmEpisodeFile), nil).view,
					crossMatchTorrent(1, "bbb", cmEpisode, candidateDir, candidateDir, nil).view,
				}},
			}
			svc := hardlinkTestService(nil)
			if backendFailure {
				svc.SetBackendPool(nil)
			}
			svc.syncManager = sm
			svc.instanceStore = newOrderedInstanceStore(&models.Instance{ID: 1, Name: "local", IsActive: true, HasLocalFilesystemAccess: true})

			result := svc.BuildCrossMatchSets(t.Context(), 1, CrossMatchNeeds{SameExists: true})

			require.Empty(t, result.SameInstanceExists)
			require.Contains(t, result.SameInstanceUnknown, "aaa")
			require.Contains(t, result.SameInstanceUnknown, "bbb", "the cached reverse pair must stay unknown too")
		})
	}
}

func TestBuildCrossMatchSets_InstanceListFailureIsUnknown(t *testing.T) {
	t.Parallel()

	sm := &tierSyncManager{
		files: map[string]qbt.TorrentFiles{
			"aaa": cmFile(cmEpisodeFile, 4),
			"bbb": {{Name: cmEpisodeFile, Size: 4}, {Name: "Menu.m2ts", Size: 9}},
			"ccc": cmFile(cmEpisodeFile, 4),
		},
		candidates: map[int][]qbittorrent.CrossInstanceTorrentView{1: {
			crossMatchTorrent(1, "aaa", cmEpisode, "/data/one", "/data/one/episode.mkv", nil).view,
			crossMatchTorrent(1, "bbb", cmEpisode, "/data/two", "/data/two", nil).view,
			crossMatchTorrent(1, "ccc", cmEpisode, "/data/one", "/data/one/episode.mkv", nil).view,
		}},
	}
	svc := newTestService()
	svc.syncManager = sm
	svc.instanceStore = &orderedInstanceStore{listErr: errors.New("instance list unavailable")}

	for _, needs := range []CrossMatchNeeds{
		{SameExists: true, SameSeeding: true, SameTags: true, SameContent: true},
		{OtherExists: true, OtherSeeding: true},
	} {
		result := svc.BuildCrossMatchSets(t.Context(), 1, needs)
		for _, hash := range []string{"aaa", "bbb", "ccc"} {
			if needs.SameExists {
				require.Contains(t, result.SameInstanceUnknown, hash)
			} else {
				require.Contains(t, result.OtherInstanceUnknown, hash)
			}
		}
		if needs.SameContent {
			require.Contains(t, result.SameInstanceContentByHash["aaa"], "ccc", "delete safety must retain known shared paths")
		}
	}
}

// failingPeerSyncManager fails the torrent snapshot of one instance.
type failingPeerSyncManager struct {
	*tierSyncManager
	failInstanceID int
}

func (m *failingPeerSyncManager) GetCachedInstanceTorrents(ctx context.Context, instanceID int) ([]qbittorrent.CrossInstanceTorrentView, error) {
	if instanceID == m.failInstanceID {
		return nil, errors.New("instance unreachable")
	}
	return m.tierSyncManager.GetCachedInstanceTorrents(ctx, instanceID)
}

func TestBuildCrossMatchSets_PeerSnapshotFailureIsUnknown(t *testing.T) {
	t.Parallel()

	const other = "Quiet.Coast.2020.720p.BluRay.x264-OTHER"
	sm := &tierSyncManager{
		files: map[string]qbt.TorrentFiles{
			"aaa": cmFile(cmEpisodeFile, 4),
			"bbb": cmFile(other+".mkv", 8),
			"ccc": cmFile(other+".mkv", 8),
		},
		candidates: map[int][]qbittorrent.CrossInstanceTorrentView{
			1: {
				crossMatchTorrent(1, "aaa", cmEpisode, "/data/a", "/data/a/"+cmEpisodeFile, nil).view,
				crossMatchTorrent(1, "bbb", other, "/data/b", "/data/b/"+other+".mkv", nil).view,
			},
			3: {crossMatchTorrent(3, "ccc", other, "/data/c", "/data/c/"+other+".mkv", nil).view},
		},
	}
	svc := newTestService()
	svc.syncManager = &failingPeerSyncManager{tierSyncManager: sm, failInstanceID: 2}
	svc.instanceStore = newOrderedInstanceStore(
		&models.Instance{ID: 1, Name: "one", IsActive: true},
		&models.Instance{ID: 2, Name: "two", IsActive: true},
		&models.Instance{ID: 3, Name: "three", IsActive: true},
	)

	result := svc.BuildCrossMatchSets(t.Context(), 1, CrossMatchNeeds{OtherExists: true, OtherSeeding: true})
	require.Contains(t, result.OtherInstanceUnknown, "aaa", "the unreachable instance can hold its only copy")
	require.NotContains(t, result.OtherInstanceExists, "aaa")
	require.Contains(t, result.OtherInstanceExists, "bbb", "a copy on a reachable instance stays confirmed")
}

func TestBuildCrossMatchSets_CurrentSnapshotFailure(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("torrent snapshot unavailable")
	svc := newTestService()
	svc.syncManager = &tierSyncManager{snapshotErr: wantErr}
	result := svc.BuildCrossMatchSets(t.Context(), 1, CrossMatchNeeds{SameContent: true})
	require.ErrorIs(t, result.Err, wantErr)
	require.Nil(t, result.SameInstanceContentByHash)
}
