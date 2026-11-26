// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
)

func scanOne(t *testing.T, savePath string, names ...string) *torrentFileInfo {
	t.Helper()
	files := make(qbt.TorrentFiles, 0, len(names))
	for _, name := range names {
		files = append(files, qbt.TorrentFile{Name: name})
	}
	return scanTorrentFiles(context.Background(), localbackend.NewBackend(), qbt.Torrent{SavePath: savePath}, files)
}

func linkPair(t *testing.T, dir string) (string, string) {
	t.Helper()
	pathA := filepath.Join(dir, "a", "movie.mkv")
	pathB := filepath.Join(dir, "b", "movie.mkv")
	createFile(t, pathA)
	require.NoError(t, os.MkdirAll(filepath.Dir(pathB), 0o700))
	require.NoError(t, os.Link(pathA, pathB))
	return pathA, pathB
}

func indexFrom(scans map[string]*torrentFileInfo) *HardlinkIndex {
	index := &HardlinkIndex{}
	index.applyLinkState(deriveLinkCounts(nil, scans, nil))
	return index
}

func TestScanTorrentFiles_MissingFileScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		priority    int
		want        string
		wantPresent bool
	}{
		{name: "skipped", priority: 0, want: HardlinkScopeNone, wantPresent: true},
		{name: "wanted", priority: 1, want: "", wantPresent: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			createFile(t, filepath.Join(dir, "movie.mkv"))
			files := qbt.TorrentFiles{
				{Name: "movie.mkv", Priority: 1},
				{Name: "missing.txt", Priority: tt.priority},
			}

			index := indexFrom(map[string]*torrentFileInfo{
				"hash": scanTorrentFiles(context.Background(), localbackend.NewBackend(), qbt.Torrent{SavePath: dir}, files),
			})
			scope, present := index.ScopeByHash["hash"]
			require.Equal(t, tt.wantPresent, present)
			require.Equal(t, tt.want, scope)
		})
	}
}

func TestScanTorrentFiles_PresentSkippedFileStillAffectsScope(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	linkPair(t, dir)
	files := qbt.TorrentFiles{{Name: "movie.mkv", Priority: 0}}

	index := indexFrom(map[string]*torrentFileInfo{
		"hash": scanTorrentFiles(context.Background(), localbackend.NewBackend(), qbt.Torrent{SavePath: filepath.Join(dir, "a")}, files),
	})
	require.Equal(t, HardlinkScopeOutsideQBitTorrent, index.ScopeByHash["hash"])
}

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

	require.NoError(t, os.Remove(pathB))
	delete(scans, "hashB")
	scans["hashA"] = scanOne(t, filepath.Join(dir, "a"), "movie.mkv")

	after := indexFrom(scans)
	require.Equal(t, HardlinkScopeNone, after.ScopeByHash["hashA"], "survivor is no longer hardlinked")
	require.NotContains(t, after.DeleteSafeSignatureByHash, "hashA")
	require.NotContains(t, after.ScopeByHash, "hashB", "the departed torrent is gone from the index")
}

func TestIncrementalUpdate_RemovedTorrentKeepingFilesMovesLinkOutside(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	linkPair(t, dir)

	scans := map[string]*torrentFileInfo{
		"hashA": scanOne(t, filepath.Join(dir, "a"), "movie.mkv"),
		"hashB": scanOne(t, filepath.Join(dir, "b"), "movie.mkv"),
	}
	require.Equal(t, HardlinkScopeTorrentsOnly, indexFrom(scans).ScopeByHash["hashA"])

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

	rescan, staleFileIDs := planTorrentRescan(&hardlinkBuildState{torrentInfoByHash: previous}, torrentRefMap(current))

	require.Contains(t, rescan, "moved", "a save path change points the torrent at different files")
	require.Contains(t, rescan, "arrived", "a torrent with no previous scan has to be read")
	require.NotContains(t, rescan, "stayed", "an untouched torrent is not re-read")
	require.NotEmpty(t, staleFileIDs, "the moved torrent's old files lose their explanation")
}

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

	staleFileIDs := map[fsops.FileKey]struct{}{}
	for _, fileID := range infoA.fileIDs {
		staleFileIDs[testFileKey(fileID, nil)] = struct{}{}
	}

	sharing := planSharingRescan(&hardlinkBuildState{torrentInfoByHash: previous}, torrentRefMap(current), map[string]struct{}{"planned": {}}, staleFileIDs)

	require.Equal(t, map[string]struct{}{"sharer": {}}, sharing)
}

func TestScopeAfterRescanSeesLinksAddedSinceTheIndexWasBuilt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pathA := filepath.Join(dir, "a", "movie.mkv")
	createFile(t, pathA)

	scans := map[string]*torrentFileInfo{"hashA": scanOne(t, filepath.Join(dir, "a"), "movie.mkv")}
	index := indexFrom(scans)
	require.Equal(t, HardlinkScopeNone, index.ScopeByHash["hashA"])

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "library"), 0o700))
	require.NoError(t, os.Link(pathA, filepath.Join(dir, "library", "movie.mkv")))

	fresh := scanOne(t, filepath.Join(dir, "a"), "movie.mkv")
	require.Equal(t, HardlinkScopeOutsideQBitTorrent, scopeAfterRescan(fresh, index.buildState.instance, index.buildState.globalFileIDMap),
		"the fresh read must report the link the index never saw")
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
	require.Empty(t, scopeAfterRescan(fresh, index.buildState.instance, index.buildState.globalFileIDMap),
		"a torrent whose files vanished has no scope, and must not read as unlinked")
}

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
		want hardlinkUse
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
			want: hardlinkUse{single: true},
		},
		{
			name: "expands to the hardlink copies",
			rule: deleteRule(&models.DeleteAction{Enabled: true, IncludeHardlinks: true}, nil),
			want: hardlinkUse{single: true},
		},
		{
			name: "sorted by the scope, which decides who wins a limited batch",
			rule: deleteRule(&models.DeleteAction{Enabled: true},
				&models.SortingConfig{Type: models.SortingTypeSimple, Field: FieldHardlinkScopeCross}),
			want: hardlinkUse{cross: true},
		},
		{
			name: "grouped by the hardlink signature",
			rule: deleteRule(&models.DeleteAction{Enabled: true, GroupID: GroupHardlinkSignature}, nil),
			want: hardlinkUse{single: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, deleteUsesHardlinkData(tt.rule))
		})
	}
}

func TestDeriveLinkCountsPrefersTheHigherLinkCount(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	linkPair(t, dir)

	stale := scanOne(t, filepath.Join(dir, "a"), "movie.mkv")
	fresh := scanOne(t, filepath.Join(dir, "b"), "movie.mkv")
	stale.linkedFiles[0].nlink = 2
	fresh.linkedFiles[0].nlink = 9

	state := deriveLinkCounts(nil, map[string]*torrentFileInfo{"stale": stale, "fresh": fresh}, nil)

	tracker := state.globalFileIDMap[testFileKey(fresh.fileIDs[0], state.instance)]
	require.NotNil(t, tracker)
	require.Equal(t, uint64(9), tracker.nlink)
	require.Equal(t, 2, tracker.uniquePathCount, "two distinct paths point at the file")
}

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

func TestApplyLinkState_BothScopeIsNotDeleteSafe(t *testing.T) {
	t.Parallel()

	index := indexFrom(bothScopeFixture(t))

	require.Equal(t, HardlinkScopeBoth, index.ScopeByHash["hashA"])
	require.Equal(t, HardlinkScopeBoth, index.ScopeByHash["hashB"])
	require.Len(t, index.GroupBySignature, 1, "the pair still groups as hardlink duplicates")
	require.Empty(t, index.DeleteSafeGroupBySignature, "outside-linked torrents must not be delete-safe")
}

func TestScopeAfterRescanReportsBoth(t *testing.T) {
	t.Parallel()

	scans := bothScopeFixture(t)
	index := indexFrom(scans)
	require.Equal(t, HardlinkScopeBoth, index.ScopeByHash["hashA"])

	require.Equal(t, HardlinkScopeBoth, scopeAfterRescan(scans["hashA"], index.buildState.instance, index.buildState.globalFileIDMap),
		"rescan must agree with the indexed scope for an unchanged torrent")
}
