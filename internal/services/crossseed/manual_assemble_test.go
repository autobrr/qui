// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/stringutils"
)

func TestManualAssembleCheckAndApply(t *testing.T) {
	const packName = "Cedar.Harbor.S02.1080p.WEB.x264-PINE"
	for _, mode := range []string{"hardlink", "reflink"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			inst := &models.Instance{
				ID: 1, Name: "local", HasLocalFilesystemAccess: true,
				UseHardlinks: mode == "hardlink", UseReflinks: mode == "reflink",
				HardlinkBaseDir: filepath.Join(root, "links"), HardlinkDirPreset: "by-instance",
			}
			contents := map[string][]byte{}
			files := map[string]qbt.TorrentFiles{}
			var torrents []qbt.Torrent
			for i := 1; i <= 4; i++ {
				packFile := fmt.Sprintf("Cedar.Harbor.S02E%02d.1080p.WEB.x264-PINE.mkv", i)
				// Title, source, and numbering differ from the uploaded pack.
				localName := fmt.Sprintf("Maple.Coast - %02d - 720p HDTV x265-OAK", i)
				fileName := localName + ".mkv"
				data := bytes.Repeat([]byte{byte(i)}, i*64)
				contents[packFile] = data
				hash := fmt.Sprintf("e%02d", i)
				files[hash] = qbt.TorrentFiles{{Name: fileName, Size: int64(len(data))}}
				source := filepath.Join(root, fileName)
				require.NoError(t, os.WriteFile(source, data, 0o600))
				torrents = append(torrents, qbt.Torrent{Hash: hash, Name: localName, ContentPath: source, Progress: 1, Category: "excluded"})
			}
			torrentData := base64.StdEncoding.EncodeToString(buildMultiFileTorrent(t, packName, 64, contents))
			sm := &seasonPackSyncManager{fakeSyncManager: newFakeSyncManager(inst, torrents, files)}
			runs := &stubSeasonPackRunStore{}
			svc := &Service{
				instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{1: inst}},
				syncManager:   sm, releaseCache: NewReleaseCache(), stringNormalizer: stringutils.DefaultNormalizer,
				seasonPackRunStore: runs, recheckResumeChan: make(chan *pendingResume, 1),
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return &models.CrossSeedAutomationSettings{
						SeasonPackCoverageThreshold: 1, SkipRecheck: true,
						SeasonPackCategory: "automatic", SeasonPackTags: []string{"automatic"},
						WebhookSourceCategories: []string{"other"},
					}, nil
				},
			}
			svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
			if mode == "reflink" {
				// Exercise the same tree on hosts without reflink support.
				svc.seasonPackLinkCreator = local.NewBackend().HardlinkTree
			}
			req := &ManualAssembleRequest{InstanceID: 1, TorrentData: torrentData, TargetHashes: []string{"e01", "e02", "e03"}, Category: "chosen", Tags: []string{"chosen"}}
			preview, err := svc.CheckManualAssemble(t.Context(), req)
			require.NoError(t, err)
			require.True(t, preview.Ready, "%+v", preview)
			require.Equal(t, 3, preview.MatchedEpisodes)
			require.Equal(t, 4, preview.TotalEpisodes)
			require.InDelta(t, 0.75, preview.Coverage, 0.00001)
			require.Equal(t, int64(256), preview.MissingBytes)
			require.Equal(t, filepath.Join(inst.HardlinkBaseDir, inst.Name), preview.Destination)
			require.Equal(t, "automatic", preview.DefaultCategory)
			_, err = os.Stat(inst.HardlinkBaseDir)
			require.True(t, os.IsNotExist(err), "check must not create a tree")
			require.Empty(t, sm.addCalls)
			require.Empty(t, runs.runs)

			unalignedReq := *req
			unalignedReq.TorrentData = base64.StdEncoding.EncodeToString(buildMultiFileTorrent(t, packName, 1024, contents))
			boundaryPreview, err := svc.CheckManualAssemble(t.Context(), &unalignedReq)
			require.NoError(t, err)
			if mode == "hardlink" {
				require.False(t, boundaryPreview.Ready)
				require.Equal(t, "unsafe_piece_boundary", boundaryPreview.Reason)
				require.Contains(t, boundaryPreview.Message, "unsafe piece boundary with pending files")
			} else {
				require.True(t, boundaryPreview.Ready)
			}

			// A moved file must not discard the two remaining selected episodes.
			require.NoError(t, os.Remove(torrents[1].ContentPath))
			applied, err := svc.ApplyManualAssemble(t.Context(), req)
			require.NoError(t, err)
			require.True(t, applied.Applied, "%+v", applied)
			require.Equal(t, 2, applied.MatchedEpisodes)
			require.Equal(t, "local_file_unavailable", applied.Targets[1].Reason)
			require.Equal(t, "e02", applied.Targets[1].Hash)
			require.Len(t, sm.addCalls, 1)
			require.Equal(t, "false", sm.addCalls[0].options["skip_checking"])
			require.Equal(t, "true", sm.addCalls[0].options["paused"])
			require.Equal(t, "true", sm.addCalls[0].options["stopped"])
			require.Equal(t, "chosen", sm.addCalls[0].options["category"])
			require.Equal(t, "chosen", sm.addCalls[0].options["tags"])
			require.Equal(t, "recheck", sm.bulkCalls[0].action)
			require.InDelta(t, 256.0/640.0*seasonPackResumeSlack, (<-svc.recheckResumeChan).threshold, 0.00001)
			require.Len(t, runs.runs, 1)
			require.Equal(t, 2, runs.runs[0].MatchedEpisodes)
			require.InDelta(t, 0.5, runs.runs[0].Coverage, 0.00001)
			require.Equal(t, mode, runs.runs[0].LinkMode)
			linked, err := os.ReadFile(filepath.Join(preview.Destination, packName, "Cedar.Harbor.S02E03.1080p.WEB.x264-PINE.mkv"))
			require.NoError(t, err)
			require.Equal(t, contents["Cedar.Harbor.S02E03.1080p.WEB.x264-PINE.mkv"], linked)
		})
	}
}

func TestManualAssembleSuggestionsAndValidation(t *testing.T) {
	fix := alignedSeasonPackFixture(t)
	root := t.TempDir()
	inst := &models.Instance{ID: 1, Name: "local", HasLocalFilesystemAccess: true, UseHardlinks: true, HardlinkBaseDir: filepath.Join(root, "links")}
	torrents := make([]qbt.Torrent, 0, len(fix.packFiles)+1)
	for i, file := range fix.packFiles {
		source := filepath.Join(root, file)
		require.NoError(t, os.WriteFile(source, bytes.Repeat([]byte("x"), 64), 0o600))
		torrents = append(torrents, qbt.Torrent{Hash: fmt.Sprintf("e%02d", i+1), Name: strings.TrimSuffix(file, ".mkv"), ContentPath: source, Progress: 1, Size: 64})
	}
	files := seasonPackEpisodeFiles(t, fix.torrentData, "e01", "e02", "e03", "e04")
	sm := &seasonPackSyncManager{fakeSyncManager: newFakeSyncManager(inst, torrents, files)}
	svc := manualMatchTestService(inst, nil, nil)
	svc.stringNormalizer = stringutils.DefaultNormalizer
	svc.syncManager = sm
	svc.automationSettingsLoader = defaultSettings(false, 1)
	svc.recheckResumeChan = make(chan *pendingResume, 1)
	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, local.NewBackend()))
	req := &ManualAssembleRequest{InstanceID: 1, TorrentData: fix.torrentData}
	preview, err := svc.CheckManualAssemble(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, preview.Targets, 4)
	require.True(t, preview.Ready)

	req.TargetHashes = []string{"e01", "e02", "e03", "e04"}
	result, err := svc.ApplyManualAssemble(t.Context(), req)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.Zero(t, result.MissingBytes)
	require.Equal(t, "false", sm.addCalls[0].options["skip_checking"])
	require.Equal(t, "true", sm.addCalls[0].options["paused"])
	require.Len(t, sm.bulkCalls, 1)
	require.InDelta(t, seasonPackResumeSlack, (<-svc.recheckResumeChan).threshold, 0.00001)

	sm.files["e02"][0].Size++
	req.TargetHashes = []string{"e01", "e02", "another-instance-hash"}
	preview, err = svc.CheckManualAssemble(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, 1, preview.MatchedEpisodes)
	require.Equal(t, "size_mismatch", preview.Targets[1].Reason)
	require.Equal(t, "target_not_found", preview.Targets[2].Reason)
	_, err = svc.ApplyManualAssemble(t.Context(), &ManualAssembleRequest{InstanceID: 1, TorrentData: fix.torrentData, TargetHashes: []string{"e01", " E01 "}})
	require.ErrorIs(t, err, ErrInvalidRequest)

	data, err := base64.StdEncoding.DecodeString(fix.torrentData)
	require.NoError(t, err)
	meta, err := ParseTorrentMetadataWithInfo(data)
	require.NoError(t, err)
	full := qbt.Torrent{Hash: "full", Name: fix.packName, Progress: 1, Size: meta.Info.TotalLength()}
	sm.fakeSyncManager = newFakeSyncManager(inst, append(torrents, full), files)
	sm.files["full"] = meta.Files
	req.TargetHashes = nil
	preview, err = svc.CheckManualAssemble(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []ManualAssembleTarget{{Hash: "full", Name: fix.packName}}, preview.Targets)

	withSidecar := make(map[string][]byte, len(fix.packFiles)+1)
	for _, file := range fix.packFiles {
		withSidecar[file] = bytes.Repeat([]byte("x"), 64)
	}
	withSidecar["release.nfo"] = []byte("info")
	req.TorrentData = base64.StdEncoding.EncodeToString(buildMultiFileTorrent(t, fix.packName, 64, withSidecar))
	preview, err = svc.CheckManualAssemble(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []ManualAssembleTarget{{Hash: "full", Name: fix.packName}}, preview.Targets)

	inst.UseHardlinks = false
	req.TargetHashes = []string{"e01", "e03"}
	preview, err = svc.CheckManualAssemble(t.Context(), req)
	require.NoError(t, err)
	require.False(t, preview.Ready)
	require.Equal(t, "no_link_mode", preview.Reason)
}
