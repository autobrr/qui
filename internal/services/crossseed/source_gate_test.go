// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
)

// regression: #3626
// regression: #3707
func TestSourceGateCheck(t *testing.T) {
	t.Parallel()

	keeper := sourceFilter{categories: []string{"tv"}}
	states := []struct {
		state       qbt.TorrentState
		recoveryOff sourceAnswer
		recoveryOn  sourceAnswer
		partialOn   sourceAnswer // recovery on, progress below 100%
		partialOff  sourceAnswer // recovery off, progress below 100%
	}{
		{qbt.TorrentStateUploading, sourcePass, sourcePass, sourceIncomplete, sourceIncomplete},
		{qbt.TorrentStatePausedUp, sourcePass, sourcePass, sourceIncomplete, sourceIncomplete},
		{qbt.TorrentStateStoppedUp, sourcePass, sourcePass, sourceIncomplete, sourceIncomplete},
		{qbt.TorrentStateQueuedUp, sourcePass, sourcePass, sourceIncomplete, sourceIncomplete},
		{qbt.TorrentStateStalledUp, sourcePass, sourcePass, sourceIncomplete, sourceIncomplete},
		{qbt.TorrentStateForcedUp, sourcePass, sourcePass, sourceIncomplete, sourceIncomplete},
		{qbt.TorrentStateAllocating, sourcePass, sourcePass, sourceIncomplete, sourceIncomplete},
		{qbt.TorrentStateDownloading, sourcePass, sourcePass, sourceIncomplete, sourceIncomplete},
		{qbt.TorrentStateMetaDl, sourcePass, sourcePass, sourceIncomplete, sourceIncomplete},
		{qbt.TorrentStatePausedDl, sourcePass, sourcePass, sourceIncomplete, sourceIncomplete},
		{qbt.TorrentStateStoppedDl, sourcePass, sourcePass, sourceIncomplete, sourceIncomplete},
		{qbt.TorrentStateQueuedDl, sourcePass, sourcePass, sourceIncomplete, sourceIncomplete},
		{qbt.TorrentStateStalledDl, sourcePass, sourcePass, sourceIncomplete, sourceIncomplete},
		{qbt.TorrentStateForcedDl, sourcePass, sourcePass, sourceIncomplete, sourceIncomplete},
		{qbt.TorrentStateUnknown, sourcePass, sourcePass, sourceIncomplete, sourceIncomplete},
		{qbt.TorrentStateError, sourceErrored, sourcePass, sourceIncomplete, sourceErrored},
		{qbt.TorrentStateMissingFiles, sourceErrored, sourcePass, sourceIncomplete, sourceErrored},
		{qbt.TorrentStateMoving, sourceMovingOrChecking, sourceMovingOrChecking, sourceMovingOrChecking, sourceMovingOrChecking},
		{qbt.TorrentStateCheckingDl, sourceMovingOrChecking, sourceMovingOrChecking, sourceMovingOrChecking, sourceMovingOrChecking},
		{qbt.TorrentStateCheckingUp, sourceMovingOrChecking, sourceMovingOrChecking, sourceMovingOrChecking, sourceMovingOrChecking},
		{qbt.TorrentStateCheckingResumeData, sourceMovingOrChecking, sourceMovingOrChecking, sourceMovingOrChecking, sourceMovingOrChecking},
	}
	for _, row := range states {
		off, on := sourceGate{}, sourceGate{recoverErrored: true}
		torrent := &qbt.Torrent{State: row.state, Progress: 1, Category: "tv"}
		assert.Equal(t, row.recoveryOff, off.check(torrent, sourceFilter{}), "%s, recovery off, empty filter", row.state)
		assert.Equal(t, row.recoveryOn, on.check(torrent, sourceFilter{}), "%s, recovery on, empty filter", row.state)
		assert.Equal(t, row.recoveryOff, off.check(torrent, keeper), "%s, recovery off, set filter", row.state)

		// The filter comes first, at any state.
		assert.Equal(t, sourceOutsideFilter, off.check(torrent, sourceFilter{excludeCategories: []string{"tv"}}), "%s, outside filter", row.state)

		torrent.Progress = 0.99
		assert.Equal(t, row.partialOff, off.check(torrent, sourceFilter{}), "%s, recovery off, incomplete", row.state)
		assert.Equal(t, row.partialOn, on.check(torrent, sourceFilter{}), "%s, recovery on, incomplete", row.state)
	}

	filters := []struct {
		name    string
		torrent qbt.Torrent
		filter  sourceFilter
		want    sourceAnswer
	}{
		{name: "empty filter passes every torrent", torrent: qbt.Torrent{Category: "movies", Tags: "cross-seed"}, want: sourcePass},
		{name: "exclude category drops a match", torrent: qbt.Torrent{Category: "cross-seed-link"}, filter: sourceFilter{excludeCategories: []string{"cross-seed-link"}}, want: sourceOutsideFilter},
		{name: "exclude category keeps another category", torrent: qbt.Torrent{Category: "movies"}, filter: sourceFilter{excludeCategories: []string{"cross-seed-link"}}, want: sourcePass},
		{name: "exclude tag drops a match", torrent: qbt.Torrent{Tags: "temp, keep"}, filter: sourceFilter{excludeTags: []string{"temp"}}, want: sourceOutsideFilter},
		{name: "exclude tag keeps other tags", torrent: qbt.Torrent{Tags: "keep"}, filter: sourceFilter{excludeTags: []string{"temp"}}, want: sourcePass},
		{name: "include category drops another category", torrent: qbt.Torrent{Category: "movies"}, filter: sourceFilter{categories: []string{"tv"}}, want: sourceOutsideFilter},
		{name: "include category keeps a match", torrent: qbt.Torrent{Category: "tv"}, filter: sourceFilter{categories: []string{"tv"}}, want: sourcePass},
		{name: "include tag keeps one match", torrent: qbt.Torrent{Tags: "other, keep"}, filter: sourceFilter{tags: []string{"keep", "best"}}, want: sourcePass},
		{name: "include tag drops other tags", torrent: qbt.Torrent{Tags: "other"}, filter: sourceFilter{tags: []string{"keep"}}, want: sourceOutsideFilter},
		{name: "tags are case-sensitive", torrent: qbt.Torrent{Tags: "KEEP"}, filter: sourceFilter{tags: []string{"keep"}}, want: sourceOutsideFilter},
		{name: "categories are case-sensitive", torrent: qbt.Torrent{Category: "TV"}, filter: sourceFilter{categories: []string{"tv"}}, want: sourceOutsideFilter},
		{name: "exclude category beats include category", torrent: qbt.Torrent{Category: "tv"}, filter: sourceFilter{categories: []string{"tv"}, excludeCategories: []string{"tv"}}, want: sourceOutsideFilter},
		{name: "exclude tag beats include tag", torrent: qbt.Torrent{Tags: "keep, temp"}, filter: sourceFilter{tags: []string{"keep"}, excludeTags: []string{"temp"}}, want: sourceOutsideFilter},
		{name: "category matches and tag does not", torrent: qbt.Torrent{Category: "tv", Tags: "other"}, filter: sourceFilter{categories: []string{"tv"}, tags: []string{"keep"}}, want: sourceOutsideFilter},
	}
	for _, row := range filters {
		row.torrent.Progress = 1
		assert.Equal(t, row.want, sourceGate{}.check(&row.torrent, row.filter), row.name)
	}
}

// turnStateSyncManager answers the per-torrent lookups at the turn of a torrent
// with a later state than the list of the queue build.
type turnStateSyncManager struct {
	*hashFilteringSyncManager
	atTurn map[string]qbt.Torrent
}

func (m *turnStateSyncManager) GetTorrents(ctx context.Context, instanceID int, opts qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	torrents, err := m.hashFilteringSyncManager.GetTorrents(ctx, instanceID, opts)
	if len(opts.Hashes) == 0 {
		return torrents, err
	}
	for i := range torrents {
		if later, ok := m.atTurn[torrents[i].Hash]; ok {
			torrents[i] = later
		}
	}
	return torrents, err
}

func requireNoSearchHistory(t *testing.T, svc *Service, instanceID int, hashes ...string) {
	t.Helper()
	for _, hash := range hashes {
		_, found, err := svc.automationStore.GetSearchHistory(t.Context(), instanceID, hash)
		require.NoError(t, err)
		require.False(t, found, "%s must get no search history", hash)
		indexers, err := svc.automationStore.GetIndexerSearchHistory(t.Context(), instanceID, hash)
		require.NoError(t, err)
		require.Empty(t, indexers, "%s must get no indexer history", hash)
	}
}

// regression: #3707
func TestSearchRunLoop_SkipsSourceThatCannotGiveFilesAtItsTurn(t *testing.T) {
	const name = "Artist - Album (2024) [WEB FLAC]"
	picked := qbt.Torrent{Hash: fmt.Sprintf("%040x", 1), Name: name, Progress: 1, Size: 1_000, AddedOn: 1, Tracker: "https://tracker.example.invalid/announce"}
	duplicate := qbt.Torrent{Hash: fmt.Sprintf("%040x", 2), Name: name, Progress: 1, Size: 1_000, AddedOn: 2, Tracker: "https://tracker.example.invalid/announce"}

	withState := func(state qbt.TorrentState, progress float64) qbt.Torrent {
		torrent := picked
		torrent.State, torrent.Progress = state, progress
		return torrent
	}
	tests := []struct {
		name    string
		atQueue qbt.Torrent
		atTurn  qbt.Torrent
		message string
	}{
		{"moving at queue build and at its turn", withState(qbt.TorrentStateMoving, 1), withState(qbt.TorrentStateMoving, 1), ErrSourceNotSettled.Error()},
		{"starts to move after the queue build", picked, withState(qbt.TorrentStateMoving, 1), ErrSourceNotSettled.Error()},
		{"checked below 100% at its turn", withState(qbt.TorrentStateCheckingUp, 0.4), withState(qbt.TorrentStateCheckingUp, 0.4), ErrSourceNotSettled.Error()},
		{"errors after the queue build", picked, withState(qbt.TorrentStateMissingFiles, 1), ErrSourceErrored.Error()},
		{"check ends below 100% before its turn", withState(qbt.TorrentStateCheckingDl, 0.4), withState(qbt.TorrentStateDownloading, 0.4), ErrTorrentNotComplete.Error()},
		{"becomes incomplete after the queue build", picked, withState(qbt.TorrentStateDownloading, 0.4), ErrTorrentNotComplete.Error()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := qbt.TorrentFiles{{Name: name + "/01 - Track.flac", Size: 1_000}}
			base := &hashFilteringSyncManager{}
			base.torrents = []qbt.Torrent{tt.atQueue, duplicate}
			base.filesByHash = map[string]qbt.TorrentFiles{picked.Hash: files, duplicate.Hash: files}
			svc, state := newSearchRunLoopFixture(t, "crossseed-source-gate-turn", base)
			svc.syncManager = &turnStateSyncManager{base, map[string]qbt.Torrent{picked.Hash: tt.atTurn}}

			runSearchLoopToCompletion(t, svc, state)
			svc.finalizeSearchRun(state, false)

			require.Equal(t, 1, state.run.Processed)
			require.Equal(t, 1, state.run.TorrentsSkipped)
			require.Zero(t, state.run.TorrentsFailed)
			require.Len(t, state.run.Results, 1)
			require.Equal(t, models.CrossSeedSearchResultStatusSkipped, state.run.Results[0].Status)
			require.Equal(t, tt.message, state.run.Results[0].Message)
			require.Equal(t, models.CrossSeedSearchRunStatusSuccess, state.run.Status)
			requireNoSearchHistory(t, svc, state.opts.InstanceID, picked.Hash, duplicate.Hash)
		})
	}
}

// regression: #3707
func TestProcessSearchCandidate_ContentAnalysisRefusalWritesNoHistory(t *testing.T) {
	tests := []struct {
		name    string
		state   qbt.TorrentState
		message string
	}{
		{"moving", qbt.TorrentStateMoving, ErrSourceNotSettled.Error()},
		{"errored", qbt.TorrentStateError, ErrSourceErrored.Error()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, env := newSearchHistoryTestEnv(t, "crossseed-source-gate-analysis", 2)
			instanceStore, err := models.NewInstanceStore(env.db, []byte("01234567890123456789012345678901"))
			require.NoError(t, err)

			torrent := qbt.Torrent{Hash: fmt.Sprintf("%040x", 3), Name: "Some.Movie.2019.1080p.BluRay.x264-GRP", Progress: 1, Size: 1_000, State: tt.state}
			base := &hashFilteringSyncManager{}
			base.torrents = []qbt.Torrent{torrent}
			svc := &Service{instanceStore: instanceStore, automationStore: env.store, syncManager: base, releaseCache: NewReleaseCache()}
			state := &searchRunState{
				run:                       &models.CrossSeedSearchRun{},
				opts:                      SearchRunOptions{InstanceID: env.instanceID},
				resolvedTorznabIndexerIDs: env.indexerIDs,
				// The stale indexers are the history that a refusal must not stamp.
				staleWork: map[string]candidateStaleWork{torrent.Hash: {indexerIDs: env.indexerIDs}},
			}

			delay, err := svc.processSearchCandidate(ctx, state, &torrent)

			require.NoError(t, err)
			require.False(t, delay)
			require.Equal(t, 1, state.run.TorrentsSkipped)
			require.Len(t, state.run.Results, 1)
			require.Equal(t, models.CrossSeedSearchResultStatusSkipped, state.run.Results[0].Status)
			require.Equal(t, tt.message, state.run.Results[0].Message)
			requireNoSearchHistory(t, svc, env.instanceID, torrent.Hash)
		})
	}
}

// laterLookupSyncManager answers the first per-torrent lookup with the list
// state and each later lookup with the state in later.
type laterLookupSyncManager struct {
	*hashFilteringSyncManager
	later   qbt.TorrentState
	lookups int
}

func (m *laterLookupSyncManager) GetTorrents(ctx context.Context, instanceID int, opts qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	torrents, err := m.hashFilteringSyncManager.GetTorrents(ctx, instanceID, opts)
	if len(opts.Hashes) > 0 {
		m.lookups++
		if m.lookups > 1 {
			for i := range torrents {
				torrents[i].State = m.later
			}
		}
	}
	return torrents, err
}

// A torrent can pass content analysis and move before the search source load.
// The stale indexer that the content filter excluded would get a cooldown stamp
// if the refusal came after the history writes.
//
// regression: #3707
func TestProcessSearchCandidate_SearchSourceLoadRefusalWritesNoHistory(t *testing.T) {
	ctx, env := newSearchHistoryTestEnv(t, "crossseed-source-gate-load", 2)
	instanceStore, err := models.NewInstanceStore(env.db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	torrent := qbt.Torrent{Hash: fmt.Sprintf("%040x", 10), Name: "Some.Movie.2019.1080p.BluRay.x264-GRP", Progress: 1, Size: 1_000}
	base := &hashFilteringSyncManager{}
	base.torrents = []qbt.Torrent{torrent}
	base.filesByHash = map[string]qbt.TorrentFiles{torrent.Hash: {{Name: torrent.Name + ".mkv", Size: 1_000}}}
	filterCache := ttlcache.New[string, *AsyncIndexerFilteringState]()
	filterCache.Set(asyncFilteringCacheKey(env.instanceID, torrent.Hash), &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		CapabilityIndexers:    env.indexerIDs,
		FilteredIndexers:      env.indexerIDs[:1],
		contentType:           "movie",
	}, ttlcache.DefaultTTL)
	svc := &Service{
		instanceStore:       instanceStore,
		automationStore:     env.store,
		syncManager:         &laterLookupSyncManager{hashFilteringSyncManager: base, later: qbt.TorrentStateMoving},
		asyncFilteringCache: filterCache,
		releaseCache:        NewReleaseCache(),
		stringNormalizer:    stringutils.NewDefaultNormalizer(),
	}
	state := &searchRunState{
		run:                       &models.CrossSeedSearchRun{},
		opts:                      SearchRunOptions{InstanceID: env.instanceID},
		resolvedTorznabIndexerIDs: env.indexerIDs,
		staleWork:                 map[string]candidateStaleWork{torrent.Hash: {indexerIDs: env.indexerIDs}},
	}

	_, err = svc.processSearchCandidate(ctx, state, &torrent)

	require.NoError(t, err)
	require.Equal(t, 1, state.run.TorrentsSkipped)
	require.Len(t, state.run.Results, 1)
	require.Equal(t, ErrSourceNotSettled.Error(), state.run.Results[0].Message)
	requireNoSearchHistory(t, svc, env.instanceID, torrent.Hash)
}

// regression: #3707
func TestSearchRunLoop_SourceThatSettledBeforeItsTurnGetsItsSearch(t *testing.T) {
	const name = "Artist - Album (2024) [WEB FLAC]"
	checking := qbt.Torrent{Hash: fmt.Sprintf("%040x", 4), Name: name, Progress: 0.4, Size: 1_000, State: qbt.TorrentStateCheckingUp, Tracker: "https://tracker.example.invalid/announce"}
	settled := checking
	settled.Progress, settled.State = 1, qbt.TorrentStateStalledUp

	base := &hashFilteringSyncManager{}
	base.torrents = []qbt.Torrent{checking}
	base.filesByHash = map[string]qbt.TorrentFiles{checking.Hash: {{Name: name + "/01 - Track.flac", Size: 1_000}}}
	svc, state := newSearchRunLoopFixture(t, "crossseed-source-gate-settled", base)
	svc.syncManager = &turnStateSyncManager{base, map[string]qbt.Torrent{checking.Hash: settled}}
	var lookups atomic.Int32
	findGazelleMatch = func(context.Context, *gazellemusic.Client, []byte, map[string]int64, int64) (*gazellemusic.Match, error) {
		lookups.Add(1)
		return nil, nil
	}

	runSearchLoopToCompletion(t, svc, state)

	require.Equal(t, 1, state.run.Processed)
	require.Positive(t, lookups.Load(), "the settled torrent must get its search")
	_, found, err := svc.automationStore.GetSearchHistory(t.Context(), state.opts.InstanceID, checking.Hash)
	require.NoError(t, err)
	require.True(t, found)
}

// regression: #3707
func TestRefreshSearchQueue_IncompleteCopyDoesNotHideCompleteCopy(t *testing.T) {
	const name = "Artist - Album (2024) [WEB FLAC]"
	older := qbt.Torrent{Hash: fmt.Sprintf("%040x", 5), Name: name, Progress: 0.5, Size: 1_000, AddedOn: 1, State: qbt.TorrentStateDownloading}
	newer := qbt.Torrent{Hash: fmt.Sprintf("%040x", 6), Name: name, Progress: 1, Size: 1_000, AddedOn: 2}
	base := &hashFilteringSyncManager{}
	base.torrents = []qbt.Torrent{older, newer}
	files := qbt.TorrentFiles{{Name: name + "/01 - Track.flac", Size: 1_000}}
	base.filesByHash = map[string]qbt.TorrentFiles{older.Hash: files, newer.Hash: files}
	svc, state := newSearchRunLoopFixture(t, "crossseed-source-gate-copies", base)

	require.NoError(t, svc.refreshSearchQueue(t.Context(), state))

	require.Len(t, state.queue, 1)
	require.Equal(t, newer.Hash, state.queue[0].Hash)
	require.Equal(t, 1, state.run.TotalTorrents)
}

// regression: #3707
func TestSearchDialogRefusesSourceThatCannotGiveFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		state    qbt.TorrentState
		progress float64
		recovery bool
		want     error
	}{
		{name: "moving", state: qbt.TorrentStateMoving, progress: 1, want: ErrSourceNotSettled},
		{name: "checked below 100%", state: qbt.TorrentStateCheckingDl, progress: 0.4, want: ErrSourceNotSettled},
		{name: "errored", state: qbt.TorrentStateError, progress: 1, want: ErrSourceErrored},
		{name: "missing files at any progress", state: qbt.TorrentStateMissingFiles, progress: 0.4, want: ErrSourceErrored},
		{name: "errored with recovery on", state: qbt.TorrentStateError, progress: 1, recovery: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			instance := &models.Instance{ID: 1, Name: "main"}
			torrent := qbt.Torrent{Hash: fmt.Sprintf("%040x", 8), Name: "Some.Movie.2019.1080p.BluRay.x264-GRP", Progress: tt.progress, State: tt.state}
			svc := &Service{
				instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
				syncManager: newFakeSyncManager(instance, []qbt.Torrent{torrent}, map[string]qbt.TorrentFiles{
					torrent.Hash: {{Name: torrent.Name + ".mkv", Size: 1_000}},
				}),
				releaseCache:                  NewReleaseCache(),
				recoverErroredTorrentsEnabled: tt.recovery,
			}

			_, analyzeErr := svc.AnalyzeTorrentForSearch(t.Context(), instance.ID, torrent.Hash)
			_, _, loadErr := svc.loadSearchSource(t.Context(), instance.ID, torrent.Hash)

			if tt.want == nil {
				require.NoError(t, analyzeErr)
				require.NoError(t, loadErr)
				return
			}
			require.EqualError(t, analyzeErr, tt.want.Error())
			require.EqualError(t, loadErr, tt.want.Error())
		})
	}
}

// regression: #3707
func TestExecuteCompletionSearch_StopsForErroredSource(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)

	const indexerID = 7
	instance := &models.Instance{ID: 1, Name: "main"}
	torrent := qbt.Torrent{Hash: fmt.Sprintf("%040x", 9), Name: "Some.Movie.2019.1080p.BluRay.x264-GRP", Progress: 1, State: qbt.TorrentStateMissingFiles}
	svc := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
		jackettService: newJackettServiceWithIndexers([]*models.TorznabIndexer{{
			ID: indexerID, Name: "Indexer", BaseURL: server.URL, Backend: models.TorznabBackendNative, TimeoutSeconds: 5, Enabled: true,
		}}),
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{torrent}, map[string]qbt.TorrentFiles{torrent.Hash: {{Name: torrent.Name + ".mkv", Size: 1_000}}}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}

	err := svc.executeCompletionSearch(jackett.WithSearchPriority(t.Context(), jackett.RateLimitPriorityCompletion), instance.ID, &torrent, models.DefaultCrossSeedAutomationSettings(), &models.InstanceCrossSeedCompletionSettings{
		InstanceID: instance.ID, Enabled: true, IndexerIDs: []int{indexerID},
	})

	require.ErrorIs(t, err, ErrSourceErrored)
	require.Zero(t, requests.Load(), "qui must search no indexer")
}
