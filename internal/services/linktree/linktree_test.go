// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package linktree

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

type stubBackend struct {
	fsops.Backend
	otherDisk   []string
	lostSameFS  []string
	lostMkdir   []string
	lostStat    []string
	failMkdir   []string
	crossDevice []string
	removeErr   error
	reflink     bool
	reflinkErr  error
}

func under(prefixes []string, p string) bool {
	for _, prefix := range prefixes {
		if p == prefix || strings.HasPrefix(p, prefix+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (s *stubBackend) SameFilesystem(ctx context.Context, p1, p2 string) (bool, error) {
	if under(s.lostSameFS, p2) {
		return false, fmt.Errorf("samefilesystem: %w", fsops.ErrConnectionLost)
	}
	if _, err := s.Backend.SameFilesystem(ctx, p1, p2); err != nil {
		return false, err
	}
	return under(s.otherDisk, p1) == under(s.otherDisk, p2), nil
}

func (s *stubBackend) Stat(ctx context.Context, p string) (*fsops.LstatInfo, error) {
	if under(s.lostStat, p) {
		return nil, fmt.Errorf("stat %s: %w", p, fsops.ErrConnectionLost)
	}
	return s.Backend.Stat(ctx, p)
}

func (s *stubBackend) MkdirAll(ctx context.Context, p string, perm fs.FileMode) error {
	if under(s.lostMkdir, p) {
		return fmt.Errorf("mkdir %s: %w", p, fsops.ErrConnectionLost)
	}
	if under(s.failMkdir, p) {
		return fmt.Errorf("mkdir %s: %w", p, fs.ErrPermission)
	}
	return s.Backend.MkdirAll(ctx, p, perm)
}

func (s *stubBackend) Remove(ctx context.Context, p string, opts fsops.RemoveOptions) error {
	if s.removeErr != nil {
		return fmt.Errorf("remove %s: %w", p, s.removeErr)
	}
	return s.Backend.Remove(ctx, p, opts)
}

func (s *stubBackend) HardlinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	if under(s.crossDevice, plan.RootDir) {
		return nil, &os.LinkError{Op: "link", Old: plan.Files[0].SourcePath, New: plan.Files[0].TargetPath, Err: syscall.EXDEV}
	}
	return s.Backend.HardlinkTree(ctx, plan)
}

func (s *stubBackend) ReflinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	return s.HardlinkTree(ctx, plan)
}

func (s *stubBackend) SupportsReflink(context.Context, string) (bool, string, error) {
	return s.reflink, "no clones here", s.reflinkErr
}

type testEnv struct {
	root, data   string
	e1, e2       string
	baseA, baseB string
	stub         *stubBackend
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	root := t.TempDir()
	e := &testEnv{
		root:  root,
		data:  filepath.Join(root, "data", "Show.S01"),
		baseA: filepath.Join(root, "links-a"),
		baseB: filepath.Join(root, "links-b"),
		stub:  &stubBackend{Backend: local.NewBackend()},
	}
	require.NoError(t, os.MkdirAll(e.data, 0o755))
	e.e1, e.e2 = filepath.Join(e.data, "e1.mkv"), filepath.Join(e.data, "e2.mkv")
	require.NoError(t, os.WriteFile(e.e1, []byte("one"), 0o600))
	require.NoError(t, os.WriteFile(e.e2, []byte("two"), 0o600))
	return e
}

func (e *testEnv) request() Request {
	return Request{
		Backend: e.stub,
		Config:  Config{BaseDirs: []string{e.baseA}, Preset: PresetFlat, Kind: Hardlink},
		Torrent: Torrent{InfoHash: "DEADBEEFcafe", Name: "Show.S01", Files: []string{"Show.S01/e1.mkv", "Show.S01/e2.mkv"}},
		Links: []Link{
			{TorrentPath: "Show.S01/e1.mkv", SourcePath: e.e1},
			{TorrentPath: "Show.S01/e2.mkv", SourcePath: e.e2},
		},
	}
}

func add(t *testing.T, req Request) *Tree {
	t.Helper()
	plan, err := Plan(t.Context(), req)
	require.NoError(t, err)
	tree, err := plan.Create(t.Context())
	require.NoError(t, err)
	return tree
}

func requireLink(t *testing.T, link, source string) {
	t.Helper()
	linkInfo, err := os.Stat(link)
	require.NoError(t, err)
	sourceInfo, err := os.Stat(source)
	require.NoError(t, err)
	require.True(t, os.SameFile(linkInfo, sourceInfo), "%s is not a link to %s", link, source)
}

func requireMissing(t *testing.T, p string) {
	t.Helper()
	_, err := os.Lstat(p)
	require.ErrorIs(t, err, fs.ErrNotExist, "%s should not exist", p)
}

func TestLinkTree(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		edit         func(t *testing.T, e *testEnv, req *Request)
		planOnly     bool
		wantPlanFail FailureKind
		wantFail     FailureKind
		wantPartial  bool
		check        func(t *testing.T, e *testEnv, plan *TreePlan, tree *Tree)
	}{
		{
			name: "the base dir that shares more of the source path comes first",
			edit: func(_ *testing.T, e *testEnv, req *Request) {
				req.Config.BaseDirs = []string{e.baseA, filepath.Join(e.root, "data", "links")}
			},
			check: func(t *testing.T, e *testEnv, _ *TreePlan, tree *Tree) {
				near := filepath.Join(e.root, "data", "links")
				require.Equal(t, near, tree.BaseDir)
				requireLink(t, filepath.Join(near, "Show.S01", "e1.mkv"), e.e1)
				requireMissing(t, e.baseA)
			},
		},
		{
			name: "a base dir on another filesystem is passed over",
			edit: func(t *testing.T, e *testEnv, req *Request) {
				other := filepath.Join(e.root, "data", "links")
				require.NoError(t, os.MkdirAll(other, 0o755))
				req.Config.BaseDirs = []string{other, e.baseA}
				e.stub.otherDisk = []string{other}
			},
			check: func(t *testing.T, e *testEnv, _ *TreePlan, tree *Tree) {
				require.Equal(t, e.baseA, tree.BaseDir)
				requireLink(t, filepath.Join(e.baseA, "Show.S01", "e2.mkv"), e.e2)
			},
		},
		{
			name: "sources on two filesystems fit no base dir",
			edit: func(t *testing.T, e *testEnv, req *Request) {
				otherDir := filepath.Join(e.root, "other")
				require.NoError(t, os.MkdirAll(otherDir, 0o755))
				moved := filepath.Join(otherDir, "e2.mkv")
				require.NoError(t, os.WriteFile(moved, []byte("two"), 0o600))
				req.Links[1].SourcePath = moved
				e.stub.otherDisk = []string{otherDir}
			},
			wantPlanFail: NoBaseDir,
		},
		{
			name: "a lost connection on one source folder wins over another filesystem",
			edit: func(t *testing.T, e *testEnv, req *Request) {
				otherDir, lostDir := filepath.Join(e.root, "other"), filepath.Join(e.root, "lost")
				for _, dir := range []string{otherDir, lostDir} {
					require.NoError(t, os.MkdirAll(dir, 0o755))
					require.NoError(t, os.WriteFile(filepath.Join(dir, "e3.mkv"), []byte("three"), 0o600))
				}
				req.Torrent.Files = append(req.Torrent.Files, "Show.S01/e3.mkv", "Show.S01/e4.mkv")
				req.Links = append(req.Links,
					Link{TorrentPath: "Show.S01/e3.mkv", SourcePath: filepath.Join(otherDir, "e3.mkv")},
					Link{TorrentPath: "Show.S01/e4.mkv", SourcePath: filepath.Join(lostDir, "e3.mkv")},
				)
				e.stub.otherDisk = []string{otherDir}
				e.stub.lostSameFS = []string{lostDir}
			},
			wantPlanFail: ConnectionLost,
		},
		{
			name: "no base dir is configured",
			edit: func(_ *testing.T, _ *testEnv, req *Request) {
				req.Config.BaseDirs = nil
			},
			wantPlanFail: NoBaseDir,
		},
		{
			name: "plan writes nothing",
			edit: func(_ *testing.T, e *testEnv, req *Request) {
				req.Config.BaseDirs = []string{e.baseA, e.baseB}
				req.Config.Preset = PresetByTracker
				req.TrackerFolder = "Tracker"
			},
			planOnly: true,
			check: func(t *testing.T, e *testEnv, plan *TreePlan, _ *Tree) {
				require.Equal(t, e.baseA, plan.BaseDir)
				require.Equal(t, filepath.Join(e.baseA, "Tracker"), plan.PresetDir)
				require.Equal(t, filepath.Join(e.baseA, "Tracker"), plan.SavePath)
				require.Equal(t, []string{"Show.S01/e1.mkv", "Show.S01/e2.mkv"}, plan.TorrentPaths)
				requireMissing(t, e.baseA)
				requireMissing(t, e.baseB)
			},
		},
		{
			name: "the flat preset links into the base dir",
			check: func(t *testing.T, e *testEnv, plan *TreePlan, tree *Tree) {
				require.Equal(t, e.baseA, plan.SavePath)
				require.Equal(t, e.baseA, tree.PresetDir)
				require.Equal(t, e.baseA, tree.SavePath)
				require.Equal(t, Hardlink, tree.Kind)
				requireLink(t, filepath.Join(e.baseA, "Show.S01", "e1.mkv"), e.e1)
				require.Equal(t, []LinkedFile{
					{TorrentPath: "Show.S01/e1.mkv", LinkPath: filepath.Join(e.baseA, "Show.S01", "e1.mkv"), Created: true},
					{TorrentPath: "Show.S01/e2.mkv", LinkPath: filepath.Join(e.baseA, "Show.S01", "e2.mkv"), Created: true},
				}, tree.Files)
			},
		},
		{
			name: "the by tracker preset links into the tracker folder",
			edit: func(_ *testing.T, _ *testEnv, req *Request) {
				req.Config.Preset = PresetByTracker
				req.TrackerFolder = "Tracker: One"
			},
			check: func(t *testing.T, e *testEnv, _ *TreePlan, tree *Tree) {
				want := filepath.Join(e.baseA, "Tracker One")
				require.Equal(t, want, tree.PresetDir)
				require.Equal(t, want, tree.SavePath)
				requireLink(t, filepath.Join(want, "Show.S01", "e1.mkv"), e.e1)
			},
		},
		{
			name: "the by instance preset links into the instance folder",
			edit: func(_ *testing.T, _ *testEnv, req *Request) {
				req.Config.Preset = PresetByInstance
				req.InstanceFolder = "seedbox"
			},
			check: func(t *testing.T, e *testEnv, _ *TreePlan, tree *Tree) {
				want := filepath.Join(e.baseA, "seedbox")
				require.Equal(t, want, tree.SavePath)
				requireLink(t, filepath.Join(want, "Show.S01", "e2.mkv"), e.e2)
			},
		},
		{
			name: "a rootless torrent gets an isolation folder",
			edit: func(_ *testing.T, e *testEnv, req *Request) {
				req.Torrent.Files = []string{"e1.mkv", "e2.mkv"}
				req.Links = []Link{{TorrentPath: "e1.mkv", SourcePath: e.e1}}
			},
			check: func(t *testing.T, e *testEnv, plan *TreePlan, tree *Tree) {
				want := filepath.Join(e.baseA, "Show.S01")
				require.Equal(t, want, plan.SavePath)
				require.Equal(t, want, tree.SavePath)
				requireLink(t, filepath.Join(want, "e1.mkv"), e.e1)
			},
		},
		{
			name: "a taken name gives the hashed folder",
			edit: func(t *testing.T, e *testEnv, _ *Request) {
				require.NoError(t, os.MkdirAll(filepath.Join(e.baseA, "Show.S01"), 0o755))
			},
			check: func(t *testing.T, e *testEnv, _ *TreePlan, tree *Tree) {
				hashed := filepath.Join(e.baseA, "Show.S01--deadbeef")
				require.Equal(t, hashed, tree.SavePath)
				requireLink(t, filepath.Join(hashed, "Show.S01", "e1.mkv"), e.e1)
				requireMissing(t, filepath.Join(e.baseA, "Show.S01", "Show.S01"))
			},
		},
		{
			name: "the same torrent a second time reuses its links",
			edit: func(t *testing.T, e *testEnv, req *Request) {
				require.NoError(t, os.MkdirAll(filepath.Join(e.baseA, "Show.S01"), 0o755))
				add(t, *req)
			},
			check: func(t *testing.T, e *testEnv, _ *TreePlan, tree *Tree) {
				hashed := filepath.Join(e.baseA, "Show.S01--deadbeef")
				require.Equal(t, hashed, tree.SavePath)
				for _, f := range tree.Files {
					require.False(t, f.Created, f.TorrentPath)
				}
				requireLink(t, filepath.Join(hashed, "Show.S01", "e1.mkv"), e.e1)
			},
		},
		{
			name: "a different file at a target is refused",
			edit: func(t *testing.T, e *testEnv, _ *Request) {
				require.NoError(t, os.MkdirAll(filepath.Join(e.baseA, "Show.S01"), 0o755))
				stranger := filepath.Join(e.baseA, "Show.S01--deadbeef", "Show.S01")
				require.NoError(t, os.MkdirAll(stranger, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(stranger, "e2.mkv"), []byte("stranger"), 0o600))
			},
			wantFail: TargetExists,
			check: func(t *testing.T, e *testEnv, _ *TreePlan, _ *Tree) {
				stranger := filepath.Join(e.baseA, "Show.S01--deadbeef", "Show.S01")
				got, err := os.ReadFile(filepath.Join(stranger, "e2.mkv"))
				require.NoError(t, err)
				require.Equal(t, "stranger", string(got))
				requireMissing(t, filepath.Join(stranger, "e1.mkv"))
			},
		},
		{
			name: "a cross-device error moves the tree to the next base dir",
			edit: func(_ *testing.T, e *testEnv, req *Request) {
				req.Config.BaseDirs = []string{e.baseA, e.baseB}
				e.stub.crossDevice = []string{e.baseA}
			},
			check: func(t *testing.T, e *testEnv, _ *TreePlan, tree *Tree) {
				require.Equal(t, e.baseB, tree.BaseDir)
				require.Equal(t, e.baseB, tree.SavePath)
				requireLink(t, filepath.Join(e.baseB, "Show.S01", "e1.mkv"), e.e1)
				requireMissing(t, filepath.Join(e.baseA, "Show.S01"))
			},
		},
		{
			name: "cross-device in every base dir",
			edit: func(_ *testing.T, e *testEnv, req *Request) {
				req.Config.BaseDirs = []string{e.baseA, e.baseB}
				e.stub.crossDevice = []string{e.baseA, e.baseB}
			},
			wantFail: CrossDevice,
		},
		{
			name: "a failed cleanup stops the walk",
			edit: func(_ *testing.T, e *testEnv, req *Request) {
				req.Config.BaseDirs = []string{e.baseA, e.baseB}
				e.stub.crossDevice = []string{e.baseA}
				e.stub.removeErr = fs.ErrPermission
			},
			wantFail:    CreateFailed,
			wantPartial: true,
			check: func(t *testing.T, e *testEnv, _ *TreePlan, _ *Tree) {
				requireMissing(t, e.baseB)
			},
		},
		{
			name: "a lost connection during the cleanup is a lost connection",
			edit: func(_ *testing.T, e *testEnv, req *Request) {
				req.Config.BaseDirs = []string{e.baseA, e.baseB}
				e.stub.crossDevice = []string{e.baseA}
				e.stub.removeErr = fsops.ErrConnectionLost
			},
			wantFail:    ConnectionLost,
			wantPartial: true,
			check: func(t *testing.T, e *testEnv, _ *TreePlan, _ *Tree) {
				requireMissing(t, e.baseB)
			},
		},
		{
			name: "a lost connection, then a cross-device error, is a lost connection",
			edit: func(_ *testing.T, e *testEnv, req *Request) {
				req.Config.BaseDirs = []string{e.baseA, e.baseB}
				e.stub.lostMkdir = []string{e.baseA}
				e.stub.crossDevice = []string{e.baseB}
			},
			wantFail: ConnectionLost,
		},
		{
			name: "a cross-device error, then a lost connection, is a lost connection",
			edit: func(_ *testing.T, e *testEnv, req *Request) {
				req.Config.BaseDirs = []string{e.baseA, e.baseB}
				e.stub.crossDevice = []string{e.baseA}
				e.stub.lostMkdir = []string{e.baseB}
			},
			wantFail: ConnectionLost,
		},
		{
			name: "a lost connection, then a refusal, is a lost connection",
			edit: func(_ *testing.T, e *testEnv, req *Request) {
				req.Config.BaseDirs = []string{e.baseA, e.baseB}
				req.Config.Kind = Reflink
				e.stub.lostMkdir = []string{e.baseA}
			},
			wantFail: ConnectionLost,
		},
		{
			name: "a lost connection on one check leaves the other base dir",
			edit: func(_ *testing.T, e *testEnv, req *Request) {
				req.Config.BaseDirs = []string{e.baseA, e.baseB}
				e.stub.lostSameFS = []string{e.baseA}
			},
			check: func(t *testing.T, e *testEnv, _ *TreePlan, tree *Tree) {
				require.Equal(t, e.baseB, tree.BaseDir)
			},
		},
		{
			name: "a lost connection on one check, then a cross-device error, is a lost connection",
			edit: func(_ *testing.T, e *testEnv, req *Request) {
				req.Config.BaseDirs = []string{e.baseA, e.baseB}
				e.stub.lostSameFS = []string{e.baseA}
				e.stub.crossDevice = []string{e.baseB}
			},
			wantFail: ConnectionLost,
		},
		{
			name: "a lost connection while a missing base dir is judged fails the plan",
			edit: func(_ *testing.T, e *testEnv, _ *Request) {
				e.stub.lostStat = []string{e.baseA}
			},
			wantPlanFail: ConnectionLost,
		},
		{
			name: "a lost connection on the only check fails the plan",
			edit: func(_ *testing.T, e *testEnv, _ *Request) {
				e.stub.lostSameFS = []string{e.baseA}
			},
			wantPlanFail: ConnectionLost,
		},
		{
			name: "a base dir that cannot be made is skipped",
			edit: func(_ *testing.T, e *testEnv, req *Request) {
				req.Config.BaseDirs = []string{e.baseA, e.baseB}
				e.stub.failMkdir = []string{e.baseA}
			},
			check: func(t *testing.T, e *testEnv, _ *TreePlan, tree *Tree) {
				require.Equal(t, e.baseB, tree.BaseDir)
			},
		},
		{
			name: "no base dir can be made",
			edit: func(_ *testing.T, e *testEnv, _ *Request) {
				e.stub.failMkdir = []string{e.baseA}
			},
			wantFail: NoBaseDir,
		},
		{
			name: "rollback removes only what this add created, also after a cancel",
			edit: func(t *testing.T, e *testEnv, req *Request) {
				require.NoError(t, os.MkdirAll(filepath.Join(e.baseA, "Show.S01"), 0o755))
				first := *req
				first.Links = req.Links[:1]
				add(t, first)
			},
			check: func(t *testing.T, e *testEnv, _ *TreePlan, tree *Tree) {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				require.NoError(t, tree.Rollback(ctx))
				folder := filepath.Join(e.baseA, "Show.S01--deadbeef", "Show.S01")
				requireLink(t, filepath.Join(folder, "e1.mkv"), e.e1)
				requireMissing(t, filepath.Join(folder, "e2.mkv"))
			},
		},
		{
			name: "rollback frees the reserved folder",
			check: func(t *testing.T, e *testEnv, _ *TreePlan, tree *Tree) {
				require.NoError(t, tree.Rollback(t.Context()))
				requireMissing(t, filepath.Join(e.baseA, "Show.S01"))
			},
		},
		{
			name: "a reflink tree when the filesystem clones",
			edit: func(_ *testing.T, e *testEnv, req *Request) {
				req.Config.Kind = Reflink
				e.stub.reflink = true
			},
			check: func(t *testing.T, _ *testEnv, _ *TreePlan, tree *Tree) {
				require.Equal(t, Reflink, tree.Kind)
			},
		},
		{
			name: "no reflink support",
			edit: func(_ *testing.T, _ *testEnv, req *Request) {
				req.Config.Kind = Reflink
			},
			wantFail: NotSupported,
			check: func(t *testing.T, e *testEnv, _ *TreePlan, _ *Tree) {
				requireMissing(t, filepath.Join(e.baseA, "Show.S01"))
			},
		},
		{
			name: "a failed reflink question",
			edit: func(_ *testing.T, e *testEnv, req *Request) {
				req.Config.Kind = Reflink
				e.stub.reflink, e.stub.reflinkErr = true, errors.New("probe failed")
			},
			wantFail: NotSupported,
		},
		{
			name: "an unsafe torrent path",
			edit: func(_ *testing.T, e *testEnv, req *Request) {
				req.Links = []Link{{TorrentPath: "Show.S01/../../e1.mkv", SourcePath: e.e1}}
			},
			wantPlanFail: InvalidPlan,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newTestEnv(t)
			req := e.request()
			if tt.edit != nil {
				tt.edit(t, e, &req)
			}

			plan, err := Plan(t.Context(), req)
			if tt.wantPlanFail != 0 {
				var failure *Error
				require.ErrorAs(t, err, &failure)
				require.Equal(t, tt.wantPlanFail, failure.Kind, err.Error())
				return
			}
			require.NoError(t, err)
			if tt.planOnly {
				tt.check(t, e, plan, nil)
				return
			}

			tree, err := plan.Create(t.Context())
			if tt.wantFail != 0 {
				var failure *Error
				require.ErrorAs(t, err, &failure)
				require.Equal(t, tt.wantFail, failure.Kind, err.Error())
				require.Equal(t, tt.wantPartial, failure.Partial)
				if tt.check != nil {
					tt.check(t, e, plan, nil)
				}
				return
			}
			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, e, plan, tree)
			}
		})
	}
}
