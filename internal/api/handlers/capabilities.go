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
	SupportsSetComment          bool   `json:"supportsSetComment"`
	SupportsTrackerHealth       bool   `json:"supportsTrackerHealth"`
	SupportsTrackerEditing      bool   `json:"supportsTrackerEditing"`
	SupportsRenameTorrent       bool   `json:"supportsRenameTorrent"`
	SupportsRenameFile          bool   `json:"supportsRenameFile"`
	SupportsRenameFolder        bool   `json:"supportsRenameFolder"`
	SupportsFilePriority        bool   `json:"supportsFilePriority"`
	SupportsSubcategories       bool   `json:"supportsSubcategories"`
	SubcategoriesAlwaysEnabled  bool   `json:"subcategoriesAlwaysEnabled"`
	SupportsTorrentTmpPath      bool   `json:"supportsTorrentTmpPath"`
	SupportsPathAutocomplete    bool   `json:"supportsPathAutocomplete"`
	SupportsTestEmail           bool   `json:"supportsTestEmail"`
	SupportsFreeSpacePathSource bool   `json:"supportsFreeSpacePathSource"`
	SupportsSetRSSFeedURL       bool   `json:"supportsSetRSSFeedURL"`
	SupportsShareLimitsAction   bool   `json:"supportsShareLimitsAction"`
	SupportsShareLimitsMode     bool   `json:"supportsShareLimitsMode"`
	SupportsLiveReannounce      bool   `json:"supportsLiveReannounce"`
	SupportsListFieldsV5        bool   `json:"supportsListFieldsV5"`
	SupportsListCreatedBy       bool   `json:"supportsListCreatedBy"`
	WebAPIVersion               string `json:"webAPIVersion,omitempty"`
}

// NewInstanceCapabilitiesResponse creates a response payload from a qBittorrent client.
func NewInstanceCapabilitiesResponse(client *internalqbittorrent.Client) InstanceCapabilitiesResponse {
	return InstanceCapabilitiesResponse{
		SupportsTorrentCreation:     client.Supports(internalqbittorrent.CapTorrentCreation),
		SupportsTorrentExport:       client.Supports(internalqbittorrent.CapTorrentExport),
		SupportsSetTags:             client.Supports(internalqbittorrent.CapSetTags),
		SupportsSetComment:          client.Supports(internalqbittorrent.CapSetComment),
		SupportsTrackerHealth:       client.Supports(internalqbittorrent.CapTrackerHealth),
		SupportsTrackerEditing:      client.Supports(internalqbittorrent.CapTrackerEditing),
		SupportsRenameTorrent:       client.Supports(internalqbittorrent.CapRenameTorrent),
		SupportsRenameFile:          client.Supports(internalqbittorrent.CapRenameFile),
		SupportsRenameFolder:        client.Supports(internalqbittorrent.CapRenameFolder),
		SupportsFilePriority:        client.Supports(internalqbittorrent.CapFilePriority),
		SupportsSubcategories:       client.Supports(internalqbittorrent.CapSubcategories),
		SubcategoriesAlwaysEnabled:  client.Supports(internalqbittorrent.CapSubcategoriesAlwaysEnabled),
		SupportsTorrentTmpPath:      client.Supports(internalqbittorrent.CapTorrentTmpPath),
		SupportsPathAutocomplete:    client.Supports(internalqbittorrent.CapPathAutocomplete),
		SupportsTestEmail:           client.Supports(internalqbittorrent.CapTestEmail),
		SupportsFreeSpacePathSource: runtime.GOOS != "windows",
		SupportsSetRSSFeedURL:       client.Supports(internalqbittorrent.CapSetRSSFeedURL),
		SupportsShareLimitsAction:   client.Supports(internalqbittorrent.CapShareLimitsAction),
		SupportsShareLimitsMode:     client.Supports(internalqbittorrent.CapShareLimitsMode),
		SupportsLiveReannounce:      client.Supports(internalqbittorrent.CapLiveReannounce),
		SupportsListFieldsV5:        client.Supports(internalqbittorrent.CapListFieldsV5),
		SupportsListCreatedBy:       client.Supports(internalqbittorrent.CapListCreatedBy),
		WebAPIVersion:               client.GetWebAPIVersion(),
	}
}
