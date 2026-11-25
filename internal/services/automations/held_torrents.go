// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"
)

type heldTorrents interface {
	HoldCheck(instanceID int) func(hash string) bool
}

func (s *Service) SetHeldTorrents(held heldTorrents) {
	s.held = held
}

func (s *Service) leaveOutHeld(instanceID int, torrents []*qbt.Torrent) []*qbt.Torrent {
	if s.held == nil {
		return torrents
	}
	isHeldHash := s.held.HoldCheck(instanceID)
	if isHeldHash == nil {
		return torrents
	}
	isHeld := func(torrent *qbt.Torrent) bool { return isHeldHash(torrent.Hash) }
	if !slices.ContainsFunc(torrents, isHeld) {
		return torrents
	}
	return slices.DeleteFunc(slices.Clone(torrents), isHeld)
}
