// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"context"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"
)

// helperTorrents is the helper registry: it names the torrents that qui added
// for a Transfer or a Share. A tag proves nothing.
type helperTorrents interface {
	// HelperCheck returns nil when the instance has no helper torrent.
	HelperCheck(instanceID int) func(hash string) bool
}

// SetHelperTorrents leaves the helper torrents out of each backup and each
// restore: no backup holds one, and no restore changes or removes one. Call
// it one time at start-up.
func (s *Service) SetHelperTorrents(helpers helperTorrents) {
	s.reader = withoutHelpers{backupReader: s.reader, helpers: helpers}
}

type withoutHelpers struct {
	backupReader
	helpers helperTorrents
}

func (w withoutHelpers) GetAllTorrentRefs(ctx context.Context, instanceID int) ([]*qbt.Torrent, error) {
	torrents, err := w.backupReader.GetAllTorrentRefs(ctx, instanceID)
	if isHelper := w.helpers.HelperCheck(instanceID); isHelper != nil {
		torrents = slices.DeleteFunc(torrents, func(t *qbt.Torrent) bool { return isHelper(t.Hash) })
	}
	return torrents, err
}
