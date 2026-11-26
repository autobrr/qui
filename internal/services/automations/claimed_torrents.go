// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"
)

type claimedTorrents interface {
	ClaimCheck(instanceID int) func(hash string) bool
}

func (s *Service) SetClaimedTorrents(claimed claimedTorrents) {
	s.claimed = claimed
}

func (s *Service) leaveOutClaimed(instanceID int, torrents []*qbt.Torrent) []*qbt.Torrent {
	if s.claimed == nil {
		return torrents
	}
	isClaimedHash := s.claimed.ClaimCheck(instanceID)
	if isClaimedHash == nil {
		return torrents
	}
	isClaimed := func(torrent *qbt.Torrent) bool { return isClaimedHash(torrent.Hash) }
	if !slices.ContainsFunc(torrents, isClaimed) {
		return torrents
	}
	return slices.DeleteFunc(slices.Clone(torrents), isClaimed)
}
