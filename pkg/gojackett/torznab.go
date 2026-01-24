// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"strconv"
	"strings"
)

// TorznabItem represents a single RSS item with helper methods for accessing torznab attributes
type TorznabItem struct {
	Title           string
	Guid            string
	Link            string
	Comments        string
	PubDate         string
	Description     string
	Size            string
	Files           string
	Grabs           string
	Category        []string
	EnclosureURL    string
	EnclosureLength string
	Attributes      map[string][]string // Map of attribute name to values (attributes can have multiple values)
}

// GetAttr retrieves the first value of an attribute by name
func (t *TorznabItem) GetAttr(name string) string {
	if values, ok := t.Attributes[name]; ok && len(values) > 0 {
		return values[0]
	}
	return ""
}

// GetAttrValues retrieves all values of an attribute by name
func (t *TorznabItem) GetAttrValues(name string) []string {
	if values, ok := t.Attributes[name]; ok {
		return values
	}
	return []string{}
}

// GetAttrInt retrieves an attribute as an integer
func (t *TorznabItem) GetAttrInt(name string) int {
	val := t.GetAttr(name)
	if val == "" {
		return 0
	}
	i, _ := strconv.Atoi(val)
	return i
}

// GetAttrInt64 retrieves an attribute as an int64
func (t *TorznabItem) GetAttrInt64(name string) int64 {
	val := t.GetAttr(name)
	if val == "" {
		return 0
	}
	i, _ := strconv.ParseInt(val, 10, 64)
	return i
}

// GetAttrFloat retrieves an attribute as a float64
func (t *TorznabItem) GetAttrFloat(name string) float64 {
	val := t.GetAttr(name)
	if val == "" {
		return 0.0
	}
	f, _ := strconv.ParseFloat(val, 64)
	return f
}

// Seeders returns the number of seeders
func (t *TorznabItem) Seeders() int {
	return t.GetAttrInt(AttrSeeders)
}

// Leechers returns the number of leechers
func (t *TorznabItem) Leechers() int {
	return t.GetAttrInt(AttrLeechers)
}

// Peers returns the total number of peers (seeders + leechers)
func (t *TorznabItem) Peers() int {
	peers := t.GetAttrInt(AttrPeers)
	if peers > 0 {
		return peers
	}
	// Calculate from seeders and leechers if peers attribute is not present
	return t.Seeders() + t.Leechers()
}

// InfoHash returns the torrent info hash
func (t *TorznabItem) InfoHash() string {
	return t.GetAttr(AttrInfoHash)
}

// MagnetURL returns the magnet link
func (t *TorznabItem) MagnetURL() string {
	return t.GetAttr(AttrMagnetURL)
}

// DownloadVolumeFactor returns the download volume factor (0.0 = freeleech)
func (t *TorznabItem) DownloadVolumeFactor() float64 {
	factor := t.GetAttrFloat(AttrDownloadVolumeFactor)
	// Default to 1.0 if not specified
	if t.GetAttr(AttrDownloadVolumeFactor) == "" {
		return 1.0
	}
	return factor
}

// UploadVolumeFactor returns the upload volume factor
func (t *TorznabItem) UploadVolumeFactor() float64 {
	factor := t.GetAttrFloat(AttrUploadVolumeFactor)
	// Default to 1.0 if not specified
	if t.GetAttr(AttrUploadVolumeFactor) == "" {
		return 1.0
	}
	return factor
}

// IsFreeleech returns true if the torrent is freeleech (downloadvolumefactor = 0)
func (t *TorznabItem) IsFreeleech() bool {
	return t.DownloadVolumeFactor() == 0.0
}

// MinimumRatio returns the minimum ratio requirement
func (t *TorznabItem) MinimumRatio() float64 {
	return t.GetAttrFloat(AttrMinimumRatio)
}

// MinimumSeedTime returns the minimum seed time in seconds
func (t *TorznabItem) MinimumSeedTime() int64 {
	return t.GetAttrInt64(AttrMinimumSeedTime)
}

// Categories returns all category IDs as strings
func (t *TorznabItem) Categories() []string {
	cats := t.GetAttrValues(AttrCategory)
	if len(cats) > 0 {
		return cats
	}
	// Fall back to RSS category if no attr categories
	return t.Category
}

// TVDBID returns the TVDB ID for TV shows
func (t *TorznabItem) TVDBID() string {
	return t.GetAttr(AttrTVDBID)
}

// TVMazeID returns the TVMaze ID for TV shows
func (t *TorznabItem) TVMazeID() string {
	return t.GetAttr(AttrTVMazeID)
}

// Season returns the season number for TV shows
func (t *TorznabItem) Season() int {
	return t.GetAttrInt(AttrSeason)
}

// Episode returns the episode number for TV shows
func (t *TorznabItem) Episode() int {
	return t.GetAttrInt(AttrEpisode)
}

// IMDBID returns the IMDB ID for movies/shows
func (t *TorznabItem) IMDBID() string {
	// Try both imdbid and imdb attributes
	id := t.GetAttr(AttrIMDBID)
	if id == "" {
		id = t.GetAttr(AttrIMDB)
	}
	return id
}

// TMDBID returns the TMDB ID for movies
func (t *TorznabItem) TMDBID() string {
	return t.GetAttr(AttrTMDBID)
}

// Genre returns the genre
func (t *TorznabItem) Genre() string {
	return t.GetAttr(AttrGenre)
}

// Year returns the release year
func (t *TorznabItem) Year() int {
	return t.GetAttrInt(AttrYear)
}

// Resolution returns the video resolution (e.g., "1920x1080")
func (t *TorznabItem) Resolution() string {
	return t.GetAttr(AttrResolution)
}

// Video returns the video codec information
func (t *TorznabItem) Video() string {
	return t.GetAttr(AttrVideo)
}

// Audio returns the audio codec information
func (t *TorznabItem) Audio() string {
	return t.GetAttr(AttrAudio)
}

// Language returns the language
func (t *TorznabItem) Language() string {
	return t.GetAttr(AttrLanguage)
}

// Subtitles returns the subtitle languages
func (t *TorznabItem) Subtitles() string {
	return t.GetAttr(AttrSubtitles)
}

// Artist returns the artist name for music
func (t *TorznabItem) Artist() string {
	return t.GetAttr(AttrArtist)
}

// Album returns the album name for music
func (t *TorznabItem) Album() string {
	return t.GetAttr(AttrAlbum)
}

// Author returns the author name for books
func (t *TorznabItem) Author() string {
	return t.GetAttr(AttrAuthor)
}

// BookTitle returns the book title
func (t *TorznabItem) BookTitle() string {
	return t.GetAttr(AttrBookTitle)
}

// Tags returns all tag values
func (t *TorznabItem) Tags() []string {
	return t.GetAttrValues(AttrTag)
}

// HasTag checks if the item has a specific tag
func (t *TorznabItem) HasTag(tag string) bool {
	tags := t.Tags()
	for _, t := range tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

// ToTorznabItems converts an RSS feed to a slice of TorznabItem with helper methods
func (r *Rss) ToTorznabItems() []TorznabItem {
	items := make([]TorznabItem, 0, len(r.Channel.Item))

	for _, item := range r.Channel.Item {
		ti := TorznabItem{
			Title:           item.Title,
			Guid:            item.Guid,
			Link:            item.Link,
			Comments:        item.Comments,
			PubDate:         item.PubDate,
			Description:     item.Description,
			Size:            item.Size,
			Files:           item.Files,
			Grabs:           item.Grabs,
			Category:        item.Category,
			EnclosureURL:    item.Enclosure.URL,
			EnclosureLength: item.Enclosure.Length,
			Attributes:      make(map[string][]string),
		}

		// Parse all attributes into the map
		for _, attr := range item.Attr {
			if _, exists := ti.Attributes[attr.Name]; !exists {
				ti.Attributes[attr.Name] = []string{}
			}
			ti.Attributes[attr.Name] = append(ti.Attributes[attr.Name], attr.Value)
		}

		items = append(items, ti)
	}

	return items
}
