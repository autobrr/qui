// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"errors"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
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
	instanceFolders      func(ctx context.Context, instanceID int) (qbittorrent.InstanceFolders, error)
	// hostKeyPins holds each instance's pinned host key by ID. A missing ID is unpinned.
	hostKeyPins map[int][]byte
}

// folders stubs the Instance folders with a ready value.
func (f *fakeSync) folders(folders qbittorrent.InstanceFolders) {
	f.instanceFolders = func(context.Context, int) (qbittorrent.InstanceFolders, error) { return folders, nil }
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

// Get reads the listInstances stub, so a test flips an instance's mode in one place.
func (f *fakeSync) Get(ctx context.Context, id int) (*models.Instance, error) {
	instances, err := f.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, inst := range instances {
		if inst != nil && inst.ID == id {
			return inst, nil
		}
	}
	return nil, models.ErrInstanceNotFound
}

func (f *fakeSync) GetLastCompletedRun(ctx context.Context, instanceID int) (*models.OrphanScanRun, error) {
	if f.getLastCompletedRun == nil {
		return nil, errNotStubbed
	}
	return f.getLastCompletedRun(ctx, instanceID)
}

func (f *fakeSync) InstanceFolders(ctx context.Context, _ fsops.PathDialect, instanceID int) (qbittorrent.InstanceFolders, error) {
	if f.instanceFolders == nil {
		return qbittorrent.InstanceFolders{}, errNotStubbed
	}
	return f.instanceFolders(ctx, instanceID)
}

func (f *fakeSync) GetHostKeyPin(inst *models.Instance) ([]byte, error) {
	if pin, ok := f.hostKeyPins[inst.ID]; ok {
		return pin, nil
	}
	return nil, models.ErrSSHHostKeyNotPinned
}
