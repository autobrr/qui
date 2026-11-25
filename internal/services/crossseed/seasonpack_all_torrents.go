// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"iter"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

func (s *Service) seasonPackAllTorrents(ctx context.Context, instanceID int, alternateTitles map[string][]string) (iter.Seq2[*qbt.Torrent, []string], error) {
	cached, err := s.syncManager.GetCachedInstanceTorrents(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return listedTorrents(cached, alternateTitles), nil
}

func listedTorrents(cached []qbittorrent.CrossInstanceTorrentView, alternateTitles map[string][]string) iter.Seq2[*qbt.Torrent, []string] {
	return func(yield func(*qbt.Torrent, []string) bool) {
		for i := range cached {
			view := cached[i].TorrentView
			if view == nil || view.Torrent == nil {
				continue
			}
			if !yield(view.Torrent, alternateTitles[view.Hash]) {
				return
			}
		}
	}
}

func (s *Service) matchEpisodeCandidatesDetailed(
	cached []qbittorrent.CrossInstanceTorrentView,
	packName string,
	packRelease *rls.Release,
	packEpisodes map[episodeIdentity]packEpisodeOrigin,
	settings *models.CrossSeedAutomationSettings,
	aliasTitles []string,
) map[episodeIdentity][]episodeMatch {
	return s.matchEpisodeTorrents(listedTorrents(cached, nil), packName, packRelease, packEpisodes, settings, aliasTitles)
}
