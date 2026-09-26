// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"errors"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
)

var errNotStubbed = errors.New("orphanscan test: call not stubbed")

// fakeSync stands in for the sync manager and the stores. A nil field fails
// the call, so a test stubs only what its path reads.
type fakeSync struct {
	getAllTorrents       func(ctx context.Context, instanceID int) ([]qbt.Torrent, error)
	getTorrentFilesBatch func(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error)
	getClient            func(ctx context.Context, instanceID int) (healthChecker, error)
	listInstances        func(ctx context.Context) ([]*models.Instance, error)
	getLastCompletedRun  func(ctx context.Context, instanceID int) (*models.OrphanScanRun, error)
	getAppPreferences    func(ctx context.Context, instanceID int) (qbt.AppPreferences, error)
	getCategories        func(ctx context.Context, instanceID int) (map[string]qbt.Category, error)
	subcategoriesEnabled func(ctx context.Context, instanceID int) (bool, error)
}

// stubSync installs a fakeSync on svc the first time it is called and returns
// it. A service built with a store keeps reading last runs from that store.
func stubSync(svc *Service) *fakeSync {
	if f, ok := svc.sync.(*fakeSync); ok {
		return f
	}
	f := &fakeSync{}
	svc.sync, svc.clients, svc.instances = f, f, f
	if svc.store == nil {
		svc.lastRuns = f
	}
	return f
}

func (f *fakeSync) GetTorrentsFresh(ctx context.Context, instanceID int, _ qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	if f.getAllTorrents == nil {
		return nil, errNotStubbed
	}
	return f.getAllTorrents(ctx, instanceID)
}

func (f *fakeSync) GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	if f.getTorrentFilesBatch == nil {
		return nil, errNotStubbed
	}
	return f.getTorrentFilesBatch(ctx, instanceID, hashes)
}

func (f *fakeSync) Client(ctx context.Context, instanceID int) (healthChecker, error) {
	if f.getClient == nil {
		return nil, errNotStubbed
	}
	return f.getClient(ctx, instanceID)
}

func (f *fakeSync) List(ctx context.Context) ([]*models.Instance, error) {
	if f.listInstances == nil {
		return nil, errNotStubbed
	}
	return f.listInstances(ctx)
}

func (f *fakeSync) GetLastCompletedRun(ctx context.Context, instanceID int) (*models.OrphanScanRun, error) {
	if f.getLastCompletedRun == nil {
		return nil, errNotStubbed
	}
	return f.getLastCompletedRun(ctx, instanceID)
}

func (f *fakeSync) GetAppPreferences(ctx context.Context, instanceID int) (qbt.AppPreferences, error) {
	if f.getAppPreferences == nil {
		return qbt.AppPreferences{}, errNotStubbed
	}
	return f.getAppPreferences(ctx, instanceID)
}

func (f *fakeSync) GetCategories(ctx context.Context, instanceID int) (map[string]qbt.Category, error) {
	if f.getCategories == nil {
		return nil, errNotStubbed
	}
	return f.getCategories(ctx, instanceID)
}

func (f *fakeSync) SubcategoriesEnabled(ctx context.Context, instanceID int) (bool, error) {
	if f.subcategoriesEnabled == nil {
		return false, errNotStubbed
	}
	return f.subcategoriesEnabled(ctx, instanceID)
}
