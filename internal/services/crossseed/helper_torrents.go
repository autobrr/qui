// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

type helperTorrents interface {
	IsHelper(instanceID int, hash string) bool
}

func (s *Service) SetHelperTorrents(helpers helperTorrents) {
	s.helpers = helpers
}

func (s *Service) isHelper(instanceID int, hash string) bool {
	return s.helpers != nil && s.helpers.IsHelper(instanceID, hash)
}
