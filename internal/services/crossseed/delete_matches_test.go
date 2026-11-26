// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

const (
	crossA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	crossB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	crossC = "cccccccccccccccccccccccccccccccccccccccc"
	crossD = "dddddddddddddddddddddddddddddddddddddddd"
	crossE = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	crossF = "ffffffffffffffffffffffffffffffffffffffff"
	crossG = "1111111111111111111111111111111111111111"
	crossH = "2222222222222222222222222222222222222222"
	crossI = "3333333333333333333333333333333333333333"
	crossJ = "4444444444444444444444444444444444444444"
	crossK = "5555555555555555555555555555555555555555"
)

func deleteMatchService(torrents ...crossMatchFixture) (*Service, *tierSyncManager) {
	sm := &tierSyncManager{
		files:      make(map[string]qbt.TorrentFiles),
		candidates: make(map[int][]qbittorrent.CrossInstanceTorrentView),
	}
	for _, torrent := range torrents {
		sm.files[torrent.view.Hash] = torrent.files
		sm.candidates[torrent.view.InstanceID] = append(sm.candidates[torrent.view.InstanceID], torrent.view)
	}
	svc := hardlinkTestService(nil)
	svc.syncManager = sm
	svc.instanceStore = newOrderedInstanceStore(&models.Instance{ID: 1, Name: "local", IsActive: true, HasLocalFilesystemAccess: true})
	return svc, sm
}

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

func TestFindDeleteMatchesSharedPath(t *testing.T) {
	t.Parallel()

	const name = "Harbor.Watch.S01.1080p.WEB.h264-GRP"
	files := qbt.TorrentFiles{{Name: name + "/Harbor.Watch.S01E01.mkv", Size: 100}}
	selected := crossMatchTorrent(1, crossA, name, "/data", "/data/"+name, files)
	selectedToo := crossMatchTorrent(1, crossB, name, "/data", "/data/"+name, files)
	sharer := crossMatchTorrent(1, crossC, "Harbor Watch S01", "/data", "/data/"+name, files)
	copyElsewhere := crossMatchTorrent(1, crossD, name, "/other", "/other/"+name, files)
	unrelated := crossMatchTorrent(1, crossE, "Other.Show.S01.1080p.WEB.h264-GRP", "/data", "/data/Other.Show.S01",
		qbt.TorrentFiles{{Name: "Other.Show.S01/Other.Show.S01E01.mkv", Size: 100}})

	svc, _ := deleteMatchService(selected, selectedToo, sharer, copyElsewhere, unrelated)
	matches, err := findDeleteMatches(t, svc, strings.ToUpper(crossA), crossB)
	require.NoError(t, err)
	require.Equal(t, map[string]string{crossC: matchTypeContentPath}, matchedTiers(matches))
	require.Equal(t, "local", matches[0].InstanceName)
}

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
	source := crossMatchTorrent(1, crossA, name, sourceDir, filepath.Join(sourceDir, name+".mkv"), files)
	linked := crossMatchTorrent(1, crossB, name, candidateDir, filepath.Join(candidateDir, name+".mkv"), files)

	svc, _ := deleteMatchService(source, linked)
	matches, err := findDeleteMatches(t, svc, crossA)
	require.NoError(t, err)
	require.Equal(t, map[string]string{crossB: matchTypeHardlink}, matchedTiers(matches))
}

func TestFindDeleteMatchesFailsWhenFilesCannotBeRead(t *testing.T) {
	t.Parallel()

	const name = "Harbor.Watch.S01.1080p.WEB.h264-GRP"
	files := qbt.TorrentFiles{{Name: name + "/Harbor.Watch.S01E01.mkv", Size: 100}}
	selected := crossMatchTorrent(1, crossA, name, "/data", "/data/"+name, files)
	sharer := crossMatchTorrent(1, crossC, "Harbor Watch S01", "/data", "/data/"+name, files)

	svc, sm := deleteMatchService(selected, sharer)
	sm.failHashes = map[string]bool{crossC: true}
	_, err := findDeleteMatches(t, svc, crossA)
	require.ErrorContains(t, err, "read the file list of torrent cccc")
}

func TestFindDeleteMatchesSelectsHybridByV1Hash(t *testing.T) {
	t.Parallel()

	const name = "Harbor.Watch.S01.1080p.WEB.h264-GRP"
	files := qbt.TorrentFiles{{Name: name + "/Harbor.Watch.S01E01.mkv", Size: 100}}
	selected := crossMatchTorrent(1, crossA, name, "/data", "/data/"+name, files)
	selected.view.InfohashV1 = "9999999999999999999999999999999999999999"
	sharer := crossMatchTorrent(1, crossC, "Harbor Watch S01", "/data", "/data/"+name, files)

	svc, _ := deleteMatchService(selected, sharer)
	matches, err := findDeleteMatches(t, svc, "9999999999999999999999999999999999999999")
	require.NoError(t, err)
	require.Equal(t, map[string]string{crossC: matchTypeContentPath}, matchedTiers(matches))
}

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
	source := crossMatchTorrent(1, crossA, name, dataDir, filepath.Join(dataDir, name+".mkv"), files)
	linkedSource := crossMatchTorrent(1, crossB, name, linkDir, filepath.Join(linkDir, name+".mkv"), files)
	sharer := crossMatchTorrent(1, crossC, name, linkDir, filepath.Join(linkDir, name+".mkv"), files)

	svc, _ := deleteMatchService(source, linkedSource, sharer)
	matches, err := findDeleteMatches(t, svc, crossA, crossB)
	require.NoError(t, err)
	require.Equal(t, map[string]string{crossC: matchTypeContentPath}, matchedTiers(matches))
}

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

			source := withTotalSize(crossMatchTorrent(1, crossA, tt.source, sourceDir, filepath.Join(sourceDir, tt.sourceFile),
				qbt.TorrentFiles{{Name: tt.sourceFile, Size: 4}}))
			linked := withTotalSize(crossMatchTorrent(1, crossB, tt.copy, copyDir, filepath.Join(copyDir, tt.copied),
				qbt.TorrentFiles{{Name: tt.copied, Size: 4}}))

			svc, _ := deleteMatchService(source, linked)
			matches, err := findDeleteMatches(t, svc, crossA)
			require.NoError(t, err)
			require.Equal(t, map[string]string{crossB: matchTypeHardlink}, matchedTiers(matches))
		})
	}
}

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
		fixture(1, crossA, "Night.Ferry.mkv", "Night.Ferry.nfo"),
		fixture(1, crossB, "Quiet.Coast.mkv"),
		fixture(2, crossC, "Night.Ferry.mkv"),
		fixture(2, crossD, "Silent.Shore.mkv"),
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
	require.Equal(t, crossC, picks[0][0].entry.hash)
	require.Empty(t, picks[1])
}
