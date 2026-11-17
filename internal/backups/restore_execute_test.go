// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
)

// captureTorrentWriter records the options passed to AddTorrent so restore
// placement decisions can be asserted without a live qBittorrent instance.
type captureTorrentWriter struct {
	stubBackupSyncManager
	addOptions []map[string]string
	postAdd    []string
	deletes    []restoreDelete
	// failBatch makes a delete of more than one torrent fail.
	failBatch bool
}

type restoreDelete struct {
	hashes      []string
	deleteFiles bool
	actor       qbittorrent.DeleteActor
}

func (w *captureTorrentWriter) AddTorrent(_ context.Context, _ int, _ []byte, options map[string]string) (*qbt.TorrentAddResponse, error) {
	captured := make(map[string]string, len(options))
	maps.Copy(captured, options)
	w.addOptions = append(w.addOptions, captured)
	return nil, nil
}

func (w *captureTorrentWriter) SetCategory(_ context.Context, _ int, hashes []string, _ string) error {
	w.postAdd = append(w.postAdd, hashes...)
	return nil
}

func (w *captureTorrentWriter) SetTags(_ context.Context, _ int, hashes []string, _ string) error {
	w.postAdd = append(w.postAdd, hashes...)
	return nil
}
func (w *captureTorrentWriter) ResumeWhenComplete(int, []string, qbittorrent.ResumeWhenCompleteOptions) {
}
func (w *captureTorrentWriter) DeleteTorrents(_ context.Context, _ int, hashes []string, deleteFiles bool, actor qbittorrent.DeleteActor) error {
	w.deletes = append(w.deletes, restoreDelete{hashes: hashes, deleteFiles: deleteFiles, actor: actor})
	if w.failBatch && len(hashes) > 1 {
		return errors.New("batch delete refused")
	}
	return nil
}

func runRestoreAdd(t *testing.T, manifest ManifestItem) (map[string]string, []string) {
	t.Helper()

	dataDir := t.TempDir()
	blobRel := filepath.ToSlash(filepath.Join("backups", "torrents", "ab", "cd", "ef", "blob.torrent"))
	blobAbs := filepath.Join(dataDir, filepath.FromSlash(blobRel))
	require.NoError(t, os.MkdirAll(filepath.Dir(blobAbs), 0o755))
	require.NoError(t, os.WriteFile(blobAbs, []byte("torrent-bytes"), 0o600))

	manifest.TorrentBlob = blobRel
	if manifest.Hash == "" {
		manifest.Hash = "deadbeef"
	}

	writer := &captureTorrentWriter{}
	svc := NewService(nil, writer, Config{WorkerCount: 1, DataDir: dataDir}, nil)
	require.NotNil(t, svc.torrentWriter)

	plan := &RestorePlan{
		Mode:       RestoreModeIncremental,
		InstanceID: 1,
		Torrents:   TorrentPlan{Add: []TorrentSpec{{Manifest: manifest}}},
	}

	applied := RestoreApplied{}
	var errs []RestoreError
	warnings, err := svc.applyTorrentPlan(context.Background(), plan, &applied, &errs, nil, RestoreOptions{SkipHashCheck: true})
	require.NoError(t, err)
	require.Empty(t, errs)
	require.Len(t, writer.addOptions, 1)
	// Metadata rides on the add options; a post-add mutation raced the sync
	// cache and failed for a torrent qBittorrent had accepted (#2259).
	require.Empty(t, writer.postAdd, "added torrents must not get post-add category/tag calls")

	return writer.addOptions[0], warnings
}

func TestApplyTorrentPlanPinsDivergentSavePath(t *testing.T) {
	t.Parallel()

	category := "movies"
	options, warnings := runRestoreAdd(t, ManifestItem{
		Hash:     "deadbeef",
		Name:     "Cross Seed Show",
		Category: &category,
		SavePath: "/data/cross-seed/Show--abc12345",
	})

	require.Equal(t, "false", options["autoTMM"])
	require.Equal(t, "/data/cross-seed/Show--abc12345", options["savepath"])
	require.Len(t, warnings, 1, "pinning should surface a single aggregate host-path warning")
}

func TestApplyTorrentPlanUsesCategoryPlacementWhenNoSavePath(t *testing.T) {
	t.Parallel()

	category := "movies"
	options, warnings := runRestoreAdd(t, ManifestItem{
		Hash:     "deadbeef",
		Name:     "Ordinary Torrent",
		Category: &category,
		Tags:     []string{"tag-a", "tag-b"},
	})

	require.Equal(t, "movies", options["category"], "add must carry the category")
	require.Equal(t, "tag-a,tag-b", options["tags"], "add must carry the tags")
	require.Equal(t, "true", options["autoTMM"], "category-managed torrents must request qB category placement")
	_, hasSavePath := options["savepath"]
	require.False(t, hasSavePath, "category-managed torrents must not pin a redundant save path")
	require.Empty(t, warnings, "category-managed torrents must not produce a pinning warning")
}

func TestApplyTorrentPlanDeletesAsBackupRestore(t *testing.T) {
	t.Parallel()

	restore := qbittorrent.DeleteActor{Kind: qbittorrent.DeleteActorBackupRestore}
	tests := []struct {
		name      string
		failBatch bool
		want      []restoreDelete
	}{
		{
			name: "one delete for the plan",
			want: []restoreDelete{{hashes: []string{"aaaa", "bbbb"}, actor: restore}},
		},
		{
			name:      "one delete for each torrent after the batch fails",
			failBatch: true,
			want: []restoreDelete{
				{hashes: []string{"aaaa", "bbbb"}, actor: restore},
				{hashes: []string{"aaaa"}, actor: restore},
				{hashes: []string{"bbbb"}, actor: restore},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			writer := &captureTorrentWriter{failBatch: tt.failBatch}
			svc := NewService(nil, writer, Config{WorkerCount: 1, DataDir: t.TempDir()}, nil)
			plan := &RestorePlan{
				Mode:       RestoreModeComplete,
				InstanceID: 1,
				Torrents:   TorrentPlan{Delete: []string{"aaaa", "bbbb"}},
			}

			applied := RestoreApplied{}
			var errs []RestoreError
			_, err := svc.applyTorrentPlan(t.Context(), plan, &applied, &errs, nil, RestoreOptions{})
			require.NoError(t, err)
			require.Empty(t, errs)
			require.Equal(t, []string{"aaaa", "bbbb"}, applied.Torrents.Deleted)
			require.Equal(t, tt.want, writer.deletes)
		})
	}
}
