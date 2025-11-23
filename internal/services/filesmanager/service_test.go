package filesmanager

import (
	"context"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
)

func TestCacheFilesAndGetCachedFiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	// Seed instance row to satisfy foreign key constraints for cache writes
	var (
		instanceNameID int64
		hostID         int64
		usernameID     int64
	)

	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "instance-name").Scan(&instanceNameID))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "instance-host").Scan(&hostID))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "instance-username").Scan(&usernameID))

	_, err = db.ExecContext(ctx, "INSERT INTO instances (id, name_id, host_id, username_id, password_encrypted) VALUES (?, ?, ?, ?, ?)", 1, instanceNameID, hostID, usernameID, "enc")
	require.NoError(t, err)

	svc := NewService(db)

	files := qbt.TorrentFiles{
		{
			Index:      0,
			Name:       "example.mkv",
			Size:       1 << 20,
			Progress:   0.5,
			Priority:   1,
			PieceRange: []int{0, 1},
		},
	}

	require.NoError(t, svc.CacheFiles(ctx, 1, "hash", files))

	cached, err := svc.GetCachedFiles(ctx, 1, "hash")
	require.NoError(t, err)
	require.NotNil(t, cached, "cache should be available")
	require.Len(t, cached, 1)
	require.Equal(t, "example.mkv", cached[0].Name)
}
