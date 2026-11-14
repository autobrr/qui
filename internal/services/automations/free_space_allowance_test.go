// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
)

// walkCounter counts the walks of each root.
type walkCounter struct {
	fsops.Backend
	walks map[string]int
}

func (b walkCounter) WalkDir(ctx context.Context, root string, opts fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	b.walks[root]++
	return b.Backend.WalkDir(ctx, root, opts)
}

func writeSized(t *testing.T, path string, size int) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, make([]byte, size), 0o600))
}

func allowanceRule(id int, path string, allowance int64) *models.Automation {
	return &models.Automation{
		ID:              id,
		FreeSpaceSource: &models.FreeSpaceSource{Type: models.FreeSpaceSourceAllowance, Path: path, AllowanceBytes: allowance},
		Conditions: &models.ActionConditions{Tag: &models.TagAction{
			Enabled: true, Tags: []string{"low"},
			Condition: &models.RuleCondition{Field: FieldFreeSpace, Operator: models.OperatorLessThan, Value: "1000"},
		}},
	}
}

func TestFreeSpaceLedgers_AllowanceMinusMeasuredUsage(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the allowance source refuses local access on Windows")
	}

	// 1000 + 200 + 300 bytes of files. The hardlink, the symlinks and the
	// folder behind the symlink add nothing.
	root := filepath.Join(t.TempDir(), "home")
	outside := t.TempDir()
	writeSized(t, filepath.Join(root, "a.bin"), 1000)
	writeSized(t, filepath.Join(root, ".hidden"), 200)
	writeSized(t, filepath.Join(root, "sub", "b.bin"), 300)
	writeSized(t, filepath.Join(outside, "big.bin"), 5000)
	require.NoError(t, os.Link(filepath.Join(root, "sub", "b.bin"), filepath.Join(root, "sub", "b-link.bin")))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "elsewhere")))
	require.NoError(t, os.Symlink(filepath.Join(root, "a.bin"), filepath.Join(root, "a-symlink.bin")))
	rootLink := filepath.Join(t.TempDir(), "home-link")
	require.NoError(t, os.Symlink(root, rootLink))

	instance := &models.Instance{ID: 1, HasLocalFilesystemAccess: true}
	backend := walkCounter{Backend: localbackend.NewBackend(), walks: make(map[string]int)}
	svc := &Service{backendPool: fsops.NewPoolWithRemote(rowGetter{row: instance}, backend, nil)}

	under := allowanceRule(1, root, 2000)
	over := allowanceRule(2, root, 1000)
	throughLink := allowanceRule(3, rootLink, 1500)

	ledgers, err := svc.freeSpaceLedgers(t.Context(), instance, []*models.Automation{under, over, throughLink}, false)
	require.NoError(t, err)
	require.Equal(t, int64(500), ledgers[under.ID].startFreeSpace())
	require.Equal(t, int64(-500), ledgers[over.ID].startFreeSpace(), "above the allowance the figure is negative")
	require.Zero(t, ledgers[throughLink.ID].startFreeSpace(), "a root that is a symlink is walked")
	require.Equal(t, map[string]int{root: 1, rootLink: 1}, backend.walks, "rules with one path share one walk")
}

// entriesBackend answers WalkDir with fixed entries, or with err.
type entriesBackend struct {
	fsops.Backend
	entries []fsops.WalkEntry
	err     error
}

func (entriesBackend) Paths() fsops.PathDialect { return fsops.SlashPaths }

func (b entriesBackend) WalkDir(context.Context, string, fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	if b.err != nil {
		return nil, b.err
	}
	ch := make(chan fsops.WalkEntry, len(b.entries))
	for _, entry := range b.entries {
		ch <- entry
	}
	close(ch)
	return ch, nil
}

func TestFreeSpaceLedgers_AllowanceFailures(t *testing.T) {
	t.Parallel()

	local := &models.Instance{ID: 1, HasLocalFilesystemAccess: true}
	shell := &models.Instance{ID: 1, SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-hostkey", SSHTier: models.SSHTierExec}
	fileTransfer := &models.Instance{ID: 1, SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-hostkey", SSHTier: models.SSHTierSFTP}
	errNoIdentity := errors.New("file identity is not available")
	file := func(path string, size int64) fsops.WalkEntry {
		return fsops.WalkEntry{Path: path, Size: size}
	}
	keyless := file("/home/user/odd.bin", 300)
	keyless.FileIDErr = errNoIdentity

	for _, test := range []struct {
		name      string
		row       *models.Instance
		backend   entriesBackend
		wantStart int64
		wantErr   error
		wantPath  string
	}{
		{
			name: "a folder that qui cannot read", row: shell,
			backend: entriesBackend{entries: []fsops.WalkEntry{file("/home/user/a.bin", 100),
				{Path: "/home/user/private", IsDir: true, Err: fs.ErrPermission}}},
			wantErr: fs.ErrPermission, wantPath: "/home/user/private",
		},
		{
			name: "a file that qui cannot read", row: shell,
			backend: entriesBackend{entries: []fsops.WalkEntry{
				{Path: "/home/user/locked.bin", StatErr: fs.ErrPermission}}},
			wantErr: fs.ErrPermission, wantPath: "/home/user/locked.bin",
		},
		{
			name: "a path that does not exist", row: shell,
			backend: entriesBackend{err: fs.ErrNotExist},
			wantErr: fs.ErrNotExist, wantPath: "/home/user",
		},
		{
			name: "a remote instance with file transfer only", row: fileTransfer,
			wantErr: fsops.ErrNotCapable,
		},
		{
			name: "a remote walk that read no file identity", row: shell,
			backend: entriesBackend{entries: []fsops.WalkEntry{keyless}},
			wantErr: errNoIdentity, wantPath: "/home/user/odd.bin",
		},
		{
			name: "a local file without identity counts as its own file", row: local,
			backend:   entriesBackend{entries: []fsops.WalkEntry{file("/home/user/a.bin", 100), keyless}},
			wantStart: 1000 - 400,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if test.row == local && runtime.GOOS == "windows" {
				t.Skip("the allowance source refuses local access on Windows")
			}

			svc := &Service{backendPool: fsops.NewPoolWithRemote(rowGetter{row: test.row}, test.backend,
				func(*models.Instance) fsops.Backend { return test.backend })}
			rule := allowanceRule(1, "/home/user", 1000)

			ledgers, err := svc.freeSpaceLedgers(t.Context(), test.row, []*models.Automation{rule}, false)
			require.ErrorIs(t, err, test.wantErr)
			if test.wantErr != nil {
				require.ErrorContains(t, err, test.wantPath)
				return
			}
			require.Equal(t, test.wantStart, ledgers[rule.ID].startFreeSpace())
		})
	}
}

func TestPreviewAllowanceRule_ReusesTheMeasurement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the allowance source refuses local access on Windows")
	}
	fixture := defaultParityFixture()
	fixture.localFS = true
	svc, instanceID := newDryRunParityService(t, fixture)
	root := t.TempDir()
	writeSized(t, filepath.Join(root, "a.bin"), 100)
	instance := &models.Instance{ID: instanceID, HasLocalFilesystemAccess: true}
	backend := walkCounter{Backend: localbackend.NewBackend(), walks: make(map[string]int)}
	svc.backendPool = fsops.NewPoolWithRemote(rowGetter{row: instance}, backend, nil)

	rule := parityTagRule(models.TagAction{})
	rule.Conditions.Tags[0].Condition = &models.RuleCondition{Field: FieldFreeSpace, Operator: models.OperatorLessThan, Value: "1000"}
	rule.FreeSpaceSource = &models.FreeSpaceSource{Type: models.FreeSpaceSourceAllowance, Path: root, AllowanceBytes: 500}

	for range 2 {
		result, err := svc.PreviewTagRule(t.Context(), instanceID, rule, 10, 0)
		require.NoError(t, err)
		require.Equal(t, 2, result.TotalMatches, "400 bytes are free, below the 1000 of the condition")
	}
	require.Equal(t, 1, backend.walks[root], "the second preview reuses the measurement")

	_, err := svc.freeSpaceLedgers(t.Context(), instance, []*models.Automation{rule}, false)
	require.NoError(t, err)
	require.Equal(t, 2, backend.walks[root], "a run measures again")
}
