// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
)

type QBittorrentInfoHandler struct {
	clientPool *internalqbittorrent.ClientPool
}

func NewQBittorrentInfoHandler(clientPool *internalqbittorrent.ClientPool) *QBittorrentInfoHandler {
	return &QBittorrentInfoHandler{
		clientPool: clientPool,
	}
}

// QBittorrentBuildInfo represents qBittorrent build information
type QBittorrentBuildInfo struct {
	Qt         string `json:"qt"`
	Libtorrent string `json:"libtorrent"`
	Boost      string `json:"boost"`
	OpenSSL    string `json:"openssl"`
	Bitness    int    `json:"bitness"`
	Platform   string `json:"platform,omitempty"`
}

// QBittorrentAppInfo represents qBittorrent application information
type QBittorrentAppInfo struct {
	Version   string                `json:"version"`
	BuildInfo *QBittorrentBuildInfo `json:"buildInfo,omitempty"`
}

// GetQBittorrentAppInfo returns qBittorrent application version and build information
func (h *QBittorrentInfoHandler) GetQBittorrentAppInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceIDStr := chi.URLParam(r, "instanceID")
	instanceID, err := strconv.Atoi(instanceIDStr)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	client, err := h.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		RespondError(w, http.StatusInternalServerError, "Failed to get qBittorrent client")
		return
	}

	// Get qBittorrent version and build info
	appInfo, err := h.getQBittorrentAppInfo(ctx, client)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get qBittorrent application info")
		RespondError(w, http.StatusInternalServerError, "Failed to get qBittorrent application info")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(appInfo)
}

// getQBittorrentAppInfo fetches application info from qBittorrent API
func (h *QBittorrentInfoHandler) getQBittorrentAppInfo(ctx context.Context, client *internalqbittorrent.Client) (*QBittorrentAppInfo, error) {
	appInfo := &QBittorrentAppInfo{
		Version: client.GetWebAPIVersion(),
	}

	// TODO: Implement actual qBittorrent API calls to get build info
	// For now, we'll provide sensible defaults that assume modern qBittorrent/libtorrent
	// This allows the UI to work while we implement proper version detection
	appInfo.BuildInfo = &QBittorrentBuildInfo{
		Libtorrent: "1.2.0",   // Assume modern libtorrent 2.x for now
		Platform:   "windows", // Default platform, could be detected from preferences
		Bitness:    64,
	}

	return appInfo, nil
}
