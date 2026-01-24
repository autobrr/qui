// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/autobrr/qui/internal/models"
)

// torznabCaps captures the parsed capability and category data from a Torznab caps response.
type torznabCaps struct {
	Capabilities []string
	Categories   []models.TorznabIndexerCategory
	LimitDefault int
	LimitMax     int
}

const defaultTorznabLimit = 100

type torznabCapsResponse struct {
	XMLName    xml.Name              `xml:"caps"`
	Searching  torznabSearchingCaps  `xml:"searching"`
	Categories []torznabCategoryNode `xml:"categories>category"`
	Limits     struct {
		Default string `xml:"default,attr"`
		Max     string `xml:"max,attr"`
	} `xml:"limits"`
}

type torznabSearchingCaps struct {
	Search      torznabSearchNode `xml:"search"`
	TVSearch    torznabSearchNode `xml:"tv-search"`
	MovieSearch torznabSearchNode `xml:"movie-search"`
	MusicSearch torznabSearchNode `xml:"music-search"`
	AudioSearch torznabSearchNode `xml:"audio-search"`
	BookSearch  torznabSearchNode `xml:"book-search"`
}

type torznabSearchNode struct {
	Available       string `xml:"available,attr"`
	SupportedParams string `xml:"supportedParams,attr"`
}

type torznabCategoryNode struct {
	ID      string              `xml:"id,attr"`
	Name    string              `xml:"name,attr"`
	Subcats []torznabSubcatNode `xml:"subcat"`
}

type torznabSubcatNode struct {
	ID   string `xml:"id,attr"`
	Name string `xml:"name,attr"`
}

func parseTorznabCaps(r io.Reader) (*torznabCaps, error) {
	var resp torznabCapsResponse
	if err := xml.NewDecoder(r).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode caps response: %w", err)
	}

	caps := &torznabCaps{
		LimitDefault: defaultTorznabLimit,
		LimitMax:     defaultTorznabLimit,
	}

	caps.Capabilities = appendSearchCapabilities(caps.Capabilities, "search", resp.Searching.Search)
	caps.Capabilities = appendSearchCapabilities(caps.Capabilities, "tv-search", resp.Searching.TVSearch)
	caps.Capabilities = appendSearchCapabilities(caps.Capabilities, "movie-search", resp.Searching.MovieSearch)
	caps.Capabilities = appendSearchCapabilities(caps.Capabilities, "music-search", resp.Searching.MusicSearch)
	caps.Capabilities = appendSearchCapabilities(caps.Capabilities, "audio-search", resp.Searching.AudioSearch)
	caps.Capabilities = appendSearchCapabilities(caps.Capabilities, "book-search", resp.Searching.BookSearch)

	for _, cat := range resp.Categories {
		parentID, err := strconv.Atoi(strings.TrimSpace(cat.ID))
		if err != nil {
			continue
		}
		caps.Categories = append(caps.Categories, models.TorznabIndexerCategory{
			CategoryID:   parentID,
			CategoryName: strings.TrimSpace(cat.Name),
		})
		for _, sub := range cat.Subcats {
			subID, err := strconv.Atoi(strings.TrimSpace(sub.ID))
			if err != nil {
				continue
			}
			parent := parentID
			caps.Categories = append(caps.Categories, models.TorznabIndexerCategory{
				CategoryID:     subID,
				CategoryName:   strings.TrimSpace(sub.Name),
				ParentCategory: &parent,
			})
		}
	}

	// Parse limits if present
	if resp.Limits.Default != "" {
		if v, err := strconv.Atoi(resp.Limits.Default); err == nil && v > 0 {
			caps.LimitDefault = v
		}
	}
	if resp.Limits.Max != "" {
		if v, err := strconv.Atoi(resp.Limits.Max); err == nil && v > 0 {
			caps.LimitMax = v
		}
	}

	if caps.LimitMax > 0 && caps.LimitDefault > caps.LimitMax {
		caps.LimitDefault = caps.LimitMax
	}

	return caps, nil
}

func appendSearchCapabilities(capabilities []string, searchType string, searchNode torznabSearchNode) []string {
	if !isCapsAvailable(searchNode.Available) {
		return capabilities
	}

	// Add the basic search capability
	capabilities = append(capabilities, searchType)

	// Add detailed parameter support capabilities if available
	if searchNode.SupportedParams != "" {
		params := strings.SplitSeq(searchNode.SupportedParams, ",")
		for param := range params {
			param = strings.TrimSpace(param)
			if param != "" {
				// Create specific capability names for supported parameters
				capName := fmt.Sprintf("%s-%s", searchType, param)
				capabilities = append(capabilities, capName)
			}
		}
	}

	return capabilities
}

func isCapsAvailable(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "true", "1":
		return true
	default:
		return false
	}
}
