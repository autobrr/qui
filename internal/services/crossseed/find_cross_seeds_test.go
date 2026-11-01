// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"os"
	"path/filepath"
	"slices"
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

	matches, err := findDeleteMatches(t, svc, "aaaa")
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
		deleted, err := findDeleteMatches(t, svc, hash)
		require.NoError(t, err)
		tab := findCrossSeeds(t, svc, hash)
		for key, tier := range tabTiers(deleted) {
			require.Equal(t, tier, tab[key], "torrent %s, match %s", hash, key)
			checked++
		}
	}
	// aaaa, bbbb, and cccc share files; dddd and eeee are linked. dddd on
	// instance 2 shares the file of dddd.
	require.Equal(t, 9, checked)
}

// The tab labels a torrent on another local instance Content when it reads a
// file of the source through the same folder entry, also through a second
// mount. A hardlinked copy stays Hardlink, and an SSH instance never gets
// Content.
// regression: #3585
func TestFindCrossSeedsFindsSharedFilesOnOtherInstances(t *testing.T) {
	t.Parallel()

	const (
		movie   = "Quiet.Coast.2020.1080p.BluRay.x264-GRP"
		linked  = "Silent.Shore.2021.1080p.BluRay.x264-GRP"
		pack    = "Harbor.Watch.S01.1080p.WEB.h264-GRP"
		episode = "Harbor.Watch.S01E01.1080p.WEB.h264-GRP"
	)
	root := t.TempDir()
	dataDir, linkDir, mountDir := filepath.Join(root, "data"), filepath.Join(root, "links"), filepath.Join(root, "mount")
	for _, dir := range []string{filepath.Join(dataDir, pack), linkDir} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	// A symlinked folder stands in for a second mount of the data folder.
	require.NoError(t, os.Symlink(dataDir, mountDir))
	write := func(name, data string) {
		require.NoError(t, os.WriteFile(filepath.Join(dataDir, name), []byte(data), 0o600))
	}
	write(movie+".mkv", "movie")
	write(linked+".mkv", "linked")
	require.NoError(t, os.Link(filepath.Join(dataDir, linked+".mkv"), filepath.Join(linkDir, linked+".mkv")))
	write(pack+"/"+episode+".mkv", "ep01")
	write(pack+"/Harbor.Watch.S01E02.1080p.WEB.h264-GRP.mkv", "ep002")

	movieFiles := qbt.TorrentFiles{{Name: movie + ".mkv", Size: 5}}
	linkedFiles := qbt.TorrentFiles{{Name: linked + ".mkv", Size: 6}}
	packDir := filepath.Join(dataDir, pack)
	library := []crossMatchFixture{
		crossMatchTorrent(1, "aaaa", movie, dataDir, filepath.Join(dataDir, movie+".mkv"), movieFiles),
		crossMatchTorrent(1, "bbbb", linked, dataDir, filepath.Join(dataDir, linked+".mkv"), linkedFiles),
		crossMatchTorrent(1, "cccc", pack, dataDir, packDir, qbt.TorrentFiles{
			{Name: pack + "/" + episode + ".mkv", Size: 4},
			{Name: pack + "/Harbor.Watch.S01E02.1080p.WEB.h264-GRP.mkv", Size: 5},
		}),
		// One folder that both instances read.
		crossMatchTorrent(2, "dddd", movie, dataDir, filepath.Join(dataDir, movie+".mkv"), movieFiles),
		// The same folder through a second mount.
		crossMatchTorrent(2, "eeee", movie, mountDir, filepath.Join(mountDir, movie+".mkv"), movieFiles),
		// A shared file that also has a hardlink elsewhere.
		crossMatchTorrent(2, "ffff", linked, dataDir, filepath.Join(dataDir, linked+".mkv"), linkedFiles),
		// The hardlinked copy.
		crossMatchTorrent(2, "gggg", linked, linkDir, filepath.Join(linkDir, linked+".mkv"), linkedFiles),
		// An episode inside the folder of the season pack. Only the path finds it.
		crossMatchTorrent(2, "hhhh", episode, packDir, filepath.Join(packDir, episode+".mkv"),
			qbt.TorrentFiles{{Name: episode + ".mkv", Size: 4}}),
		// Another name and release through the second mount. Only the total size finds it.
		crossMatchTorrent(2, "jjjj", "qc-home-encode", mountDir, filepath.Join(mountDir, movie+".mkv"), movieFiles),
		// An SSH instance that reads the same folder.
		crossMatchTorrent(3, "iiii", movie, dataDir, filepath.Join(dataDir, movie+".mkv"), movieFiles),
	}
	for i := range library {
		library[i] = withTotalSize(library[i])
	}
	svc, _ := deleteMatchService(library...)
	svc.instanceStore = newOrderedInstanceStore(
		&models.Instance{ID: 1, Name: "home", IsActive: true, HasLocalFilesystemAccess: true},
		&models.Instance{ID: 2, Name: "second", IsActive: true, HasLocalFilesystemAccess: true},
		&models.Instance{ID: 3, Name: "box", IsActive: true, SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key",
			SSHHostKeyEncrypted: "enc-hostkey", SSHTier: models.SSHTierExec},
	)
	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))

	for hash, want := range map[string]map[string]string{
		"aaaa": {"2/dddd": matchTypeContentPath, "2/eeee": matchTypeContentPath, "2/jjjj": matchTypeContentPath, "3/iiii": matchTypeName},
		"bbbb": {"2/ffff": matchTypeContentPath, "2/gggg": matchTypeHardlink},
		"cccc": {"2/hhhh": matchTypeContentPath},
	} {
		response, err := svc.FindCrossSeeds(t.Context(), 1, hash)
		require.NoError(t, err)
		require.Equal(t, want, tabTiers(response.Matches), "torrent %s", hash)
		require.Len(t, response.Matches, len(want), "torrent %s lists a match twice", hash)
	}
}

// A source without trusted identity has no Shared file, so a torrent on another
// instance that only shares the total size is no candidate. Its file list is
// not read.
func TestFindCrossSeedsSkipsSizeOnlyCandidatesWithoutTrustedSource(t *testing.T) {
	t.Parallel()

	files := qbt.TorrentFiles{{Name: "Quiet.Coast.2020.1080p.BluRay.x264-GRP.mkv", Size: 5}}
	svc, sm := deleteMatchService(
		withTotalSize(crossMatchTorrent(1, "aaaa", "Quiet.Coast.2020.1080p.BluRay.x264-GRP", "/data", "/data/Quiet.Coast.2020.1080p.BluRay.x264-GRP.mkv", files)),
		withTotalSize(crossMatchTorrent(2, "kkkk", "qc-home-encode", "/data", "/data/Quiet.Coast.2020.1080p.BluRay.x264-GRP.mkv", files)),
	)
	sm.failHashes = map[string]bool{"kkkk": true}
	svc.instanceStore = newOrderedInstanceStore(
		&models.Instance{ID: 1, Name: "remote", IsActive: true},
		&models.Instance{ID: 2, Name: "second", IsActive: true, HasLocalFilesystemAccess: true},
	)

	response, err := svc.FindCrossSeeds(t.Context(), 1, "aaaa")
	require.NoError(t, err)
	require.Empty(t, response.Matches)
}

// The automation sets hold each torrent on the instance that shares a file
// with a torrent on another local instance, also through a second mount and
// when the file has a hardlink elsewhere. A hardlinked copy is no Shared file.
// An offline local instance makes every torrent unknown and is named. A
// turned-off instance changes nothing.
// regression: #3591
func TestBuildCrossMatchSetsFindsSharedFilesOnOtherInstances(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dataDir, linkDir, mountDir := filepath.Join(root, "data"), filepath.Join(root, "links"), filepath.Join(root, "mount")
	for _, dir := range []string{dataDir, linkDir} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	// A symlinked folder stands in for a second mount of the data folder.
	require.NoError(t, os.Symlink(dataDir, mountDir))

	var library []crossMatchFixture
	add := func(instanceID int, hash, name, dir string) {
		files := qbt.TorrentFiles{{Name: name + ".mkv", Size: int64(len(name))}}
		library = append(library, withTotalSize(crossMatchTorrent(instanceID, hash, name, dir, filepath.Join(dir, name+".mkv"), files)))
	}
	for _, name := range []string{
		"Quiet.Coast.2020.1080p.BluRay.x264-GRP",
		"Silent.Shore.2021.1080p.BluRay.x264-GRP",
		"Still.Harbor.2022.1080p.BluRay.x264-GRP",
		"Calm.Bay.2023.1080p.BluRay.x264-GRP",
		"Lone.Pier.2024.1080p.BluRay.x264-GRP",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dataDir, name+".mkv"), []byte(name), 0o600))
	}
	for _, name := range []string{"Still.Harbor.2022.1080p.BluRay.x264-GRP", "Calm.Bay.2023.1080p.BluRay.x264-GRP"} {
		require.NoError(t, os.Link(filepath.Join(dataDir, name+".mkv"), filepath.Join(linkDir, name+".mkv")))
	}
	add(1, "aaaa", "Quiet.Coast.2020.1080p.BluRay.x264-GRP", dataDir)
	add(1, "bbbb", "Silent.Shore.2021.1080p.BluRay.x264-GRP", dataDir)
	add(1, "cccc", "Still.Harbor.2022.1080p.BluRay.x264-GRP", dataDir)
	add(1, "dddd", "Calm.Bay.2023.1080p.BluRay.x264-GRP", dataDir)
	add(1, "eeee", "Lone.Pier.2024.1080p.BluRay.x264-GRP", dataDir)
	// One folder that both instances read.
	add(2, "ffff", "Quiet.Coast.2020.1080p.BluRay.x264-GRP", dataDir)
	// The same folder through a second mount.
	add(2, "gggg", "Silent.Shore.2021.1080p.BluRay.x264-GRP", mountDir)
	// A shared file that also has a hardlink elsewhere.
	add(2, "hhhh", "Still.Harbor.2022.1080p.BluRay.x264-GRP", dataDir)
	// A hardlinked copy.
	add(2, "iiii", "Calm.Bay.2023.1080p.BluRay.x264-GRP", linkDir)
	// An episode inside the folder of a season pack. Only the path finds it.
	const pack, episode = "Harbor.Watch.S01.1080p.WEB.h264-GRP", "Harbor.Watch.S01E01.1080p.WEB.h264-GRP"
	packDir := filepath.Join(dataDir, pack)
	require.NoError(t, os.MkdirAll(packDir, 0o755))
	for _, name := range []string{episode, "Harbor.Watch.S01E02.1080p.WEB.h264-GRP"} {
		require.NoError(t, os.WriteFile(filepath.Join(packDir, name+".mkv"), []byte(name), 0o600))
	}
	library = append(library, withTotalSize(crossMatchTorrent(1, "kkkk", pack, dataDir, packDir, qbt.TorrentFiles{
		{Name: pack + "/" + episode + ".mkv", Size: 4},
		{Name: pack + "/Harbor.Watch.S01E02.1080p.WEB.h264-GRP.mkv", Size: 5},
	})))
	add(2, "llll", episode, packDir)
	// Two flat torrents in one save path that share only a subtitle.
	flatDir := filepath.Join(root, "flat")
	require.NoError(t, os.MkdirAll(flatDir, 0o755))
	for _, name := range []string{"Night.Ferry.mkv", "Night.Ferry.REPACK.mkv", "Night.Ferry.srt"} {
		require.NoError(t, os.WriteFile(filepath.Join(flatDir, name), []byte(name), 0o600))
	}
	library = append(library,
		withTotalSize(crossMatchTorrent(1, "mmmm", "Night.Ferry.2019.1080p.BluRay.x264-GRP", flatDir, flatDir, qbt.TorrentFiles{
			{Name: "Night.Ferry.mkv", Size: 15}, {Name: "Night.Ferry.srt", Size: 15},
		})),
		withTotalSize(crossMatchTorrent(2, "nnnn", "Night.Ferry.2019.REPACK.1080p.BluRay.x264-GRP", flatDir, flatDir, qbt.TorrentFiles{
			{Name: "Night.Ferry.REPACK.mkv", Size: 22}, {Name: "Night.Ferry.srt", Size: 15},
		})),
	)

	needs := CrossMatchNeeds{OtherShared: true}
	shared := map[string]struct{}{"aaaa": {}, "bbbb": {}, "cccc": {}, "kkkk": {}, "mmmm": {}}
	for _, tt := range []struct {
		name        string
		third       *models.Instance
		wantUnknown map[string]struct{}
		wantOffline []string
	}{
		{name: "two instances", wantUnknown: map[string]struct{}{}},
		{
			name:        "offline local instance",
			third:       &models.Instance{ID: 3, Name: "third", IsActive: true, HasLocalFilesystemAccess: true},
			wantUnknown: map[string]struct{}{"aaaa": {}, "bbbb": {}, "cccc": {}, "dddd": {}, "eeee": {}, "kkkk": {}, "mmmm": {}},
			wantOffline: []string{"third"},
		},
		{
			name:        "turned-off local instance",
			third:       &models.Instance{ID: 3, Name: "third", HasLocalFilesystemAccess: true},
			wantUnknown: map[string]struct{}{},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, sm := deleteMatchService(library...)
			svc.syncManager = &failingPeerSyncManager{tierSyncManager: sm, failInstanceID: 3}
			instances := []*models.Instance{
				{ID: 1, Name: "home", IsActive: true, HasLocalFilesystemAccess: true},
				{ID: 2, Name: "second", IsActive: true, HasLocalFilesystemAccess: true},
			}
			if tt.third != nil {
				instances = append(instances, tt.third)
			}
			svc.instanceStore = newOrderedInstanceStore(instances...)
			svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))

			result := svc.BuildCrossMatchSets(t.Context(), 1, needs)
			require.NoError(t, result.Err)
			require.Equal(t, shared, result.OtherInstanceShared)
			require.Equal(t, tt.wantUnknown, result.OtherInstanceSharedUnknown)
			require.Equal(t, tt.wantOffline, result.OfflineLocalInstances)
		})
	}
}

// The delete check lists a torrent on another local instance only for a
// Shared file, also through a second mount and when the file has a hardlink
// elsewhere. A hardlinked copy and an SSH instance stay out. An offline local
// instance and a turned-off local instance are named, and the other matches
// are still returned. A torrent of the selection is never a match.
// regression: #3592
func TestFindDeleteMatchesFindsSharedFilesOnOtherInstances(t *testing.T) {
	t.Parallel()

	const (
		movie   = "Quiet.Coast.2020.1080p.BluRay.x264-GRP"
		linked  = "Silent.Shore.2021.1080p.BluRay.x264-GRP"
		pack    = "Harbor.Watch.S01.1080p.WEB.h264-GRP"
		episode = "Harbor.Watch.S01E01.1080p.WEB.h264-GRP"
	)
	root := t.TempDir()
	dataDir, linkDir, mountDir := filepath.Join(root, "data"), filepath.Join(root, "links"), filepath.Join(root, "mount")
	for _, dir := range []string{filepath.Join(dataDir, pack), linkDir} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	// A symlinked folder stands in for a second mount of the data folder.
	require.NoError(t, os.Symlink(dataDir, mountDir))
	write := func(name, data string) {
		require.NoError(t, os.WriteFile(filepath.Join(dataDir, name), []byte(data), 0o600))
	}
	write(movie+".mkv", "movie")
	write(linked+".mkv", "linked")
	require.NoError(t, os.Link(filepath.Join(dataDir, linked+".mkv"), filepath.Join(linkDir, linked+".mkv")))
	write(pack+"/"+episode+".mkv", "ep01")
	write(pack+"/Harbor.Watch.S01E02.1080p.WEB.h264-GRP.mkv", "ep002")

	movieFiles := qbt.TorrentFiles{{Name: movie + ".mkv", Size: 5}}
	linkedFiles := qbt.TorrentFiles{{Name: linked + ".mkv", Size: 6}}
	packDir := filepath.Join(dataDir, pack)
	library := []crossMatchFixture{
		crossMatchTorrent(1, "aaaa", movie, dataDir, filepath.Join(dataDir, movie+".mkv"), movieFiles),
		crossMatchTorrent(1, "bbbb", linked, dataDir, filepath.Join(dataDir, linked+".mkv"), linkedFiles),
		crossMatchTorrent(1, "cccc", pack, dataDir, packDir, qbt.TorrentFiles{
			{Name: pack + "/" + episode + ".mkv", Size: 4},
			{Name: pack + "/Harbor.Watch.S01E02.1080p.WEB.h264-GRP.mkv", Size: 5},
		}),
		// One folder that both instances read.
		crossMatchTorrent(2, "dddd", movie, dataDir, filepath.Join(dataDir, movie+".mkv"), movieFiles),
		// The same folder through a second mount.
		crossMatchTorrent(2, "eeee", movie, mountDir, filepath.Join(mountDir, movie+".mkv"), movieFiles),
		// A shared file that also has a hardlink elsewhere.
		crossMatchTorrent(2, "ffff", linked, dataDir, filepath.Join(dataDir, linked+".mkv"), linkedFiles),
		// The hardlinked copy.
		crossMatchTorrent(2, "gggg", linked, linkDir, filepath.Join(linkDir, linked+".mkv"), linkedFiles),
		// An episode inside the folder of the season pack.
		crossMatchTorrent(2, "hhhh", episode, packDir, filepath.Join(packDir, episode+".mkv"),
			qbt.TorrentFiles{{Name: episode + ".mkv", Size: 4}}),
		// An SSH instance that reads the same folder.
		crossMatchTorrent(3, "iiii", movie, dataDir, filepath.Join(dataDir, movie+".mkv"), movieFiles),
	}
	for i := range library {
		library[i] = withTotalSize(library[i])
	}
	instances := []*models.Instance{
		{ID: 1, Name: "home", IsActive: true, HasLocalFilesystemAccess: true},
		{ID: 2, Name: "second", IsActive: true, HasLocalFilesystemAccess: true},
		{ID: 3, Name: "box", IsActive: true, SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key",
			SSHHostKeyEncrypted: "enc-hostkey", SSHTier: models.SSHTierExec},
	}
	shared := map[string]string{
		"2/dddd": matchTypeContentPath,
		"2/eeee": matchTypeContentPath,
		"2/ffff": matchTypeContentPath,
		"2/hhhh": matchTypeContentPath,
	}

	for _, tt := range []struct {
		name          string
		extra         []*models.Instance
		selection     map[int][]string
		wantMatches   map[string]string
		wantUnchecked []UncheckedInstance
	}{
		{
			name:          "two local instances",
			selection:     map[int][]string{1: {"aaaa", "bbbb", "cccc"}},
			wantMatches:   shared,
			wantUnchecked: []UncheckedInstance{},
		},
		{
			name: "offline local instance",
			extra: []*models.Instance{
				{ID: 4, Name: "offline", IsActive: true, HasLocalFilesystemAccess: true},
			},
			selection:     map[int][]string{1: {"aaaa", "bbbb", "cccc"}},
			wantMatches:   shared,
			wantUnchecked: []UncheckedInstance{{InstanceID: 4, InstanceName: "offline", Reason: UncheckedOffline}},
		},
		{
			name: "turned-off instances",
			extra: []*models.Instance{
				{ID: 5, Name: "off-local", HasLocalFilesystemAccess: true},
				{ID: 6, Name: "off-api-only"},
			},
			selection:     map[int][]string{1: {"aaaa", "bbbb", "cccc"}},
			wantMatches:   shared,
			wantUnchecked: []UncheckedInstance{{InstanceID: 5, InstanceName: "off-local", Reason: UncheckedDisabled}},
		},
		{
			name:          "selection on both instances",
			selection:     map[int][]string{1: {"aaaa"}, 2: {"dddd", "eeee"}},
			wantMatches:   map[string]string{},
			wantUnchecked: []UncheckedInstance{},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, sm := deleteMatchService(library...)
			svc.syncManager = &failingPeerSyncManager{tierSyncManager: sm, failInstanceID: 4}
			svc.instanceStore = newOrderedInstanceStore(slices.Concat(instances, tt.extra)...)
			svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))

			response, err := svc.FindDeleteMatches(t.Context(), tt.selection)
			require.NoError(t, err)
			require.Equal(t, tt.wantMatches, tabTiers(response.Matches))
			require.Len(t, response.Matches, len(tt.wantMatches), "a match is listed twice")
			require.Equal(t, tt.wantUnchecked, response.Unchecked)
		})
	}
}
