// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"net/http"

	"github.com/autobrr/qui/internal/api/middleware"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/jackett"
)

func searchResponseFor(r *http.Request, response *jackett.SearchResponse) *jackett.SearchResponse {
	if middleware.CanReadTrackerPasskeys(r) {
		return response
	}
	hidden := *response
	hidden.Results = make([]jackett.SearchResult, len(response.Results))
	for i, result := range response.Results {
		result.GUID = jackett.HiddenGUID(result.IndexerID, result.GUID, result.DownloadURL)
		result.DownloadURL = ""
		result.InfoURL = qbittorrent.TrackerOrigin(result.InfoURL)
		hidden.Results[i] = result
	}
	return &hidden
}

func torrentSearchResponseFor(r *http.Request, response *crossseed.TorrentSearchResponse) *crossseed.TorrentSearchResponse {
	if middleware.CanReadTrackerPasskeys(r) {
		return response
	}
	hidden := *response
	hidden.Results = make([]crossseed.TorrentSearchResult, len(response.Results))
	for i, result := range response.Results {
		result.GUID = jackett.HiddenGUID(result.IndexerID, result.GUID, result.DownloadURL)
		result.DownloadURL = ""
		result.InfoURL = qbittorrent.TrackerOrigin(result.InfoURL)
		hidden.Results[i] = result
	}
	return &hidden
}
