// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/hardlink"
)

// cannedSession answers every Resolve with one canned result and no member
// files, as the fake CrossMatchers do.
type cannedSession struct{ result *CrossMatchResult }

func (s cannedSession) Resolve(context.Context, ResolveRequest) *CrossMatchResult {
	return s.result
}

// recordingMatcher answers each group ask with the components of the asked
// torrents in links, as the cross-seed session does, and records each request.
type recordingMatcher struct {
	links  map[string][]string
	shared map[string]struct{}
	files  map[string]qbt.TorrentFiles // MemberFiles of the answers
	exists map[string]struct{}         // the torrents with a cross-seed on this instance
	mu     sync.Mutex
	asks   []ResolveRequest
}

func (m *recordingMatcher) OpenCrossMatchSession(context.Context, int, CrossMatchNeeds) (CrossMatchSession, error) {
	return m, nil
}

func (m *recordingMatcher) Resolve(_ context.Context, req ResolveRequest) *CrossMatchResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.asks = append(m.asks, req)
	result := &CrossMatchResult{SameInstanceContentByHash: make(map[string][]string), MemberFiles: make(map[string]qbt.TorrentFiles)}
	var start []string
	switch {
	case req.Groups && req.All:
		start = slices.Collect(maps.Keys(m.links))
	case req.Groups:
		start = req.Hashes
	}
	for _, hash := range start {
		for _, member := range componentHashes(m.links, hash) {
			if next := m.links[member]; len(next) > 0 {
				result.SameInstanceContentByHash[member] = next
				if files, ok := m.files[member]; ok {
					result.MemberFiles[member] = files
				}
			}
		}
	}
	if m.exists != nil {
		asked := req.Hashes
		if req.All {
			asked = slices.Collect(maps.Keys(m.exists))
		}
		result.SameInstanceExists = make(map[string]struct{})
		for _, hash := range asked {
			if _, ok := m.exists[hash]; ok {
				result.SameInstanceExists[hash] = struct{}{}
			}
		}
	}
	if req.OtherShared {
		result.OtherInstanceShared = maps.Clone(m.shared)
		result.OtherInstanceSharedUnknown = map[string]struct{}{}
	}
	return result
}

func (m *recordingMatcher) requests() []ResolveRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.asks)
}

// componentHashes follows links both ways from hash.
func componentHashes(links map[string][]string, hash string) []string {
	seen := map[string]struct{}{hash: {}}
	queue := []string{hash}
	for i := 0; i < len(queue); i++ {
		for a, next := range links {
			for _, b := range next {
				for _, pair := range [2][2]string{{a, b}, {b, a}} {
					if _, ok := seen[pair[1]]; pair[0] == queue[i] && !ok {
						seen[pair[1]] = struct{}{}
						queue = append(queue, pair[1])
					}
				}
			}
		}
	}
	return queue
}

const (
	sessionA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sessionB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	sessionC = "cccccccccccccccccccccccccccccccccccccccc"
)

// newRecordingStubService serves a and c as cross-seeds and b alone.
func newRecordingStubService(t *testing.T, tags map[string]string, localFS bool) (*Service, int, *stubQbt, *recordingMatcher) {
	t.Helper()
	return newRecordingStubServiceOf(t, []stubTorrent{
		{sessionA, "Quiet.Coast.2020.1080p.BluRay.x264-GRP", tags[sessionA], ""},
		{sessionB, "Silent.Shore.2021.1080p.BluRay.x264-GRP", tags[sessionB], ""},
		{sessionC, "Quiet.Coast.2020.1080p.BluRay.x264-GRP", tags[sessionC], ""},
	}, localFS)
}

func newRecordingStubServiceOf(t *testing.T, torrents []stubTorrent, localFS bool) (*Service, int, *stubQbt, *recordingMatcher) {
	t.Helper()
	stub, sharing := newWriteStub(t, torrents)
	stub.localFS = localFS
	matcher := &recordingMatcher{links: sharing}
	svc, _, instanceIDs := newStubService(t, matcher, stub)
	return svc, instanceIDs[0], stub, matcher
}

// deleteOutcome is what a run deletes: the torrents, and the torrents whose files go with them.
type deleteOutcome struct{ deleted, filesDeleted []string }

// runEachForm runs the rule live, as a dry run, and as a preview, each on a
// new service, and checks the deletes and the asks of each.
func runEachForm(t *testing.T, rule *models.Automation, tags map[string]string, localFS bool, want deleteOutcome, check func(t *testing.T, asks []ResolveRequest, stub *stubQbt)) {
	t.Helper()
	runEachFormOn(t, func(t *testing.T) (*Service, int, *stubQbt, *recordingMatcher) {
		return newRecordingStubService(t, tags, localFS)
	}, rule, want, check)
}

func runEachFormOn(t *testing.T, newService func(t *testing.T) (*Service, int, *stubQbt, *recordingMatcher), rule *models.Automation, want deleteOutcome, check func(t *testing.T, asks []ResolveRequest, stub *stubQbt)) {
	t.Helper()
	for _, form := range []string{"live", "dry run", "preview"} {
		t.Run(form, func(t *testing.T) {
			svc, instanceID, stub, matcher := newService(t)
			switch form {
			case "live", "dry run":
				r := *rule
				r.DryRun = form == "dry run"
				activities, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{&r}, r.DryRun)
				require.NoError(t, err)
				if r.DryRun {
					require.Equal(t, len(want.deleted), dryRunDeleteCount(t, activities))
					require.Empty(t, stub.writtenHashes()["delete"])
				} else {
					require.ElementsMatch(t, want.deleted, stub.writtenHashes()["delete"])
					require.ElementsMatch(t, want.filesDeleted, stub.filesDeleted())
				}
			case "preview":
				result, err := svc.PreviewDeleteRule(t.Context(), instanceID, rule, 100, 0, "needed")
				require.NoError(t, err)
				previewed := make([]string, len(result.Examples))
				for i, example := range result.Examples {
					previewed[i] = example.Hash
				}
				require.ElementsMatch(t, want.deleted, previewed)
			}
			check(t, matcher.requests(), stub)
		})
	}
}

func dryRunDeleteCount(t *testing.T, activities []*models.AutomationActivity) int {
	t.Helper()
	count := 0
	for _, activity := range activities {
		if activity.Action != models.ActivityActionDeletedCondition {
			continue
		}
		var details struct {
			Count int `json:"count"`
		}
		require.NoError(t, json.Unmarshal(activity.Details, &details))
		count += details.Count
	}
	return count
}

func TestRunSession_DeleteWithoutTriggerAsksNothing(t *testing.T) {
	for _, mode := range []string{DeleteModeWithFilesIncludeCrossSeeds, DeleteModeWithFilesPreserveCrossSeeds} {
		t.Run(mode, func(t *testing.T) {
			runEachForm(t, deleteRule(mode, tagsContain("del")), nil, false, deleteOutcome{}, func(t *testing.T, asks []ResolveRequest, stub *stubQbt) {
				require.Empty(t, asks)
				stub.mu.Lock()
				defer stub.mu.Unlock()
				require.Zero(t, stub.fileReads, "a run with no Delete trigger reads no file list")
			})
		})
	}
}

func TestRunSession_DeleteAsksTheTriggerGroupsAndTheSharedCheck(t *testing.T) {
	tags := map[string]string{sessionA: "del"}
	tests := []struct {
		mode string
		want deleteOutcome
	}{
		{DeleteModeWithFilesIncludeCrossSeeds, deleteOutcome{[]string{sessionA, sessionC}, []string{sessionA, sessionC}}},
		{DeleteModeWithFilesPreserveCrossSeeds, deleteOutcome{[]string{sessionA}, nil}},
	}
	for _, tt := range tests {
		for _, localFS := range []bool{false, true} {
			t.Run(tt.mode, func(t *testing.T) {
				runEachForm(t, deleteRule(tt.mode, tagsContain("del")), tags, localFS, tt.want, func(t *testing.T, asks []ResolveRequest, _ *stubQbt) {
					require.Equal(t, []ResolveRequest{{Hashes: []string{sessionA}, Groups: true, OtherShared: true}}, asks)
				})
			})
		}
	}
}

// freeSpaceLibrary serves count torrents of 1000 bytes on a disk with 1000
// free bytes. The name sort puts them in hash order.
func freeSpaceLibrary(count int) []stubTorrent {
	torrents := make([]stubTorrent, count)
	for i := range torrents {
		torrents[i] = stubTorrent{hash: fmt.Sprintf("%040d", i), name: fmt.Sprintf("Harbor.Watch.S01E%03d.1080p.WEB.h264-GRP", i)}
	}
	return torrents
}

func freeSpaceRule(mode, below string) *models.Automation {
	r := deleteRule(mode, &models.RuleCondition{Field: FieldFreeSpace, Operator: models.OperatorLessThan, Value: below})
	r.SortingConfig = &models.SortingConfig{SchemaVersion: "1", Type: models.SortingTypeSimple, Field: FieldName, Direction: models.SortDirectionASC}
	return r
}

func stubHashes(torrents []stubTorrent) []string {
	hashes := make([]string, len(torrents))
	for i, torrent := range torrents {
		hashes[i] = torrent.hash
	}
	return hashes
}

// A FREE_SPACE rule that stops after 2 claims asks for the groups of one
// read-ahead batch, not of the whole library. An include or preserve rule asks
// for the Shared-file check with it.
func TestRunSession_FreeSpaceDeleteReadsOneBatchAhead(t *testing.T) {
	library := freeSpaceLibrary(2*freeSpaceReadAheadBatch + 5)
	claimed := stubHashes(library[:2])
	for _, mode := range []string{DeleteModeWithFiles, DeleteModeWithFilesIncludeCrossSeeds, DeleteModeWithFilesPreserveCrossSeeds} {
		for _, localFS := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/localFS=%t", mode, localFS), func(t *testing.T) {
				newService := func(t *testing.T) (*Service, int, *stubQbt, *recordingMatcher) {
					return newRecordingStubServiceOf(t, library, localFS)
				}
				runEachFormOn(t, newService, freeSpaceRule(mode, "2500"), deleteOutcome{claimed, claimed}, func(t *testing.T, asks []ResolveRequest, _ *stubQbt) {
					want := ResolveRequest{Hashes: stubHashes(library[:freeSpaceReadAheadBatch]), Groups: true, OtherShared: mode != DeleteModeWithFiles}
					require.Equal(t, []ResolveRequest{want}, asks)
				})
			})
		}
	}
}

// The Shared-file check answers before the first claim, so a first claim that
// shares a file with another instance frees no space and the rule claims one
// more torrent.
func TestRunSession_FreeSpacePreserveAsksTheSharedCheckBeforeTheFirstClaim(t *testing.T) {
	library := freeSpaceLibrary(2*freeSpaceReadAheadBatch + 5)
	claimed := stubHashes(library[:3])
	newService := func(t *testing.T) (*Service, int, *stubQbt, *recordingMatcher) {
		svc, instanceID, stub, matcher := newRecordingStubServiceOf(t, library, true)
		matcher.shared = map[string]struct{}{claimed[0]: {}}
		return svc, instanceID, stub, matcher
	}
	runEachFormOn(t, newService, freeSpaceRule(DeleteModeWithFilesPreserveCrossSeeds, "2500"), deleteOutcome{claimed, claimed[1:]}, func(t *testing.T, asks []ResolveRequest, _ *stubQbt) {
		want := ResolveRequest{Hashes: stubHashes(library[:freeSpaceReadAheadBatch]), Groups: true, OtherShared: true}
		require.Equal(t, []ResolveRequest{want}, asks)
	})
}

// A rule that claims most of the library asks in a few rounds, each twice the
// size of the one before.
func TestRunSession_FreeSpaceReadAheadDoubles(t *testing.T) {
	library := freeSpaceLibrary(3*freeSpaceReadAheadBatch + 5)
	svc, instanceID, stub, matcher := newRecordingStubServiceOf(t, library, false)
	_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{freeSpaceRule(DeleteModeWithFilesIncludeCrossSeeds, "1000000")}, false)
	require.NoError(t, err)
	require.ElementsMatch(t, stubHashes(library), stub.writtenHashes()["delete"])
	b := freeSpaceReadAheadBatch
	require.Equal(t, []ResolveRequest{
		{Hashes: stubHashes(library[:b]), Groups: true, OtherShared: true},
		{Hashes: stubHashes(library[b : 3*b]), Groups: true},
		{Hashes: stubHashes(library[3*b:]), Groups: true},
	}, matcher.requests())
}

// The deletes take the same torrents and files as with the groups of the
// whole library.
func TestRunSession_DeleteResultsMatchTheWholeLibrary(t *testing.T) {
	tags := map[string]string{sessionA: "del", sessionB: "del"}
	tests := []struct {
		mode             string
		wantDeleted      []string
		wantFilesDeleted []string
	}{
		{DeleteModeWithFilesIncludeCrossSeeds, []string{sessionA, sessionB, sessionC}, []string{sessionA, sessionB, sessionC}},
		{DeleteModeWithFilesPreserveCrossSeeds, []string{sessionA, sessionB}, []string{sessionB}},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			svc, instanceID, stub, _ := newRecordingStubService(t, tags, false)
			_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{deleteRule(tt.mode, tagsContain("del"))}, false)
			require.NoError(t, err)
			require.ElementsMatch(t, tt.wantDeleted, stub.writtenHashes()["delete"])
			require.ElementsMatch(t, tt.wantFilesDeleted, stub.filesDeleted())
		})
	}
}

// openTestRunSession opens a run session over torrents that answers from links.
func openTestRunSession(t *testing.T, torrents []qbt.Torrent, links map[string][]string, files map[string]qbt.TorrentFiles) (*EvalContext, *recordingMatcher) {
	t.Helper()
	matcher := &recordingMatcher{links: links}
	svc := &Service{crossMatcher: matcher, filesReader: staticFilesReader(files)}
	evalCtx := &EvalContext{}
	needs := RuleNeeds{CrossMatch: CrossMatchNeeds{SameContent: true, OtherShared: true}}
	require.NoError(t, svc.openRunSession(t.Context(), 1, torrentRefs(torrents), needs, evalCtx))
	svc.loadCrossSeedFiles(t.Context(), 1, evalCtx.crossSeeds, evalCtx, nil)
	return evalCtx, matcher
}

type staticFilesReader map[string]qbt.TorrentFiles

func (r staticFilesReader) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	out := make(map[string]qbt.TorrentFiles)
	for _, hash := range hashes {
		if files, ok := r[hash]; ok {
			out[hash] = files
		}
	}
	return out, nil
}

func (staticFilesReader) GetCachedInstanceTorrents(context.Context, int) ([]qbittorrent.CrossInstanceTorrentView, error) {
	return nil, nil
}

// An include delete that follows hardlinks asks for the hardlink copies of the
// group and then their groups before the expansion, so the expansion meets no
// miss and takes what the groups of the whole library take.
func TestRunSession_HardlinkCopyLeadsToASecondGroup(t *testing.T) {
	t.Parallel()
	// source and copy are hardlink copies. copy shares files with pack, and
	// pack with episode2. episode2 and copy2 are hardlink copies, and copy2
	// shares files with pack2.
	torrents := []qbt.Torrent{
		{Hash: "source", SavePath: "/a", ContentPath: "/a/01.mkv", Tags: "delete-me"},
		{Hash: "copy", SavePath: "/b", ContentPath: "/b/01.mkv"},
		{Hash: "pack", SavePath: "/b", ContentPath: "/b"},
		{Hash: "episode2", SavePath: "/b", ContentPath: "/b/02.mkv"},
		{Hash: "copy2", SavePath: "/c", ContentPath: "/c/02.mkv"},
		{Hash: "pack2", SavePath: "/c", ContentPath: "/c"},
	}
	files := map[string]qbt.TorrentFiles{
		"source":   {{Name: "01.mkv", Size: 1000}},
		"copy":     {{Name: "01.mkv", Size: 1000}},
		"pack":     {{Name: "01.mkv", Size: 1000}, {Name: "02.mkv", Size: 1000}},
		"episode2": {{Name: "02.mkv", Size: 1000}},
		"copy2":    {{Name: "02.mkv", Size: 1000}},
		"pack2":    {{Name: "02.mkv", Size: 1000}, {Name: "03.mkv", Size: 1000}},
	}
	links := map[string][]string{
		"copy": {"pack"}, "pack": {"copy", "episode2"}, "episode2": {"pack"},
		"copy2": {"pack2"}, "pack2": {"copy2"},
	}
	hardlinks := &HardlinkIndex{
		DeleteSafeSignatureByHash:  map[string]string{"source": "one", "copy": "one", "episode2": "two", "copy2": "two"},
		DeleteSafeGroupBySignature: map[string][]string{"one": {"source", "copy"}, "two": {"episode2", "copy2"}},
	}
	rule := &models.Automation{
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{Delete: &models.DeleteAction{
			Enabled: true, Mode: DeleteModeWithFilesIncludeCrossSeeds, IncludeHardlinks: true,
			Condition: tagsContain("delete-me"),
		}},
	}
	selected := func(evalCtx *EvalContext, groups *crossSeedGroups) []string {
		evalCtx.hardlinkIndex = hardlinks
		svc := &Service{}
		selection := svc.selectDeletions(t.Context(), 1, deletionSelectionInput{
			rules: []*models.Automation{rule}, torrents: torrentRefs(slices.Clone(torrents)), evalCtx: evalCtx, crossSeeds: groups, scope: noSkipWindow(rule),
		})
		return slices.Sorted(maps.Keys(selection.byHash))
	}

	evalCtx, matcher := openTestRunSession(t, torrents, links, files)
	evalCtx.crossSeeds.resolveDeletes(nil, []string{"source"}, hardlinks, true)
	asks := []ResolveRequest{
		{Hashes: []string{"source"}, Groups: true, OtherShared: true},
		{Hashes: []string{"copy"}, Groups: true},
		{Hashes: []string{"copy2"}, Groups: true},
	}
	require.Equal(t, asks, matcher.requests())
	scoped := selected(evalCtx, evalCtx.crossSeeds)
	require.Equal(t, asks, matcher.requests(), "the expansion meets no miss")

	whole := &EvalContext{SameInstanceContentByHash: links, CrossSeedFilesByHash: files}
	require.Equal(t, selected(whole, buildCrossSeedGroups(torrentRefs(torrents), whole)), scoped)
	require.Equal(t, []string{"copy", "copy2", "episode2", "pack", "pack2", "source"}, scoped)
}

// An include FREE_SPACE rule that follows hardlinks reads the hardlink copies
// of its batch and their groups in one ask, before it counts the first claim.
// It deletes and projects what it does with the groups of the whole library.
func TestRunSession_FreeSpaceReadAheadFollowsHardlinkCopies(t *testing.T) {
	t.Parallel()
	// copyA and copyB are hardlink copies of source, each in its own group.
	torrents := []qbt.Torrent{
		{Hash: "source", SavePath: "/s", ContentPath: "/s/01.mkv", Size: 1000},
		{Hash: "copyA", SavePath: "/a", ContentPath: "/a/01.mkv", Size: 1000},
		{Hash: "packA", SavePath: "/a", ContentPath: "/a", Size: 2000},
		{Hash: "copyB", SavePath: "/b", ContentPath: "/b/01.mkv", Size: 1000},
		{Hash: "packB", SavePath: "/b", ContentPath: "/b", Size: 2000},
	}
	files := map[string]qbt.TorrentFiles{
		"source": {{Name: "01.mkv", Size: 1000}},
		"copyA":  {{Name: "01.mkv", Size: 1000}},
		"packA":  {{Name: "01.mkv", Size: 1000}, {Name: "02.mkv", Size: 1000}},
		"copyB":  {{Name: "01.mkv", Size: 1000}},
		"packB":  {{Name: "01.mkv", Size: 1000}, {Name: "02.mkv", Size: 1000}},
	}
	links := map[string][]string{"copyA": {"packA"}, "packA": {"copyA"}, "copyB": {"packB"}, "packB": {"copyB"}}
	// All five torrents link one inode for 01.mkv.
	id := hardlink.UnixFileID(1, 1)
	hardlinks := &HardlinkIndex{
		DeleteSafeSignatureByHash:  map[string]string{"source": "one", "copyA": "one", "copyB": "one"},
		DeleteSafeGroupBySignature: map[string][]string{"one": {"source", "copyA", "copyB"}},
		buildState: &hardlinkBuildState{torrentInfoByHash: map[string]*torrentFileInfo{
			"source": {linkedFiles: []linkedFile{{path: "/s/01.mkv", fileID: id}}},
			"copyA":  {linkedFiles: []linkedFile{{path: "/a/01.mkv", fileID: id}}},
			"packA":  {linkedFiles: []linkedFile{{path: "/a/01.mkv", fileID: id}}},
			"copyB":  {linkedFiles: []linkedFile{{path: "/b/01.mkv", fileID: id}}},
			"packB":  {linkedFiles: []linkedFile{{path: "/b/01.mkv", fileID: id}}},
		}},
	}
	rule := &models.Automation{
		ID: 1, TrackerPattern: "*",
		Conditions: &models.ActionConditions{Delete: &models.DeleteAction{
			Enabled: true, Mode: DeleteModeWithFilesIncludeCrossSeeds, IncludeHardlinks: true,
			Condition: &models.RuleCondition{Field: FieldFreeSpace, Operator: models.OperatorLessThan, Value: "1000"},
		}},
	}
	type outcome struct {
		selected []string
		ledger   freeSpaceLedger
	}
	run := func(evalCtx *EvalContext, groups *crossSeedGroups) outcome {
		evalCtx.hardlinkIndex = hardlinks
		evalCtx.freeSpaceLedgers = testFreeSpaceLedgers(0, rule)
		skipped := make(map[string]struct{})
		for _, torrent := range torrents {
			if torrent.Hash != "source" {
				skipped[torrent.Hash] = struct{}{}
			}
		}
		svc := &Service{}
		selection := svc.selectDeletions(t.Context(), 1, deletionSelectionInput{
			rules: []*models.Automation{rule}, torrents: torrentRefs(slices.Clone(torrents)), evalCtx: evalCtx, crossSeeds: groups,
			scope: newRuleScope([]*models.Automation{rule}, skipped, nil),
		})
		return outcome{slices.Sorted(maps.Keys(selection.byHash)), *evalCtx.freeSpaceLedgers[rule.ID]}
	}

	evalCtx, matcher := openTestRunSession(t, torrents, links, files)
	// The run reads the file lists of the hardlink groups for this rule.
	evalCtx.crossSeeds.session.loadFiles(t.Context(), hardlinks)
	scoped := run(evalCtx, evalCtx.crossSeeds)
	require.Equal(t, []ResolveRequest{
		{Hashes: []string{"source"}, Groups: true, OtherShared: true},
		{Hashes: []string{"copyA", "copyB"}, Groups: true},
	}, matcher.requests())

	whole := &EvalContext{SameInstanceContentByHash: links, CrossSeedFilesByHash: files}
	require.Equal(t, run(whole, buildCrossSeedGroups(torrentRefs(torrents), whole)), scoped)
	require.Equal(t, []string{"copyA", "copyB", "packA", "packB", "source"}, scoped.selected)
	// The shared inode counts once, plus 02.mkv of each pack.
	require.Equal(t, int64(3000), scoped.ledger.projectedFreedBytes())
}

// A and B pick X, X picks neither, and qBittorrent does not return the file
// list of X. A block list on any of them sees all three.
func TestRunSession_OneWayLinkJoinsTheWholeComponent(t *testing.T) {
	t.Parallel()
	torrents := []qbt.Torrent{
		{Hash: "x", SavePath: "/x", ContentPath: "/x/movie.mkv"},
		{Hash: "a", SavePath: "/a", ContentPath: "/a/movie.mkv", Category: "keep"},
		{Hash: "b", SavePath: "/b", ContentPath: "/b/movie.mkv"},
	}
	oneWay := map[string][]string{"a": {"x"}, "b": {"x"}}

	oracle := buildCrossSeedGroups(torrentRefs(torrents), &EvalContext{SameInstanceContentByHash: oneWay})
	require.Len(t, oracle.members("b"), 2, "the oracle group of b holds only b and x")
	require.False(t, shouldBlockCategoryChangeForCrossSeeds(torrents[2], []string{"keep"}, oracle))

	matcher := &recordingMatcher{links: oneWay}
	evalCtx := &EvalContext{}
	svc := &Service{crossMatcher: matcher}
	require.NoError(t, svc.openRunSession(t.Context(), 1, torrentRefs(torrents), RuleNeeds{CrossMatch: CrossMatchNeeds{SameContent: true}}, evalCtx))
	for _, hash := range []string{"x", "a", "b"} {
		require.ElementsMatch(t, []string{"x", "a", "b"}, memberHashes(evalCtx.crossSeeds.members(hash)), hash)
	}
	require.True(t, shouldBlockCategoryChangeForCrossSeeds(torrents[2], []string{"keep"}, evalCtx.crossSeeds), "the block list sees a")
}

// A torrent added after the session's snapshot is in no group, and a member
// deleted after it is left out of the group.
func TestRunSession_SnapshotAndTorrentListDiffer(t *testing.T) {
	t.Parallel()
	torrents := []qbt.Torrent{{Hash: "a"}, {Hash: "b"}, {Hash: "new"}}
	links := map[string][]string{"a": {"b", "gone"}, "b": {"a"}, "gone": {"a"}}
	evalCtx, _ := openTestRunSession(t, torrents, links, nil)
	require.Equal(t, []string{"a", "b"}, memberHashes(evalCtx.crossSeeds.members("a")))
	require.Nil(t, evalCtx.crossSeeds.members("new"))
}

// The file lists of a later answer go into the map that the delete expansion
// already holds. filesReader reads only the members that the answer holds no
// list for.
func TestRunSession_LaterAnswerFillsTheSameFilesMap(t *testing.T) {
	t.Parallel()
	torrents := []qbt.Torrent{{Hash: "a"}, {Hash: "b"}}
	files := map[string]qbt.TorrentFiles{"a": {{Name: "a.mkv", Size: 1}}, "b": {{Name: "a.mkv", Size: 1}}}
	matcher := &recordingMatcher{links: map[string][]string{"a": {"b"}, "b": {"a"}}, files: map[string]qbt.TorrentFiles{"a": files["a"]}}
	reader := &countingFilesReader{staticFilesReader: files}
	svc := &Service{crossMatcher: matcher, filesReader: reader}
	evalCtx := &EvalContext{}
	require.NoError(t, svc.openRunSession(t.Context(), 1, torrentRefs(torrents), RuleNeeds{CrossMatch: CrossMatchNeeds{SameContent: true}}, evalCtx))
	svc.loadCrossSeedFiles(t.Context(), 1, evalCtx.crossSeeds, evalCtx, nil)
	held := evalCtx.CrossSeedFilesByHash
	require.Empty(t, held, "the load step reads nothing before an ask")
	require.Empty(t, reader.asked)

	fetch := cachedTorrentFilesFetcher(held)
	evalCtx.crossSeeds.resolveDeletes([]string{"a"}, nil, nil, true)
	got, err := fetch(nil)
	require.NoError(t, err)
	require.Equal(t, files, got)
	require.Equal(t, []string{"b"}, reader.asked, "the member without a list from the session")
}

type countingFilesReader struct {
	staticFilesReader
	asked []string
}

func (r *countingFilesReader) GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	r.asked = append(r.asked, hashes...)
	return r.staticFilesReader.GetTorrentFilesBatch(ctx, instanceID, hashes)
}

// A tag rule that does not include cross-seeds lets no expansion override a
// tag, so its ownership check asks for no group.
func TestRunSession_TagRuleWithoutCrossSeedsAsksNothing(t *testing.T) {
	svc, instanceID, stub, matcher := newRecordingStubService(t, map[string]string{sessionA: "del"}, false)
	tagRule := &models.Automation{
		ID: 2, Name: "tag", TrackerPattern: "*", Enabled: true, SortOrder: 2,
		Conditions: &models.ActionConditions{Tags: []*models.TagAction{{Enabled: true, Tags: []string{"seen"}, Condition: tagsContain("")}}},
	}
	rules := []*models.Automation{deleteRule(DeleteModeWithFilesIncludeCrossSeeds, tagsContain("del")), tagRule}
	_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, rules, false)
	require.NoError(t, err)
	require.Equal(t, []ResolveRequest{{Hashes: []string{sessionA}, Groups: true, OtherShared: true}}, matcher.requests())
	require.ElementsMatch(t, []string{sessionA, sessionC}, stub.writtenHashes()["delete"])
	require.Empty(t, stub.writtenHashes()["tag"], "the tag rule matches no torrent, so each torrent runs the ownership check")
}

// A move asks once, before the rule loop, for the torrents whose move
// condition matches and that are not at the target yet, and for the members
// of their move GroupID. The plan step asks for nothing new, and the run plans
// the moves that the groups of the whole library plan.
func TestRunSession_MoveAsksOnlyTheMatchedTorrents(t *testing.T) {
	t.Parallel()
	// m1 and m1x share files. at is at the target, with its cross-seed atx.
	// g2 shares the save path of m1, and files with g2x. px and py share files.
	// m1, at and px carry the tag.
	torrents := []qbt.Torrent{
		{Hash: "m1", SavePath: "/old", ContentPath: "/old/m1.mkv", Tags: "mv"},
		{Hash: "m1x", SavePath: "/old", ContentPath: "/old/m1x.mkv"},
		{Hash: "at", SavePath: "/new", ContentPath: "/new/at.mkv", Tags: "mv"},
		{Hash: "atx", SavePath: "/new", ContentPath: "/new/atx.mkv"},
		{Hash: "g2", SavePath: "/old", ContentPath: "/old/g2.mkv"},
		{Hash: "g2x", SavePath: "/x", ContentPath: "/x/g2.mkv"},
		{Hash: "px", SavePath: "/p", ContentPath: "/p/p.mkv", Tags: "mv"},
		{Hash: "py", SavePath: "/p", ContentPath: "/p/py.mkv"},
	}
	links := map[string][]string{
		"m1": {"m1x"}, "m1x": {"m1"}, "at": {"atx"}, "atx": {"at"},
		"g2": {"g2x"}, "g2x": {"g2"}, "px": {"py"}, "py": {"px"},
	}
	moveTo := func(groupID string) *models.Automation {
		rule := &models.Automation{
			ID: 1, Name: "move", TrackerPattern: "*", Enabled: true,
			Conditions: &models.ActionConditions{Move: &models.MoveAction{Enabled: true, Path: "/new", GroupID: groupID, Condition: tagsContain("mv")}},
		}
		if groupID != "" {
			rule.Conditions.Grouping = &models.GroupingConfig{Groups: []models.GroupDefinition{{ID: groupID, Keys: []string{"savePath"}}}}
		}
		return rule
	}
	tests := []struct {
		name      string
		rule      *models.Automation
		wantAsk   []string
		wantMoved []string
	}{
		{"cross-seeds move along", moveTo(""), []string{"m1", "px"}, []string{"m1", "m1x", "px", "py"}},
		// g2x holds files of g2, so the group cannot move.
		{"group ID members", moveTo("bysave"), []string{"m1", "m1x", "g2", "px", "py"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := func(evalCtx *EvalContext, groups *crossSeedGroups) ([]string, ruleRunStats) {
				run := slices.Clone(torrents)
				stats := make(map[int]*ruleRunStats)
				states := processTorrents(torrentRefs(run), []*models.Automation{tt.rule}, evalCtx, groups, noSkipWindow(tt.rule), stats, nil)
				torrentByHash := make(map[string]qbt.Torrent, len(run))
				for _, torrent := range run {
					torrentByHash[torrent.Hash] = torrent
				}
				p := (&Service{}).buildRunPlan(t.Context(), runPlanInput{
					instanceID: 1, states: states, deletions: &deletionSelection{}, torrents: torrentRefs(run), torrentByHash: torrentRefMap(torrentByHash),
					ruleByID: map[int]*models.Automation{tt.rule.ID: tt.rule}, crossSeeds: groups, evalCtx: evalCtx, scope: noSkipWindow(tt.rule),
				})
				return slices.Sorted(slices.Values(p.move.byTarget["/new"])), *stats[tt.rule.ID]
			}

			evalCtx, matcher := openTestRunSession(t, torrents, links, nil)
			moved, stats := plan(evalCtx, evalCtx.crossSeeds)
			asks := matcher.requests()
			require.Len(t, asks, 1)
			require.ElementsMatch(t, tt.wantAsk, asks[0].Hashes)
			require.True(t, asks[0].Groups)
			require.Equal(t, tt.wantMoved, moved)

			whole := &EvalContext{SameInstanceContentByHash: links}
			wholeMoved, wholeStats := plan(whole, buildCrossSeedGroups(torrentRefs(torrents), whole))
			require.Equal(t, wholeMoved, moved)
			require.Equal(t, wholeStats, stats)
		})
	}
}

// A live run and a dry run move the cross-seed of a matched torrent with it,
// and ask only for the matched torrent.
func TestRunSession_MoveTakesTheCrossSeedsAlong(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("dryRun=%t", dryRun), func(t *testing.T) {
			svc, instanceID, stub, matcher := newRecordingStubService(t, map[string]string{sessionA: "mv"}, false)
			rule := moveRule("")
			rule.DryRun = dryRun
			activities, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{rule}, dryRun)
			require.NoError(t, err)
			moved := stub.writtenHashes()["move"]
			if dryRun {
				require.Empty(t, moved)
				moved = dryRunActivityHashes(t, svc, instanceID, activities)["move"]
			}
			require.Equal(t, []string{sessionA, sessionC}, moved)
			require.Equal(t, []ResolveRequest{{Hashes: []string{sessionA}, Groups: true}}, matcher.requests())
		})
	}
}

// unknownSession answers every ask with the asked torrents as Unknown.
type unknownSession struct{}

func (unknownSession) OpenCrossMatchSession(context.Context, int, CrossMatchNeeds) (CrossMatchSession, error) {
	return unknownSession{}, nil
}

func (unknownSession) Resolve(_ context.Context, req ResolveRequest) *CrossMatchResult {
	unknown := make(map[string]struct{}, len(req.Hashes))
	for _, hash := range req.Hashes {
		unknown[hash] = struct{}{}
	}
	return &CrossMatchResult{SameInstanceUnknown: unknown}
}

// A move of a torrent that no ask covered reads its group state first, so a
// torrent with an Unknown state and no group stays where it is.
func TestRunSession_MoveMissReadsTheStateFirst(t *testing.T) {
	t.Parallel()
	torrent := qbt.Torrent{Hash: "t", SavePath: "/old", ContentPath: "/old/t.mkv"}
	svc := &Service{crossMatcher: unknownSession{}}
	evalCtx := &EvalContext{}
	require.NoError(t, svc.openRunSession(t.Context(), 1, torrentRefs([]qbt.Torrent{torrent}), RuleNeeds{CrossMatch: CrossMatchNeeds{SameContent: true}}, evalCtx))
	rule := &models.Automation{ID: 1, Name: "move", TrackerPattern: "*", Enabled: true,
		Conditions: &models.ActionConditions{Move: &models.MoveAction{Enabled: true, Path: "/new"}}}
	state := &torrentDesiredState{hash: torrent.Hash}
	stats := &ruleRunStats{}
	evaluateMoveAction(rule, rule.Conditions.Move, torrent, evalCtx, evalCtx.crossSeeds, stats, state)
	require.False(t, state.shouldMove)
	require.Equal(t, 1, stats.MoveBlockedByCrossSeed)
}

// The read-ahead asks for the members of a move GroupID once, not once for
// each matched member.
func TestMoveAsks_GroupMembersOnce(t *testing.T) {
	t.Parallel()
	torrents := make([]qbt.Torrent, 0, 20)
	for i := range 20 {
		torrents = append(torrents, qbt.Torrent{Hash: fmt.Sprintf("h%02d", i), SavePath: "/old", ContentPath: fmt.Sprintf("/old/%02d.mkv", i), Tags: "mv"})
	}
	rule := &models.Automation{
		ID: 1, Name: "move", TrackerPattern: "*", Enabled: true,
		Conditions: &models.ActionConditions{
			Move:     &models.MoveAction{Enabled: true, Path: "/new", GroupID: "bysave", Condition: tagsContain("mv")},
			Grouping: &models.GroupingConfig{Groups: []models.GroupDefinition{{ID: "bysave", Keys: []string{"savePath"}}}},
		},
	}
	evalCtx := &EvalContext{moveChecks: make(map[moveCheckKey]checkedMove)}
	asked := make(map[askedGroup]struct{})
	hashes := make([]string, 0, 2*len(torrents))
	for _, torrent := range torrents {
		hashes = append(hashes, moveAsks(&torrent, []*models.Automation{rule}, torrentRefs(torrents), evalCtx, noSkipWindow(rule), asked)...)
	}
	require.Len(t, hashes, 2*len(torrents))
}

// Two rules with one GroupID each ask for their own group, also when the keys
// of both groups are the same text.
func TestMoveAsks_GroupsOfEachRule(t *testing.T) {
	t.Parallel()
	// Under a, c1 and c2 share the content path /data/x. Under b, s1 and s2
	// share the save path /data/x.
	torrents := []qbt.Torrent{
		{Hash: "c1", SavePath: "/c", ContentPath: "/data/x", Tags: "a"},
		{Hash: "c2", SavePath: "/d", ContentPath: "/data/x"},
		{Hash: "s1", SavePath: "/data/x", ContentPath: "/data/x/s1.mkv", Tags: "b"},
		{Hash: "s2", SavePath: "/data/x", ContentPath: "/data/x/s2.mkv"},
	}
	moveBy := func(id int, tag, key string) *models.Automation {
		return &models.Automation{
			ID: id, Name: tag, TrackerPattern: "*", Enabled: true,
			Conditions: &models.ActionConditions{
				Move:     &models.MoveAction{Enabled: true, Path: "/new", GroupID: "g", Condition: tagsContain(tag)},
				Grouping: &models.GroupingConfig{Groups: []models.GroupDefinition{{ID: "g", Keys: []string{key}}}},
			},
		}
	}
	rules := []*models.Automation{moveBy(1, "a", "contentPath"), moveBy(2, "b", "savePath")}
	evalCtx := &EvalContext{moveChecks: make(map[moveCheckKey]checkedMove)}
	asked := make(map[askedGroup]struct{})
	hashes := make([]string, 0, 2*len(torrents))
	for _, torrent := range torrents {
		hashes = append(hashes, moveAsks(&torrent, rules, torrentRefs(torrents), evalCtx, noSkipWindow(rules...), asked)...)
	}
	slices.Sort(hashes)
	require.Equal(t, []string{"c1", "c2", "s1", "s2"}, slices.Compact(hashes))
}

// factService serves library from a recording session that answers Exists from exists.
func factService(library []stubTorrent, exists map[string]struct{}) func(t *testing.T) (*Service, int, *stubQbt, *recordingMatcher) {
	return func(t *testing.T) (*Service, int, *stubQbt, *recordingMatcher) {
		t.Helper()
		svc, instanceID, stub, matcher := newRecordingStubServiceOf(t, library, false)
		matcher.exists = exists
		return svc, instanceID, stub, matcher
	}
}

func existsOnSameInstance() *models.RuleCondition {
	return &models.RuleCondition{Field: models.FieldExistsOnSameInstance, Operator: models.OperatorEqual, Value: "true"}
}

// factLibrary serves 150 torrents with no cross-seed group: the first 50 on
// the tracker scope.test, the rest on other.test. Every third torrent has a
// cross-seed on this instance.
func factLibrary() ([]stubTorrent, []string, map[string]struct{}) {
	torrents := make([]stubTorrent, 0, 150)
	scope := make([]string, 0, 50)
	exists := make(map[string]struct{})
	for i := range 150 {
		hash := fmt.Sprintf("%040x", i+1)
		tracker := "https://other.test/announce"
		if i < 50 {
			tracker = "https://scope.test/announce"
			scope = append(scope, hash)
		}
		if i%3 == 0 {
			exists[hash] = struct{}{}
		}
		torrents = append(torrents, stubTorrent{hash, fmt.Sprintf("Fact.Show.S01E%03d.1080p.WEB.x264-GRP", i), "", tracker})
	}
	return torrents, scope, exists
}

// A rule with an Exists condition asks once for the torrents in its tracker
// scope, with no group, and deletes the same torrents as an ask for the whole
// library.
func TestRunSession_ExistsConditionAsksOnlyTheTrackerScope(t *testing.T) {
	library, scope, exists := factLibrary()
	rule := deleteRule(DeleteModeKeepFiles, existsOnSameInstance())
	rule.TrackerPattern = "scope.test"
	var want deleteOutcome
	for _, hash := range scope {
		if _, ok := exists[hash]; ok {
			want.deleted = append(want.deleted, hash)
		}
	}
	require.Len(t, want.deleted, 17)
	runEachFormOn(t, factService(library, exists), rule, want, func(t *testing.T, asks []ResolveRequest, _ *stubQbt) {
		require.Len(t, asks, 1)
		require.False(t, asks[0].All)
		require.False(t, asks[0].Groups)
		require.ElementsMatch(t, scope, asks[0].Hashes)
	})
}

// A score sort on a cross-seed field scores every torrent, so the rule asks
// once for the whole list.
func TestRunSession_ScoreSortOnACrossSeedFieldAsksTheWholeList(t *testing.T) {
	library, scope, exists := factLibrary()
	rule := deleteRule(DeleteModeKeepFiles, &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorContains, Value: "Fact"})
	rule.TrackerPattern = "scope.test"
	rule.SortingConfig = &models.SortingConfig{
		SchemaVersion: "1", Type: models.SortingTypeScore, Direction: models.SortDirectionDESC,
		ScoreRules: []models.ScoreRule{{Type: models.ScoreRuleTypeConditional, Conditional: &models.ConditionalScoreRule{Condition: existsOnSameInstance(), Score: 1}}},
	}
	runEachFormOn(t, factService(library, exists), rule, deleteOutcome{deleted: scope}, func(t *testing.T, asks []ResolveRequest, _ *stubQbt) {
		require.Equal(t, []ResolveRequest{{All: true}}, asks)
	})
}

// A group delete checks the condition on every member of the GroupID group,
// so the one ask also holds the members outside the tracker scope.
func TestRunSession_GroupCheckAsksTheMembersOutsideTheScope(t *testing.T) {
	library := make([]stubTorrent, 0, 150)
	scope := make([]string, 0, 50)
	var partners []string
	exists := make(map[string]struct{})
	for k := range 50 {
		hash := fmt.Sprintf("%040x", k+1)
		scope = append(scope, hash)
		library = append(library, stubTorrent{hash, fmt.Sprintf("Group.Show.S01E%03d.1080p.WEB.x264-GRP", k), "", "https://scope.test/announce"})
		if k%2 == 0 {
			exists[hash] = struct{}{}
		}
	}
	// The first 10 share a content path with a torrent on another tracker.
	for k := range 100 {
		hash := fmt.Sprintf("%040x", 1000+k)
		name := fmt.Sprintf("Other.Show.S01E%03d.1080p.WEB.x264-GRP", k)
		if k < 10 {
			name = fmt.Sprintf("Group.Show.S01E%03d.1080p.WEB.x264-GRP", k)
			partners = append(partners, hash)
			if k%4 == 0 {
				exists[hash] = struct{}{}
			}
		}
		library = append(library, stubTorrent{hash, name, "", "https://other.test/announce"})
	}
	rule := deleteRule(DeleteModeKeepFiles, existsOnSameInstance())
	rule.TrackerPattern = "scope.test"
	rule.Conditions.Delete.GroupID = "bycontent"
	rule.Conditions.Grouping = &models.GroupingConfig{Groups: []models.GroupDefinition{{ID: "bycontent", Keys: []string{"contentPath"}}}}
	var want deleteOutcome
	for k := 0; k < 50; k += 2 {
		switch {
		case k >= 10:
			want.deleted = append(want.deleted, scope[k])
		case k%4 == 0:
			want.deleted = append(want.deleted, scope[k], partners[k])
		}
	}
	runEachFormOn(t, factService(library, exists), rule, want, func(t *testing.T, asks []ResolveRequest, _ *stubQbt) {
		require.Len(t, asks, 1)
		require.False(t, asks[0].All)
		require.ElementsMatch(t, slices.Concat(scope, partners), asks[0].Hashes)
	})
}

// A move that blocks on a cross-seed checks its condition on the cross-seeds
// of each matched torrent, so it asks for their facts in one ask before the
// rule loop.
func TestRunSession_MoveBlockAsksTheCrossSeedFactsOnce(t *testing.T) {
	const (
		sessionD = "dddddddddddddddddddddddddddddddddddddddd"
		sessionE = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	)
	scopeTracker, otherTracker := "https://scope.test/announce", "https://other.test/announce"
	// c is the cross-seed of a, and e the cross-seed of d, both on another tracker.
	library := []stubTorrent{
		{sessionA, "Quiet.Coast.2020.1080p.BluRay.x264-GRP", "", scopeTracker},
		{sessionB, "Silent.Shore.2021.1080p.BluRay.x264-GRP", "", scopeTracker},
		{sessionC, "Quiet.Coast.2020.1080p.BluRay.x264-GRP", "", otherTracker},
		{sessionD, "Calm.Bay.2022.1080p.BluRay.x264-GRP", "", scopeTracker},
		{sessionE, "Calm.Bay.2022.1080p.BluRay.x264-GRP", "", otherTracker},
	}
	svc, instanceID, stub, matcher := newRecordingStubServiceOf(t, library, false)
	matcher.exists = map[string]struct{}{sessionA: {}, sessionC: {}, sessionD: {}, sessionE: {}}
	rule := moveRule("")
	rule.TrackerPattern = "scope.test"
	rule.Conditions.Move.Condition = existsOnSameInstance()
	rule.Conditions.Move.BlockIfCrossSeed = true
	_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{rule}, false)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{sessionA, sessionC, sessionD, sessionE}, stub.writtenHashes()["move"])
	require.Equal(t, []ResolveRequest{
		{Hashes: []string{sessionA, sessionB, sessionD}},
		{Hashes: []string{sessionA, sessionD}, Groups: true},
		{Hashes: []string{sessionC, sessionE}},
	}, matcher.requests())
}

// A condition on a torrent that no ask covered reads its facts first, so it
// never gets "known, no cross-seed" from a torrent that the session did not read.
func TestRunSession_FactMissReadsTheTorrentFirst(t *testing.T) {
	t.Parallel()
	torrent := qbt.Torrent{Hash: sessionA}
	matcher := &recordingMatcher{exists: map[string]struct{}{sessionA: {}}}
	svc := &Service{crossMatcher: matcher}
	evalCtx := &EvalContext{}
	require.NoError(t, svc.openRunSession(t.Context(), 1, torrentRefs([]qbt.Torrent{torrent}), RuleNeeds{CrossMatch: CrossMatchNeeds{SameExists: true}}, evalCtx))
	require.Empty(t, matcher.requests())
	require.True(t, EvaluateConditionWithContext(existsOnSameInstance(), torrent, evalCtx, 0))
	require.True(t, EvaluateConditionWithContext(existsOnSameInstance(), torrent, evalCtx, 0))
	require.Equal(t, []ResolveRequest{{Hashes: []string{sessionA}}}, matcher.requests())
}

// planRules runs rules through the rule loop and the Run plan, as a run with no
// delete does.
func planRules(t *testing.T, torrents []qbt.Torrent, rules []*models.Automation, evalCtx *EvalContext, groups *crossSeedGroups) (runPlan, map[int]*ruleRunStats) {
	t.Helper()
	run := slices.Clone(torrents)
	stats := make(map[int]*ruleRunStats)
	states := processTorrents(torrentRefs(run), rules, evalCtx, groups, noSkipWindow(rules...), stats, nil)
	torrentByHash := make(map[string]qbt.Torrent, len(run))
	for _, torrent := range run {
		torrentByHash[torrent.Hash] = torrent
	}
	ruleByID := make(map[int]*models.Automation, len(rules))
	for _, rule := range rules {
		ruleByID[rule.ID] = rule
	}
	plan := (&Service{}).buildRunPlan(t.Context(), runPlanInput{
		instanceID: 1, states: states, deletions: &deletionSelection{}, torrents: torrentRefs(run), torrentByHash: torrentRefMap(torrentByHash),
		ruleByID: ruleByID, crossSeeds: groups, evalCtx: evalCtx, scope: noSkipWindow(rules...),
	})
	return plan, stats
}

// openRulesRunSession opens a run session over torrents with the needs of
// rules, answering from links.
func openRulesRunSession(t *testing.T, torrents []qbt.Torrent, links map[string][]string, rules []*models.Automation) (*EvalContext, *recordingMatcher) {
	t.Helper()
	matcher := &recordingMatcher{links: links}
	svc := &Service{crossMatcher: matcher}
	evalCtx := &EvalContext{}
	require.NoError(t, svc.openRunSession(t.Context(), 1, torrentRefs(torrents), NeedsFor(rules), evalCtx))
	return evalCtx, matcher
}

// A category action that includes cross-seeds or has a block list asks once,
// before the rule loop, for the torrents whose condition matches, and for the
// members of their category GroupID. The plan step asks for nothing new, and
// the run plans the changes that the groups of the whole library plan.
func TestRunSession_CategoryAsksOnlyTheMatchedTorrents(t *testing.T) {
	t.Parallel()
	// c1 to c5 carry the tag. c1 shares files with c1x in the protected
	// category, and c2 with c2x. o1 and o1x share files. o2 carries the tag
	// and shares the save path of c3, outside the tracker scope.
	const scope = "https://scope.test/announce"
	torrents := []qbt.Torrent{
		{Hash: "c1", SavePath: "/1", Tracker: scope, Tags: "cat"},
		{Hash: "c1x", SavePath: "/1x", Tracker: scope, Category: "keep"},
		{Hash: "c2", SavePath: "/2", Tracker: scope, Tags: "cat"},
		{Hash: "c2x", SavePath: "/2x", Tracker: scope},
		{Hash: "c3", SavePath: "/3", Tracker: scope, Tags: "cat"},
		{Hash: "c4", SavePath: "/4", Tracker: scope, Tags: "cat"},
		{Hash: "c5", SavePath: "/5", Tracker: scope, Tags: "cat"},
		{Hash: "o1", SavePath: "/o1", Tracker: scope},
		{Hash: "o1x", SavePath: "/o1x", Tracker: scope, Category: "keep"},
		{Hash: "o2", SavePath: "/3", Tracker: "https://other.test/announce", Tags: "cat"},
	}
	links := map[string][]string{"c1": {"c1x"}, "c1x": {"c1"}, "c2": {"c2x"}, "c2x": {"c2"}, "o1": {"o1x"}, "o1x": {"o1"}}
	categoryTo := func(include bool, block []string, groupID string) *models.Automation {
		rule := &models.Automation{
			ID: 1, Name: "category", TrackerPattern: "scope.test", Enabled: true,
			Conditions: &models.ActionConditions{Category: &models.CategoryAction{
				Enabled: true, Category: "new", IncludeCrossSeeds: include, BlockIfCrossSeedInCategories: block, GroupID: groupID, Condition: tagsContain("cat"),
			}},
		}
		if groupID != "" {
			rule.Conditions.Grouping = &models.GroupingConfig{Groups: []models.GroupDefinition{{ID: groupID, Keys: []string{"savePath"}}}}
		}
		return rule
	}
	matched := []string{"c1", "c2", "c3", "c4", "c5"}
	tests := []struct {
		name        string
		rule        *models.Automation
		wantAsk     []string
		wantChanged []string
	}{
		{"block list", categoryTo(false, []string{"keep"}, ""), matched, []string{"c2", "c3", "c4", "c5"}},
		{"include cross-seeds", categoryTo(true, nil, ""), matched, []string{"c1", "c1x", "c2", "c2x", "c3", "c4", "c5"}},
		// The plan step checks the block list of o2 before the group of c3 changes.
		{"block list with group ID members", categoryTo(false, []string{"keep"}, "bysave"), append(slices.Clone(matched), "o2"), []string{"c2", "c3", "c4", "c5", "o2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rules := []*models.Automation{tt.rule}
			evalCtx, matcher := openRulesRunSession(t, torrents, links, rules)
			plan, stats := planRules(t, torrents, rules, evalCtx, evalCtx.crossSeeds)
			asks := matcher.requests()
			require.Len(t, asks, 1)
			require.ElementsMatch(t, tt.wantAsk, asks[0].Hashes)
			require.True(t, asks[0].Groups)
			require.False(t, asks[0].All)
			changed := slices.Sorted(slices.Values(plan.category.byTarget["new"]))
			require.Equal(t, tt.wantChanged, changed)

			whole := &EvalContext{SameInstanceContentByHash: links}
			wholePlan, wholeStats := planRules(t, torrents, rules, whole, buildCrossSeedGroups(torrentRefs(torrents), whole))
			require.Equal(t, slices.Sorted(slices.Values(wholePlan.category.byTarget["new"])), changed)
			require.Equal(t, *wholeStats[tt.rule.ID], *stats[tt.rule.ID])
		})
	}
}

// When a due tag rule includes cross-seeds, the tag step asks once for the
// tracker scope of each tag rule, because the ownership check of every tag
// rule reads the group. The run plans the tags that the groups of the whole
// library plan.
func TestRunSession_TagAsksTheTrackerScopes(t *testing.T) {
	t.Parallel()
	const scope, other, none = "https://scope.test/announce", "https://other.test/announce", "https://none.test/announce"
	// s1 shares files with s1x, s2 with n2, and o1 with o1x.
	torrents := []qbt.Torrent{
		{Hash: "s1", Tracker: scope, Tags: "t"},
		{Hash: "s1x", Tracker: other},
		{Hash: "s2", Tracker: scope},
		{Hash: "s3", Tracker: scope, Tags: "t"},
		{Hash: "o1", Tracker: other, Tags: "t"},
		{Hash: "o1x", Tracker: other, Tags: "seen"},
		{Hash: "n1", Tracker: none},
		{Hash: "n2", Tracker: none, Tags: "seen"},
	}
	links := map[string][]string{"s1": {"s1x"}, "s1x": {"s1"}, "s2": {"n2"}, "n2": {"s2"}, "o1": {"o1x"}, "o1x": {"o1"}}
	include := &models.Automation{
		ID: 1, Name: "include", TrackerPattern: "scope.test", Enabled: true, SortOrder: 1,
		Conditions: &models.ActionConditions{Tags: []*models.TagAction{{Enabled: true, IncludeCrossSeeds: true, Tags: []string{"seen"}, Condition: tagsContain("t")}}},
	}
	plain := &models.Automation{
		ID: 2, Name: "plain", TrackerPattern: "other.test", Enabled: true, SortOrder: 2,
		Conditions: &models.ActionConditions{Tags: []*models.TagAction{{Enabled: true, Tags: []string{"seen"}, Condition: tagsContain("t")}}},
	}
	tests := []struct {
		name    string
		rules   []*models.Automation
		wantAsk []string
	}{
		{"include rule", []*models.Automation{include}, []string{"s1", "s2", "s3"}},
		{"include and plain rules", []*models.Automation{include, plain}, []string{"s1", "s1x", "s2", "s3", "o1", "o1x"}},
		{"plain rule", []*models.Automation{plain}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			evalCtx, matcher := openRulesRunSession(t, torrents, links, tt.rules)
			plan, stats := planRules(t, torrents, tt.rules, evalCtx, evalCtx.crossSeeds)
			asks := matcher.requests()
			if tt.wantAsk == nil {
				require.Empty(t, asks)
			} else {
				require.Len(t, asks, 1)
				require.ElementsMatch(t, tt.wantAsk, asks[0].Hashes)
				require.True(t, asks[0].Groups)
			}
			require.NotEmpty(t, plan.tags)

			whole := &EvalContext{SameInstanceContentByHash: links}
			wholePlan, wholeStats := planRules(t, torrents, tt.rules, whole, buildCrossSeedGroups(torrentRefs(torrents), whole))
			require.Equal(t, wholePlan.tags, plan.tags)
			for _, rule := range tt.rules {
				require.Equal(t, *wholeStats[rule.ID], *stats[rule.ID])
			}
		})
	}
}

// A live run, a dry run, and a preview of a category or tag action that
// includes cross-seeds take the cross-seed of a matched torrent along. The
// category asks only for the matched torrent, and the tag for its tracker
// scope.
func TestRunSession_CategoryAndTagTakeTheCrossSeedsAlong(t *testing.T) {
	tagRule := &models.Automation{
		ID: 3, Name: "tag", TrackerPattern: "*", Enabled: true, SortOrder: 3,
		Conditions: &models.ActionConditions{Tags: []*models.TagAction{{Enabled: true, IncludeCrossSeeds: true, Mode: models.TagModeAdd, Tags: []string{"seen"}, Condition: tagsContain("mv")}}},
	}
	tests := []struct {
		action  string
		rule    *models.Automation
		wantAsk []string
	}{
		{"category", categoryRule(true, ""), []string{sessionA}},
		{"tag", tagRule, []string{sessionA, sessionB, sessionC}},
	}
	for _, tt := range tests {
		for _, form := range []string{"live", "dry run", "preview"} {
			t.Run(tt.action+"/"+form, func(t *testing.T) {
				svc, instanceID, stub, matcher := newRecordingStubService(t, map[string]string{sessionA: "mv"}, false)
				var changed []string
				switch form {
				case "live", "dry run":
					rule := *tt.rule
					rule.DryRun = form == "dry run"
					activities, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{&rule}, rule.DryRun)
					require.NoError(t, err)
					changed = stub.writtenHashes()[tt.action]
					if rule.DryRun {
						require.Empty(t, changed)
						changed = dryRunActivityHashes(t, svc, instanceID, activities)[tt.action]
					}
				case "preview":
					preview := svc.PreviewCategoryRule
					if tt.action == "tag" {
						preview = svc.PreviewTagRule
					}
					result, err := preview(t.Context(), instanceID, tt.rule, 100, 0)
					require.NoError(t, err)
					for _, example := range result.Examples {
						changed = append(changed, example.Hash)
					}
				}
				require.ElementsMatch(t, []string{sessionA, sessionC}, changed)
				asks := matcher.requests()
				require.Len(t, asks, 1)
				require.ElementsMatch(t, tt.wantAsk, asks[0].Hashes)
				require.True(t, asks[0].Groups)
			})
		}
	}
}
