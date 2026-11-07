// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// PROTOTYPE #3895: differential and counting tests for the throwaway session.

package crossseed

import (
	"context"
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

	// Path group neighbours with disjoint files.
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

	// Asymmetry probe: a season pack and an episode with the same total size.
	const probePack, probeEp = "Glass.Hill.S03.1080p.WEB.h264-GRP", "Glass.Hill.S03E01.1080p.WEB.h264-GRP"
	add(1, "ee00", probeEp, dir("ex"), dir("ex", probeEp+".mkv"), nil, 900)
	add(1, "ee01", probePack, dir("ep"), dir("ep", probePack), qbt.TorrentFiles{{Name: probePack + "/" + probeEp + ".mkv", Size: 900}}, 0)

	// One-way links: two readable names without a group pick the unreadable
	// grouped X (Release pick), and X picks neither (a missing group on the
	// candidate side is a difference the matcher does not relax).
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

// todayGroups copies automations.buildCrossSeedGroups (service.go): a BFS
// that follows links outward only, in list order, the last group wins.
func todayGroups(order []string, sharing map[string][]string) map[string][]string {
	present := make(map[string]bool, len(order))
	for _, h := range order {
		present[h] = true
	}
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
			if present[h] {
				members = append(members, h)
			}
			for _, next := range sharing[h] {
				if _, ok := seen[next]; !ok {
					seen[next] = struct{}{}
					queue = append(queue, next)
				}
			}
		}
		for _, m := range members {
			groups[m] = members
		}
	}
	return groups
}

func todayComponent(groups map[string][]string, hash string) []string {
	if g := groups[hash]; len(g) > 0 {
		return slices.Sorted(slices.Values(g))
	}
	return []string{hash}
}

func setHas(m map[string]struct{}, h string) bool { _, ok := m[h]; return ok }

func TestPrototypeCrossMatchSessionDifferential(t *testing.T) {
	for _, offline := range []bool{false, true} {
		t.Run(map[bool]string{false: "second instance online", true: "offline third instance"}[offline], func(t *testing.T) {
			svc, counter, library := sessionFixtures(t, offline)
			ctx := t.Context()

			today := svc.BuildCrossMatchSets(ctx, 1, CrossMatchNeeds{
				SameExists: true, SameSeeding: true, SameTags: true, SameContent: true,
				OtherExists: true, OtherSeeding: true, OtherShared: true,
			})
			require.NoError(t, today.Err)
			todayCalls := counter.take()
			t.Logf("today content links: %v", today.SameInstanceContentByHash)
			t.Logf("today BuildCrossMatchSets file list hashes asked: %d (unique %d)", len(todayCalls), len(slices.Compact(slices.Sorted(slices.Values(todayCalls)))))
			groups := todayGroups(library, today.SameInstanceContentByHash)
			t.Logf("today OtherInstanceShared %v SharedUnknown %v Offline %v", today.OtherInstanceShared, today.OtherInstanceSharedUnknown, today.OfflineLocalInstances)
			oneWay := map[string][]string{"hh00": {"hh00", "hh01", "hh02"}, "hh01": {"hh00", "hh01", "hh02"}, "hh02": {"hh00", "hh01", "hh02"}}

			whole := svc.prototypeResolve(t, ResolveRequest{All: true, Groups: true, OtherShared: true})
			wholeCalls := counter.take()
			t.Logf("whole Resolve file list hashes asked: %d", len(wholeCalls))

			for _, ask := range [][]string{{"aa03"}, {"bb01"}, {"cc00"}, {"cc01"}, {"dd00"}, {"dd01"}, {"ee00"}, {"ee01"}, {"ff01"}, {"gg01"}, {"hh00"}, {"hh01"}, {"hh02"}, {"aa01", "cc01"}} {
				scoped := svc.prototypeResolve(t, ResolveRequest{Hashes: ask, Groups: true, OtherShared: true})
				calls := counter.take()
				facts := svc.prototypeResolve(t, ResolveRequest{Hashes: ask})
				factCalls := counter.take()
				t.Logf("ask %v without groups fetched %v", ask, factCalls)
				t.Logf("ask %v: scoped fetched %d hashes %v", ask, len(calls), calls)
				for _, h := range ask {
					s := componentOf(scoped.SameInstanceContentByHash, h)
					w := componentOf(whole.SameInstanceContentByHash, h)
					d := todayComponent(groups, h)
					if !slices.Equal(s, w) {
						t.Errorf("%s: scoped %v != whole %v", h, s, w)
					}
					if want, ok := oneWay[h]; ok {
						// The allowed difference: the whole component, a superset of today's group.
						require.Equal(t, want, s, h)
						require.Subset(t, s, d, h)
						require.NotEqual(t, s, d, "today the group of %s depends on the list order", h)
					} else if !slices.Equal(s, d) {
						t.Errorf("DIFF %s: scoped %v != today %v (today links %v)", h, s, d, today.SameInstanceContentByHash)
					}
					compareFacts(t, "groups", h, scoped, today)
					compareFacts(t, "no groups", h, facts, today)
				}
				require.Equal(t, today.OtherInstanceShared, scoped.OtherInstanceShared, "library-wide shared check")
				require.Equal(t, today.OtherInstanceSharedUnknown, scoped.OtherInstanceSharedUnknown)
				require.Equal(t, today.OfflineLocalInstances, scoped.OfflineLocalInstances)
			}
		})
	}
}

func compareFacts(t *testing.T, mode, h string, scoped, today *CrossMatchResult) {
	t.Helper()
	for name, pair := range map[string][2]map[string]struct{}{
		"SameExists":   {scoped.SameInstanceExists, today.SameInstanceExists},
		"SameSeeding":  {scoped.SameInstanceSeeding, today.SameInstanceSeeding},
		"SameUnknown":  {scoped.SameInstanceUnknown, today.SameInstanceUnknown},
		"OtherExists":  {scoped.OtherInstanceExists, today.OtherInstanceExists},
		"OtherSeeding": {scoped.OtherInstanceSeeding, today.OtherInstanceSeeding},
		"OtherUnknown": {scoped.OtherInstanceUnknown, today.OtherInstanceUnknown},
	} {
		if setHas(pair[0], h) != setHas(pair[1], h) {
			t.Errorf("DIFF (%s) %s %s: scoped %v today %v", mode, h, name, setHas(pair[0], h), setHas(pair[1], h))
		}
	}
	require.Equal(t, today.SameInstanceTagsByHash[h], scoped.SameInstanceTagsByHash[h], h)
}

func (s *Service) prototypeResolve(t *testing.T, req ResolveRequest) *CrossMatchResult {
	t.Helper()
	sess, err := s.OpenCrossMatchSession(t.Context(), 1, sessionNeeds)
	require.NoError(t, err)
	return sess.Resolve(t.Context(), req)
}

func TestPrototypeCrossMatchSessionCounts(t *testing.T) {
	svc, counter, library := sessionFixtures(t, false)
	ctx := t.Context()

	sess, err := svc.OpenCrossMatchSession(ctx, 1, sessionNeeds)
	require.NoError(t, err)
	require.Empty(t, counter.take(), "open reads no file list")

	sess.Resolve(ctx, ResolveRequest{Groups: true})
	require.Empty(t, counter.take(), "an empty request reads nothing")

	sess.Resolve(ctx, ResolveRequest{Hashes: []string{"gg01"}, Groups: true})
	require.Empty(t, counter.take(), "a torrent with no pick and no path group needs no list")

	sess.Resolve(ctx, ResolveRequest{Hashes: []string{"aa03"}, Groups: true})
	require.ElementsMatch(t, []string{"1:aa01", "1:aa02", "1:aa03"}, counter.take(), "the chain reads only its path group")

	sess.Resolve(ctx, ResolveRequest{Hashes: []string{"hh01"}, Groups: true})
	// hh00 twice: the batch misses it and get retries it alone, as today.
	require.ElementsMatch(t, []string{"1:hh00", "1:hh01", "1:hh02", "1:hh00"}, counter.take(), "the one-way link reads the reverse pickers")

	sess.Resolve(ctx, ResolveRequest{Hashes: []string{"aa03", "hh01"}, Groups: true})
	require.Empty(t, counter.take(), "a second ask reads from the memo")

	fresh, err := svc.OpenCrossMatchSession(ctx, 1, sessionNeeds)
	require.NoError(t, err)
	fresh.Resolve(ctx, ResolveRequest{All: true, Groups: true})
	all := counter.take()
	today := svc.BuildCrossMatchSets(ctx, 1, sessionNeeds)
	require.NoError(t, today.Err)
	todayCalls := counter.take()
	t.Logf("whole library Resolve: %d hashes; BuildCrossMatchSets: %d hashes; %d torrents on instance 1", len(all), len(todayCalls), len(library))
	require.ElementsMatch(t, todayCalls, all, "a whole-library ask reads what today's build reads")

	// Local reads: the fixture names need no files, so they fetch nothing.
	reads := svc.readLocalTorrents(ctx, 1, viewTorrents(must(svc.syncManager.GetCachedInstanceTorrents(ctx, 1))))
	require.Empty(t, reads)
	require.Empty(t, counter.take())
}

// A scoped ask on a library with names that need files fills the local reads
// of the whole library first.
func TestPrototypeCrossMatchSessionLocalReadsFetchLibrary(t *testing.T) {
	svc, sm := deleteMatchService(
		crossMatchTorrent(1, "aaaa", "Lone.Pier.2024.1080p.BluRay.x264-GRP", "/d/a", "/d/a/a.mkv", qbt.TorrentFiles{{Name: "a.mkv", Size: 1}}),
		// Season folders: their names need files (nameNeedsFiles).
		crossMatchTorrent(1, "bbbb", "Season 1", "/d/b", "/d/b/Season 1", qbt.TorrentFiles{{Name: "Season 1/Show.S01E01.mkv", Size: 2}}),
		crossMatchTorrent(1, "cccc", "Season 2", "/d/c", "/d/c/Season 2", qbt.TorrentFiles{{Name: "Season 2/Show.S02E01.mkv", Size: 3}}),
	)
	svc.automationSettingsLoader = func(context.Context) (*models.CrossSeedAutomationSettings, error) {
		return &models.CrossSeedAutomationSettings{}, nil
	}
	counter := &countingSync{qbittorrentSync: sm}
	svc.syncManager = counter
	require.True(t, nameNeedsFiles(svc.matcher().readName("Season 1").release))

	sess, err := svc.OpenCrossMatchSession(t.Context(), 1, sessionNeeds)
	require.NoError(t, err)
	require.Empty(t, counter.take())
	sess.Resolve(t.Context(), ResolveRequest{Hashes: []string{"aaaa"}, Groups: true})
	calls := counter.take()
	t.Logf("scoped ask of aaaa fetched %v", calls)
	require.ElementsMatch(t, []string{"1:bbbb", "1:cccc"}, calls, "the first Resolve fills the local reads of every name that needs files")
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
