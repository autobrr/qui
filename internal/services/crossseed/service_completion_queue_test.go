package crossseed

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func setupCompletionStoreForQueueTests(t *testing.T) *models.InstanceCrossSeedCompletionStore {
	t.Helper()

	dsn := fmt.Sprintf(
		"file:completion_queue_tests_%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()),
	)
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	q := &testQuerier{DB: db}

	_, err = q.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS instance_crossseed_completion_settings (
			instance_id INTEGER PRIMARY KEY,
			enabled INTEGER NOT NULL,
			categories_json TEXT NOT NULL,
			tags_json TEXT NOT NULL,
			exclude_categories_json TEXT NOT NULL,
			exclude_tags_json TEXT NOT NULL,
			indexer_ids_json TEXT NOT NULL,
			bypass_torznab_cache INTEGER NOT NULL DEFAULT 0,
			completion_delay_seconds INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME NOT NULL
		);
	`)
	require.NoError(t, err)

	_, err = q.ExecContext(context.Background(), `
		INSERT OR REPLACE INTO instance_crossseed_completion_settings (
			instance_id, enabled, categories_json, tags_json,
			exclude_categories_json, exclude_tags_json, indexer_ids_json, bypass_torznab_cache, updated_at
		) VALUES (1, 1, '[]', '[]', '[]', '[]', '[]', 0, ?);
	`, time.Now().UTC())
	require.NoError(t, err)

	return models.NewInstanceCrossSeedCompletionStore(q)
}

type completionPollingSyncMock struct {
	mu        sync.Mutex
	sequences map[string][]qbt.Torrent
	hits      map[string]int
	// lookups, when set, receives the hash of each lookup.
	lookups chan string
}

func newCompletionPollingSyncMock(sequences map[string][]qbt.Torrent) *completionPollingSyncMock {
	normalized := make(map[string][]qbt.Torrent, len(sequences))
	for hash, sequence := range sequences {
		normalized[normalizeHash(hash)] = sequence
	}

	return &completionPollingSyncMock{
		sequences: normalized,
		hits:      make(map[string]int),
	}
}

func (m *completionPollingSyncMock) GetTorrents(_ context.Context, _ int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	if len(filter.Hashes) == 0 {
		return nil, nil
	}

	hash := normalizeHash(filter.Hashes[0])
	if m.lookups != nil {
		m.lookups <- hash
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	sequence, ok := m.sequences[hash]
	if !ok || len(sequence) == 0 {
		return nil, nil
	}

	index := m.hits[hash]
	if index >= len(sequence) {
		index = len(sequence) - 1
	}
	m.hits[hash]++

	torrent := sequence[index]
	return []qbt.Torrent{torrent}, nil
}

func (m *completionPollingSyncMock) hitCount(hash string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.hits[normalizeHash(hash)]
}

func (m *completionPollingSyncMock) GetTorrentFilesBatch(context.Context, int, []string) (map[string]qbt.TorrentFiles, error) {
	return nil, nil
}

func (*completionPollingSyncMock) ExportTorrent(context.Context, int, string) ([]byte, string, string, error) {
	return nil, "", "", nil
}

func (*completionPollingSyncMock) HasTorrentByAnyHash(context.Context, int, []string) (*qbt.Torrent, bool, error) {
	return nil, false, nil
}

func (*completionPollingSyncMock) GetTorrentProperties(context.Context, int, string) (*qbt.TorrentProperties, error) {
	return &qbt.TorrentProperties{}, nil
}

func (*completionPollingSyncMock) GetAppPreferences(context.Context, int) (qbt.AppPreferences, error) {
	return qbt.AppPreferences{}, nil
}

func (*completionPollingSyncMock) AddTorrent(context.Context, int, []byte, map[string]string) (*qbt.TorrentAddResponse, error) {
	return nil, nil
}

func (*completionPollingSyncMock) BulkAction(context.Context, int, []string, string) error {
	return nil
}

func (*completionPollingSyncMock) GetCachedInstanceTorrents(context.Context, int) ([]internalqb.CrossInstanceTorrentView, error) {
	return nil, nil
}

func (*completionPollingSyncMock) ExtractDomainFromURL(string) string {
	return ""
}

func (*completionPollingSyncMock) GetQBittorrentSyncManager(context.Context, int) (*qbt.SyncManager, error) {
	return nil, nil
}

func (*completionPollingSyncMock) RenameTorrent(context.Context, int, string, string) error {
	return nil
}

func (*completionPollingSyncMock) RenameTorrentFile(context.Context, int, string, string, string) error {
	return nil
}

func (*completionPollingSyncMock) RenameTorrentFolder(context.Context, int, string, string, string) error {
	return nil
}

func (*completionPollingSyncMock) SetTags(context.Context, int, []string, string) error {
	return nil
}

func (*completionPollingSyncMock) GetCategories(context.Context, int) (map[string]qbt.Category, error) {
	return map[string]qbt.Category{}, nil
}

func (*completionPollingSyncMock) CreateCategory(context.Context, int, string, string) error {
	return nil
}

// awaitLookup waits until the fake looks up hash.
func awaitLookup(t *testing.T, lookups <-chan string, hash string) {
	t.Helper()
	for {
		select {
		case got := <-lookups:
			if got == hash {
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no lookup of %s", hash)
		}
	}
}

func setCompletionCheckingTimings(svc *Service, pollInterval time.Duration, timeout time.Duration) {
	svc.completionPollInterval = pollInterval
	svc.completionTimeout = timeout
}

func setCompletionCheckingRetryPolicy(svc *Service, retryDelay time.Duration, maxAttempts int) {
	svc.completionRetryDelay = retryDelay
	svc.completionMaxAttempts = maxAttempts
}

func TestHandleTorrentCompletion_QueuesPerInstance(t *testing.T) {
	completionStore := setupCompletionStoreForQueueTests(t)

	firstHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	secondHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	syncMock := newCompletionPollingSyncMock(map[string][]qbt.Torrent{
		firstHash: {{
			Hash:         firstHash,
			Name:         "first",
			Progress:     1.0,
			State:        qbt.TorrentStateUploading,
			CompletionOn: 123,
		}},
		secondHash: {{
			Hash:         secondHash,
			Name:         "second",
			Progress:     1.0,
			State:        qbt.TorrentStateUploading,
			CompletionOn: 124,
		}},
	})
	syncMock.lookups = make(chan string, 16)
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	invocationOrder := make([]string, 0, 2)
	var orderMu sync.Mutex
	var firstOnce sync.Once
	var secondOnce sync.Once

	svc := &Service{
		completionStore: completionStore,
		syncManager:     syncMock,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
		completionSearchInvoker: func(_ context.Context, _ int, torrent *qbt.Torrent, _ *models.CrossSeedAutomationSettings, _ *models.InstanceCrossSeedCompletionSettings) error {
			orderMu.Lock()
			invocationOrder = append(invocationOrder, torrent.Hash)
			orderMu.Unlock()

			switch torrent.Hash {
			case firstHash:
				firstOnce.Do(func() { close(firstStarted) })
				<-releaseFirst
			case secondHash:
				secondOnce.Do(func() { close(secondStarted) })
			}
			return nil
		},
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		svc.HandleTorrentCompletion(context.Background(), 1, qbt.Torrent{
			Hash:         firstHash,
			Name:         "first",
			Progress:     1.0,
			CompletionOn: 123,
		})
	}()

	select {
	case <-firstStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("first completion search did not start")
	}

	go func() {
		defer wg.Done()
		svc.HandleTorrentCompletion(context.Background(), 1, qbt.Torrent{
			Hash:         secondHash,
			Name:         "second",
			Progress:     1.0,
			CompletionOn: 124,
		})
	}()

	// The second torrent is ready, so the second search goes on to the lock.
	awaitLookup(t, syncMock.lookups, secondHash)
	select {
	case <-secondStarted:
		t.Fatal("second completion search started before first released")
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseFirst)
	wg.Wait()

	select {
	case <-secondStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("second completion search did not start after first completed")
	}

	orderMu.Lock()
	defer orderMu.Unlock()
	require.Equal(t, []string{firstHash, secondHash}, invocationOrder)
}

func TestHandleTorrentCompletion_ContinuesPollingWhileSearchIsSerialized(t *testing.T) {
	completionStore := setupCompletionStoreForQueueTests(t)

	firstHash := "abababababababababababababababababababab"
	secondHash := "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"
	syncMock := newCompletionPollingSyncMock(map[string][]qbt.Torrent{
		firstHash: {{
			Hash:         firstHash,
			Name:         "first",
			Progress:     1.0,
			State:        qbt.TorrentStateUploading,
			CompletionOn: 123,
		}},
		secondHash: {
			{
				Hash:         secondHash,
				Name:         "second",
				Progress:     0.42,
				State:        qbt.TorrentStateCheckingResumeData,
				CompletionOn: 124,
			},
			{
				Hash:         secondHash,
				Name:         "second",
				Progress:     1.0,
				State:        qbt.TorrentStateUploading,
				CompletionOn: 124,
			},
		},
	})

	syncMock.lookups = make(chan string, 16)
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var firstOnce sync.Once
	var secondOnce sync.Once

	svc := &Service{
		completionStore: completionStore,
		syncManager:     syncMock,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
		completionSearchInvoker: func(_ context.Context, _ int, torrent *qbt.Torrent, _ *models.CrossSeedAutomationSettings, _ *models.InstanceCrossSeedCompletionSettings) error {
			switch torrent.Hash {
			case firstHash:
				firstOnce.Do(func() { close(firstStarted) })
				<-releaseFirst
			case secondHash:
				secondOnce.Do(func() { close(secondStarted) })
			}
			return nil
		},
	}
	setCompletionCheckingTimings(svc, 5*time.Millisecond, 200*time.Millisecond)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		svc.HandleTorrentCompletion(context.Background(), 1, qbt.Torrent{
			Hash:         firstHash,
			Name:         "first",
			Progress:     1.0,
			CompletionOn: 123,
		})
	}()

	select {
	case <-firstStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("first completion search did not start")
	}

	go func() {
		defer wg.Done()
		svc.HandleTorrentCompletion(context.Background(), 1, qbt.Torrent{
			Hash:         secondHash,
			Name:         "second",
			Progress:     1.0,
			CompletionOn: 124,
		})
	}()

	// The second wait polls while the first search holds the lock: the first
	// poll sees checking, the second sees it ready.
	awaitLookup(t, syncMock.lookups, secondHash)
	awaitLookup(t, syncMock.lookups, secondHash)
	select {
	case <-secondStarted:
		t.Fatal("second completion search started before first released")
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseFirst)

	select {
	case <-secondStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("second completion search did not start after first completed")
	}

	wg.Wait()
}

func TestSnapshotCompletionWaits_CopiesSchedulingFields(t *testing.T) {
	lane := &completionLane{
		waits: make(map[string]*completionWaitState),
	}

	initialRetryAt := time.Now().Add(15 * time.Second)
	wait := &completionWaitState{
		done:    make(chan struct{}),
		retryAt: initialRetryAt,
	}
	lane.waits["abc"] = wait

	svc := &Service{}
	snapshot := svc.snapshotCompletionWaits(lane)

	lane.mu.Lock()
	updatedRetryAt := initialRetryAt.Add(30 * time.Second)
	wait.retryAt = updatedRetryAt
	lane.mu.Unlock()

	entry, ok := snapshot["abc"]
	require.True(t, ok)
	require.Same(t, wait, entry.state)
	require.True(t, entry.retryAt.Equal(initialRetryAt))
	require.False(t, entry.retryAt.Equal(updatedRetryAt))
}

func TestHandleTorrentCompletion_DoesNotRetryRateLimitError(t *testing.T) {
	completionStore := setupCompletionStoreForQueueTests(t)

	attempts := 0
	svc := &Service{
		completionStore: completionStore,
		syncManager: newCompletionPollingSyncMock(map[string][]qbt.Torrent{
			"cccccccccccccccccccccccccccccccccccccccc": {{
				Hash:         "cccccccccccccccccccccccccccccccccccccccc",
				Name:         "retry-me",
				Progress:     1.0,
				State:        qbt.TorrentStateUploading,
				CompletionOn: 125,
			}},
		}),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
		completionSearchInvoker: func(context.Context, int, *qbt.Torrent, *models.CrossSeedAutomationSettings, *models.InstanceCrossSeedCompletionSettings) error {
			attempts++
			return &jackett.RateLimitError{
				IndexerID:   1,
				IndexerName: "test",
				Scope:       "query",
				RetryAt:     time.Now().Add(10 * time.Millisecond),
			}
		},
	}

	svc.HandleTorrentCompletion(context.Background(), 1, qbt.Torrent{
		Hash:         "cccccccccccccccccccccccccccccccccccccccc",
		Name:         "retry-me",
		Progress:     1.0,
		CompletionOn: 125,
	})

	assert.Equal(t, 1, attempts)
}

func TestHandleTorrentCompletion_DefersWhileChecking(t *testing.T) {
	completionStore := setupCompletionStoreForQueueTests(t)

	hash := "dddddddddddddddddddddddddddddddddddddddd"
	syncMock := newCompletionPollingSyncMock(map[string][]qbt.Torrent{
		hash: {
			{
				Hash:         hash,
				Name:         "checking",
				Progress:     0.27,
				State:        qbt.TorrentStateCheckingResumeData,
				CompletionOn: 200,
			},
			{
				Hash:         hash,
				Name:         "checking",
				Progress:     1.0,
				State:        qbt.TorrentStateUploading,
				CompletionOn: 200,
			},
		},
	})

	invoked := make(chan qbt.Torrent, 1)
	svc := &Service{
		completionStore: completionStore,
		syncManager:     syncMock,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
		completionSearchInvoker: func(_ context.Context, _ int, torrent *qbt.Torrent, _ *models.CrossSeedAutomationSettings, _ *models.InstanceCrossSeedCompletionSettings) error {
			invoked <- *torrent
			return nil
		},
	}
	setCompletionCheckingTimings(svc, 5*time.Millisecond, 50*time.Millisecond)

	svc.HandleTorrentCompletion(context.Background(), 1, qbt.Torrent{
		Hash:         hash,
		Name:         "checking",
		Progress:     0.27,
		State:        qbt.TorrentStateCheckingResumeData,
		CompletionOn: 200,
	})

	select {
	case torrent := <-invoked:
		require.InDelta(t, 1.0, torrent.Progress, 0.0001)
		require.Equal(t, qbt.TorrentStateUploading, torrent.State)
	case <-time.After(time.Second):
		t.Fatal("completion search was not invoked after checking finished")
	}
}

func TestHandleTorrentCompletion_DefersWhileMoving(t *testing.T) {
	completionStore := setupCompletionStoreForQueueTests(t)

	hash := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	syncMock := newCompletionPollingSyncMock(map[string][]qbt.Torrent{
		hash: {
			{
				Hash:         hash,
				Name:         "moving",
				Progress:     1.0,
				State:        qbt.TorrentStateMoving,
				CompletionOn: 210,
			},
			{
				Hash:         hash,
				Name:         "moving",
				Progress:     1.0,
				State:        qbt.TorrentStateMoving,
				CompletionOn: 210,
			},
			{
				Hash:         hash,
				Name:         "moving",
				Progress:     1.0,
				State:        qbt.TorrentStateUploading,
				CompletionOn: 210,
			},
		},
	})

	invoked := make(chan qbt.Torrent, 1)
	svc := &Service{
		completionStore: completionStore,
		syncManager:     syncMock,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
		completionSearchInvoker: func(_ context.Context, _ int, torrent *qbt.Torrent, _ *models.CrossSeedAutomationSettings, _ *models.InstanceCrossSeedCompletionSettings) error {
			invoked <- *torrent
			return nil
		},
	}
	setCompletionCheckingTimings(svc, 5*time.Millisecond, 50*time.Millisecond)

	svc.HandleTorrentCompletion(context.Background(), 1, qbt.Torrent{
		Hash:         hash,
		Name:         "moving",
		Progress:     1.0,
		State:        qbt.TorrentStateMoving,
		CompletionOn: 210,
	})

	select {
	case torrent := <-invoked:
		require.Equal(t, qbt.TorrentStateUploading, torrent.State)
	case <-time.After(time.Second):
		t.Fatal("completion search was not invoked after moving finished")
	}

	require.GreaterOrEqual(t, syncMock.hitCount(hash), 3)
}

func TestHandleTorrentCompletion_RetriesAfterCheckingTimeout(t *testing.T) {
	completionStore := setupCompletionStoreForQueueTests(t)

	hash := "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"
	syncMock := newCompletionPollingSyncMock(map[string][]qbt.Torrent{
		hash: {
			{
				Hash:         hash,
				Name:         "checking-retry",
				Progress:     0.27,
				State:        qbt.TorrentStateCheckingResumeData,
				CompletionOn: 220,
			},
			{
				Hash:         hash,
				Name:         "checking-retry",
				Progress:     0.27,
				State:        qbt.TorrentStateCheckingResumeData,
				CompletionOn: 220,
			},
			{
				Hash:         hash,
				Name:         "checking-retry",
				Progress:     0.27,
				State:        qbt.TorrentStateCheckingResumeData,
				CompletionOn: 220,
			},
			{
				Hash:         hash,
				Name:         "checking-retry",
				Progress:     0.27,
				State:        qbt.TorrentStateCheckingResumeData,
				CompletionOn: 220,
			},
			{
				Hash:         hash,
				Name:         "checking-retry",
				Progress:     1.0,
				State:        qbt.TorrentStateUploading,
				CompletionOn: 220,
			},
		},
	})

	invoked := make(chan qbt.Torrent, 1)
	svc := &Service{
		completionStore: completionStore,
		syncManager:     syncMock,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
		completionSearchInvoker: func(_ context.Context, _ int, torrent *qbt.Torrent, _ *models.CrossSeedAutomationSettings, _ *models.InstanceCrossSeedCompletionSettings) error {
			invoked <- *torrent
			return nil
		},
	}
	setCompletionCheckingTimings(svc, 5*time.Millisecond, 12*time.Millisecond)
	setCompletionCheckingRetryPolicy(svc, 8*time.Millisecond, 3)

	svc.HandleTorrentCompletion(context.Background(), 1, qbt.Torrent{
		Hash:         hash,
		Name:         "checking-retry",
		Progress:     0.27,
		State:        qbt.TorrentStateCheckingResumeData,
		CompletionOn: 220,
	})

	select {
	case torrent := <-invoked:
		require.InDelta(t, 1.0, torrent.Progress, 0.0001)
		require.Equal(t, qbt.TorrentStateUploading, torrent.State)
	case <-time.After(time.Second):
		t.Fatal("completion search was not invoked after checking retry")
	}

	require.Equal(t, 6, syncMock.hitCount(hash))
}

func TestHandleTorrentCompletion_RechecksSkipConditionsAfterWaiting(t *testing.T) {
	completionStore := setupCompletionStoreForQueueTests(t)

	hash := "abababababababababababababababababababab"
	syncMock := newCompletionPollingSyncMock(map[string][]qbt.Torrent{
		hash: {
			{
				Hash:         hash,
				Name:         "checking-then-tagged",
				Progress:     0.27,
				State:        qbt.TorrentStateCheckingResumeData,
				CompletionOn: 300,
			},
			{
				Hash:         hash,
				Name:         "checking-then-tagged",
				Progress:     1.0,
				State:        qbt.TorrentStateUploading,
				CompletionOn: 300,
				Tags:         "cross-seed",
			},
		},
	})

	invoked := false
	svc := &Service{
		completionStore: completionStore,
		syncManager:     syncMock,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
		completionSearchInvoker: func(_ context.Context, _ int, _ *qbt.Torrent, _ *models.CrossSeedAutomationSettings, _ *models.InstanceCrossSeedCompletionSettings) error {
			invoked = true
			return nil
		},
	}
	setCompletionCheckingTimings(svc, 5*time.Millisecond, 50*time.Millisecond)

	svc.HandleTorrentCompletion(context.Background(), 1, qbt.Torrent{
		Hash:         hash,
		Name:         "checking-then-tagged",
		Progress:     0.27,
		State:        qbt.TorrentStateCheckingResumeData,
		CompletionOn: 300,
	})

	require.False(t, invoked, "completion search should be skipped after refreshed torrent gains cross-seed tag")
}

// completionLookupsPerAttempt counts the lookups of a torrent that stays
// checking for one attempt: one at the start and one at each poll.
const completionLookupsPerAttempt = int(defaultCompletionCheckingTimeout/defaultCompletionCheckingPollInterval) + 1

func TestWaitForCompletionTorrentReady_ReturnsNotCompleteAfterChecking(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hash := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
		syncMock := newCompletionPollingSyncMock(map[string][]qbt.Torrent{
			hash: {
				{
					Hash:     hash,
					Name:     "partial",
					Progress: 0.27,
					State:    qbt.TorrentStateCheckingResumeData,
				},
				{
					Hash:     hash,
					Name:     "partial",
					Progress: 0.27,
					State:    qbt.TorrentStatePausedUp,
				},
			},
		})
		svc := &Service{syncManager: syncMock}

		_, err := svc.waitForCompletionTorrentReady(t.Context(), 1, qbt.Torrent{
			Hash: hash,
			Name: "partial",
		})
		require.ErrorIs(t, err, ErrTorrentNotComplete)
		require.Contains(t, err.Error(), "progress 0.27")
		require.Equal(t, 2, syncMock.hitCount(hash))
	})
}

func TestWaitForCompletionTorrentReady_TimesOutWhileChecking(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hash := "ffffffffffffffffffffffffffffffffffffffff"
		syncMock := newCompletionPollingSyncMock(map[string][]qbt.Torrent{
			hash: {{
				Hash:     hash,
				Name:     "stuck-checking",
				Progress: 0.27,
				State:    qbt.TorrentStateCheckingResumeData,
			}},
		})
		svc := &Service{syncManager: syncMock, completionMaxAttempts: 1}

		start := time.Now()
		_, err := svc.waitForCompletionTorrentReady(t.Context(), 1, qbt.Torrent{
			Hash: hash,
			Name: "stuck-checking",
		})
		require.EqualError(t, err, "completion torrent stuck-checking still checking after 5m0s")
		require.Equal(t, defaultCompletionCheckingTimeout, time.Since(start))
		require.Equal(t, completionLookupsPerAttempt, syncMock.hitCount(hash))
	})
}

func TestWaitForCompletionTorrentReady_DeduplicatesConcurrentWaiters(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hash := "9999999999999999999999999999999999999999"
		syncMock := newCompletionPollingSyncMock(map[string][]qbt.Torrent{
			hash: {
				{
					Hash:     hash,
					Name:     "shared-wait",
					Progress: 0.27,
					State:    qbt.TorrentStateCheckingResumeData,
				},
				{
					Hash:     hash,
					Name:     "shared-wait",
					Progress: 1.0,
					State:    qbt.TorrentStateUploading,
				},
			},
		})
		svc := &Service{syncManager: syncMock}

		start := make(chan struct{})
		errs := make(chan error, 2)

		for range 2 {
			go func() {
				<-start
				_, err := svc.waitForCompletionTorrentReady(t.Context(), 1, qbt.Torrent{
					Hash: hash,
					Name: "shared-wait",
				})
				errs <- err
			}()
		}

		close(start)

		for range 2 {
			require.NoError(t, <-errs)
		}

		require.Equal(t, 2, syncMock.hitCount(hash))
	})
}

func TestWaitForCompletionTorrentReady_TimesOutAfterCheckingRetries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hash := "1212121212121212121212121212121212121212"
		syncMock := newCompletionPollingSyncMock(map[string][]qbt.Torrent{
			hash: {{
				Hash:     hash,
				Name:     "retry-timeout",
				Progress: 0.27,
				State:    qbt.TorrentStateCheckingResumeData,
			}},
		})
		svc := &Service{syncManager: syncMock}

		start := time.Now()
		_, err := svc.waitForCompletionTorrentReady(t.Context(), 1, qbt.Torrent{
			Hash: hash,
			Name: "retry-timeout",
		})
		require.EqualError(t, err, "completion torrent retry-timeout still checking after 5m0s")
		require.Equal(t, 3*defaultCompletionCheckingTimeout+2*defaultCompletionCheckingRetryDelay, time.Since(start))
		require.Equal(t, 3*completionLookupsPerAttempt, syncMock.hitCount(hash))
	})
}

func TestWaitForCompletionTorrentReady_DeduplicatesConcurrentWaitersDuringRetryBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hash := "3434343434343434343434343434343434343434"
		checking := qbt.Torrent{
			Hash:     hash,
			Name:     "shared-retry-wait",
			Progress: 0.27,
			State:    qbt.TorrentStateCheckingResumeData,
		}
		ready := checking
		ready.Progress = 1.0
		ready.State = qbt.TorrentStateUploading
		// The first attempt times out, and the torrent is ready at the first
		// lookup after the retry delay.
		syncMock := newCompletionPollingSyncMock(map[string][]qbt.Torrent{
			hash: append(slices.Repeat([]qbt.Torrent{checking}, completionLookupsPerAttempt), ready),
		})
		svc := &Service{syncManager: syncMock}

		start := make(chan struct{})
		errs := make(chan error, 2)

		for range 2 {
			go func() {
				<-start
				_, err := svc.waitForCompletionTorrentReady(t.Context(), 1, qbt.Torrent{
					Hash: hash,
					Name: "shared-retry-wait",
				})
				errs <- err
			}()
		}

		close(start)

		for range 2 {
			require.NoError(t, <-errs)
		}

		require.Equal(t, completionLookupsPerAttempt+1, syncMock.hitCount(hash))
	})
}

type countingCompletionQuerier struct {
	dbinterface.Querier
	settingsReads atomic.Int64
}

func (q *countingCompletionQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if strings.Contains(query, "FROM instance_crossseed_completion_settings") {
		q.settingsReads.Add(1)
	}
	return q.Querier.QueryRowContext(ctx, query, args...)
}

func TestHandleTorrentCompletion_BurstReadsSettingsOncePerInstance(t *testing.T) {
	q := &countingCompletionQuerier{Querier: testdb.NewMigratedSQLite(t, "completion-burst")}
	svc := &Service{completionStore: models.NewInstanceCrossSeedCompletionStore(q)}

	var wg sync.WaitGroup
	for i := range 10_000 {
		wg.Go(func() {
			svc.HandleTorrentCompletion(t.Context(), 1+i%2, qbt.Torrent{
				Hash:     fmt.Sprintf("%040x", i),
				Progress: 1.0,
			})
		})
	}
	wg.Wait()

	assert.Equal(t, int64(2), q.settingsReads.Load())
}
