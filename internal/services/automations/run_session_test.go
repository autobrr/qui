// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"encoding/json/v2"
	"maps"
	"slices"
	"sync"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// cannedSession answers every Resolve with one canned result and no member
// files, as the fake CrossMatchers do.
type cannedSession struct{ result *CrossMatchResult }

func (s cannedSession) Resolve(context.Context, ResolveRequest) *CrossMatchResult {
	return s.result
}

// recordingMatcher answers each Resolve with the components of the asked
// torrents in links, as the cross-seed session does, and records each request.
type recordingMatcher struct {
	links  map[string][]string
	shared map[string]struct{}
	files  map[string]qbt.TorrentFiles // MemberFiles of the answers
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
	start := req.Hashes
	if req.All {
		start = slices.Collect(maps.Keys(m.links))
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
	stub, sharing := newWriteStub(t, []stubTorrent{
		{sessionA, "Quiet.Coast.2020.1080p.BluRay.x264-GRP", tags[sessionA], ""},
		{sessionB, "Silent.Shore.2021.1080p.BluRay.x264-GRP", tags[sessionB], ""},
		{sessionC, "Quiet.Coast.2020.1080p.BluRay.x264-GRP", tags[sessionC], ""},
	})
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
	for _, form := range []string{"live", "dry run", "preview"} {
		t.Run(form, func(t *testing.T) {
			svc, instanceID, stub, matcher := newRecordingStubService(t, tags, localFS)
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

func TestRunSession_FreeSpaceDeleteAsksTheWholeLibraryWithTheSharedCheckFirst(t *testing.T) {
	rule := func(mode string) *models.Automation {
		r := deleteRule(mode, &models.RuleCondition{Field: FieldFreeSpace, Operator: models.OperatorLessThan, Value: "1500"})
		r.SortingConfig = &models.SortingConfig{SchemaVersion: "1", Type: models.SortingTypeSimple, Field: FieldName, Direction: models.SortDirectionDESC}
		return r
	}
	// The sort puts b first, and its delete frees enough space.
	want := deleteOutcome{[]string{sessionB}, []string{sessionB}}
	for _, mode := range []string{DeleteModeWithFilesIncludeCrossSeeds, DeleteModeWithFilesPreserveCrossSeeds} {
		for _, localFS := range []bool{false, true} {
			t.Run(mode, func(t *testing.T) {
				runEachForm(t, rule(mode), nil, localFS, want, func(t *testing.T, asks []ResolveRequest, _ *stubQbt) {
					require.Equal(t, []ResolveRequest{{All: true, Groups: true, OtherShared: true}}, asks,
						"one whole-library ask, before the first claim, and no second build")
				})
			})
		}
	}
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
	require.NoError(t, svc.openRunSession(t.Context(), 1, torrents, needs, evalCtx))
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
			rules: []*models.Automation{rule}, torrents: slices.Clone(torrents), evalCtx: evalCtx, crossSeeds: groups,
		})
		return slices.Sorted(maps.Keys(selection.byHash))
	}

	evalCtx, matcher := openTestRunSession(t, torrents, links, files)
	evalCtx.crossSeeds.resolveDeletes(nil, []string{"source"}, hardlinks)
	asks := []ResolveRequest{
		{Hashes: []string{"source"}, Groups: true, OtherShared: true},
		{Hashes: []string{"copy"}, Groups: true},
		{Hashes: []string{"copy2"}, Groups: true},
	}
	require.Equal(t, asks, matcher.requests())
	scoped := selected(evalCtx, evalCtx.crossSeeds)
	require.Equal(t, asks, matcher.requests(), "the expansion meets no miss")

	whole := &EvalContext{SameInstanceContentByHash: links, CrossSeedFilesByHash: files}
	require.Equal(t, selected(whole, buildCrossSeedGroups(torrents, whole)), scoped)
	require.Equal(t, []string{"copy", "copy2", "episode2", "pack", "pack2", "source"}, scoped)
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

	oracle := buildCrossSeedGroups(torrents, &EvalContext{SameInstanceContentByHash: oneWay})
	require.Len(t, oracle.members("b"), 2, "the oracle group of b holds only b and x")
	require.False(t, shouldBlockCategoryChangeForCrossSeeds(torrents[2], []string{"keep"}, oracle))

	matcher := &recordingMatcher{links: oneWay}
	evalCtx := &EvalContext{}
	svc := &Service{crossMatcher: matcher}
	require.NoError(t, svc.openRunSession(t.Context(), 1, torrents, RuleNeeds{CrossMatch: CrossMatchNeeds{SameContent: true}, CrossSeedGroupsAll: true}, evalCtx))
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
	require.NoError(t, svc.openRunSession(t.Context(), 1, torrents, RuleNeeds{CrossMatch: CrossMatchNeeds{SameContent: true}}, evalCtx))
	svc.loadCrossSeedFiles(t.Context(), 1, evalCtx.crossSeeds, evalCtx, nil)
	held := evalCtx.CrossSeedFilesByHash
	require.Empty(t, held, "the load step reads nothing before an ask")
	require.Empty(t, reader.asked)

	fetch := cachedTorrentFilesFetcher(held)
	evalCtx.crossSeeds.resolveDeletes([]string{"a"}, nil, nil)
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
