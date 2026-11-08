// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
)

// countingSync records the hashes of each file list request.
type countingSync struct {
	qbittorrentSync
	mu    sync.Mutex
	calls []string // "instance:hash"
}

func (c *countingSync) GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	c.mu.Lock()
	for _, h := range hashes {
		c.calls = append(c.calls, fmt.Sprintf("%d:%s", instanceID, normalizeHash(h)))
	}
	c.mu.Unlock()
	return c.qbittorrentSync.GetTorrentFilesBatch(ctx, instanceID, hashes)
}

func (c *countingSync) take() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	calls := c.calls
	c.calls = nil
	return calls
}

// sessionFixtures builds one library on instance 1 with a trusted second
// instance, and optionally an offline third one. A fixture with nil files
// has an unreadable file list.
func sessionFixtures(t *testing.T, offline bool) (*Service, *countingSync, []string) {
	t.Helper()
	root := t.TempDir()
	dir := func(parts ...string) string { return filepath.Join(append([]string{root}, parts...)...) }
	write := func(path string, size int) {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, make([]byte, size), 0o600))
	}

	var lib []crossMatchFixture
	add := func(instanceID int, hash, name, savePath, contentPath string, files qbt.TorrentFiles, size int64) {
		f := crossMatchTorrent(instanceID, hash, name, savePath, contentPath, files)
		f.view.Size = size
		if size == 0 {
			f.view.Size = sourceFilesTotalSize(files)
		}
		lib = append(lib, f)
	}

	// Chain: A (season pack folder) shares e1 with B (flat folder at the
	// same path), B shares an nfo with C; A and C share nothing.
	const pack = "Coral.Reef.S01.1080p.WEB.h264-GRP"
	packDir := dir("tv", pack)
	write(filepath.Join(packDir, "e1.mkv"), 100)
	write(filepath.Join(packDir, "e2.mkv"), 101)
	write(filepath.Join(packDir, "extra.nfo"), 5)
	add(1, "aa01", pack, dir("tv"), packDir, qbt.TorrentFiles{{Name: pack + "/e1.mkv", Size: 100}, {Name: pack + "/e2.mkv", Size: 101}}, 0)
	add(1, "aa02", "Coral.Reef.S01E01.1080p.WEB.h264-OTHER", packDir, packDir, qbt.TorrentFiles{{Name: "e1.mkv", Size: 100}, {Name: "extra.nfo", Size: 5}}, 0)
	add(1, "aa03", "Quiet.Notes.2020.1080p.BluRay.x264-NFO", packDir, filepath.Join(packDir, "extra.nfo"), qbt.TorrentFiles{{Name: "extra.nfo", Size: 5}}, 0)

	// Path group neighbors with disjoint files.
	flat := dir("flat")
	add(1, "bb01", "Night.Ferry.2019.1080p.BluRay.x264-GRP", flat, flat, qbt.TorrentFiles{{Name: "ferry.mkv", Size: 15}, {Name: "ferry.nfo", Size: 2}}, 0)
	add(1, "bb02", "Silent.Shore.2021.1080p.BluRay.x264-GRP", flat, flat, qbt.TorrentFiles{{Name: "shore.mkv", Size: 25}, {Name: "shore.nfo", Size: 3}}, 0)

	// An unreadable torrent that two readable copies pick by name.
	const movie = "Lantern.Bay.2023.1080p.BluRay.x264-GRP"
	add(1, "cc00", movie, dir("x"), dir("x", movie+".mkv"), nil, 1000)
	add(1, "cc01", movie, dir("p1"), dir("p1", movie+".mkv"), qbt.TorrentFiles{{Name: movie + ".mkv", Size: 1000}}, 0)
	add(1, "cc02", movie, dir("p2"), dir("p2", movie+".mkv"), qbt.TorrentFiles{{Name: movie + ".mkv", Size: 1000}}, 0)

	// A readable torrent that picks an unreadable trigger (Release pick).
	const ep = "Amber.Field.S02E03.1080p.WEB.h264-GRP"
	add(1, "dd00", ep, dir("dx"), dir("dx", ep+".mkv"), nil, 700)
	add(1, "dd01", "Amber Field S02E03 1080p WEB h264-GRP", dir("dr"), dir("dr", ep+".mkv"), qbt.TorrentFiles{{Name: ep + ".mkv", Size: 700}}, 0)

	// A season pack and an episode with the same total size, which the
	// matcher keeps apart.
	const probePack, probeEp = "Glass.Hill.S03.1080p.WEB.h264-GRP", "Glass.Hill.S03E01.1080p.WEB.h264-GRP"
	add(1, "ee00", probeEp, dir("ex"), dir("ex", probeEp+".mkv"), nil, 900)
	add(1, "ee01", probePack, dir("ep"), dir("ep", probePack), qbt.TorrentFiles{{Name: probePack + "/" + probeEp + ".mkv", Size: 900}}, 0)

	// The one-way link: two readable names without a group pick the
	// unreadable grouped X (Release pick), and X picks neither.
	const grouped = "Glass.Hill.2020.1080p.BluRay.x264-GRP"
	add(1, "hh00", grouped, dir("hx"), dir("hx", "g.mkv"), nil, 1234)
	add(1, "hh01", "Glass.Hill.2020.1080p.BluRay.x264", dir("h1"), dir("h1", "g.mkv"), qbt.TorrentFiles{{Name: "g.mkv", Size: 1234}}, 0)
	add(1, "hh02", "Glass Hill 2020 1080p BluRay x264", dir("h2"), dir("h2", "g.mkv"), qbt.TorrentFiles{{Name: "g.mkv", Size: 1234}}, 0)

	// A Shared file with a torrent on the trusted second instance.
	const shared = "Still.Harbor.2022.1080p.BluRay.x264-GRP"
	write(dir("shared", shared+".mkv"), 40)
	sharedFiles := qbt.TorrentFiles{{Name: shared + ".mkv", Size: 40}}
	add(1, "ff01", shared, dir("shared"), dir("shared", shared+".mkv"), sharedFiles, 0)
	add(2, "ff02", shared, dir("shared"), dir("shared", shared+".mkv"), sharedFiles, 0)
	// A torrent with no cross-seed at all.
	add(1, "gg01", "Lone.Pier.2024.1080p.BluRay.x264-GRP", dir("lone"), dir("lone", "pier.mkv"), qbt.TorrentFiles{{Name: "pier.mkv", Size: 77}}, 0)

	svc, sm := deleteMatchService(lib...)
	for _, f := range lib {
		if f.files == nil {
			delete(sm.files, normalizeHash(f.view.Hash))
		}
	}
	instances := []*models.Instance{
		{ID: 1, Name: "home", IsActive: true, HasLocalFilesystemAccess: true},
		{ID: 2, Name: "second", IsActive: true, HasLocalFilesystemAccess: true},
	}
	if offline {
		instances = append(instances, &models.Instance{ID: 3, Name: "third", IsActive: true, HasLocalFilesystemAccess: true})
	}
	svc.instanceStore = newOrderedInstanceStore(instances...)
	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
	counter := &countingSync{qbittorrentSync: &failingPeerSyncManager{tierSyncManager: sm, failInstanceID: 3}}
	svc.syncManager = counter

	var hashes []string
	for _, f := range lib {
		if f.view.InstanceID == 1 {
			hashes = append(hashes, f.view.Hash)
		}
	}
	return svc, counter, hashes
}

var sessionNeeds = CrossMatchNeeds{SameExists: true, SameSeeding: true, SameTags: true, SameContent: true, OtherExists: true, OtherSeeding: true}

// componentOf follows the links both ways.
func componentOf(links map[string][]string, hash string) []string {
	undirected := make(map[string]map[string]struct{})
	for a, next := range links {
		for _, b := range next {
			for _, p := range [2][2]string{{a, b}, {b, a}} {
				if undirected[p[0]] == nil {
					undirected[p[0]] = make(map[string]struct{})
				}
				undirected[p[0]][p[1]] = struct{}{}
			}
		}
	}
	seen := map[string]struct{}{hash: {}}
	queue := []string{hash}
	for len(queue) > 0 {
		h := queue[0]
		queue = queue[1:]
		for n := range undirected[h] {
			if _, ok := seen[n]; !ok {
				seen[n] = struct{}{}
				queue = append(queue, n)
			}
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// oracleGroups builds groups as automations.buildCrossSeedGroups does: it
// follows the links outward only, in list order, and the last group wins.
func oracleGroups(order []string, sharing map[string][]string) map[string][]string {
	groups := make(map[string][]string)
	for _, t := range order {
		if _, grouped := groups[t]; grouped || len(sharing[t]) == 0 {
			continue
		}
		var members []string
		seen := map[string]struct{}{t: {}}
		queue := []string{t}
		for len(queue) > 0 {
			h := queue[0]
			queue = queue[1:]
			members = append(members, h)
			for _, next := range sharing[h] {
				if _, ok := seen[next]; !ok {
					seen[next] = struct{}{}
					queue = append(queue, next)
				}
			}
		}
		slices.Sort(members)
		for _, m := range members {
			groups[m] = members
		}
	}
	return groups
}

func oracleComponent(groups map[string][]string, hash string) []string {
	if g := groups[hash]; len(g) > 0 {
		return g
	}
	return []string{hash}
}

func setHas(m map[string]struct{}, h string) bool { _, ok := m[h]; return ok }

func openTestSession(t *testing.T, svc *Service) CrossMatchSession {
	t.Helper()
	sess, err := svc.OpenCrossMatchSession(t.Context(), 1, sessionNeeds)
	require.NoError(t, err)
	return sess
}

func TestCrossMatchSession_ScopedAskEqualsWholeLibrary(t *testing.T) {
	t.Parallel()

	chain := []string{"aa01", "aa02", "aa03"}
	unread := []string{"cc00", "cc01", "cc02"}
	release := []string{"dd00", "dd01"}
	oneWay := []string{"hh00", "hh01", "hh02"}
	// The groups of buildCrossSeedGroups. For the one-way link, its group of
	// each of hh00, hh01 and hh02 depends on the list order.
	wantOracle := map[string][]string{
		"aa01": chain, "aa02": chain, "aa03": chain,
		"bb01": {"bb01"}, "bb02": {"bb02"},
		"cc00": unread, "cc01": unread, "cc02": unread,
		"dd00": release, "dd01": release,
		"ee00": {"ee00"}, "ee01": {"ee01"},
		"ff01": {"ff01"}, "gg01": {"gg01"},
		"hh00": {"hh00", "hh02"}, "hh01": {"hh00", "hh01"}, "hh02": {"hh00", "hh02"},
	}
	wantSession := maps.Clone(wantOracle)
	for _, h := range oneWay {
		wantSession[h] = oneWay
	}

	for _, offline := range []bool{false, true} {
		t.Run(map[bool]string{false: "second instance online", true: "offline third instance"}[offline], func(t *testing.T) {
			t.Parallel()
			svc, _, library := sessionFixtures(t, offline)
			ctx := t.Context()

			full := svc.BuildCrossMatchSets(ctx, 1, CrossMatchNeeds{
				SameExists: true, SameSeeding: true, SameTags: true, SameContent: true,
				OtherExists: true, OtherSeeding: true, OtherShared: true,
			})
			require.NoError(t, full.Err)
			groups := oracleGroups(library, full.SameInstanceContentByHash)
			for _, h := range library {
				require.Equal(t, wantOracle[h], oracleComponent(groups, h), "oracle group of %s", h)
			}
			require.Equal(t, map[string]struct{}{"ff01": {}}, full.OtherInstanceShared)
			if offline {
				require.Equal(t, []string{"third"}, full.OfflineLocalInstances)
				require.Len(t, full.OtherInstanceSharedUnknown, len(library))
			} else {
				require.Empty(t, full.OtherInstanceSharedUnknown)
			}

			whole := openTestSession(t, svc).Resolve(ctx, ResolveRequest{All: true, Groups: true, OtherShared: true})
			for _, ask := range [][]string{
				{"aa03"}, {"bb01"}, {"cc00"}, {"cc01"}, {"dd00"}, {"dd01"}, {"ee00"}, {"ee01"},
				{"ff01"}, {"gg01"}, {"hh00"}, {"hh01"}, {"hh02"},
				{"aa01", "aa02"}, // two Delete triggers that share files
				{"aa01", "cc01"},
			} {
				scoped := openTestSession(t, svc).Resolve(ctx, ResolveRequest{Hashes: ask, Groups: true, OtherShared: true})
				facts := openTestSession(t, svc).Resolve(ctx, ResolveRequest{Hashes: ask})
				for _, h := range ask {
					require.Equal(t, wantSession[h], componentOf(scoped.SameInstanceContentByHash, h), "scoped group of %s", h)
					require.Equal(t, wantSession[h], componentOf(whole.SameInstanceContentByHash, h), "whole-library group of %s", h)
					compareFacts(t, "groups", h, scoped, full)
					compareFacts(t, "no groups", h, facts, full)
				}
				require.Equal(t, full.OtherInstanceShared, scoped.OtherInstanceShared, "the library-wide shared check")
				require.Equal(t, full.OtherInstanceSharedUnknown, scoped.OtherInstanceSharedUnknown)
				require.Equal(t, full.OfflineLocalInstances, scoped.OfflineLocalInstances)
			}
		})
	}
}

func compareFacts(t *testing.T, mode, h string, scoped, full *CrossMatchResult) {
	t.Helper()
	for name, pair := range map[string][2]map[string]struct{}{
		"SameExists":   {scoped.SameInstanceExists, full.SameInstanceExists},
		"SameSeeding":  {scoped.SameInstanceSeeding, full.SameInstanceSeeding},
		"SameUnknown":  {scoped.SameInstanceUnknown, full.SameInstanceUnknown},
		"OtherExists":  {scoped.OtherInstanceExists, full.OtherInstanceExists},
		"OtherSeeding": {scoped.OtherInstanceSeeding, full.OtherInstanceSeeding},
		"OtherUnknown": {scoped.OtherInstanceUnknown, full.OtherInstanceUnknown},
	} {
		require.Equal(t, setHas(pair[1], h), setHas(pair[0], h), "(%s) %s %s", mode, h, name)
	}
	require.Equal(t, full.SameInstanceTagsByHash[h], scoped.SameInstanceTagsByHash[h], h)
}

func TestCrossMatchSession_ReadsOnlyTheClosure(t *testing.T) {
	t.Parallel()
	svc, counter, _ := sessionFixtures(t, false)
	ctx := t.Context()

	sess := openTestSession(t, svc)
	require.Empty(t, counter.take(), "open reads no file list")

	sess.Resolve(ctx, ResolveRequest{Groups: true})
	require.Empty(t, counter.take(), "an empty request reads nothing")

	sess.Resolve(ctx, ResolveRequest{Hashes: []string{"gg01"}, Groups: true})
	require.Empty(t, counter.take(), "a torrent with no pick and no path group needs no list")

	result := sess.Resolve(ctx, ResolveRequest{Hashes: []string{"aa03"}, Groups: true})
	require.ElementsMatch(t, []string{"1:aa01", "1:aa02", "1:aa03"}, counter.take(), "the chain reads only its path group")
	require.ElementsMatch(t, []string{"aa01", "aa02", "aa03"}, slices.Collect(maps.Keys(result.MemberFiles)))

	sess.Resolve(ctx, ResolveRequest{Hashes: []string{"hh01"}, Groups: true})
	// hh00 twice: the batch misses it and get reads it again alone.
	require.ElementsMatch(t, []string{"1:hh00", "1:hh01", "1:hh02", "1:hh00"}, counter.take(), "the one-way link reads the torrents that pick X")

	sess.Resolve(ctx, ResolveRequest{Hashes: []string{"aa03", "hh01"}, Groups: true})
	require.Empty(t, counter.take(), "a second ask reads from the memo")

	sess.Resolve(ctx, ResolveRequest{Hashes: []string{"ff01"}, OtherShared: true})
	require.ElementsMatch(t, []string{"1:ff01", "2:ff02"}, counter.take(), "the shared check reads the trusted candidates")
	sess.Resolve(ctx, ResolveRequest{OtherShared: true})
	require.Empty(t, counter.take(), "the shared check runs once")

	fresh := openTestSession(t, svc)
	fresh.Resolve(ctx, ResolveRequest{All: true, Groups: true})
	all := counter.take()
	full := svc.BuildCrossMatchSets(ctx, 1, sessionNeeds)
	require.NoError(t, full.Err)
	require.ElementsMatch(t, counter.take(), all, "a whole-library ask reads what BuildCrossMatchSets reads")

	// The fixture names need no files, so the local reads fetch nothing.
	reads := svc.readLocalTorrents(ctx, 1, viewTorrents(must(svc.syncManager.GetCachedInstanceTorrents(ctx, 1))))
	require.Empty(t, reads)
	require.Empty(t, counter.take())
}

// The first Resolve fills the local reads of every name that needs files, as
// BuildCrossMatchSets does, also for a scoped ask.
func TestCrossMatchSession_LocalReadsFillTheLibrary(t *testing.T) {
	t.Parallel()
	svc, sm := deleteMatchService(
		crossMatchTorrent(1, "aaaa", "Lone.Pier.2024.1080p.BluRay.x264-GRP", "/d/a", "/d/a/a.mkv", qbt.TorrentFiles{{Name: "a.mkv", Size: 1}}),
		crossMatchTorrent(1, "bbbb", "Season 1", "/d/b", "/d/b/Season 1", qbt.TorrentFiles{{Name: "Season 1/Show.S01E01.mkv", Size: 2}}),
		crossMatchTorrent(1, "cccc", "Season 2", "/d/c", "/d/c/Season 2", qbt.TorrentFiles{{Name: "Season 2/Show.S02E01.mkv", Size: 3}}),
	)
	svc.automationSettingsLoader = func(context.Context) (*models.CrossSeedAutomationSettings, error) {
		return &models.CrossSeedAutomationSettings{}, nil
	}
	counter := &countingSync{qbittorrentSync: sm}
	svc.syncManager = counter
	require.True(t, nameNeedsFiles(svc.matcher().readName("Season 1").release))

	sess := openTestSession(t, svc)
	require.Empty(t, counter.take())
	sess.Resolve(t.Context(), ResolveRequest{Hashes: []string{"aaaa"}, Groups: true})
	require.ElementsMatch(t, []string{"1:bbbb", "1:cccc"}, counter.take())
}

func TestCrossMatchSession_InstanceListFailureIsUnknown(t *testing.T) {
	t.Parallel()
	svc, _, library := sessionFixtures(t, false)
	svc.instanceStore = &orderedInstanceStore{listErr: errors.New("instance list unavailable")}

	result := openTestSession(t, svc).Resolve(t.Context(), ResolveRequest{Hashes: []string{"aa01"}, Groups: true, OtherShared: true})
	require.Equal(t, []string{"aa01", "aa02", "aa03"}, componentOf(result.SameInstanceContentByHash, "aa01"), "the groups stay")
	require.Len(t, result.SameInstanceUnknown, len(library))
	require.Len(t, result.OtherInstanceUnknown, len(library))
	require.Len(t, result.OtherInstanceSharedUnknown, len(library))
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
