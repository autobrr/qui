// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/fsops/remote"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/sshpool"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

type faultyLinkBackend struct {
	fsops.Backend
	otherDisk   []string
	lost        []string
	lostLink    []string
	denied      []string
	deniedLink  []string
	crossDevice []string
	createErr   error
	removeErr   error
	reflink     bool
}

func closedRemote() fsops.Backend {
	pool := sshpool.NewPool(sshpool.NewDialer(nil))
	pool.Close()
	return remote.New(pool, &models.Instance{ID: 1})
}

func deniedError(op, p string) error {
	return &fs.PathError{Op: op, Path: p, Err: fs.ErrPermission}
}

func pathUnder(prefixes []string, p string) bool {
	return slices.ContainsFunc(prefixes, func(prefix string) bool {
		return p == prefix || strings.HasPrefix(p, prefix+string(filepath.Separator))
	})
}

func (b *faultyLinkBackend) SameFilesystem(ctx context.Context, p1, p2 string) (bool, error) {
	if pathUnder(b.lost, p2) {
		return closedRemote().SameFilesystem(ctx, p1, p2)
	}
	if pathUnder(b.denied, p2) {
		return false, deniedError("statvfs", p2)
	}
	if _, err := b.Backend.SameFilesystem(ctx, p1, p2); err != nil {
		return false, err
	}
	return pathUnder(b.otherDisk, p1) == pathUnder(b.otherDisk, p2), nil
}

func (b *faultyLinkBackend) HardlinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	switch {
	case pathUnder(b.lostLink, plan.RootDir):
		return closedRemote().HardlinkTree(ctx, plan)
	case pathUnder(b.deniedLink, plan.RootDir):
		return nil, deniedError("link", plan.RootDir)
	case pathUnder(b.crossDevice, plan.RootDir):
		return nil, crossDeviceLinkError(plan.RootDir)
	}
	if b.createErr != nil {
		return nil, b.createErr
	}
	return b.Backend.HardlinkTree(ctx, plan)
}

func (b *faultyLinkBackend) ReflinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	return b.HardlinkTree(ctx, plan)
}

func (b *faultyLinkBackend) Remove(ctx context.Context, p string, opts fsops.RemoveOptions) error {
	if b.removeErr != nil {
		return b.removeErr
	}
	return b.Backend.Remove(ctx, p, opts)
}

func (b *faultyLinkBackend) SupportsReflink(context.Context, string) (bool, string, error) {
	return b.reflink, "no clones here", nil
}

type linkFailureEnv struct {
	downloads    string
	linksA       string
	linksB       string
	backend      *faultyLinkBackend
	files        qbt.TorrentFiles
	matchedFiles qbt.TorrentFiles
}

func TestProcessLinkMode_FailureKindRules(t *testing.T) {
	t.Parallel()

	movie := qbt.TorrentFiles{{Name: "Movie/movie.mkv", Size: 5}}
	tests := []struct {
		name            string
		mode            string
		fallbackOff     bool
		setup           func(t *testing.T, e *linkFailureEnv)
		wantFallback    bool
		wantFullRecheck bool
		wantMessage     string
		wantPartialTree bool
	}{
		{
			name: "no base dir fits",
			mode: "hardlink",
			setup: func(_ *testing.T, e *linkFailureEnv) {
				e.backend.otherDisk = []string{e.linksA, e.linksB}
			},
			wantFallback: true, wantFullRecheck: true,
		},
		{
			name:        "no base dir fits without fallback",
			mode:        "hardlink",
			fallbackOff: true,
			setup: func(_ *testing.T, e *linkFailureEnv) {
				e.backend.otherDisk = []string{e.linksA, e.linksB}
			},
			wantMessage: "No suitable base directory: ",
		},
		{
			name: "sources on two disks",
			mode: "reflink",
			setup: func(t *testing.T, e *linkFailureEnv) {
				extras := filepath.Join(e.downloads, "Movie", "Extras")
				require.NoError(t, os.MkdirAll(extras, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(extras, "extra.mkv"), []byte("extra12"), 0o600))
				e.files = qbt.TorrentFiles{{Name: "Movie/movie.mkv", Size: 5}, {Name: "Movie/Extras/extra.mkv", Size: 7}}
				e.matchedFiles = e.files
				e.backend.otherDisk = []string{extras}
			},
			wantFallback: true, wantFullRecheck: true,
		},
		{
			name:         "link kind not supported",
			mode:         "reflink",
			setup:        func(_ *testing.T, e *linkFailureEnv) { e.backend.reflink = false },
			wantFallback: true, wantFullRecheck: true,
		},
		{
			name: "invalid plan",
			mode: "hardlink",
			setup: func(_ *testing.T, e *linkFailureEnv) {
				e.files = qbt.TorrentFiles{{Name: "../Movie/movie.mkv", Size: 5}}
			},
			wantFallback: true,
		},
		{
			name: "invalid plan",
			mode: "reflink",
			setup: func(_ *testing.T, e *linkFailureEnv) {
				e.files = qbt.TorrentFiles{{Name: "../Movie/movie.mkv", Size: 5}}
			},
			wantMessage: "Failed to build reflink plan: ",
		},
		{
			name: "cross-device in every base dir",
			mode: "hardlink",
			setup: func(_ *testing.T, e *linkFailureEnv) {
				e.backend.crossDevice = []string{e.linksA, e.linksB}
			},
			wantFallback: true, wantFullRecheck: true,
		},
		{
			name: "cross-device in every base dir",
			mode: "reflink",
			setup: func(_ *testing.T, e *linkFailureEnv) {
				e.backend.crossDevice = []string{e.linksA, e.linksB}
			},
			wantMessage: "Failed to create reflink tree: ",
		},
		{
			name:         "another create error",
			mode:         "hardlink",
			setup:        func(_ *testing.T, e *linkFailureEnv) { e.backend.createErr = errors.New("disk full") },
			wantFallback: true, wantFullRecheck: true,
		},
		{
			name:        "another create error",
			mode:        "reflink",
			setup:       func(_ *testing.T, e *linkFailureEnv) { e.backend.createErr = errors.New("disk full") },
			wantMessage: "Failed to create reflink tree: disk full",
		},
		{
			name: "cleanup of the failed tree also fails",
			mode: "reflink",
			setup: func(_ *testing.T, e *linkFailureEnv) {
				e.backend.createErr = fmt.Errorf("disk full (%w: %w)", hardlinktree.ErrRollbackFailed, errors.New("busy"))
			},
			wantPartialTree: true,
		},
		{
			name: "target exists",
			mode: "hardlink",
			setup: func(t *testing.T, e *linkFailureEnv) {
				require.NoError(t, os.MkdirAll(filepath.Join(e.linksA, "Movie"), 0o755))
				stranger := filepath.Join(e.linksA, "TorrentName--"+hash123[:8], "Movie", "movie.mkv")
				require.NoError(t, os.MkdirAll(filepath.Dir(stranger), 0o755))
				require.NoError(t, os.WriteFile(stranger, []byte("stranger"), 0o600))
			},
			wantMessage: "Failed to create hardlink tree: ",
		},
		{
			name: "target exists after a lost connection on the other base dir",
			mode: "hardlink",
			setup: func(t *testing.T, e *linkFailureEnv) {
				e.backend.lost = []string{e.linksB}
				require.NoError(t, os.MkdirAll(filepath.Join(e.linksA, "Movie"), 0o755))
				stranger := filepath.Join(e.linksA, "TorrentName--"+hash123[:8], "Movie", "movie.mkv")
				require.NoError(t, os.MkdirAll(filepath.Dir(stranger), 0o755))
				require.NoError(t, os.WriteFile(stranger, []byte("stranger"), 0o600))
			},
			wantMessage: "Failed to create hardlink tree: ",
		},
		{
			name:         "denied at the check",
			mode:         "hardlink",
			setup:        func(_ *testing.T, e *linkFailureEnv) { e.backend.denied = []string{e.linksA, e.linksB} },
			wantFallback: true, wantFullRecheck: true,
		},
		{
			name:         "denied at the link",
			mode:         "hardlink",
			setup:        func(_ *testing.T, e *linkFailureEnv) { e.backend.deniedLink = []string{e.linksA, e.linksB} },
			wantFallback: true, wantFullRecheck: true,
		},
		{
			name:        "lost connection at the check",
			mode:        "hardlink",
			setup:       func(_ *testing.T, e *linkFailureEnv) { e.backend.lost = []string{e.linksA, e.linksB} },
			wantMessage: "connection to the filesystem backend was lost: ssh pool is closed",
		},
		{
			name:        "lost connection during the create",
			mode:        "hardlink",
			setup:       func(_ *testing.T, e *linkFailureEnv) { e.backend.lostLink = []string{e.linksA, e.linksB} },
			wantMessage: "connection to the filesystem backend was lost: ssh pool is closed",
		},
		{
			name: "cleanup of the reserved folder fails",
			mode: "hardlink",
			setup: func(_ *testing.T, e *linkFailureEnv) {
				e.backend.createErr = errors.New("disk full")
				e.backend.removeErr = errors.New("busy")
			},
			wantPartialTree: true,
		},
		{
			name:        "lost connection at the check",
			mode:        "reflink",
			setup:       func(_ *testing.T, e *linkFailureEnv) { e.backend.lost = []string{e.linksA, e.linksB} },
			wantMessage: "connection to the filesystem backend was lost: ssh pool is closed",
		},
		{
			name:        "lost connection during the create",
			mode:        "reflink",
			setup:       func(_ *testing.T, e *linkFailureEnv) { e.backend.lostLink = []string{e.linksA, e.linksB} },
			wantMessage: "connection to the filesystem backend was lost: ssh pool is closed",
		},
		{
			name: "cleanup of the reserved folder fails",
			mode: "reflink",
			setup: func(_ *testing.T, e *linkFailureEnv) {
				e.backend.createErr = errors.New("disk full")
				e.backend.removeErr = errors.New("busy")
			},
			wantPartialTree: true,
		},
		{
			name: "cleanup of the failed tree also fails",
			mode: "hardlink",
			setup: func(_ *testing.T, e *linkFailureEnv) {
				e.backend.createErr = fmt.Errorf("disk full (%w: %w)", hardlinktree.ErrRollbackFailed, errors.New("busy"))
			},
			wantPartialTree: true,
		},
		{
			name: "lost connection at the check of A, cross-device at the link of B",
			mode: "hardlink",
			setup: func(_ *testing.T, e *linkFailureEnv) {
				e.backend.lost = []string{e.linksA}
				e.backend.crossDevice = []string{e.linksB}
			},
			wantMessage: "connection to the filesystem backend was lost: ssh pool is closed",
		},
		{
			name: "lost connection at the check of A, denied at the check of B",
			mode: "hardlink",
			setup: func(_ *testing.T, e *linkFailureEnv) {
				e.backend.lost = []string{e.linksA}
				e.backend.denied = []string{e.linksB}
			},
			wantMessage: "connection to the filesystem backend was lost: ssh pool is closed",
		},
		{
			name: "lost connection at the check of A, denied at the link of B",
			mode: "hardlink",
			setup: func(_ *testing.T, e *linkFailureEnv) {
				e.backend.lost = []string{e.linksA}
				e.backend.deniedLink = []string{e.linksB}
			},
			wantMessage: "connection to the filesystem backend was lost: ssh pool is closed",
		},
		{
			name: "lost connection at the check of B, cross-device at the link of A",
			mode: "hardlink",
			setup: func(_ *testing.T, e *linkFailureEnv) {
				e.backend.lost = []string{e.linksB}
				e.backend.crossDevice = []string{e.linksA}
			},
			wantMessage: "connection to the filesystem backend was lost: ssh pool is closed",
		},
		{
			name: "lost connection at the check of B, denied at the check of A",
			mode: "hardlink",
			setup: func(_ *testing.T, e *linkFailureEnv) {
				e.backend.lost = []string{e.linksB}
				e.backend.denied = []string{e.linksA}
			},
			wantMessage: "connection to the filesystem backend was lost: ssh pool is closed",
		},
		{
			name: "lost connection at the check of B, denied at the link of A",
			mode: "hardlink",
			setup: func(_ *testing.T, e *linkFailureEnv) {
				e.backend.lost = []string{e.linksB}
				e.backend.deniedLink = []string{e.linksA}
			},
			wantMessage: "connection to the filesystem backend was lost: ssh pool is closed",
		},
	}

	for _, tt := range tests {
		for _, skipRecheck := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%s/skip recheck %t", tt.name, tt.mode, skipRecheck), func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				e := &linkFailureEnv{
					downloads:    filepath.Join(root, "downloads"),
					linksA:       filepath.Join(root, "links-a"),
					linksB:       filepath.Join(root, "links-b"),
					backend:      &faultyLinkBackend{Backend: local.NewBackend(), reflink: true},
					files:        movie,
					matchedFiles: movie,
				}
				for _, dir := range []string{filepath.Join(e.downloads, "Movie"), e.linksA, e.linksB} {
					require.NoError(t, os.MkdirAll(dir, 0o755))
				}
				require.NoError(t, os.WriteFile(filepath.Join(e.downloads, "Movie", "movie.mkv"), []byte("movie"), 0o600))
				tt.setup(t, e)

				instance := &models.Instance{
					ID: 1, Name: "qbt1", HasLocalFilesystemAccess: true,
					UseHardlinks: tt.mode == "hardlink", UseReflinks: tt.mode == "reflink",
					HardlinkBaseDir:       e.linksA + "," + e.linksB,
					FallbackToRegularMode: !tt.fallbackOff,
				}
				s := &Service{instanceStore: &mockInstanceStore{instances: map[int]*models.Instance{1: instance}}}
				s.SetBackendPool(fsops.NewPool(s.instanceStore, e.backend))
				adds := withLinkAdder(s)
				process := s.processHardlinkMode
				if tt.mode == "reflink" {
					process = s.processReflinkMode
				}

				result := process(t.Context(), linkCheck(t, 1, hash123), CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
					[]byte("torrent"), hash123, "TorrentName", &CrossSeedRequest{SkipRecheck: skipRecheck},
					&qbt.Torrent{Hash: "matched", ContentPath: filepath.Join(e.downloads, "Movie")},
					"exact", e.files, e.matchedFiles, &qbt.TorrentProperties{SavePath: e.downloads}, "category", "category.cross")

				require.False(t, result.Success, "result: %+v", result)
				assert.Equal(t, tt.wantFallback, result.FallbackToRegular, "result: %+v", result)
				assert.Equal(t, tt.wantFullRecheck, result.RequiresFullRecheck)
				assert.Equal(t, !tt.wantFallback, result.Used)
				if !tt.wantFallback {
					assert.Equal(t, tt.mode+"_error", result.Result.Status)
					assert.Contains(t, result.Result.Message, tt.wantMessage)
				}
				if tt.wantPartialTree {
					assert.Contains(t, result.Result.Message, "; a partial link tree can remain in "+filepath.Join(e.linksA, "Movie"))
				}
				assert.Empty(t, adds.added, "a failed link tree adds nothing")
				_, categoryCreated := s.createdCategories.Load("1:category.cross")
				assert.False(t, categoryCreated, "a failed link tree creates no category")
				for _, base := range []string{e.linksA, e.linksB} {
					assert.NoFileExists(t, filepath.Join(base, "Movie", "movie.mkv"))
					assert.NoFileExists(t, filepath.Join(base, "Movie", "Extras", "extra.mkv"))
				}
			})
		}
	}
}

func newLostLinkEnv(t *testing.T) (*linkFailureEnv, *Service, *adderFake) {
	t.Helper()
	root := t.TempDir()
	e := &linkFailureEnv{
		downloads: filepath.Join(root, "downloads"),
		linksA:    filepath.Join(root, "links-a"),
		linksB:    filepath.Join(root, "links-b"),
		files:     qbt.TorrentFiles{{Name: "Movie/movie.mkv", Size: 5}},
	}
	e.matchedFiles = e.files
	e.backend = &faultyLinkBackend{Backend: local.NewBackend(), lost: []string{e.linksA}}
	require.NoError(t, os.MkdirAll(filepath.Join(e.downloads, "Movie"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(e.downloads, "Movie", "movie.mkv"), []byte("movie"), 0o600))
	instance := &models.Instance{
		ID: 1, Name: "qbt1", HasLocalFilesystemAccess: true, UseHardlinks: true,
		HardlinkBaseDir: e.linksA + "," + e.linksB, FallbackToRegularMode: true,
	}
	s := &Service{instanceStore: &mockInstanceStore{instances: map[int]*models.Instance{1: instance}}}
	s.SetBackendPool(fsops.NewPool(s.instanceStore, e.backend))
	return e, s, withLinkAdder(s)
}

func TestProcessLinkMode_LostCheckOnOneLinkBaseDirectoryLinksInTheNext(t *testing.T) {
	t.Parallel()

	e, s, adds := newLostLinkEnv(t)

	result := s.processHardlinkMode(t.Context(), linkCheck(t, 1, hash123), CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"), hash123, "TorrentName", &CrossSeedRequest{},
		&qbt.Torrent{Hash: "matched", ContentPath: filepath.Join(e.downloads, "Movie")},
		"exact", e.files, e.matchedFiles, &qbt.TorrentProperties{SavePath: e.downloads}, "", "")

	require.True(t, result.Success, "result: %+v", result)
	assert.False(t, result.FallbackToRegular)
	assert.Len(t, adds.added, 1)
	assert.FileExists(t, filepath.Join(e.linksB, "Movie", "movie.mkv"))
	assert.NoDirExists(t, filepath.Join(e.linksA, "Movie"))
}

func TestProcessLinkMode_SkipRecheckRefusesPendingFilesBeforeTheFilesystem(t *testing.T) {
	t.Parallel()

	e, s, adds := newLostLinkEnv(t)
	e.backend.Backend = nil
	sourceFiles := append(slices.Clone(e.files), qbt.TorrentFile{Name: "Movie/extra.nfo", Size: 3})

	result := s.processHardlinkMode(t.Context(), linkCheck(t, 1, hash123), CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
		[]byte("torrent"), hash123, "TorrentName", &CrossSeedRequest{SkipRecheck: true},
		&qbt.Torrent{Hash: "matched", ContentPath: filepath.Join(e.downloads, "Movie")},
		"exact", sourceFiles, e.matchedFiles, &qbt.TorrentProperties{SavePath: e.downloads}, "", "")

	require.True(t, result.Used, "result: %+v", result)
	assert.False(t, result.FallbackToRegular)
	assert.Equal(t, "skipped_recheck", result.Result.Status)
	assert.Empty(t, adds.added)
}

func TestProcessLinkMode_RouteWithoutRowLeavesNoTree(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	downloads, links := filepath.Join(root, "downloads"), filepath.Join(root, "links")
	require.NoError(t, os.MkdirAll(filepath.Join(downloads, "Movie"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(downloads, "Movie", "movie.mkv"), []byte("movie"), 0o600))
	instance := &models.Instance{ID: 1, Name: "lib", HasLocalFilesystemAccess: true, UseHardlinks: true, HardlinkBaseDir: links}
	s := &Service{instanceStore: &mockInstanceStore{instances: map[int]*models.Instance{1: instance}}}
	adds := withLinkAdder(s)
	files := qbt.TorrentFiles{{Name: "Movie/movie.mkv", Size: 5}}
	candidate := CrossSeedCandidate{InstanceID: 1, InstanceName: "lib", route: &candidateRoute{
		rows:      []models.CrossSeedRouteRow{{BaseDir: filepath.Join(root, "other"), MappedPath: "/seeds"}},
		crossSeed: &models.Instance{ID: 2, Name: "seeds"},
		source:    instance,
		backend:   local.NewBackend(),
	}}

	result := s.processHardlinkMode(t.Context(), linkCheck(t, 1, hash123), candidate,
		[]byte("torrent"), hash123, "TorrentName", &CrossSeedRequest{},
		&qbt.Torrent{Hash: "matched", ContentPath: filepath.Join(downloads, "Movie")},
		"exact", files, files, &qbt.TorrentProperties{SavePath: downloads}, "category", "category.cross")

	require.False(t, result.Success, "result: %+v", result)
	assert.False(t, result.FallbackToRegular)
	assert.Equal(t, "hardlink_error", result.Result.Status)
	assert.Equal(t, "Failed to build hardlink plan: the cross-seed route has no row for base folder "+links, result.Result.Message)
	assert.Empty(t, adds.added)
	assert.NoDirExists(t, filepath.Join(links, "Movie"))
}
