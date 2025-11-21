package crossseed

import (
	"context"
	"strings"
	"sync"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestContextWithTorrentFileStashRoundTrip(t *testing.T) {
	baseCtx := context.Background()
	stash := newTorrentFileStash()
	stash.Set("abc123", qbt.TorrentFiles{{Name: "movie.mkv", Size: 123}})

	ctx := contextWithTorrentFileStash(baseCtx, stash)
	require.NotNil(t, ctx)

	recovered := torrentFileStashFromContext(ctx)
	require.NotNil(t, recovered)
	assert.Same(t, stash, recovered)
	files, ok := recovered.Get("abc123")
	require.True(t, ok)
	assert.Len(t, files, 1)
}

func TestEnsureTorrentFileStashReusesExisting(t *testing.T) {
	ctx := context.Background()

	ctxWithStash, stash := ensureTorrentFileStash(ctx)
	require.NotNil(t, stash)

	stashKey := "abc"
	stash.Set(stashKey, qbt.TorrentFiles{{Name: "keep.me", Size: 1}})

	ctxWithStashAgain, reused := ensureTorrentFileStash(ctxWithStash)
	require.NotNil(t, reused)

	assert.Equal(t, ctxWithStash, ctxWithStashAgain)
	assert.Same(t, stash, reused)
	files, ok := reused.Get(stashKey)
	require.True(t, ok)
	assert.Len(t, files, 1)
}

func TestServiceGetTorrentFilesFromStashUsesContextCache(t *testing.T) {
	hash := "abc123"
	instance := &models.Instance{ID: 1, Name: "primary"}
	torrents := []qbt.Torrent{
		{Hash: hash, Name: "Example.Torrent", Progress: 1.0},
	}
	files := map[string]qbt.TorrentFiles{
		hash: {
			{Name: "Example.Torrent.mkv", Size: 5 << 30},
		},
	}

	base := newFakeSyncManager(instance, torrents, files)
	counting := newCountingSyncManager(base)

	svc := &Service{syncManager: counting}

	ctx, _ := ensureTorrentFileStash(context.Background())

	first, err := svc.getTorrentFilesFromStash(ctx, instance.ID, hash)
	require.NoError(t, err)
	require.Len(t, first, 1)
	assert.Equal(t, 1, counting.callCount("abc123"))

	second, err := svc.getTorrentFilesFromStash(ctx, instance.ID, strings.ToUpper(hash))
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.Equal(t, counting.callCount("abc123"), 1)

	_, err = svc.getTorrentFilesFromStash(context.Background(), instance.ID, hash)
	require.NoError(t, err)
	assert.Equal(t, counting.callCount("abc123"), 2)
}

// countingSyncManager wraps fakeSyncManager to track GetTorrentFiles invocations per hash.
type countingSyncManager struct {
	*fakeSyncManager
	mu    sync.Mutex
	calls map[string]int
}

func newCountingSyncManager(base *fakeSyncManager) *countingSyncManager {
	return &countingSyncManager{
		fakeSyncManager: base,
		calls:           make(map[string]int),
	}
}

func (c *countingSyncManager) GetTorrentFiles(ctx context.Context, instanceID int, hash string) (*qbt.TorrentFiles, error) {
	normalized := strings.ToLower(strings.TrimSpace(hash))
	c.mu.Lock()
	c.calls[normalized]++
	c.mu.Unlock()
	return c.fakeSyncManager.GetTorrentFiles(ctx, instanceID, hash)
}

func (c *countingSyncManager) callCount(hash string) int {
	normalized := strings.ToLower(strings.TrimSpace(hash))
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[normalized]
}
