// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
)

var copyGroupTestKey = []byte("0123456789abcdef0123456789abcdef")

// copyGroupFixture is one torrent for FindCopyGroups. files nil means the job
// holds no file list for it.
type copyGroupFixture struct {
	instanceID                    int
	hash, name, savePath, content string
	addedOn                       int64
	files                         qbt.TorrentFiles
}

// findCopyGroups runs FindCopyGroups over the fixtures and returns each group
// as its sorted "instance|hash" members.
func findCopyGroups(t *testing.T, svc *Service, fixtures ...copyGroupFixture) ([][]string, []CopyGroup) {
	t.Helper()
	return findCopyGroupsWithKey(t, svc, copyGroupTestKey, fixtures...)
}

func findCopyGroupsWithKey(t *testing.T, svc *Service, key []byte, fixtures ...copyGroupFixture) ([][]string, []CopyGroup) {
	t.Helper()
	if svc == nil {
		svc = newTestService()
	}
	torrents := make(map[int][]qbittorrent.CrossInstanceTorrentView)
	files := make(map[int]map[string]qbt.TorrentFiles)
	for _, f := range fixtures {
		torrent := &qbt.Torrent{Hash: f.hash, Name: f.name, SavePath: f.savePath, ContentPath: f.content, AddedOn: f.addedOn}
		torrent.Size = sourceFilesTotalSize(f.files)
		torrents[f.instanceID] = append(torrents[f.instanceID], qbittorrent.CrossInstanceTorrentView{
			TorrentView: &qbittorrent.TorrentView{Torrent: torrent},
			InstanceID:  f.instanceID,
		})
		if f.files != nil {
			if files[f.instanceID] == nil {
				files[f.instanceID] = make(map[string]qbt.TorrentFiles)
			}
			files[f.instanceID][f.hash] = f.files
		}
	}
	groups, err := svc.FindCopyGroups(t.Context(), torrents, files, nil, key)
	require.NoError(t, err)
	members := make([][]string, 0, len(groups))
	for _, group := range groups {
		var keys []string
		for _, m := range group.Members {
			keys = append(keys, cacheKeyForTorrentFiles(m.InstanceID, m.Hash))
		}
		slices.Sort(keys)
		members = append(members, keys)
	}
	slices.SortFunc(members, func(a, b []string) int { return strings.Compare(a[0], b[0]) })
	return members, groups
}

const (
	cgPack     = "Harbor.Watch.S01.1080p.WEB.h264-GRP"
	cgEpisode1 = "Harbor.Watch.S01E01.1080p.WEB.h264-GRP.mkv"
	cgEpisode2 = "Harbor.Watch.S01E02.1080p.WEB.h264-GRP.mkv"
)

func cgPackFiles(extra ...qbt.TorrentFile) qbt.TorrentFiles {
	return append(qbt.TorrentFiles{
		{Name: cgPack + "/" + cgEpisode1, Size: 1000},
		{Name: cgPack + "/" + cgEpisode2, Size: 2000},
	}, extra...)
}

func cgHash(c byte) string {
	return strings.Repeat(string(c), 40)
}

func TestFindCopyGroups(t *testing.T) {
	a, b, c := cgHash('a'), cgHash('b'), cgHash('c')
	inFolder := func(hash, name, folder string, files qbt.TorrentFiles) copyGroupFixture {
		return copyGroupFixture{instanceID: 1, hash: hash, name: name, savePath: folder, content: folder + "/" + cgPack, files: files}
	}
	tests := []struct {
		name     string
		torrents []copyGroupFixture
		want     [][]string
	}{
		{
			name: "same folder with different group tags",
			torrents: []copyGroupFixture{
				inFolder(a, cgPack, "/data", cgPackFiles()),
				inFolder(b, "Harbor.Watch.S01.1080p.WEB.h264-OTHER", "/data", cgPackFiles()),
			},
			want: [][]string{{"1|" + a, "1|" + b}},
		},
		{
			name: "separate copies with another name form",
			torrents: []copyGroupFixture{
				inFolder(a, cgPack, "/data/a", cgPackFiles()),
				inFolder(b, "Harbor Watch S01 1080p WEB h264-GRP", "/data/b", cgPackFiles()),
			},
			want: [][]string{{"1|" + a, "1|" + b}},
		},
		{
			name: "retitled copy with the same sizes",
			torrents: []copyGroupFixture{
				inFolder(a, cgPack, "/data/a", cgPackFiles()),
				inFolder(b, "Harbor.Lookout.S01.1080p.WEB.h264-GRP", "/data/b", cgPackFiles()),
			},
			want: [][]string{{"1|" + a, "1|" + b}},
		},
		{
			name: "a name without a group tag between two tags",
			torrents: []copyGroupFixture{
				inFolder(a, cgPack, "/data/a", cgPackFiles()),
				inFolder(b, "Harbor.Watch.S01.1080p.WEB.h264", "/data/b", cgPackFiles()),
				inFolder(c, "Harbor.Watch.S01.1080p.WEB.h264-OTHER", "/data/c", cgPackFiles()),
			},
			want: [][]string{{"1|" + a, "1|" + b, "1|" + c}},
		},
		{
			name: "a pack with its episodes in one folder",
			torrents: []copyGroupFixture{
				inFolder(a, cgPack, "/data", cgPackFiles()),
				{instanceID: 1, hash: b, name: strings.TrimSuffix(cgEpisode1, ".mkv"), savePath: "/data/" + cgPack, content: "/data/" + cgPack + "/" + cgEpisode1,
					files: qbt.TorrentFiles{{Name: cgEpisode1, Size: 1000}}},
				{instanceID: 1, hash: c, name: strings.TrimSuffix(cgEpisode2, ".mkv"), savePath: "/data/" + cgPack, content: "/data/" + cgPack + "/" + cgEpisode2,
					files: qbt.TorrentFiles{{Name: cgEpisode2, Size: 2000}}},
			},
			want: [][]string{},
		},
		{
			name: "an extra file keeps a copy out and a sidecar does not",
			torrents: []copyGroupFixture{
				inFolder(a, cgPack, "/data/a", cgPackFiles()),
				inFolder(b, cgPack, "/data/b", cgPackFiles(qbt.TorrentFile{Name: cgPack + "/Harbor.Watch.S01E03.1080p.WEB.h264-GRP.mkv", Size: 500})),
				inFolder(c, cgPack, "/data/c", cgPackFiles(qbt.TorrentFile{Name: cgPack + "/" + cgPack + ".nfo", Size: 5})),
			},
			want: [][]string{{"1|" + a, "1|" + c}},
		},
		{
			name: "an unreadable file list",
			torrents: []copyGroupFixture{
				inFolder(a, cgPack, "/data/a", cgPackFiles()),
				inFolder(b, cgPack, "/data/b", nil),
				inFolder(c, cgPack, "/data/c", cgPackFiles()),
			},
			want: [][]string{{"1|" + a, "1|" + c}},
		},
		{
			name: "an unreadable member of a shared folder",
			torrents: []copyGroupFixture{
				inFolder(a, cgPack, "/data", cgPackFiles()),
				inFolder(b, cgPack, "/data", nil),
				inFolder(c, "Harbor.Watch.S01.1080p.WEB.h264-OTHER", "/data", cgPackFiles()),
			},
			want: [][]string{{"1|" + a, "1|" + c}},
		},
		{
			name: "separate hardlink trees with one name",
			torrents: []copyGroupFixture{
				{instanceID: 1, hash: cgHash('a'), name: cgPack, savePath: "/data/a", content: "/data/a/" + cgPack, files: cgPackFiles()},
				{instanceID: 1, hash: cgHash('b'), name: cgPack, savePath: "/data/b", content: "/data/b/" + cgPack, files: cgPackFiles()},
			},
			want: [][]string{{"1|" + cgHash('a'), "1|" + cgHash('b')}},
		},
		{
			name: "one torrent on two instances",
			torrents: []copyGroupFixture{
				{instanceID: 1, hash: a, name: cgPack, savePath: "/data", content: "/data/" + cgPack, files: cgPackFiles()},
				{instanceID: 2, hash: a, name: cgPack, savePath: "/data", content: "/data/" + cgPack, files: cgPackFiles()},
			},
			want: [][]string{{"1|" + a, "2|" + a}},
		},
		{
			name: "copies on two instances",
			torrents: []copyGroupFixture{
				{instanceID: 1, hash: cgHash('a'), name: cgPack, savePath: "/data", content: "/data/" + cgPack, files: cgPackFiles()},
				{instanceID: 2, hash: cgHash('a'), name: cgPack, savePath: "/data", content: "/data/" + cgPack, files: cgPackFiles()},
				{instanceID: 2, hash: cgHash('b'), name: cgPack, savePath: "/other", content: "/other/" + cgPack, files: cgPackFiles()},
			},
			want: [][]string{{"1|" + cgHash('a'), "2|" + cgHash('a'), "2|" + cgHash('b')}},
		},
		{
			name: "a shared folder links copies that another instance names the other way",
			torrents: []copyGroupFixture{
				inFolder(a, cgPack, "/data", cgPackFiles()),
				inFolder(b, "Harbor.Watch.S01.1080p.WEB.h264-OTHER", "/data", cgPackFiles()),
				{instanceID: 2, hash: a, name: "Harbor.Watch.S01.1080p.WEB.h264-OTHER", savePath: "/x", content: "/x/" + cgPack, files: cgPackFiles()},
				{instanceID: 2, hash: b, name: cgPack, savePath: "/y", content: "/y/" + cgPack, files: cgPackFiles()},
			},
			want: [][]string{{"1|" + a, "1|" + b, "2|" + a, "2|" + b}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := findCopyGroups(t, nil, tc.torrents...)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFindCopyGroups_ReadsOnlyTheHeldFileLists(t *testing.T) {
	a, b, c := cgHash('a'), cgHash('b'), cgHash('c')
	// The client would return a list for b, and nothing for the files of the
	// season folder that only the held list gives.
	svc := newTestService()
	svc.syncManager = &tierSyncManager{files: map[string]qbt.TorrentFiles{b: cgPackFiles()}}
	seasonFiles := qbt.TorrentFiles{
		{Name: "Season 1/" + cgEpisode1, Size: 1000},
		{Name: "Season 1/" + cgEpisode2, Size: 2000},
	}

	got, _ := findCopyGroups(t, svc,
		copyGroupFixture{instanceID: 1, hash: a, name: cgPack, savePath: "/data/a", content: "/data/a/" + cgPack, files: cgPackFiles()},
		copyGroupFixture{instanceID: 1, hash: b, name: cgPack, savePath: "/data/b", content: "/data/b/" + cgPack},
		copyGroupFixture{instanceID: 2, hash: c, name: "Season 1", savePath: "/data", content: "/data/Season 1", files: seasonFiles},
	)

	assert.Equal(t, [][]string{{"1|" + a, "2|" + c}}, got)
}

func TestFindCopyGroups_GroupID(t *testing.T) {
	a, b, c := cgHash('a'), cgHash('b'), cgHash('c')
	copyOf := func(hash string, addedOn int64) copyGroupFixture {
		return copyGroupFixture{instanceID: 1, hash: hash, name: cgPack, savePath: "/data/" + hash[:1], content: "/data/" + hash[:1] + "/" + cgPack, addedOn: addedOn, files: cgPackFiles()}
	}
	_, before := findCopyGroups(t, nil, copyOf(b, 200), copyOf(c, 300))
	require.Len(t, before, 1)

	t.Run("a younger copy keeps the id", func(t *testing.T) {
		_, after := findCopyGroups(t, nil, copyOf(a, 400), copyOf(b, 200), copyOf(c, 300))
		require.Len(t, after, 1)
		assert.Equal(t, before[0].ID, after[0].ID)
	})

	t.Run("an older copy is the new representative", func(t *testing.T) {
		_, after := findCopyGroups(t, nil, copyOf(a, 100), copyOf(b, 200), copyOf(c, 300))
		require.Len(t, after, 1)
		assert.NotEqual(t, before[0].ID, after[0].ID)
	})

	t.Run("the id needs the key", func(t *testing.T) {
		representative := []byte(cacheKeyForTorrentFiles(1, b))
		plain := sha256.Sum256(representative)
		assert.NotEqual(t, binary.BigEndian.Uint64(plain[:]), before[0].ID)
		assert.NotContains(t, fmt.Sprintf("%016x", before[0].ID), b[:16])
		_, otherKey := findCopyGroupsWithKey(t, nil, []byte("another key of thirty-two bytes!"), copyOf(b, 200), copyOf(c, 300))
		require.Len(t, otherKey, 1)
		assert.NotEqual(t, before[0].ID, otherKey[0].ID)
	})
}
