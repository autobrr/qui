package crossseed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/jackett"
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
	delay     time.Duration
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

	if m.delay > 0 {
		time.Sleep(m.delay)
	}

	hash := normalizeHash(filter.Hashes[0])

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

func (*completionPollingSyncMock) AddTorrent(context.Context, int, []byte, map[string]string) error {
	return nil
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

func setCompletionCheckingTimings(svc *Service, pollInterval time.Duration, timeout time.Duration) {
	svc.completionPollInterval = pollInterval
	svc.completionTimeout = timeout
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

	select {
	case <-secondStarted:
		t.Fatal("second completion search started before first released")
	case <-time.After(50 * time.Millisecond):
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

func TestHandleTorrentCompletion_RetriesOnRateLimitError(t *testing.T) {
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
			if attempts == 1 {
				return &jackett.RateLimitWaitError{
					IndexerID:   1,
					IndexerName: "test",
					Wait:        10 * time.Millisecond,
					MaxWait:     30 * time.Second,
					Priority:    jackett.RateLimitPriorityCompletion,
				}
			}
			return nil
		},
	}

	svc.HandleTorrentCompletion(context.Background(), 1, qbt.Torrent{
		Hash:         "cccccccccccccccccccccccccccccccccccccccc",
		Name:         "retry-me",
		Progress:     1.0,
		CompletionOn: 125,
	})

	assert.Equal(t, 2, attempts)
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

	invoked := make(chan struct{}, 1)
	svc := &Service{
		completionStore: completionStore,
		syncManager:     syncMock,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
		completionSearchInvoker: func(_ context.Context, _ int, _ *qbt.Torrent, _ *models.CrossSeedAutomationSettings, _ *models.InstanceCrossSeedCompletionSettings) error {
			invoked <- struct{}{}
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

	select {
	case <-invoked:
		t.Fatal("completion search should be skipped after refreshed torrent gains cross-seed tag")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestWaitForCompletionTorrentReady_ReturnsNotCompleteAfterChecking(t *testing.T) {
	hash := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	svc := &Service{
		syncManager: newCompletionPollingSyncMock(map[string][]qbt.Torrent{
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
		}),
	}
	setCompletionCheckingTimings(svc, 5*time.Millisecond, 50*time.Millisecond)

	_, err := svc.waitForCompletionTorrentReady(context.Background(), 1, qbt.Torrent{
		Hash: hash,
		Name: "partial",
	})
	require.ErrorIs(t, err, ErrTorrentNotComplete)
	require.Contains(t, err.Error(), "progress 0.27")
}

func TestWaitForCompletionTorrentReady_TimesOutWhileChecking(t *testing.T) {
	hash := "ffffffffffffffffffffffffffffffffffffffff"
	svc := &Service{
		syncManager: newCompletionPollingSyncMock(map[string][]qbt.Torrent{
			hash: {{
				Hash:     hash,
				Name:     "stuck-checking",
				Progress: 0.27,
				State:    qbt.TorrentStateCheckingResumeData,
			}},
		}),
	}
	setCompletionCheckingTimings(svc, 5*time.Millisecond, 20*time.Millisecond)

	_, err := svc.waitForCompletionTorrentReady(context.Background(), 1, qbt.Torrent{
		Hash: hash,
		Name: "stuck-checking",
	})
	require.EqualError(t, err, "completion torrent stuck-checking still checking after 20ms")
}

func TestWaitForCompletionTorrentReady_DeduplicatesConcurrentWaiters(t *testing.T) {
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
	syncMock.delay = 2 * time.Millisecond

	svc := &Service{
		syncManager: syncMock,
	}
	setCompletionCheckingTimings(svc, 5*time.Millisecond, 50*time.Millisecond)

	start := make(chan struct{})
	errs := make(chan error, 2)

	for range 2 {
		go func() {
			<-start
			_, err := svc.waitForCompletionTorrentReady(context.Background(), 1, qbt.Torrent{
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
}

func TestCompletionRetryDelay_FallbackRateLimitMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "rate-limited wording from jackett cooldown path",
			err:  errors.New("all indexers are currently rate-limited. 2 indexer(s) in cooldown"),
			want: true,
		},
		{
			name: "cooldown wording",
			err:  errors.New("skipping request due to cooldown"),
			want: true,
		},
		{
			name: "prowlarr query limit wording",
			err:  errors.New("user configurable indexer query limit of 10 in last 1 hour(s) reached"),
			want: true,
		},
		{
			name: "prowlarr grab limit wording",
			err:  errors.New("user configurable indexer grab limit of 10 in last 1 hour(s) reached"),
			want: true,
		},
		{
			name: "torznab request limit wording",
			err:  errors.New("Request limit reached"),
			want: true,
		},
		{
			name: "non rate limit error",
			err:  errors.New("network timeout"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delay, retry := completionRetryDelay(tt.err)
			assert.Equal(t, tt.want, retry)
			if tt.want {
				assert.Equal(t, defaultCompletionRetryDelay, delay)
			} else {
				assert.Zero(t, delay)
			}
		})
	}
}
