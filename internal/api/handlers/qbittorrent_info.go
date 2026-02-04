// Copyright (c) 2025-2026, s0up and the autobrr contributors.
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
	Zlib       string `json:"zlib"`
	Bitness    int    `json:"bitness"`
	Platform   string `json:"platform,omitempty"`
}

// QBittorrentAppInfo represents qBittorrent application information
type QBittorrentAppInfo struct {
	Version       string                `json:"version"`
	WebAPIVersion string                `json:"webAPIVersion,omitempty"`
	BuildInfo     *QBittorrentBuildInfo `json:"buildInfo,omitempty"`
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
		if respondIfInstanceDisabled(w, err, instanceID, "qbittorrentInfo:getAppInfo") {
			return
		}
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
	// Get qBittorrent application version
	version, err := client.GetAppVersionCtx(ctx)
	if err != nil {
		return nil, err
	}

	// Get qBittorrent Web API version
	webAPIVersion, err := client.GetWebAPIVersionCtx(ctx)
	if err != nil {
		return nil, err
	}

	// Get build information from qBittorrent API
	buildInfo, err := client.GetBuildInfoCtx(ctx)
	if err != nil {
		return nil, err
	}

	// Log the buildinfo
	log.Trace().Msgf("qBittorrent BuildInfo - App Version: %s, Web API Version: %s, Platform: %s, Libtorrent: %s, Qt: %s, Bitness: %d",
		version, webAPIVersion, buildInfo.Platform, buildInfo.Libtorrent, buildInfo.Qt, buildInfo.Bitness)

	// Convert from go-qbittorrent BuildInfo to our QBittorrentBuildInfo
	appInfo := &QBittorrentAppInfo{
		Version:       version,
		WebAPIVersion: webAPIVersion,
		BuildInfo: &QBittorrentBuildInfo{
			Qt:         buildInfo.Qt,
			Libtorrent: buildInfo.Libtorrent,
			Boost:      buildInfo.Boost,
			OpenSSL:    buildInfo.Openssl,
			Zlib:       buildInfo.Zlib,
			Bitness:    buildInfo.Bitness,
			Platform:   buildInfo.Platform,
		},
	}

	return appInfo, nil
}
