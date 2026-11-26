// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"sync"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
	"github.com/autobrr/qui/internal/testutil"
)

type heldFakeSyncs struct {
	qbittorrentSync

	svc    *Service
	mu     sync.Mutex
	owners map[int]*syncowner.Owner
	sinks  []qbittorrent.SyncEventSink
	rid    int64
}

func startHeldTitlesOnFake(ctx context.Context, svc *Service, sources ...AlternateTitleSource) {
	syncs := &heldFakeSyncs{qbittorrentSync: svc.syncManager, svc: svc, owners: map[int]*syncowner.Owner{}}
	svc.syncManager = syncs
	svc.StartHeldTitles(ctx, syncs, syncs, sources)
	instances, _ := svc.instanceStore.List(ctx)
	for _, instance := range instances {
		_, _ = syncs.GetSyncOwner(ctx, instance.ID)
	}
}

func checkOnHeld(t *testing.T, svc *Service, req *WebhookCheckRequest) (*WebhookCheckResponse, error) {
	t.Helper()
	if svc.heldTitles == nil {
		startHeldTitlesOnFake(t.Context(), svc)
	}
	return svc.CheckWebhook(t.Context(), req)
}

func (f *heldFakeSyncs) owner(instanceID int) *syncowner.Owner {
	f.mu.Lock()
	defer f.mu.Unlock()
	if owner, ok := f.owners[instanceID]; ok {
		return owner
	}
	client := qbt.NewClient(qbt.Config{Host: "http://qbt.example.invalid"}).
		WithHTTPClient(&http.Client{Transport: testutil.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
			return f.mainData(r.Context(), instanceID)
		})})
	options := qbt.DefaultSyncOptions()
	options.OnSyncUpdate = func(update qbt.SyncUpdate) {
		f.mu.Lock()
		sinks := slices.Clone(f.sinks)
		f.mu.Unlock()
		for _, sink := range sinks {
			sink.HandleSyncUpdate(instanceID, update)
		}
	}
	owner := syncowner.New(client, options)
	f.owners[instanceID] = owner
	return owner
}

func (f *heldFakeSyncs) sync(ctx context.Context, instanceID int) error {
	return f.owner(instanceID).Sync(ctx)
}

func (f *heldFakeSyncs) mainData(ctx context.Context, instanceID int) (*http.Response, error) {
	torrents := map[string]qbt.Torrent{}
	if views, err := f.qbittorrentSync.GetCachedInstanceTorrents(ctx, instanceID); err == nil {
		for _, view := range views {
			torrents[view.Torrent.Hash] = *view.Torrent
		}
	}
	f.mu.Lock()
	f.rid++
	rid := f.rid
	f.mu.Unlock()
	rec := httptest.NewRecorder()
	err := json.MarshalWrite(rec, heldMainData{Rid: rid, FullUpdate: true, Torrents: torrents})
	return rec.Result(), err
}

func (f *heldFakeSyncs) GetSyncOwner(ctx context.Context, instanceID int) (*syncowner.Owner, error) {
	owner := f.owner(instanceID)
	if err := owner.Sync(ctx); err != nil {
		return nil, err
	}
	for f.svc.heldTitles.buildsPending() {
		runtime.Gosched()
	}
	return owner, nil
}

func (f *heldFakeSyncs) AddSyncEventSink(sink qbittorrent.SyncEventSink) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sinks = append(f.sinks, sink)
}

func (f *heldFakeSyncs) InstanceSnapshot(_ context.Context, instanceID int) *qbt.SyncSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	if owner, ok := f.owners[instanceID]; ok {
		return owner.SnapshotUnchecked()
	}
	return nil
}

func (f *heldFakeSyncs) ListSnapshot(_ int, snapshot *qbt.SyncSnapshot) []*qbt.Torrent {
	return snapshot.Torrents()
}

func (f *heldFakeSyncs) ListedLookup(_ int, snapshot *qbt.SyncSnapshot) func(hash string) (*qbt.Torrent, bool) {
	return snapshot.Torrent
}

func (f *heldFakeSyncs) CopyGroups() *qbittorrent.CopyGroups {
	return nil
}
