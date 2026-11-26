// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
)

type TorrentListView struct {
	InstanceID  int
	Unified     bool
	InstanceIDs []int

	Limit   int
	Offset  int
	Sort    string
	Order   string
	Search  string
	Filters FilterOptions

	NoRows            bool
	SidebarCounts     bool
	CopyGroups        bool
	UniqueSize        bool
	CacheOnly         bool
	FullPasskeyAccess bool

	WithHelperTorrents bool
}

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
