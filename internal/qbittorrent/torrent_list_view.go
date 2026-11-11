// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
)

// TorrentListView is one Torrent view: what a reader of the torrent list asks
// for. The zero value asks for the rows of instance 0 with no Sidebar counts,
// no Copy groups, a read that can start a sync, and hidden passkeys.
type TorrentListView struct {
	// InstanceID is the one instance of the view. A Unified view ignores it.
	InstanceID int
	// Unified reads every active instance, or only the active instances in
	// InstanceIDs when that list is not empty.
	Unified     bool
	InstanceIDs []int

	// Limit of 0 or less means no limit.
	Limit   int
	Offset  int
	Sort    string
	Order   string
	Search  string
	Filters FilterOptions

	// NoRows gives the totals of the view without its rows, and does no sort.
	NoRows        bool
	SidebarCounts bool
	CopyGroups    bool
	// UniqueSize adds the Unique size to the stats of a single-instance view.
	UniqueSize bool
	// CacheOnly reads the sync cache and starts no sync.
	CacheOnly bool
	// FullPasskeyAccess keeps the tracker passkeys in the rows and in the value
	// that a filter expression reads. Without it, each tracker URL is its origin
	// and the expression sees no magnet link.
	FullPasskeyAccess bool
}

// Validate rejects a view without rows that asks for Copy groups, a Unified
// view without rows, whose total counts rows, and a Unified view that asks for
// the Unique size.
func (v *TorrentListView) Validate() error {
	switch {
	case v.NoRows && v.CopyGroups:
		return errors.New("summary has no rows to group")
	case v.NoRows && v.Unified:
		return errors.New("summary needs a single instance")
	case v.UniqueSize && v.Unified:
		return errors.New("the Unique size needs a single instance")
	}
	return nil
}

// ListTorrents returns what view asks for. It returns the error of
// [TorrentListView.Validate] for a view that cannot be read.
func (sm *SyncManager) ListTorrents(ctx context.Context, view TorrentListView) (*TorrentResponse, error) {
	if err := view.Validate(); err != nil {
		return nil, err
	}

	var response *TorrentResponse
	var err error
	if view.Unified {
		response, err = sm.listUnifiedTorrents(ctx, &view)
	} else {
		response, err = sm.listInstanceTorrents(ctx, &view)
	}
	if err != nil {
		return nil, err
	}

	if !view.FullPasskeyAccess {
		response = response.withoutTrackerPasskeys()
	}
	return response, nil
}
