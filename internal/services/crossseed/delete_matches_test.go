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

// findDeleteMatches runs the delete check for the selected torrents of
// instance 1.
func findDeleteMatches(t *testing.T, svc *Service, hashes ...string) ([]LocalMatch, error) {
	t.Helper()
	response, err := svc.FindDeleteMatches(t.Context(), map[int][]string{1: hashes})
	if err != nil {
		return nil, err
	}
	return response.Matches, nil
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
	matches, err := findDeleteMatches(t, svc, "AAAA", "bbbb")
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
	matches, err := findDeleteMatches(t, svc, "aaaa")
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
	_, err := findDeleteMatches(t, svc, "aaaa")
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
	matches, err := findDeleteMatches(t, svc, "1111")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"cccc": matchTypeContentPath}, matchedTiers(matches))
}

// A torrent that shares the files of one selected torrent and is a linked copy
// of another breaks when the selection is deleted, whatever the torrent order.
// regression: #3341
func TestFindDeleteMatchesSharedPathOutranksLink(t *testing.T) {
	t.Parallel()

	const name = "Harbor.Watch.S01E01.1080p.WEB.h264-GRP"
	root := t.TempDir()
	dataDir, linkDir := filepath.Join(root, "data"), filepath.Join(root, "links")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))
	require.NoError(t, os.MkdirAll(linkDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, name+".mkv"), []byte("data"), 0o600))
	require.NoError(t, os.Link(filepath.Join(dataDir, name+".mkv"), filepath.Join(linkDir, name+".mkv")))

	files := qbt.TorrentFiles{{Name: name + ".mkv", Size: 4}}
	source := crossMatchTorrent(1, "aaaa", name, dataDir, filepath.Join(dataDir, name+".mkv"), files)
	linkedSource := crossMatchTorrent(1, "bbbb", name, linkDir, filepath.Join(linkDir, name+".mkv"), files)
	// A linked copy of aaaa, and a sharer of the files of bbbb.
	sharer := crossMatchTorrent(1, "cccc", name, linkDir, filepath.Join(linkDir, name+".mkv"), files)

	svc, _ := deleteMatchService(source, linkedSource, sharer)
	matches, err := findDeleteMatches(t, svc, "aaaa", "bbbb")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"cccc": matchTypeContentPath}, matchedTiers(matches))
}

// The size group selects a hardlinked copy whose name shares no Title key or
// episode with the selected torrent.
// regression: #3587
func TestFindDeleteMatchesLinkedCopyInSizeGroup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		source, copy       string
		sourceFile, copied string
	}{
		{
			name:       "retitled copy",
			source:     "Quiet.Coast.2020.1080p.BluRay.x264-GRP",
			copy:       "Silent Shore 2020 1080p",
			sourceFile: "Quiet.Coast.2020.1080p.BluRay.x264-GRP.mkv",
			copied:     "Silent Shore 2020 1080p.mkv",
		},
		{
			name:       "copy that numbers its episodes in another way",
			source:     "Harbor Watch - 25 1080p WEB h264-GRP",
			copy:       "Harbor.Watch.S02E01.1080p.WEB.h264-GRP",
			sourceFile: "Harbor Watch - 25.mkv",
			copied:     "Harbor.Watch.S02E01.mkv",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			sourceDir, copyDir := filepath.Join(root, "a"), filepath.Join(root, "b")
			require.NoError(t, os.MkdirAll(sourceDir, 0o755))
			require.NoError(t, os.MkdirAll(copyDir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(sourceDir, tt.sourceFile), []byte("data"), 0o600))
			require.NoError(t, os.Link(filepath.Join(sourceDir, tt.sourceFile), filepath.Join(copyDir, tt.copied)))

			source := withTotalSize(crossMatchTorrent(1, "aaaa", tt.source, sourceDir, filepath.Join(sourceDir, tt.sourceFile),
				qbt.TorrentFiles{{Name: tt.sourceFile, Size: 4}}))
			linked := withTotalSize(crossMatchTorrent(1, "bbbb", tt.copy, copyDir, filepath.Join(copyDir, tt.copied),
				qbt.TorrentFiles{{Name: tt.copied, Size: 4}}))

			svc, _ := deleteMatchService(source, linked)
			matches, err := findDeleteMatches(t, svc, "aaaa")
			require.NoError(t, err)
			require.Equal(t, map[string]string{"bbbb": matchTypeHardlink}, matchedTiers(matches))
		})
	}
}

// A flat folder that two instances use pairs a selected torrent only with the
// torrents that share a file path with it, not with every torrent there.
func TestAddOtherPathPicksPairsOnlySharedPaths(t *testing.T) {
	t.Parallel()

	const flat = "/data/flat"
	fixture := func(instanceID int, hash string, names ...string) crossMatchFixture {
		files := make(qbt.TorrentFiles, 0, len(names))
		for _, name := range names {
			files = append(files, qbt.TorrentFile{Name: name, Size: 10})
		}
		return crossMatchTorrent(instanceID, hash, "Night.Ferry.2019.1080p.BluRay.x264-GRP", flat, flat, files)
	}
	fixtures := []crossMatchFixture{
		fixture(1, "aaaa", "Night.Ferry.mkv", "Night.Ferry.nfo"),
		fixture(1, "bbbb", "Quiet.Coast.mkv"),
		fixture(2, "cccc", "Night.Ferry.mkv"),
		fixture(2, "dddd", "Silent.Shore.mkv"),
	}

	instances := []*models.Instance{
		{ID: 1, Name: "home", IsActive: true, HasLocalFilesystemAccess: true},
		{ID: 2, Name: "second", IsActive: true, HasLocalFilesystemAccess: true},
	}
	files := &batchFiles{instances: instances, byKey: make(map[string]*batchFileData), fetched: make(map[string]qbt.TorrentFiles)}
	views := make([]qbittorrent.CrossInstanceTorrentView, 0, len(fixtures))
	for _, f := range fixtures {
		views = append(views, f.view)
		files.fetched[cacheKeyForTorrentFiles(f.view.InstanceID, f.view.Hash)] = f.files
	}
	torrents, otherViews := views[:2], views[2:]

	groups := crossPathGroups(instances, torrents, otherViews, func(string) bool { return true })
	picks := make([][]*candidatePick, len(torrents))
	files.addOtherPathPicks(t.Context(), 1, torrents, groups, picks)

	require.Len(t, picks[0], 1)
	require.Equal(t, "cccc", picks[0][0].entry.hash)
	require.Empty(t, picks[1])
}
