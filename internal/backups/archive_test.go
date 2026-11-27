// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
	"github.com/ulikunitz/xz"

	"github.com/autobrr/qui/internal/models"
)

type archiveEntry struct {
	data    []byte
	modTime time.Time
}

type archiveFixture struct {
	service *Service
	run     *models.BackupRun
	dataDir string
}

func newArchiveFixture(t *testing.T, blobs map[string][]byte) archiveFixture {
	t.Helper()
	ctx := t.Context()
	db := setupTestBackupDB(t)
	dataDir := t.TempDir()
	store := models.NewBackupStore(db)
	service := NewService(store, nil, Config{DataDir: dataDir, WorkerCount: 1}, nil)
	instanceID := insertTestInstance(t, db, "archive")

	run := &models.BackupRun{InstanceID: instanceID, Kind: models.BackupRunKindManual, Status: models.BackupRunStatusSuccess, RequestedBy: "test", RequestedAt: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)}
	require.NoError(t, store.CreateRun(ctx, run))

	items := []models.BackupItem{
		{RunID: run.ID, TorrentHash: "aaaa", Name: "Example.Show.S01.1080p-GRP", SizeBytes: 10, ArchiveRelPath: new("Example.Show.S01.1080p-GRP.torrent"), TorrentBlobPath: new("torrents/aa/aa/aaaa.torrent")},
		{RunID: run.ID, TorrentHash: "bbbb", Name: "Missing.Blob-GRP", SizeBytes: 20, ArchiveRelPath: new("Missing.Blob-GRP.torrent"), TorrentBlobPath: new("torrents/bb/bb/bbbb.torrent")},
		{RunID: run.ID, TorrentHash: "cccc", Name: "No.Blob-GRP", SizeBytes: 30},
	}
	require.NoError(t, store.InsertItems(ctx, run.ID, items))

	for rel, data := range blobs {
		abs := filepath.Join(dataDir, "backups", filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, data, 0o600))
	}

	run, err := store.GetRun(ctx, run.ID)
	require.NoError(t, err)
	return archiveFixture{service: service, run: run, dataDir: dataDir}
}

func readZipEntries(t *testing.T, body []byte) map[string]archiveEntry {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	require.NoError(t, err)
	entries := make(map[string]archiveEntry)
	for _, f := range zr.File {
		rc, err := f.Open()
		require.NoError(t, err)
		data, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		entries[f.Name] = archiveEntry{data: data, modTime: f.Modified}
	}
	return entries
}

func readTarEntries(t *testing.T, r io.Reader) map[string]archiveEntry {
	t.Helper()
	tr := tar.NewReader(r)
	entries := make(map[string]archiveEntry)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return entries
		}
		require.NoError(t, err)
		data, err := io.ReadAll(tr)
		require.NoError(t, err)
		entries[header.Name] = archiveEntry{data: data, modTime: header.ModTime}
	}
}

func TestArchiveReadsBackInEachFormat(t *testing.T) {
	blob := []byte("d4:infod4:name4:abcdee")
	fx := newArchiveFixture(t, map[string][]byte{"torrents/aa/aa/aaaa.torrent": blob})

	archive, err := fx.service.LoadArchive(t.Context(), fx.run)
	require.NoError(t, err)

	formats := []struct {
		name string
		read func(t *testing.T, body []byte) map[string]archiveEntry
	}{
		{"zip", readZipEntries},
		{"tar", func(t *testing.T, body []byte) map[string]archiveEntry {
			return readTarEntries(t, bytes.NewReader(body))
		}},
		{"tar.gz", func(t *testing.T, body []byte) map[string]archiveEntry {
			gr, err := gzip.NewReader(bytes.NewReader(body))
			require.NoError(t, err)
			return readTarEntries(t, gr)
		}},
		{"tar.zst", func(t *testing.T, body []byte) map[string]archiveEntry {
			zr, err := zstd.NewReader(bytes.NewReader(body))
			require.NoError(t, err)
			defer zr.Close()
			return readTarEntries(t, zr)
		}},
		{"tar.br", func(t *testing.T, body []byte) map[string]archiveEntry {
			return readTarEntries(t, brotli.NewReader(bytes.NewReader(body)))
		}},
		{"tar.xz", func(t *testing.T, body []byte) map[string]archiveEntry {
			xr, err := xz.NewReader(bytes.NewReader(body))
			require.NoError(t, err)
			return readTarEntries(t, xr)
		}},
	}

	for _, tc := range formats {
		t.Run(tc.name, func(t *testing.T) {
			format, err := ParseArchiveFormat(tc.name)
			require.NoError(t, err)

			var body bytes.Buffer
			require.NoError(t, archive.Write(&body, format))

			entries := tc.read(t, body.Bytes())
			require.Len(t, entries, 2)
			require.Equal(t, blob, entries["Example.Show.S01.1080p-GRP.torrent"].data)

			var manifest Manifest
			require.NoError(t, json.Unmarshal(entries["manifest.json"].data, &manifest))
			require.Equal(t, 3, manifest.TorrentCount)
			require.Len(t, manifest.Items, 3)
			require.Equal(t, "torrents/aa/aa/aaaa.torrent", manifest.Items[0].TorrentBlob)
			require.Contains(t, string(entries["manifest.json"].data), "\n  \"instanceId\": ")

			for name, entry := range entries {
				require.True(t, entry.modTime.Equal(fx.run.RequestedAt), "%s modified %s, want %s", name, entry.modTime, fx.run.RequestedAt)
			}
		})
	}
}

func TestArchiveWriteStreamsTheTorrentFiles(t *testing.T) {
	allocatedFor := func(size int) uint64 {
		blob := bytes.Repeat([]byte{0x5a}, size)
		fx := newArchiveFixture(t, map[string][]byte{"torrents/aa/aa/aaaa.torrent": blob})
		archive, err := fx.service.LoadArchive(t.Context(), fx.run)
		require.NoError(t, err)
		format, err := ParseArchiveFormat("tar")
		require.NoError(t, err)

		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		require.NoError(t, archive.Write(io.Discard, format))
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}

	small := allocatedFor(1 << 20)
	large := allocatedFor(32 << 20)
	require.Less(t, large, small+(4<<20), "writing a 32 MiB torrent file allocated %d bytes, a 1 MiB one %d", large, small)
}
