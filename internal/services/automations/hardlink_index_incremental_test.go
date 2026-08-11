// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"os"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlink"
)

// scanOne reads one torrent's files the way the index does, from real files on disk.
func scanOne(t *testing.T, savePath string, names ...string) *torrentFileInfo {
	t.Helper()
	files := make(qbt.TorrentFiles, 0, len(names))
	for _, name := range names {
		files = append(files, qbt.TorrentFile{Name: name})
	}
	return scanTorrentFiles(qbt.Torrent{SavePath: savePath}, files)
}

// linkPair creates one file under a/ and hardlinks it into b/, returning both paths.
// Two paths to one inode is the shape every scope answer turns on.
func linkPair(t *testing.T, dir string) (string, string) {
	t.Helper()
	pathA := filepath.Join(dir, "a", "movie.mkv")
	pathB := filepath.Join(dir, "b", "movie.mkv")
	createFile(t, pathA)
	require.NoError(t, os.MkdirAll(filepath.Dir(pathB), 0o700))
	require.NoError(t, os.Link(pathA, pathB))
	return pathA, pathB
}

// indexFrom derives the link counts and every derived map from a set of scans, exactly
// as a full build or an incremental update does.
func indexFrom(scans map[string]*torrentFileInfo) *HardlinkIndex {
	index := &HardlinkIndex{}
	index.applyLinkState(deriveLinkCounts(scans))
	return index
}

// Two torrents holding the same physical file are hardlinked to each other and to
// nothing else. Dropping one of them, files and all, leaves the survivor with a single
// link, and the survivor's scope has to follow even though nothing happened to it
// directly. This is what an incremental update has to get right.
func TestIncrementalUpdate_RemovedTorrentChangesSurvivorScope(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, pathB := linkPair(t, dir)

	scans := map[string]*torrentFileInfo{
		"hashA": scanOne(t, filepath.Join(dir, "a"), "movie.mkv"),
		"hashB": scanOne(t, filepath.Join(dir, "b"), "movie.mkv"),
	}
	before := indexFrom(scans)
	require.Equal(t, HardlinkScopeTorrentsOnly, before.ScopeByHash["hashA"])
	require.Equal(t, HardlinkScopeTorrentsOnly, before.ScopeByHash["hashB"])

	// hashB leaves and takes its link with it.
	require.NoError(t, os.Remove(pathB))
	delete(scans, "hashB")
	scans["hashA"] = scanOne(t, filepath.Join(dir, "a"), "movie.mkv")

	after := indexFrom(scans)
	require.Equal(t, HardlinkScopeNone, after.ScopeByHash["hashA"], "survivor is no longer hardlinked")
	require.NotContains(t, after.DeleteSafeSignatureByHash, "hashA")
	require.NotContains(t, after.ScopeByHash, "hashB", "the departed torrent is gone from the index")
}

// A torrent removed from qBittorrent while its files stay on disk leaves a link that
// nothing in the client explains any more. The survivor has to read as linked outside
// the client, which is what keeps it out of the delete-safe groups.
func TestIncrementalUpdate_RemovedTorrentKeepingFilesMovesLinkOutside(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	linkPair(t, dir)

	scans := map[string]*torrentFileInfo{
		"hashA": scanOne(t, filepath.Join(dir, "a"), "movie.mkv"),
		"hashB": scanOne(t, filepath.Join(dir, "b"), "movie.mkv"),
	}
	require.Equal(t, HardlinkScopeTorrentsOnly, indexFrom(scans).ScopeByHash["hashA"])

	// hashB is removed from the client, the file it linked stays where it is.
	delete(scans, "hashB")

	after := indexFrom(scans)
	require.Equal(t, HardlinkScopeOutsideQBitTorrent, after.ScopeByHash["hashA"])
	require.NotContains(t, after.DeleteSafeSignatureByHash, "hashA", "outside links are never delete-safe")
}

func TestPlanTorrentRescan(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pathA := filepath.Join(dir, "a", "movie.mkv")
	createFile(t, pathA)

	previous := map[string]*torrentFileInfo{
		"moved":  scanOne(t, filepath.Join(dir, "a"), "movie.mkv"),
		"stayed": scanOne(t, filepath.Join(dir, "a"), "movie.mkv"),
	}

	current := map[string]qbt.Torrent{
		"moved":   {Hash: "moved", SavePath: filepath.Join(dir, "elsewhere")},
		"stayed":  {Hash: "stayed", SavePath: filepath.Join(dir, "a")},
		"arrived": {Hash: "arrived", SavePath: filepath.Join(dir, "c")},
	}

	rescan, staleFileIDs := planTorrentRescan(previous, current)

	require.Contains(t, rescan, "moved", "a save path change points the torrent at different files")
	require.Contains(t, rescan, "arrived", "a torrent with no previous scan has to be read")
	require.NotContains(t, rescan, "stayed", "an untouched torrent is not re-read")
	require.NotEmpty(t, staleFileIDs, "the moved torrent's old files lose their explanation")
}

// The plan must not schedule work twice, and must not schedule torrents that left.
func TestPlanSharingRescanSkipsAlreadyPlannedAndDepartedTorrents(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	linkPair(t, dir)

	infoA := scanOne(t, filepath.Join(dir, "a"), "movie.mkv")
	previous := map[string]*torrentFileInfo{
		"planned":  infoA,
		"departed": scanOne(t, filepath.Join(dir, "b"), "movie.mkv"),
		"sharer":   scanOne(t, filepath.Join(dir, "b"), "movie.mkv"),
	}
	current := map[string]qbt.Torrent{
		"planned": {Hash: "planned", SavePath: filepath.Join(dir, "a")},
		"sharer":  {Hash: "sharer", SavePath: filepath.Join(dir, "b")},
	}

	staleFileIDs := map[hardlink.FileID]struct{}{}
	for _, fileID := range infoA.fileIDs {
		staleFileIDs[fileID] = struct{}{}
	}

	sharing := planSharingRescan(previous, current, map[string]struct{}{"planned": {}}, staleFileIDs)

	require.Equal(t, map[string]struct{}{"sharer": {}}, sharing)
}

// A rule decides on the index, then the disk changes before the delete runs. The
// verification has to see the new link count, not the one the index recorded.
func TestScopeAfterRescanSeesLinksAddedSinceTheIndexWasBuilt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pathA := filepath.Join(dir, "a", "movie.mkv")
	createFile(t, pathA)

	scans := map[string]*torrentFileInfo{"hashA": scanOne(t, filepath.Join(dir, "a"), "movie.mkv")}
	index := indexFrom(scans)
	require.Equal(t, HardlinkScopeNone, index.ScopeByHash["hashA"])

	// Something outside qBittorrent hardlinks the file after the index was built.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "library"), 0o700))
	require.NoError(t, os.Link(pathA, filepath.Join(dir, "library", "movie.mkv")))

	fresh := scanOne(t, filepath.Join(dir, "a"), "movie.mkv")
	require.Equal(t, HardlinkScopeOutsideQBitTorrent, index.scopeAfterRescan(fresh),
		"the fresh read must report the link the index never saw")
}

// Cross-instance augmentation raises uniquePathCount in place under crossScopeMu. A
// preview request can run it while a scheduled apply verifies its delete candidates, so
// the read side has to take the same lock. Run with -race.
func TestScopeAfterRescanIsSafeAgainstConcurrentAugmentation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	linkPair(t, dir)

	info := scanOne(t, filepath.Join(dir, "a"), "movie.mkv")
	index := indexFrom(map[string]*torrentFileInfo{"hashA": info})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			index.crossScopeMu.Lock()
			for _, tracker := range index.buildState.globalFileIDMap {
				tracker.uniquePathCount++
			}
			index.crossScopeMu.Unlock()
		}
	}()

	for range 200 {
		index.scopeAfterRescan(info)
	}
	<-done
}

func TestScopeAfterRescanReportsUnknownForUnreadableFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pathA := filepath.Join(dir, "a", "movie.mkv")
	createFile(t, pathA)

	scans := map[string]*torrentFileInfo{"hashA": scanOne(t, filepath.Join(dir, "a"), "movie.mkv")}
	index := indexFrom(scans)

	require.NoError(t, os.Remove(pathA))

	fresh := scanOne(t, filepath.Join(dir, "a"), "movie.mkv")
	require.Empty(t, index.scopeAfterRescan(fresh),
		"a torrent whose files vanished has no scope, and must not read as unlinked")
}

// A delete is only re-verified when the rule consulted the index to choose its
// candidates. Every route into setupDeleteHardlinkContext has to count, or a rule that
// sorts or groups by hardlink data deletes on an index nobody re-read.
func TestDeleteUsesHardlinkData(t *testing.T) {
	t.Parallel()

	deleteRule := func(action *models.DeleteAction, sorting *models.SortingConfig) *models.Automation {
		return &models.Automation{
			Conditions:    &models.ActionConditions{Delete: action},
			SortingConfig: sorting,
		}
	}

	tests := []struct {
		name string
		rule *models.Automation
		want bool
	}{
		{
			name: "unregistered rule owes the index nothing",
			rule: deleteRule(&models.DeleteAction{
				Enabled:   true,
				Condition: &models.RuleCondition{Field: models.FieldIsUnregistered},
			}, nil),
		},
		{
			name: "no delete action",
			rule: deleteRule(nil, &models.SortingConfig{Type: models.SortingTypeSimple, Field: FieldHardlinkScope}),
		},
		{
			name: "condition on the scope",
			rule: deleteRule(&models.DeleteAction{
				Enabled:   true,
				Condition: &models.RuleCondition{Field: FieldHardlinkScope},
			}, nil),
			want: true,
		},
		{
			name: "expands to the hardlink copies",
			rule: deleteRule(&models.DeleteAction{Enabled: true, IncludeHardlinks: true}, nil),
			want: true,
		},
		{
			name: "sorted by the scope, which decides who wins a limited batch",
			rule: deleteRule(&models.DeleteAction{Enabled: true},
				&models.SortingConfig{Type: models.SortingTypeSimple, Field: FieldHardlinkScopeCross}),
			want: true,
		},
		{
			name: "grouped by the hardlink signature",
			rule: deleteRule(&models.DeleteAction{Enabled: true, GroupID: GroupHardlinkSignature}, nil),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, deleteUsesHardlinkData(tt.rule))
		})
	}
}

// Scans taken at different moments can disagree about a file. The higher link count has
// to win, because it is the one that reports a link outside the torrent set.
func TestDeriveLinkCountsPrefersTheHigherLinkCount(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	linkPair(t, dir)

	stale := scanOne(t, filepath.Join(dir, "a"), "movie.mkv")
	fresh := scanOne(t, filepath.Join(dir, "b"), "movie.mkv")
	stale.linkedFiles[0].nlink = 2
	fresh.linkedFiles[0].nlink = 9

	state := deriveLinkCounts(map[string]*torrentFileInfo{"stale": stale, "fresh": fresh})

	tracker := state.globalFileIDMap[fresh.fileIDs[0]]
	require.NotNil(t, tracker)
	require.Equal(t, uint64(9), tracker.nlink)
	require.Equal(t, 2, tracker.uniquePathCount, "two distinct paths point at the file")
}

// bothScopeFixture builds two torrents sharing one file (inside link) that is also
// hardlinked into a library path (outside link): nlink=3, two set paths -> "both".
func bothScopeFixture(t *testing.T) map[string]*torrentFileInfo {
	t.Helper()
	dir := t.TempDir()
	pathA, _ := linkPair(t, dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "library"), 0o700))
	require.NoError(t, os.Link(pathA, filepath.Join(dir, "library", "movie.mkv")))
	return map[string]*torrentFileInfo{
		"hashA": scanOne(t, filepath.Join(dir, "a"), "movie.mkv"),
		"hashB": scanOne(t, filepath.Join(dir, "b"), "movie.mkv"),
	}
}

// A "both" torrent still has links outside the client, so it must never enter the
// delete-safe groups, or a delete rule would remove data a library still points at.
func TestApplyLinkState_BothScopeIsNotDeleteSafe(t *testing.T) {
	t.Parallel()

	index := indexFrom(bothScopeFixture(t))

	require.Equal(t, HardlinkScopeBoth, index.ScopeByHash["hashA"])
	require.Equal(t, HardlinkScopeBoth, index.ScopeByHash["hashB"])
	require.Len(t, index.GroupBySignature, 1, "the pair still groups as hardlink duplicates")
	require.Empty(t, index.DeleteSafeGroupBySignature, "outside-linked torrents must not be delete-safe")
}

// The delete verifier compares a fresh rescan against the indexed scope and vetoes on
// mismatch. The rescan must be able to answer "both", or every both-scoped torrent
// would fail verification forever.
func TestScopeAfterRescanReportsBoth(t *testing.T) {
	t.Parallel()

	scans := bothScopeFixture(t)
	index := indexFrom(scans)
	require.Equal(t, HardlinkScopeBoth, index.ScopeByHash["hashA"])

	require.Equal(t, HardlinkScopeBoth, index.scopeAfterRescan(scans["hashA"]),
		"rescan must agree with the indexed scope for an unchanged torrent")
}
