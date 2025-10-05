package handlers

import (
	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
)

// InstanceCapabilitiesResponse describes supported features for an instance.
type InstanceCapabilitiesResponse struct {
	SupportsTorrentCreation bool   `json:"supportsTorrentCreation"`
	SupportsSetTags         bool   `json:"supportsSetTags"`
	WebAPIVersion           string `json:"webAPIVersion,omitempty"`
}

// NewInstanceCapabilitiesResponse creates a response payload from a qBittorrent client.
func NewInstanceCapabilitiesResponse(client *internalqbittorrent.Client) InstanceCapabilitiesResponse {
	capabilities := InstanceCapabilitiesResponse{
		SupportsTorrentCreation: client.SupportsTorrentCreation(),
		SupportsSetTags:         client.SupportsSetTags(),
	}

	if version := client.GetWebAPIVersion(); version != "" {
		capabilities.WebAPIVersion = version
	}

	return capabilities
}
