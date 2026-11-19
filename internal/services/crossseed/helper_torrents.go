// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/qbittorrent"
)

// helperTorrents is the helper registry: it names the torrents that qui added
// for a Transfer or a Share. A tag proves nothing. ADR 0005.
type helperTorrents interface {
	IsHelper(instanceID int, hash string) bool
	// HelperCheck returns nil when the instance has no helper torrent.
	HelperCheck(instanceID int) func(hash string) bool
}

// SetHelperTorrents hides the helper torrents from cross-seed: no search,
// match, or add starts from one, and no completion of one starts a search.
// Call it one time at start-up.
func (s *Service) SetHelperTorrents(helpers helperTorrents) {
	s.helpers = helpers
	s.syncManager = withoutHelpers{qbittorrentSync: s.syncManager, helpers: helpers}
}

func (s *Service) isHelper(instanceID int, hash string) bool {
	return s.helpers != nil && s.helpers.IsHelper(instanceID, hash)
}

// withoutHelpers leaves the helper torrents out of the torrent lists that
// cross-seed reads.
type withoutHelpers struct {
	qbittorrentSync
	helpers helperTorrents
}

func (w withoutHelpers) GetTorrentRefs(ctx context.Context, instanceID int, filter qbittorrent.TorrentRefFilter) ([]*qbt.Torrent, error) {
	torrents, err := w.qbittorrentSync.GetTorrentRefs(ctx, instanceID, filter)
	if isHelper := w.helpers.HelperCheck(instanceID); isHelper != nil {
		torrents = leaveOut(torrents, func(t *qbt.Torrent) bool { return isHelper(t.Hash) })
	}
	return torrents, err
}

func (w withoutHelpers) GetCachedInstanceTorrents(ctx context.Context, instanceID int) ([]qbittorrent.CrossInstanceTorrentView, error) {
	torrents, err := w.qbittorrentSync.GetCachedInstanceTorrents(ctx, instanceID)
	// The row builder badges each helper torrent from this registry. qui
	// registers a helper before it adds the torrent, so no view of one lacks
	// the badge.
	if w.helpers.HelperCheck(instanceID) != nil {
		torrents = leaveOut(torrents, func(t qbittorrent.CrossInstanceTorrentView) bool {
			return t.TorrentView != nil && t.QuiHelper != ""
		})
	}
	return torrents, err
}

// leaveOut returns items without the ones that out names. items can be a
// cached list, so leaveOut copies it before it leaves one out.
func leaveOut[T any](items []T, out func(T) bool) []T {
	if !slices.ContainsFunc(items, out) {
		return items
	}
	return slices.DeleteFunc(slices.Clone(items), out)
}
