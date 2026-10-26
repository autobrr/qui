// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"io/fs"
	"os"
	"path"
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
			result, _ := buildMissingFilesResult(
				t.Context(),
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

// batchBackend is a remote host that answers only batch stats, so a check
// that falls back to one Stat per file panics on the nil Backend. A path in
// errs gets that error, and any other path exists.
type batchBackend struct {
	fsops.Backend
	errs map[string]error
}

func (batchBackend) Paths() fsops.PathDialect { return fsops.SlashPaths }

func (b batchBackend) StatBatch(_ context.Context, paths []string) ([]*fsops.LstatInfo, []error) {
	infos := make([]*fsops.LstatInfo, len(paths))
	errs := make([]error, len(paths))
	for i, p := range paths {
		if err, ok := b.errs[p]; ok {
			errs[i] = &fs.PathError{Op: "stat", Path: p, Err: err}
			continue
		}
		infos[i] = &fsops.LstatInfo{Path: p}
	}
	return infos, errs
}

func (batchBackend) LstatBatch(context.Context, []string) ([]*fsops.LstatInfo, []error) {
	panic("the missing files check follows links")
}

// The callers gate on a snapshot that can be minutes old by the time the
// backend is resolved. The check runs over the backend of the current row, and
// only while that row still grants Read.
func TestMissingFilesUsesTheBackendOfTheCurrentRow(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "present.mkv"), []byte("present"), 0o600))
	cond := &RuleCondition{Field: FieldHasMissingFiles, Operator: OperatorEqual, Value: "true"}
	snapshot := &models.Instance{ID: 1, HasLocalFilesystemAccess: true}
	remote := &models.Instance{ID: 1, SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-hostkey"}

	for _, test := range []struct {
		name     string
		row      *models.Instance
		savePath string
		known    bool
		missing  bool
	}{
		{name: "still local", row: snapshot, savePath: dir, known: true},
		{name: "now remote", row: remote, savePath: "/remote", known: true, missing: true},
		{name: "now without access", row: &models.Instance{ID: 1}, savePath: dir},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			torrent := qbt.Torrent{Hash: "hash", SavePath: test.savePath, Progress: 1}

			service := &Service{
				filesReader: fakeFilesReader{files: qbt.TorrentFiles{{Name: "present.mkv"}}},
				backendPool: fsops.NewPoolWithRemote(rowGetter{row: test.row}, localbackend.NewBackend(),
					func(*models.Instance) fsops.Backend {
						// The remote host does not have the local test directory.
						return batchBackend{errs: map[string]error{path.Join(test.savePath, "present.mkv"): fs.ErrNotExist}}
					}),
			}
			rule := &models.Automation{Conditions: &models.ActionConditions{Delete: &models.DeleteAction{Enabled: true, Condition: cond}}}
			evalCtx, _, err := service.buildEvalContext(t.Context(), 1, snapshot, []qbt.Torrent{torrent}, previewNeeds(rule))
			require.NoError(t, err)

			missing, known := evalCtx.HasMissingFilesByHash[torrent.Hash]
			require.Equal(t, test.known, known)
			require.Equal(t, test.missing, missing)
			require.Equal(t, test.missing, EvaluateConditionWithContext(cond, torrent, evalCtx, 0))
		})
	}
}

// A remote instance reads the field: a torrent with a file that is not on the
// host has missing files, and a torrent with every file there has none.
func TestMissingFilesOnARemoteInstance(t *testing.T) {
	t.Parallel()

	remote := &models.Instance{ID: 1, SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-hostkey"}
	complete := qbt.Torrent{Hash: "complete", SavePath: "/data/complete", Progress: 1}
	broken := qbt.Torrent{Hash: "broken", SavePath: "/data/broken", Progress: 1}
	service := &Service{
		filesReader: fakeFilesReader{files: qbt.TorrentFiles{{Name: "Show/e01.mkv"}, {Name: "Show/e02.mkv"}}},
		backendPool: fsops.NewPoolWithRemote(rowGetter{row: remote}, localbackend.NewBackend(),
			func(*models.Instance) fsops.Backend {
				return batchBackend{errs: map[string]error{"/data/broken/Show/e02.mkv": fs.ErrNotExist}}
			}),
	}
	cond := &RuleCondition{Field: FieldHasMissingFiles, Operator: OperatorEqual, Value: "true"}
	rule := &models.Automation{Conditions: &models.ActionConditions{Pause: &models.PauseAction{Enabled: true, Condition: cond}}}

	evalCtx, _, err := service.buildEvalContext(t.Context(), 1, remote, []qbt.Torrent{complete, broken}, previewNeeds(rule))
	require.NoError(t, err)

	require.Equal(t, map[string]bool{"complete": false, "broken": true}, evalCtx.HasMissingFilesByHash)
	require.False(t, EvaluateConditionWithContext(cond, complete, evalCtx, 0))
	require.True(t, EvaluateConditionWithContext(cond, broken, evalCtx, 0))
}

// A file that a lost connection left unchecked makes the torrent unknown, unless
// another file is confirmed missing. A symlink loop is missing on a remote
// instance, because OpenSSH's sftp-server answers ELOOP as no such file.
func TestBuildMissingFilesResultOnARemoteHost(t *testing.T) {
	t.Parallel()

	backend := batchBackend{errs: map[string]error{
		"/data/lost.mkv":    fsops.ErrConnectionLost,
		"/data/missing.mkv": fs.ErrNotExist,
		"/data/loop.mkv":    fs.ErrNotExist,
		"/data/denied.mkv":  fs.ErrPermission,
	}}
	for _, test := range []struct {
		name           string
		files          []string
		want           map[string]bool
		connectionLost bool
	}{
		{name: "lost", files: []string{"present.mkv", "lost.mkv"}, want: map[string]bool{}, connectionLost: true},
		{name: "missing and lost", files: []string{"lost.mkv", "missing.mkv"}, want: map[string]bool{"hash": true}, connectionLost: true},
		{name: "symlink loop", files: []string{"present.mkv", "loop.mkv"}, want: map[string]bool{"hash": true}},
		{name: "other error", files: []string{"present.mkv", "denied.mkv"}, want: map[string]bool{"hash": false}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			torrent := qbt.Torrent{Hash: "hash", SavePath: "/data", Progress: 1}
			var files qbt.TorrentFiles
			for _, name := range test.files {
				files = append(files, qbt.TorrentFile{Name: name})
			}
			result, connectionLost := buildMissingFilesResult(t.Context(), backend,
				map[string]qbt.Torrent{torrent.Hash: torrent}, map[string]qbt.TorrentFiles{torrent.Hash: files})

			require.Equal(t, test.want, result)
			require.Equal(t, test.connectionLost, connectionLost)
		})
	}
}

// A local os.Stat returns ELOOP for a symlink loop, so the local check skips
// the file and the torrent reads from its other files.
func TestBuildMissingFilesResultSkipsALocalSymlinkLoop(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "present.mkv"), []byte("present"), 0o600))
	require.NoError(t, os.Symlink("loop-b.mkv", filepath.Join(dir, "loop-a.mkv")))
	require.NoError(t, os.Symlink("loop-a.mkv", filepath.Join(dir, "loop-b.mkv")))

	torrent := qbt.Torrent{Hash: "hash", SavePath: dir, Progress: 1}
	result, connectionLost := buildMissingFilesResult(t.Context(), localbackend.NewBackend(),
		map[string]qbt.Torrent{torrent.Hash: torrent},
		map[string]qbt.TorrentFiles{torrent.Hash: {{Name: "present.mkv"}, {Name: "loop-a.mkv"}}})

	require.Equal(t, map[string]bool{torrent.Hash: false}, result)
	require.False(t, connectionLost)
}
