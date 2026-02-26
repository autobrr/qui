package crossseed

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
)

func setupCompletionStoreForQueueTests(t *testing.T) *models.InstanceCrossSeedCompletionStore {
	t.Helper()

	db, err := sql.Open("sqlite", "file:completion_queue_tests?mode=memory&cache=shared")
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
			updated_at DATETIME NOT NULL
		);
	`)
	require.NoError(t, err)

	_, err = q.ExecContext(context.Background(), `
		INSERT OR REPLACE INTO instance_crossseed_completion_settings (
			instance_id, enabled, categories_json, tags_json,
			exclude_categories_json, exclude_tags_json, indexer_ids_json, updated_at
		) VALUES (1, 1, '[]', '[]', '[]', '[]', '[]', ?);
	`, time.Now().UTC())
	require.NoError(t, err)

	return models.NewInstanceCrossSeedCompletionStore(q)
}

func TestHandleTorrentCompletion_QueuesPerInstance(t *testing.T) {
	completionStore := setupCompletionStoreForQueueTests(t)

	firstHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	secondHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	invocationOrder := make([]string, 0, 2)
	var orderMu sync.Mutex
	var firstOnce sync.Once
	var secondOnce sync.Once

	svc := &Service{
		completionStore: completionStore,
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
			Hash:     firstHash,
			Name:     "first",
			Progress: 1.0,
		})
	}()

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first completion search did not start")
	}

	go func() {
		defer wg.Done()
		svc.HandleTorrentCompletion(context.Background(), 1, qbt.Torrent{
			Hash:     secondHash,
			Name:     "second",
			Progress: 1.0,
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
	case <-time.After(time.Second):
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

	started := time.Now()
	svc.HandleTorrentCompletion(context.Background(), 1, qbt.Torrent{
		Hash:     "cccccccccccccccccccccccccccccccccccccccc",
		Name:     "retry-me",
		Progress: 1.0,
	})

	assert.Equal(t, 2, attempts)
	assert.GreaterOrEqual(t, time.Since(started), 8*time.Millisecond)
}
