// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/qbittorrent"
)

type helperTorrents interface {
	IsHelper(instanceID int, hash string) bool
	HelperCheck(instanceID int) func(hash string) bool
}

func (s *Service) SetHelperTorrents(helpers helperTorrents) {
	s.helpers = helpers
	s.syncManager = withoutHelpers{qbittorrentSync: s.syncManager, helpers: helpers}
}

func (s *Service) isHelper(instanceID int, hash string) bool {
	return s.helpers != nil && s.helpers.IsHelper(instanceID, hash)
}

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
	if w.helpers.HelperCheck(instanceID) != nil {
		torrents = leaveOut(torrents, func(t qbittorrent.CrossInstanceTorrentView) bool {
			return t.TorrentView != nil && t.QuiHelper != ""
		})
	}
	return torrents, err
}

func leaveOut[T any](items []T, out func(T) bool) []T {
	if !slices.ContainsFunc(items, out) {
		return items
	}
	return slices.DeleteFunc(slices.Clone(items), out)
}
