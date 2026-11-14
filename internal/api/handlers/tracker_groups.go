// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"maps"
	"net/http"
	"slices"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/trackergroup"
)

type TrackerGroupsHandler struct {
	syncManager    *qbittorrent.SyncManager
	customizations *models.TrackerCustomizationStore
}

func NewTrackerGroupsHandler(syncManager *qbittorrent.SyncManager, customizations *models.TrackerCustomizationStore) *TrackerGroupsHandler {
	return &TrackerGroupsHandler{syncManager: syncManager, customizations: customizations}
}

// LiveTrackerGroup holds the torrents of one Tracker group that are in
// qBittorrent now. Uploaded is their lifetime upload.
type LiveTrackerGroup struct {
	Group    string `json:"group"`
	Torrents int    `json:"torrents"`
	Size     int64  `json:"size"`
	Uploaded int64  `json:"uploaded"`
}

type trackerGroupsResponse struct {
	Groups []LiveTrackerGroup `json:"groups"`
}

// List returns the torrents of one instance that are in qBittorrent now,
// added up by Tracker group. A torrent counts toward one group.
// GET /api/instances/{instanceID}/tracker-groups
func (h *TrackerGroupsHandler) List(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}
	customizations, err := h.customizations.List(r.Context())
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("tracker groups: failed to read tracker customizations")
		RespondError(w, http.StatusInternalServerError, "Failed to read tracker customizations")
		return
	}
	groups, err := h.syncManager.TrackerGroups(r.Context(), instanceID, trackergroup.New(customizations))
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "trackerGroups:list") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("tracker groups: failed to read the torrents")
		RespondError(w, http.StatusInternalServerError, "Failed to read tracker groups")
		return
	}
	RespondJSON(w, http.StatusOK, trackerGroupsResponse{Groups: liveTrackerGroups(groups)})
}

// liveTrackerGroups adds up the groups that share a name, for example a
// customization whose display name equals a tracker domain, so that each name
// is one row, as in the rows of the transfer query.
func liveTrackerGroups(groups map[trackergroup.Group]qbittorrent.TrackerTransferStats) []LiveTrackerGroup {
	byName := make(map[string]*LiveTrackerGroup, len(groups))
	for group, stats := range groups {
		live := byName[group.Name]
		if live == nil {
			live = &LiveTrackerGroup{Group: group.Name}
			byName[group.Name] = live
		}
		live.Torrents += stats.Count
		live.Size += stats.TotalSize
		live.Uploaded += stats.Uploaded
	}
	out := make([]LiveTrackerGroup, 0, len(byName))
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		out = append(out, *byName[name])
	}
	return out
}
