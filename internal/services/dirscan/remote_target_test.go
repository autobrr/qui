// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"bytes"
	"io/fs"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlink"
)

// An exec walk that fell back to sftp reads no identity, which a row claiming Identity should explain once.
func TestScanLogsFilesWithoutIdentity(t *testing.T) {
	t.Parallel()

	identified := slashEntry("/data/scan/Film/a.mkv", "Film/a.mkv", 0)
	identified.FileID = remoteID(1)
	for _, tt := range []struct {
		name string
		tier models.SSHTier
		walk []fsops.WalkEntry
		want string
	}{
		{name: "exec row, every identity read", tier: models.SSHTierExec, walk: []fsops.WalkEntry{identified}},
		{name: "exec row, identity missing", tier: models.SSHTierExec, walk: []fsops.WalkEntry{identified, slashEntry("/data/scan/Film/b.mkv", "Film/b.mkv", 0), slashEntry("/data/scan/Film/c.mkv", "Film/c.mkv", 0)}, want: `"files":2`},
		{name: "sftp row never has identity", tier: models.SSHTierSFTP, walk: []fsops.WalkEntry{slashEntry("/data/scan/Film/b.mkv", "Film/b.mkv", 0)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			walk := append([]fsops.WalkEntry{slashEntry("/data/scan", ".", fs.ModeDir), slashEntry("/data/scan/Film", "Film", fs.ModeDir)}, tt.walk...)
			row := remoteInstance(tt.tier)
			svc := &Service{torrentFiles: stubTorrentFiles{empty: true}}
			var buf bytes.Buffer
			l := zerolog.New(&buf)

			_, _, ok := svc.runScanPhase(t.Context(), &models.DirScanDirectory{TargetInstanceID: row.ID}, &scanTarget{backend: newSlashBackend(walk...), instance: row}, "/data/scan", 1, &l)

			require.True(t, ok)
			if tt.want == "" {
				require.NotContains(t, buf.String(), "no identity")
				return
			}
			require.Contains(t, buf.String(), "no identity")
			require.Contains(t, buf.String(), tt.want)
		})
	}
}

// A stored identity only names a file on the endpoint that read it, so another server's file with the
// same device and inode is a new file, even after the status write of the search phase.
//
// regression: #2919
func TestStoredIdentityFollowsTheEndpoint(t *testing.T) {
	base := remoteInstance(models.SSHTierExec)
	base.SSHUsername = "seed"
	changed := func(change func(*models.Instance)) *models.Instance {
		row := *base
		change(&row)
		return &row
	}
	const oldPath, newPath = "/data/old/film.mkv", "/data/new/film.mkv"
	for _, tt := range []struct {
		name       string
		next       *models.Instance
		wantRename bool
	}{
		{name: "same endpoint keeps the verdict through a rename", next: changed(func(*models.Instance) {}), wantRename: true},
		{name: "host", next: changed(func(r *models.Instance) { r.SSHHost = "other.example.invalid" })},
		{name: "port", next: changed(func(r *models.Instance) { r.SSHPort = 2222 })},
		{name: "account", next: changed(func(r *models.Instance) { r.SSHUsername = "alt" })},
		{name: "host key replaced", next: changed(func(r *models.Instance) { r.SSHHostKeyEncrypted = "enc-hostkey-2" })},
		{name: "private key replaced", next: changed(func(r *models.Instance) { r.SSHKeyEncrypted = "enc-key-2" })},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			svc, store, dir := newDirScanTestService(t)
			modTime := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
			scan := func(p string) *ScanResult {
				return &ScanResult{Searchees: []*Searchee{{Name: "film", Path: p, Files: []*ScannedFile{
					{Path: p, RelPath: "film.mkv", Size: 100, ModTime: modTime, FileID: remoteID(1)},
				}}}}
			}
			l := zerolog.Nop()

			first := scan(oldPath)
			tracked, err := svc.refreshTrackedFilesFromScan(ctx, dir.ID, base, first, &seedingIndex{instance: base}, &l)
			require.NoError(t, err)
			require.NoError(t, svc.upsertDirScanFileStatus(ctx, dir.ID, first.Searchees[0].Files[0], tracked, dirScanFileUpdate{status: models.DirScanFileStatusNoMatch}))

			second, err := svc.refreshTrackedFilesFromScan(ctx, dir.ID, tt.next, scan(newPath), &seedingIndex{instance: tt.next}, &l)

			require.NoError(t, err)
			files, err := store.ListFiles(ctx, dir.ID, nil, 10, 0)
			require.NoError(t, err)
			byPath := map[string]models.DirScanFileStatus{}
			for _, f := range files {
				byPath[f.FilePath] = f.Status
			}
			if tt.wantRename {
				require.Equal(t, map[string]models.DirScanFileStatus{newPath: models.DirScanFileStatusNoMatch}, byPath)
				require.Equal(t, models.DirScanFileStatusNoMatch, second.byPath[newPath].Status)
				return
			}
			require.Equal(t, map[string]models.DirScanFileStatus{
				oldPath: models.DirScanFileStatusNoMatch,
				newPath: models.DirScanFileStatusPending,
			}, byPath)
			require.Equal(t, models.DirScanFileStatusPending, second.byPath[newPath].Status)
		})
	}
}

// Identity stored in local mode keeps its exact bytes, and never matches a remote file.
func TestStoredIdentityOfALocalRowIsUnchanged(t *testing.T) {
	t.Parallel()

	local := &models.Instance{ID: 1, HasLocalFilesystemAccess: true}
	id := hardlink.FileIDFromBytes(append([]byte{byte(hardlink.KindUnix)}, make([]byte, 24)...))
	idx := &trackedFilesIndex{instance: local, endpoint: endpointTag(local)}
	require.Equal(t, id.Bytes(), idx.storedFileID(id))
	require.Nil(t, idx.storedFileID(hardlink.FileID{}))

	remote := remoteInstance(models.SSHTierExec)
	remoteIdx := &trackedFilesIndex{instance: remote, endpoint: endpointTag(remote)}
	_, ok := remoteIdx.keyOfStored(id.Bytes())
	require.False(t, ok)
	_, ok = idx.keyOfStored(remoteIdx.storedFileID(remoteID(1)))
	require.False(t, ok)
}
