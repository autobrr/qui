// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
)

// crossSeedsService puts the fixtures on two local instances.
func crossSeedsService(torrents ...crossMatchFixture) (*Service, *tierSyncManager) {
	svc, sm := deleteMatchService(torrents...)
	svc.instanceStore = newOrderedInstanceStore(
		&models.Instance{ID: 1, Name: "local", IsActive: true, HasLocalFilesystemAccess: true},
		&models.Instance{ID: 2, Name: "second", IsActive: true, HasLocalFilesystemAccess: true},
	)
	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
	return svc, sm
}

// tabTiers keys each match by its instance and hash, because one torrent can
// sit on two instances.
func tabTiers(matches []LocalMatch) map[string]string {
	tiers := make(map[string]string, len(matches))
	for _, match := range matches {
		tiers[strconv.Itoa(match.InstanceID)+"/"+match.Hash] = match.MatchType
	}
	return tiers
}

func findCrossSeeds(t *testing.T, svc *Service, hash string) map[string]string {
	t.Helper()
	response, err := svc.FindCrossSeeds(t.Context(), 1, hash)
	require.NoError(t, err)
	return tabTiers(response.Matches)
}

// The tab lists more than a delete touches: a copy with its own files, and
// the copies on other instances.
func TestFindCrossSeedsListsCopiesThatKeepTheirFiles(t *testing.T) {
	t.Parallel()

	const name = "Harbor.Watch.S01E01.1080p.WEB.h264-GRP"
	files := qbt.TorrentFiles{{Name: name + ".mkv", Size: 100}}
	source := crossMatchTorrent(1, "aaaa", name, "/data", "/data/"+name+".mkv", files)
	copyElsewhere := crossMatchTorrent(1, "bbbb", name, "/other", "/other/"+name+".mkv", files)
	sameTorrentOnSecond := crossMatchTorrent(2, "aaaa", name, "/data", "/data/"+name+".mkv", files)
	releaseOnSecond := crossMatchTorrent(2, "cccc", "Harbor Watch S01E01 1080p WEB h264-GRP", "/data", "/data/"+name+".mkv", files)

	svc, _ := crossSeedsService(source, copyElsewhere, sameTorrentOnSecond, releaseOnSecond)
	require.Equal(t, map[string]string{
		"1/bbbb": matchTypeName,
		"2/aaaa": matchTypeName,
		"2/cccc": matchTypeRelease,
	}, findCrossSeeds(t, svc, "aaaa"))

	matches, err := svc.FindDeleteMatches(t.Context(), 1, []string{"aaaa"})
	require.NoError(t, err)
	require.Empty(t, matches)
}

// For each torrent of one library, the delete check of that torrent is part
// of its Cross-Seed tab.
// regression: #3555
func TestFindCrossSeedsHoldsTheDeleteCheck(t *testing.T) {
	t.Parallel()

	const (
		pack    = "Harbor.Watch.S01.1080p.WEB.h264-GRP"
		episode = "Harbor.Watch.S01E01.1080p.WEB.h264-GRP"
		movie   = "Quiet.Coast.2020.1080p.BluRay.x264-GRP"
	)
	root := t.TempDir()
	dataDir, linkDir, otherDir := filepath.Join(root, "data"), filepath.Join(root, "links"), filepath.Join(root, "other")
	for _, dir := range []string{filepath.Join(dataDir, pack), linkDir, otherDir} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	episodeFile := filepath.Join(dataDir, pack, episode+".mkv")
	require.NoError(t, os.WriteFile(episodeFile, []byte("data"), 0o600))
	movieFile := filepath.Join(dataDir, movie+".mkv")
	require.NoError(t, os.WriteFile(movieFile, []byte("movie"), 0o600))
	require.NoError(t, os.Link(movieFile, filepath.Join(linkDir, "Silent Shore 2020 1080p.mkv")))
	require.NoError(t, os.WriteFile(filepath.Join(otherDir, movie+".mkv"), []byte("movie"), 0o600))

	packFiles := qbt.TorrentFiles{{Name: pack + "/" + episode + ".mkv", Size: 4}}
	library := []crossMatchFixture{
		withTotalSize(crossMatchTorrent(1, "aaaa", pack, dataDir, filepath.Join(dataDir, pack), packFiles)),
		// A retitled cross-seed of the same folder.
		withTotalSize(crossMatchTorrent(1, "bbbb", "Harbor Watch S01", dataDir, filepath.Join(dataDir, pack), packFiles)),
		// An episode inside the folder of its season pack.
		withTotalSize(crossMatchTorrent(1, "cccc", episode, filepath.Join(dataDir, pack), episodeFile,
			qbt.TorrentFiles{{Name: episode + ".mkv", Size: 4}})),
		withTotalSize(crossMatchTorrent(1, "dddd", movie, dataDir, movieFile, qbt.TorrentFiles{{Name: movie + ".mkv", Size: 5}})),
		// A retitled hardlinked copy.
		withTotalSize(crossMatchTorrent(1, "eeee", "Silent Shore 2020 1080p", linkDir, filepath.Join(linkDir, "Silent Shore 2020 1080p.mkv"),
			qbt.TorrentFiles{{Name: "Silent Shore 2020 1080p.mkv", Size: 5}})),
		// A copy with its own files.
		withTotalSize(crossMatchTorrent(1, "ffff", movie, otherDir, filepath.Join(otherDir, movie+".mkv"), qbt.TorrentFiles{{Name: movie + ".mkv", Size: 5}})),
		withTotalSize(crossMatchTorrent(2, "dddd", movie, dataDir, movieFile, qbt.TorrentFiles{{Name: movie + ".mkv", Size: 5}})),
	}
	svc, _ := crossSeedsService(library...)

	var checked int
	for _, torrent := range library {
		if torrent.view.InstanceID != 1 {
			continue
		}
		hash := torrent.view.Hash
		deleted, err := svc.FindDeleteMatches(t.Context(), 1, []string{hash})
		require.NoError(t, err)
		tab := findCrossSeeds(t, svc, hash)
		for key, tier := range tabTiers(deleted) {
			require.Equal(t, tier, tab[key], "torrent %s, match %s", hash, key)
			checked++
		}
	}
	// aaaa, bbbb, and cccc share files; dddd and eeee are linked.
	require.Equal(t, 8, checked)
}
