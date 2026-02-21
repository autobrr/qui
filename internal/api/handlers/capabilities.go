// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"runtime"

	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
)

// InstanceCapabilitiesResponse describes supported features for an instance.
type InstanceCapabilitiesResponse struct {
	SupportsTorrentCreation     bool   `json:"supportsTorrentCreation"`
	SupportsTorrentExport       bool   `json:"supportsTorrentExport"`
	SupportsSetTags             bool   `json:"supportsSetTags"`
	SupportsTrackerHealth       bool   `json:"supportsTrackerHealth"`
	SupportsTrackerEditing      bool   `json:"supportsTrackerEditing"`
	SupportsRenameTorrent       bool   `json:"supportsRenameTorrent"`
	SupportsRenameFile          bool   `json:"supportsRenameFile"`
	SupportsRenameFolder        bool   `json:"supportsRenameFolder"`
	SupportsFilePriority        bool   `json:"supportsFilePriority"`
	SupportsSubcategories       bool   `json:"supportsSubcategories"`
	SupportsTorrentTmpPath      bool   `json:"supportsTorrentTmpPath"`
	SupportsPathAutocomplete    bool   `json:"supportsPathAutocomplete"`
	SupportsFreeSpacePathSource bool   `json:"supportsFreeSpacePathSource"`
	SupportsSetRSSFeedURL       bool   `json:"supportsSetRSSFeedURL"`
	WebAPIVersion               string `json:"webAPIVersion,omitempty"`
}

// NewInstanceCapabilitiesResponse creates a response payload from a qBittorrent client.
func NewInstanceCapabilitiesResponse(client *internalqbittorrent.Client) InstanceCapabilitiesResponse {
	capabilities := InstanceCapabilitiesResponse{
		SupportsTorrentCreation:     client.SupportsTorrentCreation(),
		SupportsTorrentExport:       client.SupportsTorrentExport(),
		SupportsSetTags:             client.SupportsSetTags(),
		SupportsTrackerHealth:       client.SupportsTrackerHealth(),
		SupportsTrackerEditing:      client.SupportsTrackerEditing(),
		SupportsRenameTorrent:       client.SupportsRenameTorrent(),
		SupportsRenameFile:          client.SupportsRenameFile(),
		SupportsRenameFolder:        client.SupportsRenameFolder(),
		SupportsFilePriority:        client.SupportsFilePriority(),
		SupportsSubcategories:       client.SupportsSubcategories(),
		SupportsTorrentTmpPath:      client.SupportsTorrentTmpPath(),
		SupportsPathAutocomplete:    client.SupportsPathAutocomplete(),
		SupportsFreeSpacePathSource: runtime.GOOS != osWindows,
		SupportsSetRSSFeedURL:       client.SupportsSetRSSFeedURL(),
	}

	if version := client.GetWebAPIVersion(); version != "" {
		capabilities.WebAPIVersion = version
	}

	return capabilities
}
