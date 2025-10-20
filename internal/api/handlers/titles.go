// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/titles"
)

type TitlesHandler struct {
	syncManager *qbittorrent.SyncManager
	parser      *titles.Parser
}

// TorrentWithParsedTitle represents a torrent with its parsed title information
type TorrentWithParsedTitle struct {
	// Torrent information
	Hash     string `json:"hash"`
	Name     string `json:"name"`
	AddedOn  int64  `json:"addedOn"`
	Size     int64  `json:"size"`
	State    string `json:"state"`
	Category string `json:"category"`
	Tags     string `json:"tags"`
	Tracker  string `json:"tracker,omitempty"`

	// Parsed title information
	titles.ParsedTitle
}

// TitlesResponse represents the response for the titles endpoint
type TitlesResponse struct {
	Titles []TorrentWithParsedTitle `json:"titles"`
	Total  int                      `json:"total"`
}

// FilterOptions for titles
type TitlesFilterOptions struct {
	Type       string `json:"type,omitempty"`
	Source     string `json:"source,omitempty"`
	Resolution string `json:"resolution,omitempty"`
	Codec      string `json:"codec,omitempty"`
	Audio      string `json:"audio,omitempty"`
	Group      string `json:"group,omitempty"`
	Category   string `json:"category,omitempty"`
	Year       int    `json:"year,omitempty"`
	Search     string `json:"search,omitempty"`
}

func NewTitlesHandler(syncManager *qbittorrent.SyncManager) *TitlesHandler {
	return &TitlesHandler{
		syncManager: syncManager,
		parser:      titles.NewParser(),
	}
}

// ListParsedTitles returns all torrents with parsed title information
func (h *TitlesHandler) ListParsedTitles(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	ctx := r.Context()

	// Parse filters from query parameters
	var filters TitlesFilterOptions
	if f := r.URL.Query().Get("filters"); f != "" {
		if err := json.Unmarshal([]byte(f), &filters); err != nil {
			log.Warn().Err(err).Msg("Failed to parse filters, ignoring")
		}
	}

	// Get all torrents from sync manager
	torrents, err := h.syncManager.GetAllTorrents(ctx, instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get torrents")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrents")
		return
	}

	// torrents is already []qbt.Torrent
	var torrentNames []string
	var torrentList []qbt.Torrent

	for _, t := range torrents {
		if t.Name != "" {
			torrentNames = append(torrentNames, t.Name)
			torrentList = append(torrentList, t)
		}
	}

	// Parse titles
	parsedTitles := h.parser.ParseTitles(ctx, torrentNames)

	// Combine torrent data with parsed titles
	var combinedTitles []TorrentWithParsedTitle
	for i, parsed := range parsedTitles {
		if i >= len(torrentList) {
			continue
		}

		t := torrentList[i]
		combined := TorrentWithParsedTitle{
			ParsedTitle: parsed,
			Hash:        t.Hash,
			Name:        t.Name,
			AddedOn:     t.AddedOn,
			Size:        t.Size,
			State:       string(t.State),
			Category:    t.Category,
			Tags:        t.Tags,
			Tracker:     t.Tracker,
		}

		combinedTitles = append(combinedTitles, combined)
	}

	// Parse and filter titles
	filteredTitles := h.filterTitles(combinedTitles, filters)

	// Sort by added date (newest first)
	slices.SortFunc(filteredTitles, func(a, b TorrentWithParsedTitle) int {
		if a.AddedOn > b.AddedOn {
			return -1
		}
		if a.AddedOn < b.AddedOn {
			return 1
		}
		return 0
	})

	response := TitlesResponse{
		Titles: filteredTitles,
		Total:  len(filteredTitles),
	}

	RespondJSON(w, http.StatusOK, response)
}

// filterTitles applies filters to the parsed titles
func (h *TitlesHandler) filterTitles(titles []TorrentWithParsedTitle, filters TitlesFilterOptions) []TorrentWithParsedTitle {
	if filters.Type == "" && filters.Source == "" && filters.Resolution == "" &&
		filters.Codec == "" && filters.Audio == "" && filters.Group == "" &&
		filters.Category == "" && filters.Year == 0 && filters.Search == "" {
		return titles
	}

	var filtered []TorrentWithParsedTitle
	for _, title := range titles {
		// Type filter
		if filters.Type != "" && !strings.EqualFold(title.Type, filters.Type) {
			continue
		}

		// Source filter
		if filters.Source != "" && !strings.EqualFold(title.Source, filters.Source) {
			continue
		}

		// Resolution filter
		if filters.Resolution != "" && !strings.EqualFold(title.Resolution, filters.Resolution) {
			continue
		}

		// Codec filter
		if filters.Codec != "" {
			found := false
			for _, codec := range title.Codec {
				if strings.EqualFold(codec, filters.Codec) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Audio filter
		if filters.Audio != "" {
			found := false
			for _, audio := range title.Audio {
				if strings.EqualFold(audio, filters.Audio) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Group filter
		if filters.Group != "" && !strings.EqualFold(title.Group, filters.Group) {
			continue
		}

		// Category filter
		if filters.Category != "" && !strings.EqualFold(title.Category, filters.Category) {
			continue
		}

		// Year filter
		if filters.Year != 0 && title.Year != filters.Year {
			continue
		}

		// Search filter - search in name, title, and group
		if filters.Search != "" {
			searchLower := strings.ToLower(filters.Search)
			nameLower := strings.ToLower(title.Name)
			titleLower := strings.ToLower(title.Title)
			groupLower := strings.ToLower(title.Group)

			if !strings.Contains(nameLower, searchLower) &&
				!strings.Contains(titleLower, searchLower) &&
				!strings.Contains(groupLower, searchLower) {
				continue
			}
		}

		filtered = append(filtered, title)
	}

	return filtered
}
