// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"maps"
	"math/rand/v2"
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
	members, groups, _ := findCopyGroupsAndLinks(t, svc, key, fixtures...)
	return members, groups
}

func findCopyGroupsAndLinks(t *testing.T, svc *Service, key []byte, fixtures ...copyGroupFixture) ([][]string, []CopyGroup, *CopyLinks) {
	t.Helper()
	if svc == nil {
		svc = newTestService()
	}
	torrents, files := copyGroupInput(fixtures...)
	groups, links, err := svc.FindCopyGroups(t.Context(), torrents, heldCopyGroupFiles(files, nil), nil, nil, key)
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
	return members, groups, links
}

func copyGroupInput(fixtures ...copyGroupFixture) (map[int][]qbittorrent.CrossInstanceTorrentView, map[int]map[string]qbt.TorrentFiles) {
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
	return torrents, files
}

// heldCopyGroupFiles answers FindCopyGroups from files. It adds each torrent
// that the call asks for to asked, as "instance|hash".
func heldCopyGroupFiles(files map[int]map[string]qbt.TorrentFiles, asked *[]string) CopyGroupFiles {
	return func(_ context.Context, instanceID int, torrents []qbittorrent.CrossInstanceTorrentView) (map[string]qbt.TorrentFiles, error) {
		out := make(map[string]qbt.TorrentFiles, len(torrents))
		for _, t := range torrents {
			if asked != nil {
				*asked = append(*asked, cacheKeyForTorrentFiles(instanceID, t.Hash))
			}
			if list, ok := files[instanceID][t.Hash]; ok {
				out[t.Hash] = list
			}
		}
		return out, nil
	}
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

func TestFindCopyGroups_AsksOnlyForTheListsOfPossibleCopies(t *testing.T) {
	a, b, c, d := cgHash('a'), cgHash('b'), cgHash('c'), cgHash('d')
	single := "Quiet.Harbor.2019.1080p.BluRay.x264-GRP"
	torrents := map[int][]qbittorrent.CrossInstanceTorrentView{}
	files := map[int]map[string]qbt.TorrentFiles{1: {}}
	add := func(hash, name, folder string, list qbt.TorrentFiles) {
		torrents[1] = append(torrents[1], qbittorrent.CrossInstanceTorrentView{
			TorrentView: &qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: hash, Name: name, SavePath: folder, ContentPath: folder + "/" + name, Size: sourceFilesTotalSize(list)}},
			InstanceID:  1,
		})
		files[1][hash] = list
	}
	add(a, cgPack, "/data/a", cgPackFiles())
	add(b, cgPack, "/data/b", cgPackFiles())
	add(c, single, "/data/c", qbt.TorrentFiles{{Name: single + ".mkv", Size: 777}})
	// The name of a season folder reads too little, so its files give the read.
	add(d, "Season 1", "/data/d", qbt.TorrentFiles{{Name: "Season 1/" + cgEpisode1, Size: 1000}, {Name: "Season 1/" + cgEpisode2, Size: 2000}})

	var asked []string
	groups, _, err := newTestService().FindCopyGroups(t.Context(), torrents, heldCopyGroupFiles(files, &asked), nil, nil, copyGroupTestKey)
	require.NoError(t, err)

	require.Len(t, groups, 1)
	assert.Len(t, groups[0].Members, 3)
	slices.Sort(asked)
	assert.Equal(t, []string{"1|" + a, "1|" + b, "1|" + d}, asked)
}

// The first read of an instance gets no file list for the torrents of the
// other instances, so a flat folder there must cost its torrent count.
// regression: #4024
func TestFindCopyGroups_AnUnreadFlatFolderCostsItsTorrentCount(t *testing.T) {
	const count = 300
	torrents := map[int][]qbittorrent.CrossInstanceTorrentView{}
	for i := range count {
		name := fmt.Sprintf("Quiet.Harbor.%03d.2019.1080p.BluRay.x264-GRP", i)
		content := "/data/flat/" + name + ".mkv"
		if i == 0 {
			content = "/data/flat"
		}
		torrents[1] = append(torrents[1], qbittorrent.CrossInstanceTorrentView{
			TorrentView: &qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: fmt.Sprintf("%040d", i), Name: name, SavePath: "/data/flat", ContentPath: content, Size: int64(1000 + i)}},
			InstanceID:  1,
		})
	}
	svc := newTestService()

	allocs := testing.AllocsPerRun(1, func() {
		groups, _, err := svc.FindCopyGroups(t.Context(), torrents, heldCopyGroupFiles(nil, nil), nil, nil, copyGroupTestKey)
		require.NoError(t, err)
		require.Empty(t, groups)
	})

	// A pick for each pair of torrents takes over 1,500 allocations for each torrent.
	assert.Less(t, allocs/count, 300.0)
}

func TestFindCopyGroups_ACopyThatHoldsSomeFilesSharesThemButStaysOutOfTheGroup(t *testing.T) {
	a, b, c, d, e, f, g := cgHash('a'), cgHash('b'), cgHash('c'), cgHash('d'), cgHash('e'), cgHash('f'), cgHash('g')
	episode3 := qbt.TorrentFile{Name: cgPack + "/Harbor.Watch.S01E03.1080p.WEB.h264-GRP.mkv", Size: 500}
	fixture := func(hash, name, folder string, files qbt.TorrentFiles) copyGroupFixture {
		return copyGroupFixture{instanceID: 1, hash: hash, name: name, savePath: folder, content: folder + "/" + name, files: wanted(files)}
	}
	other := "Quiet.Meadow.S01.1080p.WEB.h264-OTHER"
	skipped := wanted(cgPackFiles(episode3))
	skipped[2].Priority = 0
	members, _, links := findCopyGroupsAndLinks(t, nil, copyGroupTestKey,
		fixture(a, cgPack, "/data", cgPackFiles(episode3)),
		fixture(c, cgPack, "/data/b", cgPackFiles(episode3)),
		// b holds two of the three episodes.
		fixture(b, cgPack, "/links", cgPackFiles()),
		// d is another release with files of the same sizes.
		fixture(d, other, "/data", qbt.TorrentFiles{
			{Name: other + "/Quiet.Meadow.S01E01.1080p.WEB.h264-OTHER.mkv", Size: 1000},
			{Name: other + "/Quiet.Meadow.S01E02.1080p.WEB.h264-OTHER.mkv", Size: 2000},
			{Name: other + "/Quiet.Meadow.S01E03.1080p.WEB.h264-OTHER.mkv", Size: 500},
		}),
		// e skips the third episode, so qBittorrent holds only two of its files.
		copyGroupFixture{instanceID: 1, hash: e, name: cgPack, savePath: "/data/e", content: "/data/e/" + cgPack, files: skipped},
		// f holds only an extra file, and skips the episode of the size that a holds.
		copyGroupFixture{instanceID: 1, hash: f, name: cgPack, savePath: "/data/f", content: "/data/f/" + cgPack, files: qbt.TorrentFiles{
			{Name: cgPack + "/Harbor.Watch.S01E03.1080p.WEB.h264-GRP.mkv", Size: 500},
			{Name: cgPack + "/Harbor.Watch.S01.Extras.mkv", Size: 777, Priority: 1},
		}},
		// g has another name, but it uses the files of a. Only a links it to b.
		copyGroupFixture{instanceID: 1, hash: g, name: "Calm.Orchard.S01.1080p.WEB.h264-THIRD", savePath: "/data", content: "/data/" + cgPack, files: wanted(cgPackFiles(episode3))},
	)
	require.Equal(t, [][]string{{"1|" + a, "1|" + c, "1|" + e, "1|" + g}}, members, "only the copies with equal file sizes group")
	require.Equal(t, []string{
		fmt.Sprintf("3000 [1|%s 1|%s 1|%s 1|%s 1|%s]", a, b, c, e, g),
		fmt.Sprintf("500 [1|%s 1|%s 1|%s]", a, c, g),
	}, partHolders(links.parts), "the torrents of the release share the files that they hold, and d and f share nothing")
}

func TestFindCopyGroups_AnEpisodeSharesItsFileWithItsSeasonPack(t *testing.T) {
	a, b, c, d := cgHash('a'), cgHash('b'), cgHash('c'), cgHash('d')
	episode := func(hash, name, folder string, size int64) copyGroupFixture {
		return copyGroupFixture{instanceID: 1, hash: hash, name: name, savePath: folder, content: folder + "/" + name + ".mkv",
			files: wanted(qbt.TorrentFiles{{Name: name + ".mkv", Size: size}})}
	}
	members, _, links := findCopyGroupsAndLinks(t, nil, copyGroupTestKey,
		copyGroupFixture{instanceID: 1, hash: a, name: cgPack, savePath: "/data", content: "/data/" + cgPack, files: wanted(cgPackFiles())},
		episode(b, "Harbor.Watch.S01E01.1080p.WEB.h264-GRP", "/links", 1000),
		// c is the first episode of the next season, with a file of the same size.
		episode(c, "Harbor.Watch.S02E01.1080p.WEB.h264-GRP", "/links", 1000),
		// d is the second episode from another source, with another size.
		episode(d, "Harbor.Watch.S01E02.1080p.WEB.h264-GRP", "/links", 1999),
	)
	require.Empty(t, members, "a pack and its episode are not copies")
	require.Equal(t, []string{
		fmt.Sprintf("1000 [1|%s 1|%s]", a, b),
		fmt.Sprintf("2000 [1|%s]", a),
	}, partHolders(links.parts), "the pack and its first episode share one file, and c and d share nothing")
}

// A call that starts from the links of the call before it must find the groups
// that a call with no links finds, after each change of the library.
func TestFindCopyGroups_LinksGiveTheGroupsOfAFullSearch(t *testing.T) {
	type release struct {
		name  string
		files qbt.TorrentFiles
	}
	movie := "Quiet.Harbor.2019.1080p.BluRay.x264-GRP"
	releases := []release{
		{cgPack, cgPackFiles()},
		{"Harbor.Watch.S01.1080p.WEB.h264-OTHER", cgPackFiles()},
		{"Harbor.Lookout.S01.1080p.WEB.h264-GRP", cgPackFiles()},
		{cgPack, cgPackFiles(qbt.TorrentFile{Name: cgPack + "/Harbor.Watch.S01E03.1080p.WEB.h264-GRP.mkv", Size: 500})},
		{"Season 1", qbt.TorrentFiles{{Name: "Season 1/" + cgEpisode1, Size: 1000}, {Name: "Season 1/" + cgEpisode2, Size: 2000}}},
		{movie, qbt.TorrentFiles{{Name: movie + ".mkv", Size: 777}}},
		{strings.TrimSuffix(cgEpisode1, ".mkv"), qbt.TorrentFiles{{Name: cgEpisode1, Size: 1000}}},
	}
	folders := []string{"/data", "/data/a", "/data/b", "/links"}
	type torrent struct {
		view     qbittorrent.CrossInstanceTorrentView
		files    qbt.TorrentFiles
		readable bool
	}
	library := map[string]*torrent{}
	rng := rand.New(rand.NewPCG(1, 2))
	place := func(tr *torrent, r release, folder string) {
		tr.files = wanted(r.files)
		// qBittorrent gives the content path from the files, not from the name.
		root, _, _ := strings.Cut(r.files[0].Name, "/")
		tr.view.Name, tr.view.SavePath, tr.view.ContentPath = r.name, folder, folder+"/"+root
		tr.view.Size = sourceFilesTotalSize(r.files)
	}
	pick := func() *torrent {
		keys := slices.Sorted(maps.Keys(library))
		return library[keys[rng.IntN(len(keys))]]
	}
	read := func(_ context.Context, instanceID int, torrents []qbittorrent.CrossInstanceTorrentView) (map[string]qbt.TorrentFiles, error) {
		out := map[string]qbt.TorrentFiles{}
		for _, v := range torrents {
			if tr := library[cacheKeyForTorrentFiles(instanceID, v.Hash)]; tr.readable {
				out[v.Hash] = tr.files
			}
		}
		return out, nil
	}
	members := func(groups []CopyGroup) map[uint64][]qbittorrent.CopyGroupMember {
		out := map[uint64][]qbittorrent.CopyGroupMember{}
		for _, group := range groups {
			out[group.ID] = slices.SortedFunc(slices.Values(group.Members), func(a, b qbittorrent.CopyGroupMember) int {
				return cmp.Or(cmp.Compare(a.InstanceID, b.InstanceID), strings.Compare(a.Hash, b.Hash))
			})
		}
		return out
	}

	svc := newTestService()
	var links *CopyLinks
	grouped, split := 0, 0
	for step := range 400 {
		switch op := rng.IntN(10); {
		case len(library) == 0 || op < 4 && len(library) < 30:
			tr := &torrent{readable: rng.IntN(8) > 0}
			tr.view = qbittorrent.CrossInstanceTorrentView{
				TorrentView: &qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: fmt.Sprintf("%040d", step), AddedOn: int64(step)}},
				InstanceID:  1 + rng.IntN(2),
			}
			place(tr, releases[rng.IntN(len(releases))], folders[rng.IntN(len(folders))])
			library[cacheKeyForTorrentFiles(tr.view.InstanceID, tr.view.Hash)] = tr
		case op < 6:
			tr := pick()
			delete(library, cacheKeyForTorrentFiles(tr.view.InstanceID, tr.view.Hash))
		case op < 8:
			// A move or a rename gives the torrent a new torrent value, as a sync does.
			tr := pick()
			moved := *tr.view.Torrent
			tr.view.TorrentView = &qbittorrent.TorrentView{Torrent: &moved}
			place(tr, releases[rng.IntN(len(releases))], folders[rng.IntN(len(folders))])
		default:
			pick().readable = true
		}
		torrents := map[int][]qbittorrent.CrossInstanceTorrentView{}
		for _, key := range slices.Sorted(maps.Keys(library)) {
			tr := library[key]
			torrents[tr.view.InstanceID] = append(torrents[tr.view.InstanceID], tr.view)
		}

		full, fullLinks, err := svc.FindCopyGroups(t.Context(), torrents, read, nil, nil, copyGroupTestKey)
		require.NoError(t, err)
		var fromLinks []CopyGroup
		fromLinks, links, err = svc.FindCopyGroups(t.Context(), torrents, read, links, nil, copyGroupTestKey)
		require.NoError(t, err)
		require.Equal(t, members(full), members(fromLinks), "step %d", step)
		require.Equal(t, partHolders(fullLinks.parts), partHolders(links.parts), "step %d", step)
		if step%4 == 0 {
			job := &copyGroupsJob{svc: svc, key: copyGroupTestKey, sliceFrom: 3}
			inSlices, sliceLinks, err := job.findInSlices(t.Context(), torrents, read, nil, nil, func() {})
			require.NoError(t, err)
			require.Equal(t, members(full), members(inSlices), "step %d, in slices", step)
			require.Equal(t, partHolders(fullLinks.parts), partHolders(sliceLinks.parts), "step %d, in slices", step)
			require.Equal(t, fullLinks.prints, sliceLinks.prints, "step %d, in slices", step)
		}
		grouped += len(full)
		split += len(fullLinks.parts)
	}
	require.Greater(t, grouped, 400, "the library must hold groups in most steps")
	require.Greater(t, split, 100, "the library must hold Shared parts in most steps")
}

// wanted returns the files with the priority that qBittorrent gives a file
// that it downloads.
func wanted(files qbt.TorrentFiles) qbt.TorrentFiles {
	out := slices.Clone(files)
	for i := range out {
		out[i].Priority = 1
	}
	return out
}

// partHolders gives each Shared part as its size and its sorted holders, so
// two results compare without their part ids.
func partHolders(parts map[int]map[string][]qbittorrent.SharedPart) []string {
	holders := map[qbittorrent.SharedPart][]string{}
	for instanceID, byHash := range parts {
		for hash, list := range byHash {
			for _, part := range list {
				holders[part] = append(holders[part], fmt.Sprintf("%d|%s", instanceID, hash))
			}
		}
	}
	out := make([]string, 0, len(holders))
	for part, list := range holders {
		slices.Sort(list)
		out = append(out, fmt.Sprintf("%d %v", part.Size, list))
	}
	slices.Sort(out)
	return out
}

func TestFindCopyGroups_AsksOnlyForTheChangedTorrentsAndTheirCopies(t *testing.T) {
	a, b, c, d, e, f := cgHash('a'), cgHash('b'), cgHash('c'), cgHash('d'), cgHash('e'), cgHash('f')
	single := "Quiet.Harbor.2019.1080p.BluRay.x264-GRP"
	torrents := map[int][]qbittorrent.CrossInstanceTorrentView{}
	files := map[int]map[string]qbt.TorrentFiles{1: {}}
	add := func(hash, name, folder string, list qbt.TorrentFiles) {
		// The content path is the folder of the files, so two names can share it.
		content, _, _ := strings.Cut(list[0].Name, "/")
		torrents[1] = append(torrents[1], qbittorrent.CrossInstanceTorrentView{
			TorrentView: &qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: hash, Name: name, SavePath: folder, ContentPath: folder + "/" + content, Size: sourceFilesTotalSize(list)}},
			InstanceID:  1,
		})
		files[1][hash] = list
	}
	svc := newTestService()
	var links *CopyLinks
	search := func() (groups []CopyGroup, asked []string) {
		var err error
		groups, links, err = svc.FindCopyGroups(t.Context(), torrents, heldCopyGroupFiles(files, &asked), links, nil, copyGroupTestKey)
		require.NoError(t, err)
		slices.Sort(asked)
		return groups, asked
	}

	// a and b share a folder, c has its own, and the season folder f gives its
	// read from its files.
	add(a, cgPack, "/data", cgPackFiles())
	add(b, "Harbor.Watch.S01.1080p.WEB.h264-OTHER", "/data", cgPackFiles())
	add(c, cgPack, "/data/c", cgPackFiles())
	add(f, "Season 1", "/data/f", qbt.TorrentFiles{{Name: "Season 1/" + cgEpisode1, Size: 1000}, {Name: "Season 1/" + cgEpisode2, Size: 2000}})
	_, asked := search()
	require.Equal(t, []string{"1|" + a, "1|" + b, "1|" + c, "1|" + f}, asked)

	add(e, single, "/data/e", qbt.TorrentFiles{{Name: single + ".mkv", Size: 777}})
	groups, asked := search()
	require.Empty(t, asked, "a torrent with no copy changes no link")
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Members, 4)

	add(d, cgPack, "/data/d", cgPackFiles())
	groups, asked = search()
	require.Equal(t, []string{"1|" + a, "1|" + c, "1|" + d, "1|" + f}, asked, "the new copy is compared with each torrent that can be its copy, and b has another group tag")
	require.Len(t, groups[0].Members, 5)

	torrents[1] = slices.DeleteFunc(torrents[1], func(v qbittorrent.CrossInstanceTorrentView) bool { return v.Hash == b })
	groups, asked = search()
	require.Empty(t, asked, "the other copies keep their links")
	require.Len(t, groups[0].Members, 4)
}

// regression: #4024
func TestFindCopyGroups_ATorrentWhoseListNoLongerReadsLeavesItsGroup(t *testing.T) {
	a, b, c := cgHash('a'), cgHash('b'), cgHash('c')
	copyOf := func(hash string) qbittorrent.CrossInstanceTorrentView {
		folder := "/data/" + hash[:1]
		return qbittorrent.CrossInstanceTorrentView{
			TorrentView: &qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: hash, Name: cgPack, SavePath: folder, ContentPath: folder + "/" + cgPack, Size: 3000}},
			InstanceID:  1,
		}
	}
	torrents := map[int][]qbittorrent.CrossInstanceTorrentView{1: {copyOf(a), copyOf(b)}}
	files := map[int]map[string]qbt.TorrentFiles{1: {a: cgPackFiles(), b: cgPackFiles(), c: cgPackFiles()}}
	svc := newTestService()
	groups, links, err := svc.FindCopyGroups(t.Context(), torrents, heldCopyGroupFiles(files, nil), nil, nil, copyGroupTestKey)
	require.NoError(t, err)
	require.Len(t, groups, 1)

	// The new copy makes the search read a again, and that read gives no list.
	delete(files[1], a)
	torrents[1] = append(torrents[1], copyOf(c))
	groups, _, err = svc.FindCopyGroups(t.Context(), torrents, heldCopyGroupFiles(files, nil), links, nil, copyGroupTestKey)
	require.NoError(t, err)

	require.Len(t, groups, 1)
	assert.ElementsMatch(t, []qbittorrent.CopyGroupMember{{InstanceID: 1, Hash: b}, {InstanceID: 1, Hash: c}}, groups[0].Members)
}

func TestFindCopyGroups_APackThatMovesKeepsTheFileOfItsEpisode(t *testing.T) {
	a, b := cgHash('a'), cgHash('b')
	view := func(hash, name, folder, content string, size int64) qbittorrent.CrossInstanceTorrentView {
		return qbittorrent.CrossInstanceTorrentView{
			TorrentView: &qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: hash, Name: name, SavePath: folder, ContentPath: folder + "/" + content, Size: size}},
			InstanceID:  1,
		}
	}
	episode := view(b, strings.TrimSuffix(cgEpisode1, ".mkv"), "/links", cgEpisode1, 1000)
	torrents := map[int][]qbittorrent.CrossInstanceTorrentView{1: {view(a, cgPack, "/data", cgPack, 3000), episode}}
	files := map[int]map[string]qbt.TorrentFiles{1: {a: wanted(cgPackFiles()), b: wanted(qbt.TorrentFiles{{Name: cgEpisode1, Size: 1000}})}}
	svc := newTestService()
	_, links, err := svc.FindCopyGroups(t.Context(), torrents, heldCopyGroupFiles(files, nil), nil, nil, copyGroupTestKey)
	require.NoError(t, err)
	shared := []string{fmt.Sprintf("1000 [1|%s 1|%s]", a, b), fmt.Sprintf("2000 [1|%s]", a)}
	require.Equal(t, shared, partHolders(links.parts))

	// Only the pack changes, so the search does not compare the episode by its name again.
	torrents[1] = []qbittorrent.CrossInstanceTorrentView{view(a, cgPack, "/moved", cgPack, 3000), episode}
	_, links, err = svc.FindCopyGroups(t.Context(), torrents, heldCopyGroupFiles(files, nil), links, nil, copyGroupTestKey)
	require.NoError(t, err)
	require.Equal(t, shared, partHolders(links.parts))
}

// regression: #4041
func TestFindCopyGroups_APackThatSkipsAFileNoLongerHoldsIt(t *testing.T) {
	a, b := cgHash('a'), cgHash('b')
	view := func(hash, name, content string, size, totalSize int64) qbittorrent.CrossInstanceTorrentView {
		return qbittorrent.CrossInstanceTorrentView{
			TorrentView: &qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: hash, Name: name, SavePath: "/data", ContentPath: "/data/" + content, Size: size, TotalSize: totalSize}},
			InstanceID:  1,
		}
	}
	episode := view(b, strings.TrimSuffix(cgEpisode1, ".mkv"), cgEpisode1, 1000, 1000)
	torrents := map[int][]qbittorrent.CrossInstanceTorrentView{1: {view(a, cgPack, cgPack, 3000, 3000), episode}}
	files := map[int]map[string]qbt.TorrentFiles{1: {a: wanted(cgPackFiles()), b: wanted(qbt.TorrentFiles{{Name: cgEpisode1, Size: 1000}})}}
	svc := newTestService()
	_, links, err := svc.FindCopyGroups(t.Context(), torrents, heldCopyGroupFiles(files, nil), nil, nil, copyGroupTestKey)
	require.NoError(t, err)
	require.Equal(t, []string{fmt.Sprintf("1000 [1|%s 1|%s]", a, b), fmt.Sprintf("2000 [1|%s]", a)}, partHolders(links.parts))

	// The user deselects the second episode of the pack. Its total size stays.
	skipped := wanted(cgPackFiles())
	skipped[1].Priority = 0
	files[1][a] = skipped
	torrents[1] = []qbittorrent.CrossInstanceTorrentView{view(a, cgPack, cgPack, 1000, 3000), episode}
	_, links, err = svc.FindCopyGroups(t.Context(), torrents, heldCopyGroupFiles(files, nil), links, nil, copyGroupTestKey)
	require.NoError(t, err)
	require.Equal(t, []string{fmt.Sprintf("1000 [1|%s 1|%s]", a, b)}, partHolders(links.parts))
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
