// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"fmt"
	"sync"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

type completionPickWarnings struct {
	mu     sync.Mutex
	warned map[int]string
}

func (w *completionPickWarnings) first(instanceID int, state string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.warned[instanceID] == state {
		return false
	}
	if w.warned == nil {
		w.warned = make(map[int]string)
	}
	w.warned[instanceID] = state
	return true
}

func (w *completionPickWarnings) clear(instanceID int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.warned, instanceID)
}

func (s *Service) logCompletionPickWithoutKey(instanceID int, torrent *qbt.Torrent, pick models.SitePick, clients *gazelleClientSet) {
	state := fmt.Sprintf("%v|%v|%t", pick.Clean(), clients.hosts(), clients.disabled())
	event := log.Trace()
	if s.completionPickWarnings.first(instanceID, state) {
		event = log.Warn().Str("fix", "add a RED/OPS API key in the Gazelle (OPS/RED) settings, or pick other sites in the completion settings")
	}
	event.
		Int("instanceID", instanceID).
		Str("hash", torrent.Hash).
		Str("name", torrent.Name).
		Msg("[CROSSSEED-COMPLETION] Skipping completion searches: the Site pick asks only RED/OPS sites, and none of them has a usable API key")
}
