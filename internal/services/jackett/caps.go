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
}

type torznabCapsResponse struct {
	XMLName    xml.Name              `xml:"caps"`
	Searching  torznabSearchingCaps  `xml:"searching"`
	Categories []torznabCategoryNode `xml:"categories>category"`
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
	Available string `xml:"available,attr"`
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

	caps := &torznabCaps{}

	caps.Capabilities = appendCapabilityIf(caps.Capabilities, "search", resp.Searching.Search.Available)
	caps.Capabilities = appendCapabilityIf(caps.Capabilities, "tv-search", resp.Searching.TVSearch.Available)
	caps.Capabilities = appendCapabilityIf(caps.Capabilities, "movie-search", resp.Searching.MovieSearch.Available)
	caps.Capabilities = appendCapabilityIf(caps.Capabilities, "music-search", resp.Searching.MusicSearch.Available)
	caps.Capabilities = appendCapabilityIf(caps.Capabilities, "audio-search", resp.Searching.AudioSearch.Available)
	caps.Capabilities = appendCapabilityIf(caps.Capabilities, "book-search", resp.Searching.BookSearch.Available)

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

	return caps, nil
}

func appendCapabilityIf(capabilities []string, name, available string) []string {
	if isCapsAvailable(available) {
		return append(capabilities, name)
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
