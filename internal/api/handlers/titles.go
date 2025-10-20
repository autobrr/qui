// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	"github.com/go-chi/chi/v5"
	"github.com/moistari/rls"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/qbittorrent"
)

type TitlesHandler struct {
	syncManager *qbittorrent.SyncManager
	cache       *ttlcache.Cache[string, ParsedTitle]
}

// ParsedTitle represents a torrent with its parsed title information
type ParsedTitle struct {
	// Torrent information
	Hash     string `json:"hash"`
	Name     string `json:"name"`
	AddedOn  int64  `json:"addedOn"`
	Size     int64  `json:"size"`
	State    string `json:"state"`
	Category string `json:"category"`
	Tags     string `json:"tags"`
	Tracker  string `json:"tracker,omitempty"`

	// Parsed release information
	Type        string   `json:"type"`
	Artist      string   `json:"artist,omitempty"`
	Title       string   `json:"title,omitempty"`
	Subtitle    string   `json:"subtitle,omitempty"`
	Alt         string   `json:"alt,omitempty"`
	Platform    string   `json:"platform,omitempty"`
	Arch        string   `json:"arch,omitempty"`
	Source      string   `json:"source,omitempty"`
	Resolution  string   `json:"resolution,omitempty"`
	Collection  string   `json:"collection,omitempty"`
	Year        int      `json:"year,omitempty"`
	Month       int      `json:"month,omitempty"`
	Day         int      `json:"day,omitempty"`
	Series      int      `json:"series,omitempty"`
	Episode     int      `json:"episode,omitempty"`
	Version     string   `json:"version,omitempty"`
	Disc        string   `json:"disc,omitempty"`
	Codec       []string `json:"codec,omitempty"`
	HDR         []string `json:"hdr,omitempty"`
	Audio       []string `json:"audio,omitempty"`
	Channels    string   `json:"channels,omitempty"`
	Other       []string `json:"other,omitempty"`
	Cut         []string `json:"cut,omitempty"`
	Edition     []string `json:"edition,omitempty"`
	Language    []string `json:"language,omitempty"`
	ReleaseSize string   `json:"releaseSize,omitempty"`
	Region      string   `json:"region,omitempty"`
	Container   string   `json:"container,omitempty"`
	Genre       string   `json:"genre,omitempty"`
	ID          string   `json:"id,omitempty"`
	Group       string   `json:"group,omitempty"`
	Meta        []string `json:"meta,omitempty"`
	Site        string   `json:"site,omitempty"`
	Sum         string   `json:"sum,omitempty"`
	Pass        string   `json:"pass,omitempty"`
	Req         bool     `json:"req,omitempty"`
	Ext         string   `json:"ext,omitempty"`
}

// TitlesResponse represents the response for the titles endpoint
type TitlesResponse struct {
	Titles []ParsedTitle `json:"titles"`
	Total  int           `json:"total"`
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
		cache:       ttlcache.New(ttlcache.Options[string, ParsedTitle]{}.SetDefaultTTL(30 * time.Minute)),
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

	// Parse and filter titles
	parsedTitles := h.parseTorrents(ctx, torrents)
	filteredTitles := h.filterTitles(parsedTitles, filters)

	// Sort by added date (newest first)
	slices.SortFunc(filteredTitles, func(a, b ParsedTitle) int {
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

// parseTorrents parses all torrent names using the rls library
func (h *TitlesHandler) parseTorrents(ctx context.Context, torrents interface{}) []ParsedTitle {
	var result []ParsedTitle

	// Convert torrents to a slice we can iterate
	torrentsData, err := json.Marshal(torrents)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal torrents")
		return result
	}

	var torrentsList []map[string]interface{}
	if err := json.Unmarshal(torrentsData, &torrentsList); err != nil {
		log.Error().Err(err).Msg("Failed to unmarshal torrents")
		return result
	}

	for _, torrentMap := range torrentsList {
		hash, _ := torrentMap["hash"].(string)
		name, _ := torrentMap["name"].(string)
		if name == "" {
			continue
		}

		// Check cache first
		if cached, found := h.cache.Get(name); found {
			// Use cached parsed title but update torrent-specific fields
			parsed := cached
			parsed.Hash = hash
			parsed.Name = name

			// Update torrent-specific metadata
			if val, ok := torrentMap["added_on"].(float64); ok {
				parsed.AddedOn = int64(val)
			} else if val, ok := torrentMap["addedOn"].(float64); ok {
				parsed.AddedOn = int64(val)
			}

			if val, ok := torrentMap["size"].(float64); ok {
				parsed.Size = int64(val)
			}

			parsed.State, _ = torrentMap["state"].(string)
			parsed.Category, _ = torrentMap["category"].(string)
			parsed.Tags, _ = torrentMap["tags"].(string)

			if trackerVal, ok := torrentMap["tracker"]; ok {
				parsed.Tracker, _ = trackerVal.(string)
			}

			result = append(result, parsed)
			continue
		}

		// Parse the torrent name using rls
		release := rls.ParseString(name)

		// Extract tracker domain if available
		tracker := ""
		if trackerVal, ok := torrentMap["tracker"]; ok {
			tracker, _ = trackerVal.(string)
		}

		// Get numeric fields with type checking
		addedOn := int64(0)
		if val, ok := torrentMap["added_on"].(float64); ok {
			addedOn = int64(val)
		} else if val, ok := torrentMap["addedOn"].(float64); ok {
			addedOn = int64(val)
		}

		size := int64(0)
		if val, ok := torrentMap["size"].(float64); ok {
			size = int64(val)
		}

		state, _ := torrentMap["state"].(string)
		category, _ := torrentMap["category"].(string)
		tags, _ := torrentMap["tags"].(string)

		parsed := ParsedTitle{
			Hash:     hash,
			Name:     name,
			AddedOn:  addedOn,
			Size:     size,
			State:    state,
			Category: category,
			Tags:     tags,
			Tracker:  tracker,

			// Parsed fields from rls
			Type:        release.Type.String(),
			Artist:      release.Artist,
			Title:       release.Title,
			Subtitle:    release.Subtitle,
			Alt:         release.Alt,
			Platform:    release.Platform,
			Arch:        release.Arch,
			Source:      release.Source,
			Resolution:  release.Resolution,
			Collection:  release.Collection,
			Year:        release.Year,
			Month:       release.Month,
			Day:         release.Day,
			Series:      release.Series,
			Episode:     release.Episode,
			Version:     release.Version,
			Disc:        release.Disc,
			Codec:       release.Codec,
			HDR:         release.HDR,
			Audio:       release.Audio,
			Channels:    release.Channels,
			Other:       release.Other,
			Cut:         release.Cut,
			Edition:     release.Edition,
			Language:    release.Language,
			ReleaseSize: release.Size,
			Region:      release.Region,
			Container:   release.Container,
			Genre:       release.Genre,
			ID:          release.ID,
			Group:       release.Group,
			Meta:        release.Meta,
			Site:        release.Site,
			Sum:         release.Sum,
			Pass:        release.Pass,
			Req:         release.Req,
			Ext:         release.Ext,
		}

		// Cache the parsed title
		h.cache.Set(name, parsed, ttlcache.DefaultTTL)

		result = append(result, parsed)
	}

	return result
}

// filterTitles applies filters to the parsed titles
func (h *TitlesHandler) filterTitles(titles []ParsedTitle, filters TitlesFilterOptions) []ParsedTitle {
	if filters.Type == "" && filters.Source == "" && filters.Resolution == "" &&
		filters.Codec == "" && filters.Audio == "" && filters.Group == "" &&
		filters.Category == "" && filters.Year == 0 && filters.Search == "" {
		return titles
	}

	var filtered []ParsedTitle
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
