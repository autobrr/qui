package handlers

import (
	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
)

// InstanceCapabilitiesResponse describes supported features for an instance.
type InstanceCapabilitiesResponse struct {
	SupportsTorrentCreation bool   `json:"supportsTorrentCreation"`
	SupportsSetTags         bool   `json:"supportsSetTags"`
	SupportsTrackerHealth   bool   `json:"supportsTrackerHealth"`
	WebAPIVersion           string `json:"webAPIVersion,omitempty"`
}

// NewInstanceCapabilitiesResponse creates a response payload from a qBittorrent client.
func NewInstanceCapabilitiesResponse(client *internalqbittorrent.Client) InstanceCapabilitiesResponse {
	capabilities := InstanceCapabilitiesResponse{
		SupportsTorrentCreation: client.SupportsTorrentCreation(),
		SupportsSetTags:         client.SupportsSetTags(),
		SupportsTrackerHealth:   client.SupportsTrackerHealth(),
	}

	if version := client.GetWebAPIVersion(); version != "" {
		capabilities.WebAPIVersion = version
	}

	return capabilities
}
