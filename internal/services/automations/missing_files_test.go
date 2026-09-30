// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
)

func TestBuildMissingFilesResultInvalidPathLeavesUnknown(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "present.mkv"), []byte("present"), 0o600))

	for _, test := range []struct {
		name     string
		fileName string
	}{
		{name: "rejected path", fileName: `AC\DC.mkv`},
		{name: "empty name", fileName: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			torrent := qbt.Torrent{Hash: "hash", SavePath: dir, Progress: 1}
			result := buildMissingFilesResult(
				context.Background(),
				localbackend.NewBackend(),
				map[string]qbt.Torrent{torrent.Hash: torrent},
				map[string]qbt.TorrentFiles{
					torrent.Hash: {
						{Name: "present.mkv"},
						{Name: test.fileName},
					},
				},
			)

			require.NotContains(t, result, torrent.Hash)
		})
	}
}

// rowGetter answers every Get with the current row, as the instance store does.
type rowGetter struct{ row *models.Instance }

func (g rowGetter) Get(context.Context, int) (*models.Instance, error) { return g.row, nil }

// absentBackend stands in for a host where none of qBittorrent's paths exist.
type absentBackend struct{ fsops.Backend }

func (absentBackend) Stat(_ context.Context, p string) (*fsops.LstatInfo, error) {
	return nil, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
}

// The callers gate on a snapshot that can be minutes old by the time the
// backend is resolved. If local access was turned off in between, the check
// must not run over the SSH host and report every torrent missing.
func TestMissingFilesSkipsAnInstanceThatLeftLocalMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "present.mkv"), []byte("present"), 0o600))
	torrent := qbt.Torrent{Hash: "hash", SavePath: dir, Progress: 1}
	cond := &RuleCondition{Field: FieldHasMissingFiles, Operator: OperatorEqual, Value: "true"}
	snapshot := &models.Instance{ID: 1, HasLocalFilesystemAccess: true}

	for _, test := range []struct {
		name  string
		row   *models.Instance
		known bool
	}{
		{name: "still local", row: snapshot, known: true},
		{name: "now remote", row: &models.Instance{ID: 1, SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-hostkey"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service := &Service{
				filesReader: fakeFilesReader{files: qbt.TorrentFiles{{Name: "present.mkv"}}},
				backendPool: fsops.NewPoolWithRemote(rowGetter{row: test.row}, localbackend.NewBackend(),
					func(*models.Instance) fsops.Backend { return absentBackend{} }),
			}
			evalCtx := &EvalContext{InstanceHasLocalAccess: true}
			service.setupMissingFilesContext(t.Context(), 1, &models.Automation{}, cond, []qbt.Torrent{torrent}, evalCtx, snapshot)

			missing, known := evalCtx.HasMissingFilesByHash[torrent.Hash]
			require.Equal(t, test.known, known)
			require.False(t, missing)
			require.False(t, EvaluateConditionWithContext(cond, torrent, evalCtx, 0), "a present file must never match the missing files condition")
		})
	}
}
