// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/qbittorrent"
)

const routedRefsMaxAge = 2 * time.Second

type routedFileOwners struct{ *qbittorrent.SyncManager }

func (r routedFileOwners) GetAllTorrentRefs(ctx context.Context, instanceID int) ([]*qbt.Torrent, error) {
	return r.GetTorrentRefsWithin(ctx, instanceID, qbittorrent.TorrentRefFilter{WithHelperTorrents: true}, routedRefsMaxAge)
}
