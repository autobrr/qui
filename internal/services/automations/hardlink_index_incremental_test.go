// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"os"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

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
	pathA := filepath.Join(dir, "a", "movie.mkv")
	pathB := filepath.Join(dir, "b", "movie.mkv")
	createFile(t, pathA)
	require.NoError(t, os.MkdirAll(filepath.Dir(pathB), 0o700))
	require.NoError(t, os.Link(pathA, pathB))

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
	pathA := filepath.Join(dir, "a", "movie.mkv")
	pathB := filepath.Join(dir, "b", "movie.mkv")
	createFile(t, pathA)
	require.NoError(t, os.MkdirAll(filepath.Dir(pathB), 0o700))
	require.NoError(t, os.Link(pathA, pathB))

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
	pathA := filepath.Join(dir, "a", "movie.mkv")
	pathB := filepath.Join(dir, "b", "movie.mkv")
	createFile(t, pathA)
	require.NoError(t, os.MkdirAll(filepath.Dir(pathB), 0o700))
	require.NoError(t, os.Link(pathA, pathB))

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

// Scans taken at different moments can disagree about a file. The higher link count has
// to win, because it is the one that reports a link outside the torrent set.
func TestDeriveLinkCountsPrefersTheHigherLinkCount(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pathA := filepath.Join(dir, "a", "movie.mkv")
	pathB := filepath.Join(dir, "b", "movie.mkv")
	createFile(t, pathA)
	require.NoError(t, os.MkdirAll(filepath.Dir(pathB), 0o700))
	require.NoError(t, os.Link(pathA, pathB))

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
