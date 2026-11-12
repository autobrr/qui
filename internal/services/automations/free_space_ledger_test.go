// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
)

// testFreeSpaceLedger makes a ledger with the free space at the start and the freed bytes
// that its rule counted.
func testFreeSpaceLedger(startBytes, freedBytes int64) *freeSpaceLedger {
	return &freeSpaceLedger{startBytes: startBytes, freedBytes: freedBytes}
}

// testFreeSpaceLedgers makes the ledgers that the run makes for the rules, with the same
// free space at the start for each source.
func testFreeSpaceLedgers(startBytes int64, rules ...*models.Automation) map[int]*freeSpaceLedger {
	ledgers := make(map[int]*freeSpaceLedger)
	for _, rule := range rules {
		if ruleUsesCondition(rule, FieldFreeSpace) {
			ledgers[rule.ID] = newFreeSpaceLedger(rule, startBytes, false)
		}
	}
	return ledgers
}

// countingLedger is the ledger of a FREE_SPACE delete rule in the mode.
func countingLedger(mode string, includeHardlinks bool) *freeSpaceLedger {
	return newFreeSpaceLedger(&models.Automation{Conditions: &models.ActionConditions{Delete: &models.DeleteAction{
		Enabled: true, Mode: mode, IncludeHardlinks: includeHardlinks,
		Condition: &models.RuleCondition{Field: FieldFreeSpace, Operator: models.OperatorLessThan, Value: "1"},
	}}}, 0, false)
}

func TestFreeSpaceLedger_Count(t *testing.T) {
	t.Run("adds size for non-cross-seed torrent with deleteWithFiles", func(t *testing.T) {
		ledger := countingLedger(DeleteModeWithFiles, true)
		// A torrent without a cross-seed is a group of its own.
		torrent := qbt.Torrent{Hash: "abc123", Size: 50000000000}

		require.True(t, ledger.count(torrent, nil, &EvalContext{}))
		require.Equal(t, int64(50000000000), ledger.projectedFreedBytes())
	})

	t.Run("does not double-count cross-seed torrents", func(t *testing.T) {
		ledger := countingLedger(DeleteModeWithFiles, true)
		// The cross-seed has the same paths and a different hash.
		torrent1 := qbt.Torrent{Hash: "abc123", Size: 50000000000, ContentPath: "/data/movie", SavePath: "/data"}
		torrent2 := qbt.Torrent{Hash: "def456", Size: 50000000000, ContentPath: "/data/movie", SavePath: "/data"}
		evalCtx := &EvalContext{CrossSeedFilesByHash: map[string]qbt.TorrentFiles{
			"abc123": {{Name: "movie", Size: 50000000000}},
			"def456": {{Name: "movie", Size: 50000000000}},
		}}
		groups := testCrossSeedGroups([]qbt.Torrent{torrent1, torrent2})

		ledger.count(torrent1, groups, evalCtx)
		ledger.count(torrent2, groups, evalCtx)

		require.Equal(t, int64(50000000000), ledger.projectedFreedBytes())
	})

	t.Run("counts the file union of an episode and its pack", func(t *testing.T) {
		ledger := countingLedger(DeleteModeWithFiles, true)
		evalCtx := &EvalContext{CrossSeedFilesByHash: map[string]qbt.TorrentFiles{
			"ep":   {{Name: "ep1.mkv", Size: 1000}},
			"pack": {{Name: "Pack/ep1.mkv", Size: 1000}, {Name: "Pack/ep2.mkv", Size: 1000}},
		}}
		episode := qbt.Torrent{Hash: "ep", Size: 1000, ContentPath: "/data/Pack/ep1.mkv", SavePath: "/data/Pack"}
		pack := qbt.Torrent{Hash: "pack", Size: 2000, ContentPath: "/data/Pack", SavePath: "/data"}
		groups := buildCrossSeedGroups(torrentRefs([]qbt.Torrent{episode, pack}), &EvalContext{
			SameInstanceContentByHash: map[string][]string{"ep": {"pack"}, "pack": {"ep"}},
		})

		ledger.count(episode, groups, evalCtx)
		ledger.count(pack, groups, evalCtx)

		require.Equal(t, int64(2000), ledger.projectedFreedBytes(), "the pack holds the episode file")
	})

	t.Run("counts different content paths separately", func(t *testing.T) {
		ledger := countingLedger(DeleteModeWithFiles, true)
		torrent1 := qbt.Torrent{Hash: "abc123", Size: 50000000000, ContentPath: "/data/movie1", SavePath: "/data"}
		torrent2 := qbt.Torrent{Hash: "def456", Size: 30000000000, ContentPath: "/data/movie2", SavePath: "/data"}
		groups := testCrossSeedGroups([]qbt.Torrent{torrent1, torrent2})

		ledger.count(torrent1, groups, &EvalContext{})
		ledger.count(torrent2, groups, &EvalContext{})

		require.Equal(t, int64(80000000000), ledger.projectedFreedBytes())
	})

	t.Run("a rule without a ledger counts nothing", func(t *testing.T) {
		var ledger *freeSpaceLedger
		torrent := qbt.Torrent{Hash: "abc123", Size: 50000000000, ContentPath: "/data/movie", SavePath: "/data"}

		require.True(t, ledger.count(torrent, nil, nil))
		require.Zero(t, ledger.projectedFreedBytes())
	})

	t.Run("does not add size for DeleteModeKeepFiles", func(t *testing.T) {
		ledger := countingLedger(DeleteModeKeepFiles, true)
		torrent := qbt.Torrent{Hash: "abc123", Size: 50000000000, ContentPath: "/data/movie", SavePath: "/data"}

		require.True(t, ledger.count(torrent, nil, &EvalContext{}))
		require.Zero(t, ledger.projectedFreedBytes())
	})

	t.Run("does not add size for preserve-cross-seeds mode when cross-seeds exist", func(t *testing.T) {
		ledger := countingLedger(DeleteModeWithFilesPreserveCrossSeeds, true)
		// Two torrents resolve to the same file and are cross-seeds.
		torrent1 := qbt.Torrent{Hash: "abc123", Size: 50000000000, ContentPath: "/data/movie", SavePath: "/data"}
		torrent2 := qbt.Torrent{Hash: "def456", Size: 50000000000, ContentPath: "/data/movie", SavePath: "/data"}
		evalCtx := &EvalContext{CrossSeedFilesByHash: map[string]qbt.TorrentFiles{
			"abc123": {{Name: "movie/main.mkv", Size: 50000000000}},
			"def456": {{Name: "movie/main.mkv", Size: 50000000000}},
		}}

		// torrent2 keeps the files.
		require.True(t, ledger.count(torrent1, testCrossSeedGroups([]qbt.Torrent{torrent1, torrent2}), evalCtx))
		require.Zero(t, ledger.projectedFreedBytes())
	})

	t.Run("adds size for preserve-cross-seeds mode when no cross-seeds exist", func(t *testing.T) {
		ledger := countingLedger(DeleteModeWithFilesPreserveCrossSeeds, true)
		torrent := qbt.Torrent{Hash: "abc123", Size: 50000000000, ContentPath: "/data/movie", SavePath: "/data"}

		require.True(t, ledger.count(torrent, testCrossSeedGroups([]qbt.Torrent{torrent}), &EvalContext{}))
		require.Equal(t, int64(50000000000), ledger.projectedFreedBytes())
	})
}

// Each delete of the "needed" view adds to the Projected freed bytes, so the FREE_SPACE
// condition stops matching once the target is reached.
func TestFreeSpaceLedger_CountsEachDeleteOfNeededView(t *testing.T) {
	allTorrents := []qbt.Torrent{
		{Hash: "a", Size: 100 * 1024 * 1024 * 1024, ContentPath: "/data/movie1", SavePath: "/data"},
		{Hash: "b", Size: 50 * 1024 * 1024 * 1024, ContentPath: "/data/movie2", SavePath: "/data"},
		{Hash: "c", Size: 30 * 1024 * 1024 * 1024, ContentPath: "/data/movie3", SavePath: "/data"},
	}
	ledger := countingLedger(DeleteModeWithFiles, true)
	crossSeeds := testCrossSeedGroups(allTorrents)

	ledger.count(allTorrents[0], crossSeeds, &EvalContext{})
	require.Equal(t, int64(100*1024*1024*1024), ledger.projectedFreedBytes())
	ledger.count(allTorrents[1], crossSeeds, &EvalContext{})
	require.Equal(t, int64(150*1024*1024*1024), ledger.projectedFreedBytes())
	ledger.count(allTorrents[2], crossSeeds, &EvalContext{})
	require.Equal(t, int64(180*1024*1024*1024), ledger.projectedFreedBytes())
}

// The "eligible" view keeps the free space at the start, so the ledger counts nothing.
func TestFreeSpaceLedger_EligibleViewCountsNothing(t *testing.T) {
	rule := &models.Automation{ID: 1, Conditions: &models.ActionConditions{Delete: &models.DeleteAction{
		Enabled: true, Mode: DeleteModeWithFiles,
		Condition: &models.RuleCondition{Field: FieldFreeSpace, Operator: models.OperatorLessThan, Value: "1"},
	}}}
	ledger := newFreeSpaceLedger(rule, 100, true)

	require.False(t, ledger.projects())
	require.True(t, ledger.count(qbt.Torrent{Hash: "abc123", Size: 50000000000}, nil, &EvalContext{}))
	require.Zero(t, ledger.projectedFreedBytes())
	require.Equal(t, int64(100), ledger.projectedFreeSpace())
}

func TestFreeSpaceLedger_UnreadExpandedMemberDoesNotCount(t *testing.T) {
	t.Parallel()
	torrents, evalCtx, _ := mixedDeletionProjectionFixture()
	evalCtx.torrentsByHash = make(map[string]*qbt.Torrent)
	for _, torrent := range torrentRefs(torrents) {
		evalCtx.torrentsByHash[torrent.Hash] = torrent
	}
	sourceFiles := evalCtx.CrossSeedFilesByHash["source"]
	delete(evalCtx.CrossSeedFilesByHash, "source")
	ledger := countingLedger(DeleteModeWithFilesIncludeCrossSeeds, true)
	groups := buildCrossSeedGroups(torrentRefs(torrents), evalCtx)

	require.False(t, ledger.count(torrents[0], groups, evalCtx))
	require.Zero(t, ledger.projectedFreedBytes())

	// The failed count took no member, so the group counts once its file list is read.
	evalCtx.CrossSeedFilesByHash["source"] = sourceFiles
	require.True(t, ledger.count(torrents[0], groups, evalCtx))
	require.Equal(t, int64(2000), ledger.projectedFreedBytes())
}

func TestFreeSpaceLedger_ResetReadsZero(t *testing.T) {
	ledger := countingLedger(DeleteModeWithFiles, true)
	torrents := []qbt.Torrent{
		{Hash: "ep", Size: 1000, ContentPath: "/data/Pack/ep1.mkv", SavePath: "/data/Pack"},
		{Hash: "pack", Size: 2000, ContentPath: "/data/Pack", SavePath: "/data"},
	}
	evalCtx := &EvalContext{CrossSeedFilesByHash: map[string]qbt.TorrentFiles{
		"ep":   {{Name: "ep1.mkv", Size: 1000}},
		"pack": {{Name: "Pack/ep1.mkv", Size: 1000}, {Name: "Pack/ep2.mkv", Size: 1000}},
	}}
	groups := buildCrossSeedGroups(torrentRefs(torrents), &EvalContext{
		SameInstanceContentByHash: map[string][]string{"ep": {"pack"}, "pack": {"ep"}},
	})
	ledger.count(torrents[0], groups, evalCtx)

	ledger.reset()
	require.Zero(t, ledger.projectedFreedBytes())

	// The reset forgets the counted paths, so the episode file counts again.
	ledger.count(torrents[1], groups, evalCtx)
	require.Equal(t, int64(2000), ledger.projectedFreedBytes())
}

// statfsCounter answers Statfs by path and counts the calls.
type statfsCounter struct {
	fsops.Backend
	available map[string]int64
	calls     map[string]int
}

func (statfsCounter) Paths() fsops.PathDialect { return fsops.SlashPaths }

func (b statfsCounter) Statfs(_ context.Context, path string) (*fsops.StatfsResult, error) {
	b.calls[path]++
	available, ok := b.available[path]
	if !ok {
		return nil, errStatfs
	}
	return &fsops.StatfsResult{BytesAvailable: available}, nil
}

func TestFreeSpaceLedgers_ReadEachSourceOnce(t *testing.T) {
	t.Parallel()
	instance := &models.Instance{ID: 1, SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-hostkey"}
	backend := statfsCounter{available: map[string]int64{"/mnt/a": 100, "/mnt/b": 200}, calls: make(map[string]int)}
	svc := &Service{backendPool: fsops.NewPoolWithRemote(rowGetter{row: instance}, localbackend.NewBackend(),
		func(*models.Instance) fsops.Backend { return backend })}
	source := func(path string) *models.FreeSpaceSource {
		return &models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: path}
	}
	freeSpaceBelow := &models.RuleCondition{Field: FieldFreeSpace, Operator: models.OperatorLessThan, Value: "1000"}
	deleteRule := &models.Automation{ID: 1, FreeSpaceSource: source("/mnt/a"), Conditions: &models.ActionConditions{
		Delete: &models.DeleteAction{Enabled: true, Mode: DeleteModeWithFiles, Condition: freeSpaceBelow},
	}}
	tagRule := &models.Automation{ID: 2, FreeSpaceSource: source("/mnt/a"), Conditions: &models.ActionConditions{
		Tag: &models.TagAction{Enabled: true, Tags: []string{"low"}, Condition: freeSpaceBelow},
	}}
	sortRule := &models.Automation{ID: 3, FreeSpaceSource: source("/mnt/b"), Conditions: &models.ActionConditions{},
		SortingConfig: &models.SortingConfig{Type: models.SortingTypeSimple, Field: FieldFreeSpace}}
	plainRule := &models.Automation{ID: 4, Conditions: &models.ActionConditions{
		Tag: &models.TagAction{Enabled: true, Tags: []string{"x"}, Condition: parityDeleteTag},
	}}
	rules := []*models.Automation{deleteRule, tagRule, sortRule, plainRule}

	ledgers, err := svc.freeSpaceLedgers(t.Context(), instance, rules, false)
	require.NoError(t, err)
	require.Equal(t, map[string]int{"/mnt/a": 1, "/mnt/b": 1}, backend.calls)
	require.Len(t, ledgers, 3)
	require.Equal(t, int64(100), ledgers[deleteRule.ID].startFreeSpace())
	require.Equal(t, int64(100), ledgers[tagRule.ID].startFreeSpace())
	require.Equal(t, int64(200), ledgers[sortRule.ID].startFreeSpace())
	require.True(t, ledgers[deleteRule.ID].projects())
	require.False(t, ledgers[tagRule.ID].projects())

	eligible, err := svc.freeSpaceLedgers(t.Context(), instance, rules, true)
	require.NoError(t, err)
	require.False(t, eligible[deleteRule.ID].projects())

	_, err = svc.freeSpaceLedgers(t.Context(), nil, rules, false)
	require.Error(t, err, "a rule that reads FREE_SPACE needs the instance")
	ledgers, err = svc.freeSpaceLedgers(t.Context(), nil, []*models.Automation{plainRule}, false)
	require.NoError(t, err)
	require.Empty(t, ledgers)

	sortRule.FreeSpaceSource = source("/mnt/unreadable")
	_, err = svc.freeSpaceLedgers(t.Context(), instance, rules, false)
	require.ErrorIs(t, err, errStatfs)
}
