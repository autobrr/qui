// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"testing/synctest"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/externalprograms"
	"github.com/autobrr/qui/pkg/infohash"
	"github.com/autobrr/qui/pkg/sharedextents"
)

const testProgramID = 7

type programRecorder struct {
	mu   sync.Mutex
	runs []string
}

func (r *programRecorder) Execute(_ context.Context, req externalprograms.ExecuteRequest) externalprograms.ExecuteResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs = append(r.runs, programRun(req.ProgramID, req.InstanceID, req.Torrent.Hash))
	return externalprograms.SuccessResult("ran")
}

func (r *programRecorder) ran() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.runs)
}

func programRun(programID, instanceID int, hash string) string {
	return fmt.Sprintf("program %d on instance %d for %s", programID, instanceID, hash)
}

type listsAddedTorrent struct {
	qbittorrentSync
	svc     *Service
	torrent qbt.Torrent
}

func (l *listsAddedTorrent) listed(hashes []infohash.Hash) bool {
	adds := l.svc.adder.sync.(*adderFake)
	return len(adds.added) > 0 && slices.ContainsFunc(hashes, func(h infohash.Hash) bool { return h.Is(l.torrent.Hash) })
}

func (l *listsAddedTorrent) GetTorrentRefs(ctx context.Context, instanceID int, filter qbittorrent.TorrentRefFilter) ([]*qbt.Torrent, error) {
	if l.listed(filter.Hashes) {
		return testTorrentRefs([]qbt.Torrent{l.torrent}), nil
	}
	return l.qbittorrentSync.GetTorrentRefs(ctx, instanceID, filter)
}

func (l *listsAddedTorrent) HasTorrentByAnyHash(ctx context.Context, instanceID int, hashes []infohash.Hash) (*qbt.Torrent, bool, error) {
	if l.listed(hashes) {
		torrent := l.torrent
		return &torrent, true, nil
	}
	return l.qbittorrentSync.HasTorrentByAnyHash(ctx, instanceID, hashes)
}

func runProgramAfterAdd(svc *Service, added qbt.Torrent) *programRecorder {
	recorder := &programRecorder{}
	svc.programRunner = recorder
	load := svc.automationSettingsLoader
	svc.automationSettingsLoader = func(ctx context.Context) (*models.CrossSeedAutomationSettings, error) {
		settings := models.DefaultCrossSeedAutomationSettings()
		if load != nil {
			loaded, err := load(ctx)
			if err != nil {
				return nil, err
			}
			settings = new(*loaded)
		}
		settings.RunExternalProgramID = new(testProgramID)
		return settings, nil
	}
	svc.syncManager = &listsAddedTorrent{qbittorrentSync: svc.syncManager, svc: svc, torrent: added}
	return recorder
}

type countingLookupSync struct {
	*rootlessSavePathSyncManager
	lookups int
}

func (c *countingLookupSync) GetTorrentRefs(context.Context, int, qbittorrent.TorrentRefFilter) ([]*qbt.Torrent, error) {
	c.lookups++
	return nil, nil
}

func (c *countingLookupSync) HasTorrentByAnyHash(context.Context, int, []infohash.Hash) (*qbt.Torrent, bool, error) {
	c.lookups++
	return nil, false, nil
}

func TestNewServiceWithoutProgramServiceRunsNoProgram(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, nil)
	sync := &countingLookupSync{rootlessSavePathSyncManager: &rootlessSavePathSyncManager{}}
	svc.syncManager = sync
	svc.automationSettingsLoader = func(context.Context) (*models.CrossSeedAutomationSettings, error) {
		settings := models.DefaultCrossSeedAutomationSettings()
		settings.RunExternalProgramID = new(testProgramID)
		return settings, nil
	}

	synctest.Test(t, func(t *testing.T) {
		svc.RunExternalProgram(t.Context(), 1, []string{hash123})
		synctest.Wait()

		require.Zero(t, sync.lookups, "no program service means no wait for the added torrent")
	})
}

func TestNewServiceFillsMediaInfoReadAndPlatformReflinkCheck(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, nil)

	require.NotNil(t, svc.analyzeMediaFile)
	require.Equal(t, sharedextents.Supported, svc.filesShareAllocation != nil, "the reflink check is there exactly when the platform has one")
}
