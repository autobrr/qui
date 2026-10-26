// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"os"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// deleteMatchService puts the fixtures on instance 1, a local instance.
func deleteMatchService(torrents ...crossMatchFixture) (*Service, *tierSyncManager) {
	sm := &tierSyncManager{
		files:      make(map[string]qbt.TorrentFiles),
		candidates: make(map[int][]qbittorrent.CrossInstanceTorrentView),
	}
	for _, torrent := range torrents {
		sm.files[normalizeHash(torrent.view.Hash)] = torrent.files
		sm.candidates[torrent.view.InstanceID] = append(sm.candidates[torrent.view.InstanceID], torrent.view)
	}
	svc := hardlinkTestService(nil)
	svc.syncManager = sm
	svc.instanceStore = newOrderedInstanceStore(&models.Instance{ID: 1, Name: "local", IsActive: true, HasLocalFilesystemAccess: true})
	return svc, sm
}

func matchedTiers(matches []LocalMatch) map[string]string {
	tiers := make(map[string]string, len(matches))
	for _, match := range matches {
		tiers[match.Hash] = match.MatchType
	}
	return tiers
}

// regression: #3341
func TestFindDeleteMatchesSharedPath(t *testing.T) {
	t.Parallel()

	const name = "Harbor.Watch.S01.1080p.WEB.h264-GRP"
	files := qbt.TorrentFiles{{Name: name + "/Harbor.Watch.S01E01.mkv", Size: 100}}
	selected := crossMatchTorrent(1, "aaaa", name, "/data", "/data/"+name, files)
	selectedToo := crossMatchTorrent(1, "bbbb", name, "/data", "/data/"+name, files)
	// A retitled cross-seed of the same folder.
	sharer := crossMatchTorrent(1, "cccc", "Harbor Watch S01", "/data", "/data/"+name, files)
	// Same name and sizes in another folder: a cross-seed, but a delete leaves its files.
	copyElsewhere := crossMatchTorrent(1, "dddd", name, "/other", "/other/"+name, files)
	unrelated := crossMatchTorrent(1, "eeee", "Other.Show.S01.1080p.WEB.h264-GRP", "/data", "/data/Other.Show.S01",
		qbt.TorrentFiles{{Name: "Other.Show.S01/Other.Show.S01E01.mkv", Size: 100}})

	svc, _ := deleteMatchService(selected, selectedToo, sharer, copyElsewhere, unrelated)
	matches, err := svc.FindDeleteMatches(t.Context(), 1, []string{"AAAA", "bbbb"})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"cccc": matchTypeContentPath}, matchedTiers(matches))
	require.Equal(t, "local", matches[0].InstanceName)
}

// regression: #3341
func TestFindDeleteMatchesHardlinkWithPairedSizes(t *testing.T) {
	t.Parallel()

	const name = "Harbor.Watch.S01E01.1080p.WEB.h264-GRP"
	root := t.TempDir()
	sourceDir, candidateDir := filepath.Join(root, "a"), filepath.Join(root, "b")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(candidateDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, name+".mkv"), []byte("data"), 0o600))
	require.NoError(t, os.Link(filepath.Join(sourceDir, name+".mkv"), filepath.Join(candidateDir, name+".mkv")))

	files := qbt.TorrentFiles{{Name: name + ".mkv", Size: 4}}
	source := crossMatchTorrent(1, "aaaa", name, sourceDir, filepath.Join(sourceDir, name+".mkv"), files)
	// The file sizes pair, so the batch matcher reads this as a Name match. The
	// delete check still sees the hardlink.
	linked := crossMatchTorrent(1, "bbbb", name, candidateDir, filepath.Join(candidateDir, name+".mkv"), files)

	svc, _ := deleteMatchService(source, linked)
	matches, err := svc.FindDeleteMatches(t.Context(), 1, []string{"aaaa"})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"bbbb": matchTypeHardlink}, matchedTiers(matches))
}

// regression: #3341
func TestFindDeleteMatchesFailsWhenFilesCannotBeRead(t *testing.T) {
	t.Parallel()

	const name = "Harbor.Watch.S01.1080p.WEB.h264-GRP"
	files := qbt.TorrentFiles{{Name: name + "/Harbor.Watch.S01E01.mkv", Size: 100}}
	selected := crossMatchTorrent(1, "aaaa", name, "/data", "/data/"+name, files)
	sharer := crossMatchTorrent(1, "cccc", "Harbor Watch S01", "/data", "/data/"+name, files)

	svc, sm := deleteMatchService(selected, sharer)
	sm.failHashes = map[string]bool{"cccc": true}
	_, err := svc.FindDeleteMatches(t.Context(), 1, []string{"aaaa"})
	require.ErrorContains(t, err, "read the file list of torrent cccc")
}

// regression: #3341
func TestFindDeleteMatchesSelectsHybridByV1Hash(t *testing.T) {
	t.Parallel()

	const name = "Harbor.Watch.S01.1080p.WEB.h264-GRP"
	files := qbt.TorrentFiles{{Name: name + "/Harbor.Watch.S01E01.mkv", Size: 100}}
	selected := crossMatchTorrent(1, "aaaa", name, "/data", "/data/"+name, files)
	selected.view.InfohashV1 = "1111"
	sharer := crossMatchTorrent(1, "cccc", "Harbor Watch S01", "/data", "/data/"+name, files)

	svc, _ := deleteMatchService(selected, sharer)
	matches, err := svc.FindDeleteMatches(t.Context(), 1, []string{"1111"})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"cccc": matchTypeContentPath}, matchedTiers(matches))
}
