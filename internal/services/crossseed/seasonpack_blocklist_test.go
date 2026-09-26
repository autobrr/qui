// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/stringutils"
)

// blocklistWithPack returns a blocklist store that blocks the torrent's infohash
// on instance 1.
func blocklistWithPack(t *testing.T, torrentData string) *models.CrossSeedBlocklistStore {
	t.Helper()

	ctx := t.Context()
	db := testdb.NewMigratedSQLite(t, "crossseed-blocklist")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://127.0.0.1:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	require.Equal(t, 1, instance.ID)

	torrentBytes, err := base64.StdEncoding.DecodeString(torrentData)
	require.NoError(t, err)
	meta, err := ParseTorrentMetadataWithInfo(torrentBytes)
	require.NoError(t, err)

	store := models.NewCrossSeedBlocklistStore(db)
	_, err = store.Upsert(ctx, &models.CrossSeedBlocklistEntry{InstanceID: instance.ID, InfoHash: meta.HashV1})
	require.NoError(t, err)
	return store
}

type blockedPackFixture struct {
	svc         *Service
	sm          *seasonPackSyncManager
	runs        *stubSeasonPackRunStore
	packName    string
	torrentData string
	treeCalls   int
}

// newBlockedPackFixture builds a season pack that applies in full on instance 1,
// with its infohash on the blocklist when blocked is true.
func newBlockedPackFixture(t *testing.T, blocked bool) *blockedPackFixture {
	t.Helper()

	packName := "Cool.Show.S03.1080p.WEB-DL.x264-GRP"
	packFiles := []string{
		"Cool.Show.S03E01.1080p.WEB-DL.x264-GRP.mkv",
		"Cool.Show.S03E02.1080p.WEB-DL.x264-GRP.mkv",
		"Cool.Show.S03E03.1080p.WEB-DL.x264-GRP.mkv",
		"Cool.Show.S03E04.1080p.WEB-DL.x264-GRP.mkv",
	}
	torrentData := base64.StdEncoding.EncodeToString(createTestTorrent(t, packName, packFiles, 262144))

	inst := &models.Instance{
		ID: 1, Name: "Test", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}
	episodeTorrents := make([]qbt.Torrent, 0, len(packFiles))
	for i, file := range packFiles {
		episodeTorrents = append(episodeTorrents, qbt.Torrent{
			Hash: fmt.Sprintf("e%02d", i+1), Name: strings.TrimSuffix(file, ".mkv"), ContentPath: "/media/" + file, Progress: 1.0,
		})
	}

	baseSM := newMultiFakeSyncManager(
		map[int][]qbt.Torrent{inst.ID: episodeTorrents},
		map[int]*models.Instance{inst.ID: inst},
	)
	baseSM.files = seasonPackEpisodeFiles(t, torrentData, "e01", "e02", "e03", "e04")

	f := &blockedPackFixture{
		sm:          &seasonPackSyncManager{fakeSyncManager: baseSM},
		runs:        &stubSeasonPackRunStore{},
		packName:    packName,
		torrentData: torrentData,
	}
	f.svc = &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
		syncManager:      f.sm,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{
				SeasonPackEnabled:           true,
				SeasonPackAutomationEnabled: true,
				SeasonPackCoverageThreshold: 0.75,
			}, nil
		},
		seasonPackRunStore: f.runs,
		seasonPackLinkCreator: func(context.Context, *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
			f.treeCalls++
			return &fsops.TreeCreateResult{}, nil
		},
	}
	if blocked {
		f.svc.blocklistStore = blocklistWithPack(t, torrentData)
	}
	f.svc.SetBackendPool(fsops.NewPool(f.svc.instanceStore, local.NewBackend()))
	return f
}

func TestApplySeasonPackWebhook_SkipsBlocklistedPack(t *testing.T) {
	for _, blocked := range []bool{true, false} {
		t.Run(fmt.Sprintf("blocked=%v", blocked), func(t *testing.T) {
			f := newBlockedPackFixture(t, blocked)

			resp, err := f.svc.ApplySeasonPackWebhook(t.Context(), &SeasonPackApplyRequest{
				TorrentName: f.packName, TorrentData: f.torrentData, InstanceIDs: []int{1},
			})
			require.NoError(t, err)

			if !blocked {
				require.True(t, resp.Applied, "%+v", resp)
				require.Len(t, f.sm.addCalls, 1)
				return
			}
			require.False(t, resp.Applied)
			require.Equal(t, "blocked", resp.Reason)
			require.Zero(t, f.treeCalls, "a blocked pack must not build a link tree")
			require.Empty(t, f.sm.addCalls)
			require.Len(t, f.runs.runs, 1)
			require.Equal(t, "blocked", f.runs.runs[0].Reason)
			require.Equal(t, "skipped", f.runs.runs[0].Status)
		})
	}
}

func TestCrossSeed_SeasonPackDiversionHonorsBlocklist(t *testing.T) {
	for _, blocked := range []bool{true, false} {
		t.Run(fmt.Sprintf("blocked=%v", blocked), func(t *testing.T) {
			f := newBlockedPackFixture(t, blocked)

			resp, err := f.svc.CrossSeed(t.Context(), &CrossSeedRequest{
				TorrentData:            f.torrentData,
				TargetInstanceIDs:      []int{1},
				FindIndividualEpisodes: true,
			})
			require.NoError(t, err)

			require.Equal(t, !blocked, resp.Success)
			if blocked {
				require.Zero(t, f.treeCalls, "a blocked pack must not build a link tree")
				require.Empty(t, f.sm.addCalls)
			} else {
				require.Len(t, f.sm.addCalls, 1)
			}
		})
	}
}

// addUnblockedInstanceB adds instance 2 with the same episodes as instance 1
// and no blocklist entry.
func addUnblockedInstanceB(t *testing.T, f *blockedPackFixture) {
	t.Helper()
	instB := &models.Instance{
		ID: 2, Name: "B", IsActive: true,
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          t.TempDir(),
	}
	f.svc.instanceStore.(*fakeInstanceStore).instances[instB.ID] = instB
	fsm := f.sm.fakeSyncManager
	torrents := slices.Clone(fsm.all[1])
	fsm.all[instB.ID] = torrents
	fsm.cached[instB.ID] = buildCrossInstanceViews(instB, torrents)
}

func TestApplySeasonPackWebhook_BlockedOnOneInstanceAppliesOnAnother(t *testing.T) {
	f := newBlockedPackFixture(t, true)
	addUnblockedInstanceB(t, f)

	resp, err := f.svc.ApplySeasonPackWebhook(t.Context(), &SeasonPackApplyRequest{
		TorrentName: f.packName, TorrentData: f.torrentData, InstanceIDs: []int{1, 2},
	})
	require.NoError(t, err)
	require.True(t, resp.Applied, "%+v", resp)
	require.Len(t, f.sm.addCalls, 1)
	require.Equal(t, 2, f.sm.addCalls[0].instanceID)
}

func TestApplySeasonPackWebhook_BlockedAndSeededIsAlreadyExists(t *testing.T) {
	f := newBlockedPackFixture(t, true)
	addUnblockedInstanceB(t, f)
	torrentBytes, err := base64.StdEncoding.DecodeString(f.torrentData)
	require.NoError(t, err)
	meta, err := ParseTorrentMetadataWithInfo(torrentBytes)
	require.NoError(t, err)
	fsm := f.sm.fakeSyncManager
	fsm.all[1] = append(fsm.all[1], qbt.Torrent{Hash: meta.HashV1, Name: f.packName, Progress: 1})

	resp, err := f.svc.ApplySeasonPackWebhook(t.Context(), &SeasonPackApplyRequest{
		TorrentName: f.packName, TorrentData: f.torrentData, InstanceIDs: []int{1, 2},
	})
	require.NoError(t, err)
	require.Equal(t, "already_exists", resp.Reason, "%+v", resp)
	require.Empty(t, f.sm.addCalls)
}
