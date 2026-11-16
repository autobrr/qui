// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"
)

// heldTorrents names the torrents that no rule may change: the torrents that
// a Transfer holds, and the helper torrents. Hold returns the reason, or nil
// for a torrent that qui does not hold. ADR 0005.
type heldTorrents interface {
	Hold(instanceID int, hash string) error
}

// SetHeldTorrents is called one time at start-up, before the first run.
func (s *Service) SetHeldTorrents(held heldTorrents) {
	s.held = held
}

// leaveOutHeld returns the torrents that the rules can see. A Transfer claims
// the whole Shared-file group of a torrent, so no torrent that a rule sees
// shares files with a held torrent on the instance. torrents is the list of
// the sync manager: leaveOutHeld copies it before it leaves a torrent out.
func (s *Service) leaveOutHeld(instanceID int, torrents []*qbt.Torrent) []*qbt.Torrent {
	if s.held == nil {
		return torrents
	}
	isHeld := func(torrent *qbt.Torrent) bool { return s.held.Hold(instanceID, torrent.Hash) != nil }
	if !slices.ContainsFunc(torrents, isHeld) {
		return torrents
	}
	return slices.DeleteFunc(slices.Clone(torrents), isHeld)
}
