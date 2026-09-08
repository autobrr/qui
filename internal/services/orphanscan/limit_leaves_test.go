package orphanscan

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// TestExecuteScan_LimitTakesRemovableLeavesFirst covers a limit smaller than the
// number of abandoned directories. Sorting parents first would keep a directory
// that cannot be removed while its child is still there, and every later scan
// would re-select the same parent, so the tree could never drain.
func TestExecuteScan_LimitTakesRemovableLeavesFirst(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "torrents")
	a := filepath.Join(root, "a")
	b := filepath.Join(a, "b")
	seeded := filepath.Join(root, "seeded")

	require.NoError(t, os.MkdirAll(b, 0o750))
	require.NoError(t, os.MkdirAll(seeded, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(seeded, "owned.mkv"), []byte("x"), 0o600))

	db := testdb.NewMigratedSQLite(t, "orphanscan-limit-leaves")
	is, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	_, err = is.Create(t.Context(), "t", "http://127.0.0.1:8080", "u", "p", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewOrphanScanStore(db)
	svc := NewService(DefaultConfig(), nil, store, nil, nil, fsops.NewPool(stubInstanceGetter{}, local.NewBackend()))
	svc.getClientProvider = func(context.Context, int) (healthChecker, error) {
		return stubHealthChecker{healthy: true, lastSync: time.Now().Add(-time.Minute)}, nil
	}
	svc.listInstancesProvider = func(context.Context) ([]*models.Instance, error) {
		return []*models.Instance{{ID: 1, Name: "t", IsActive: true, HasLocalFilesystemAccess: true}}, nil
	}
	svc.getAllTorrentsProvider = func(context.Context, int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{{Hash: "o", SavePath: seeded, State: qbt.TorrentStatePausedUp}}, nil
	}
	svc.getTorrentFilesBatchProvider = func(context.Context, int, []string) (map[string]qbt.TorrentFiles, error) {
		return map[string]qbt.TorrentFiles{"o": {{Name: "owned.mkv", Size: 1}}}, nil
	}
	svc.getAppPreferencesProvider = func(context.Context, int) (qbt.AppPreferences, error) {
		return qbt.AppPreferences{SavePath: root}, nil
	}
	svc.subcategoriesEnabledProvider = func(_ context.Context, _ int) (bool, error) { return false, nil }
	svc.getCategoriesProvider = func(context.Context, int) (map[string]qbt.Category, error) {
		return map[string]qbt.Category{}, nil
	}

	// MaxFilesPerRun = 1 is the reported trigger.
	_, err = store.UpsertSettings(t.Context(), &models.OrphanScanSettings{
		InstanceID: 1, GracePeriodMinutes: 0, IgnorePaths: []string{},
		ScanIntervalHours: 24, PreviewSort: "size_desc", MaxFilesPerRun: 1,
		AutoCleanupMaxFiles: 100, ScanDefaultSavePath: true, DeleteAbandonedDirs: true,
	})
	require.NoError(t, err)

	// The deepest directory has to be taken first, then its parent on the next
	// run, exactly as the documentation promises.
	runLeaf := runCycle(t, svc, store)
	require.Equal(t, []string{b}, runLeaf, "the first run must take the leaf, not the parent that cannot be removed")
	require.NoDirExists(t, b)
	require.DirExists(t, a)

	runParent := runCycle(t, svc, store)
	require.Equal(t, []string{a}, runParent, "the parent becomes removable once the leaf is gone")
	require.NoDirExists(t, a)
}

// runCycle scans, returns what the run previewed, and confirms the deletion.
func runCycle(t *testing.T, svc *Service, store *models.OrphanScanStore) []string {
	t.Helper()

	runID, err := store.CreateRunIfNoActive(t.Context(), 1, "manual")
	require.NoError(t, err)
	svc.executeScan(context.Background(), 1, runID)

	files, err := store.GetFilesForDeletion(t.Context(), runID)
	require.NoError(t, err)
	previewed := make([]string, 0, len(files))
	for _, f := range files {
		previewed = append(previewed, f.FilePath)
	}

	svc.executeDeletion(context.Background(), 1, runID)
	return previewed
}
