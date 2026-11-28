// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"cmp"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/infohash"
)

const recentlyUnregisteredWindow = 48 * time.Hour

type UnregisteredDeleteWarning struct {
	Count    int                          `json:"count"`
	Since    time.Time                    `json:"since,omitzero"`
	Trackers []UnregisteredWarningTracker `json:"trackers"`
}

type UnregisteredWarningTracker struct {
	Host  string `json:"host"`
	Count int    `json:"count"`
}

type unregisteredReader interface {
	Health(hash string) qbittorrent.TrackerHealth
	UnregisteredSince(hash string) (time.Time, bool)
	UnregisteredBy(hash string) (qbittorrent.UnregisteredTracker, bool)
	UnregisteredSinceLoaded() bool
}

type unregisteredSource interface {
	unregisteredOf(instanceID int) unregisteredReader
}

type syncUnregisteredSource struct {
	syncManager *qbittorrent.SyncManager
}

func (s syncUnregisteredSource) unregisteredOf(instanceID int) unregisteredReader {
	return s.syncManager.GetTrackerHealthSnapshot(instanceID)
}

func (h *TorrentsHandler) GetUnregisteredDeleteWarning(w http.ResponseWriter, r *http.Request) {
	_, targetsByInstance, ok := h.decodeTorrentSelection(w, r)
	if !ok {
		return
	}
	RespondJSON(w, r, http.StatusOK, recentlyUnregistered(h.unregistered, targetsByInstance, time.Now()))
}

func (h *TorrentsHandler) decodeTorrentSelection(w http.ResponseWriter, r *http.Request) (int, map[int][]string, bool) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return 0, nil, false
	}

	var req BulkActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return 0, nil, false
	}
	normalizedInstanceIDs, instanceIDsErr := normalizeInstanceIDs(req.InstanceIDs)
	if instanceIDsErr != nil {
		RespondError(w, http.StatusBadRequest, instanceIDsErr.Error())
		return 0, nil, false
	}
	req.InstanceIDs = normalizedInstanceIDs
	if message := torrentSelectionError(instanceID, req); message != "" {
		RespondError(w, http.StatusBadRequest, message)
		return 0, nil, false
	}

	targetsByInstance, ok := h.resolveTorrentSelection(w, r, instanceID, req)
	return instanceID, targetsByInstance, ok
}

func recentlyUnregistered(source unregisteredSource, targetsByInstance map[int][]string, now time.Time) UnregisteredDeleteWarning {
	warning := UnregisteredDeleteWarning{Trackers: []UnregisteredWarningTracker{}}
	byHost := make(map[string]int)
	for instanceID, hashes := range targetsByInstance {
		health := source.unregisteredOf(instanceID)
		if !health.UnregisteredSinceLoaded() {
			continue
		}
		for _, hash := range infohash.Unique(hashes) {
			key := hash.String()
			if health.Health(key) != qbittorrent.TrackerHealthUnregistered {
				continue
			}
			since, ok := health.UnregisteredSince(key)
			if !ok {
				since = now
			}
			if now.Sub(since) >= recentlyUnregisteredWindow {
				continue
			}
			warning.Count++
			if warning.Since.IsZero() || since.Before(warning.Since) {
				warning.Since = since
			}
			tracker, _ := health.UnregisteredBy(key)
			byHost[tracker.Host]++
		}
	}
	for host, count := range byHost {
		warning.Trackers = append(warning.Trackers, UnregisteredWarningTracker{Host: host, Count: count})
	}
	slices.SortFunc(warning.Trackers, func(a, b UnregisteredWarningTracker) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Host, b.Host))
	})
	return warning
}
