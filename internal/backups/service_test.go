package backups

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/autobrr/qui/internal/models"
)

func setupTestBackupDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", "file:queue_run_cleanup?mode=memory&cache=shared")
	require.NoError(t, err)

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	schema := []string{
		`CREATE TABLE IF NOT EXISTS instances (
		    id INTEGER PRIMARY KEY AUTOINCREMENT,
		    name TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS instance_backup_runs (
		    id INTEGER PRIMARY KEY AUTOINCREMENT,
		    instance_id INTEGER NOT NULL,
		    kind TEXT NOT NULL,
		    status TEXT NOT NULL DEFAULT 'pending',
		    requested_by TEXT NOT NULL DEFAULT 'system',
		    requested_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		    started_at TIMESTAMP,
		    completed_at TIMESTAMP,
		    archive_path TEXT,
		    manifest_path TEXT,
		    total_bytes INTEGER NOT NULL DEFAULT 0,
		    torrent_count INTEGER NOT NULL DEFAULT 0,
		    category_counts_json TEXT,
		    categories_json TEXT,
		    tags_json TEXT,
		    error_message TEXT,
		    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
		)`,
	}

	for _, stmt := range schema {
		_, err = db.Exec(stmt)
		require.NoError(t, err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return db
}

func TestQueueRunCleansPendingRunOnContextCancel(t *testing.T) {
	db := setupTestBackupDB(t)

	ctx := context.Background()
	result, err := db.ExecContext(ctx, "INSERT INTO instances (name) VALUES (?)", "test-instance")
	require.NoError(t, err)

	instanceID64, err := result.LastInsertId()
	require.NoError(t, err)
	instanceID := int(instanceID64)

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, Config{WorkerCount: 1})
	svc.jobs = make(chan job)
	svc.now = func() time.Time { return time.Unix(0, 0) }

	runCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)

	go func() {
		_, err := svc.QueueRun(runCtx, instanceID, models.BackupRunKindManual, "tester")
		errCh <- err
	}()

	var runID int64
	deadline := time.After(1 * time.Second)

	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for backup run to be created")
		default:
		}

		err := db.QueryRowContext(context.Background(), "SELECT id FROM instance_backup_runs LIMIT 1").Scan(&runID)
		if err == nil {
			break
		}
		if errors.Is(err, sql.ErrNoRows) {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		require.NoError(t, err)
	}

	cancel()

	var queueErr error
	select {
	case queueErr = <-errCh:
	case <-time.After(time.Second):
		t.Fatal("QueueRun did not return after context cancellation")
	}
	require.ErrorIs(t, queueErr, context.Canceled)

	checkCtx, checkCancel := context.WithTimeout(context.Background(), time.Second)
	defer checkCancel()
	var count int
	require.NoError(t, db.QueryRowContext(checkCtx, "SELECT COUNT(*) FROM instance_backup_runs WHERE id = ?", runID).Scan(&count))
	require.Equal(t, 0, count, "pending run should be removed once context is canceled")

	svc.inflightMu.Lock()
	_, exists := svc.inflight[instanceID]
	svc.inflightMu.Unlock()
	require.False(t, exists, "instance inflight marker should be cleared")
}
