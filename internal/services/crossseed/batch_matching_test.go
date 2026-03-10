// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"

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

func TestMatchAgainstIndex_ContentPath(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	views := []qbittorrent.CrossInstanceTorrentView{
		makeView("aaa", "Movie.2024", "/data/movie.mkv", "/data", 1.0, qbt.TorrentStateUploading),
		makeView("bbb", "Other.2024", "/data/other.mkv", "/data", 0.5, qbt.TorrentStateDownloading),
	}
	idx := svc.buildIndexFromViews(views, true)

	t.Run("content path match found", func(t *testing.T) {
		t.Parallel()
		source := &qbt.Torrent{Hash: "xxx", ContentPath: "/data/movie.mkv", SavePath: "/data", Name: "Different.Name"}
		matched, seeding := svc.matchAgainstIndex(source, idx, false)
		if !matched {
			t.Error("expected match by content path")
		}
		if !seeding {
			t.Error("expected seeding match (aaa is uploading)")
		}
	})

	t.Run("no content path match", func(t *testing.T) {
		t.Parallel()
		source := &qbt.Torrent{Hash: "xxx", ContentPath: "/data/unique.mkv", SavePath: "/data", Name: "Unique.Name"}
		matched, _ := svc.matchAgainstIndex(source, idx, false)
		if matched {
			t.Error("expected no match")
		}
	})

	t.Run("ambiguous content path equals save path excluded", func(t *testing.T) {
		t.Parallel()
		source := &qbt.Torrent{Hash: "xxx", ContentPath: "/data", SavePath: "/data", Name: "Unique.Name"}
		matched, _ := svc.matchAgainstIndex(source, idx, false)
		if matched {
			t.Error("expected no match for ambiguous content path")
		}
	})
}

func TestMatchAgainstIndex_ExactName(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	views := []qbittorrent.CrossInstanceTorrentView{
		makeView("aaa", "Movie.2024.1080p.BluRay", "/other/path", "/other", 1.0, qbt.TorrentStateStalledUp),
	}
	idx := svc.buildIndexFromViews(views, true)

	t.Run("exact name match", func(t *testing.T) {
		t.Parallel()
		source := &qbt.Torrent{Hash: "xxx", ContentPath: "/different/content", SavePath: "/different", Name: "Movie.2024.1080p.BluRay"}
		matched, seeding := svc.matchAgainstIndex(source, idx, false)
		if !matched {
			t.Error("expected match by exact name")
		}
		if !seeding {
			t.Error("expected seeding (stalledUP)")
		}
	})

	t.Run("case insensitive name match", func(t *testing.T) {
		t.Parallel()
		source := &qbt.Torrent{Hash: "xxx", ContentPath: "/different/content", SavePath: "/different", Name: "movie.2024.1080p.bluray"}
		matched, _ := svc.matchAgainstIndex(source, idx, false)
		if !matched {
			t.Error("expected case-insensitive name match")
		}
	})
}

func TestMatchAgainstIndex_ReleaseMetadata(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	views := []qbittorrent.CrossInstanceTorrentView{
		makeView("aaa", "Movie.2024.1080p.BluRay.x264-GROUP", "/data/a", "/data", 1.0, qbt.TorrentStateUploading),
	}
	idx := svc.buildIndexFromViews(views, true)

	t.Run("release metadata match - same release different content path", func(t *testing.T) {
		t.Parallel()
		// Same title, year, resolution, source, codec, group — different content path and hash
		source := &qbt.Torrent{Hash: "xxx", ContentPath: "/other/b", SavePath: "/other", Name: "Movie.2024.1080p.BluRay.x264-GROUP"}
		matched, _ := svc.matchAgainstIndex(source, idx, false)
		if !matched {
			t.Error("expected match by release metadata")
		}
	})
}

func TestMatchAgainstIndex_ExcludeSelf(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	views := []qbittorrent.CrossInstanceTorrentView{
		makeView("aaa", "Movie.2024", "/data/movie.mkv", "/data", 1.0, qbt.TorrentStateUploading),
	}
	idx := svc.buildIndexFromViews(views, false)

	t.Run("self excluded by hash", func(t *testing.T) {
		t.Parallel()
		source := &qbt.Torrent{Hash: "aaa", ContentPath: "/data/movie.mkv", SavePath: "/data", Name: "Movie.2024"}
		matched, _ := svc.matchAgainstIndex(source, idx, true)
		if matched {
			t.Error("expected no match when excluding self")
		}
	})

	t.Run("different hash not excluded", func(t *testing.T) {
		t.Parallel()
		source := &qbt.Torrent{Hash: "bbb", ContentPath: "/data/movie.mkv", SavePath: "/data", Name: "Movie.2024"}
		matched, _ := svc.matchAgainstIndex(source, idx, true)
		if !matched {
			t.Error("expected match for different hash")
		}
	})
}

func TestMatchAgainstIndex_SeedingDetection(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	t.Run("non-seeding candidate returns matched but not seeding", func(t *testing.T) {
		t.Parallel()
		views := []qbittorrent.CrossInstanceTorrentView{
			makeView("aaa", "Movie.2024", "/data/movie.mkv", "/data", 0.5, qbt.TorrentStateDownloading),
		}
		idx := svc.buildIndexFromViews(views, true)
		source := &qbt.Torrent{Hash: "xxx", ContentPath: "/data/movie.mkv", SavePath: "/data", Name: "Different"}
		matched, seeding := svc.matchAgainstIndex(source, idx, false)
		if !matched {
			t.Error("expected match")
		}
		if seeding {
			t.Error("expected not seeding")
		}
	})

	t.Run("paused candidate not seeding", func(t *testing.T) {
		t.Parallel()
		views := []qbittorrent.CrossInstanceTorrentView{
			makeView("aaa", "Movie.2024", "/data/movie.mkv", "/data", 1.0, qbt.TorrentStatePausedUp),
		}
		idx := svc.buildIndexFromViews(views, true)
		source := &qbt.Torrent{Hash: "xxx", ContentPath: "/data/movie.mkv", SavePath: "/data", Name: "Different"}
		matched, seeding := svc.matchAgainstIndex(source, idx, false)
		if !matched {
			t.Error("expected match")
		}
		if seeding {
			t.Error("expected not seeding for paused candidate")
		}
	})

	t.Run("multiple candidates one seeding", func(t *testing.T) {
		t.Parallel()
		views := []qbittorrent.CrossInstanceTorrentView{
			makeView("aaa", "Movie.2024", "/data/movie.mkv", "/data", 1.0, qbt.TorrentStatePausedUp),
			makeView("bbb", "Movie.2024", "/data/movie.mkv", "/data", 1.0, qbt.TorrentStateUploading),
		}
		idx := svc.buildIndexFromViews(views, true)
		source := &qbt.Torrent{Hash: "xxx", ContentPath: "/data/movie.mkv", SavePath: "/data", Name: "Different"}
		matched, seeding := svc.matchAgainstIndex(source, idx, false)
		if !matched {
			t.Error("expected match")
		}
		if !seeding {
			t.Error("expected seeding (bbb is uploading)")
		}
	})
}

func TestBuildIndexFromViews(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	t.Run("empty views returns nil", func(t *testing.T) {
		t.Parallel()
		idx := svc.buildIndexFromViews(nil, false)
		if idx != nil {
			t.Error("expected nil index for empty views")
		}
	})

	t.Run("indexes all three strategies", func(t *testing.T) {
		t.Parallel()
		views := []qbittorrent.CrossInstanceTorrentView{
			makeView("aaa", "Movie.2024.1080p.BluRay.x264-GROUP", "/data/movie.mkv", "/data", 1.0, qbt.TorrentStateUploading),
		}
		idx := svc.buildIndexFromViews(views, false)
		if idx == nil {
			t.Fatal("expected non-nil index")
		}
		if len(idx.byContentPath) == 0 {
			t.Error("expected content path index entries")
		}
		if len(idx.byName) == 0 {
			t.Error("expected name index entries")
		}
		if len(idx.byReleaseKey) == 0 {
			t.Error("expected release key index entries")
		}
	})

	t.Run("ambiguous content path not indexed", func(t *testing.T) {
		t.Parallel()
		views := []qbittorrent.CrossInstanceTorrentView{
			makeView("aaa", "Movie.2024", "/data", "/data", 1.0, qbt.TorrentStateUploading),
		}
		idx := svc.buildIndexFromViews(views, false)
		if idx == nil {
			t.Fatal("expected non-nil index")
		}
		if len(idx.byContentPath) != 0 {
			t.Error("expected empty content path index for ambiguous path")
		}
	})
}

func TestSameInstanceMatchingEndToEnd(t *testing.T) {
	t.Parallel()
	svc := newTestService()

	t.Run("same-instance cross-seeds by content path", func(t *testing.T) {
		t.Parallel()
		views := []qbittorrent.CrossInstanceTorrentView{
			makeView("aaa", "Movie.A", "/data/movie.mkv", "/data", 1.0, qbt.TorrentStateUploading),
			makeView("bbb", "Movie.B", "/data/movie.mkv", "/data", 0.5, qbt.TorrentStateDownloading),
			makeView("ccc", "Unrelated", "/data/other.mkv", "/data", 1.0, qbt.TorrentStateUploading),
		}
		idx := svc.buildIndexFromViews(views, true)

		// aaa should match bbb (same content path, different hash)
		source := views[0].Torrent
		matched, seeding := svc.matchAgainstIndex(source, idx, true)
		if !matched {
			t.Error("expected aaa to match (bbb has same content path)")
		}
		if seeding {
			t.Error("expected not seeding (bbb is downloading)")
		}

		// bbb should match aaa
		source = views[1].Torrent
		matched, seeding = svc.matchAgainstIndex(source, idx, true)
		if !matched {
			t.Error("expected bbb to match (aaa has same content path)")
		}
		if !seeding {
			t.Error("expected seeding (aaa is uploading)")
		}

		// ccc should not match (different content path, unique name)
		source = views[2].Torrent
		matched, _ = svc.matchAgainstIndex(source, idx, true)
		if matched {
			t.Error("expected ccc to NOT match")
		}
	})

	t.Run("same-instance cross-seeds by name", func(t *testing.T) {
		t.Parallel()
		views := []qbittorrent.CrossInstanceTorrentView{
			makeView("aaa", "Same.Name.2024", "/data/a/content", "/data/a", 1.0, qbt.TorrentStateStalledUp),
			makeView("bbb", "Same.Name.2024", "/data/b/content", "/data/b", 1.0, qbt.TorrentStateUploading),
		}
		idx := svc.buildIndexFromViews(views, true)

		source := views[0].Torrent
		matched, seeding := svc.matchAgainstIndex(source, idx, true)
		if !matched {
			t.Error("expected match by name")
		}
		if !seeding {
			t.Error("expected seeding (bbb is uploading)")
		}
	})

	t.Run("three-way group all seeding", func(t *testing.T) {
		t.Parallel()
		views := []qbittorrent.CrossInstanceTorrentView{
			makeView("aaa", "Movie.A", "/data/movie.mkv", "/data", 1.0, qbt.TorrentStateForcedUp),
			makeView("bbb", "Movie.B", "/data/movie.mkv", "/data", 1.0, qbt.TorrentStateQueuedUp),
			makeView("ccc", "Movie.C", "/data/movie.mkv", "/data", 1.0, qbt.TorrentStateUploading),
		}
		idx := svc.buildIndexFromViews(views, true)

		for i, hash := range []string{"aaa", "bbb", "ccc"} {
			source := views[i].Torrent
			matched, seeding := svc.matchAgainstIndex(source, idx, true)
			if !matched {
				t.Errorf("expected %s to match", hash)
			}
			if !seeding {
				t.Errorf("expected %s to have seeding cross-seed", hash)
			}
		}
	})

	t.Run("all paused - matched but not seeding", func(t *testing.T) {
		t.Parallel()
		views := []qbittorrent.CrossInstanceTorrentView{
			makeView("aaa", "Movie.A", "/data/movie.mkv", "/data", 1.0, qbt.TorrentStatePausedUp),
			makeView("bbb", "Movie.B", "/data/movie.mkv", "/data", 1.0, qbt.TorrentStatePausedUp),
		}
		idx := svc.buildIndexFromViews(views, true)

		source := views[0].Torrent
		matched, seeding := svc.matchAgainstIndex(source, idx, true)
		if !matched {
			t.Error("expected match")
		}
		if seeding {
			t.Error("expected not seeding (both paused)")
		}
	})
}
