// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/anacrolix/torrent/metainfo"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
)

// ContentTypeInfo contains all information about a torrent's detected content type
type ContentTypeInfo struct {
	ContentType  string   // "movie", "tv", "music", "audiobook", "book", "comic", "game", "app", "unknown"
	Categories   []int    // Torznab category IDs
	SearchType   string   // "search", "movie", "tvsearch", "music", "book"
	RequiredCaps []string // Required indexer capabilities
	IsMusic      bool     // Helper flag for music-related content
}

// DetermineContentType analyzes a release and returns comprehensive content type information
func DetermineContentType(release rls.Release) ContentTypeInfo {
	release = normalizeReleaseTypeForContent(release)
	var info ContentTypeInfo

	switch release.Type {
	case rls.Movie:
		info.ContentType = "movie"
		info.Categories = []int{2000} // Movies
		info.SearchType = "movie"
		info.RequiredCaps = []string{"movie-search"}
	case rls.Episode, rls.Series:
		info.ContentType = "tv"
		info.Categories = []int{5000} // TV
		info.SearchType = "tvsearch"
		info.RequiredCaps = []string{"tv-search"}
	case rls.Music:
		info.ContentType = "music"
		info.Categories = []int{3000} // Audio
		info.SearchType = "music"
		info.RequiredCaps = []string{"music-search", "audio-search"}
		info.IsMusic = true
	case rls.Audiobook:
		info.ContentType = "audiobook"
		info.Categories = []int{3000} // Audio
		info.SearchType = "music"
		info.RequiredCaps = []string{"music-search", "audio-search"}
		info.IsMusic = true
	case rls.Book:
		info.ContentType = "book"
		info.Categories = []int{8000} // Books
		info.SearchType = "book"
		info.RequiredCaps = []string{"book-search"}
	case rls.Comic:
		info.ContentType = "comic"
		info.Categories = []int{8000} // Books (comics are under books)
		info.SearchType = "book"
		info.RequiredCaps = []string{"book-search"}
	case rls.Game:
		info.ContentType = "game"
		info.Categories = []int{4000} // PC
		info.SearchType = "search"
		info.RequiredCaps = []string{}
	case rls.App:
		info.ContentType = "app"
		info.Categories = []int{4000} // PC
		info.SearchType = "search"
		info.RequiredCaps = []string{}
	default:
		// Fallback logic based on series/episode/year detection for unknown types
		if release.Series > 0 || release.Episode > 0 {
			info.ContentType = "tv"
			info.Categories = []int{5000}
			info.SearchType = "tvsearch"
			info.RequiredCaps = []string{"tv-search"}
		} else if release.Year > 0 {
			info.ContentType = "movie"
			info.Categories = []int{2000}
			info.SearchType = "movie"
			info.RequiredCaps = []string{"movie-search"}
		} else {
			info.ContentType = "unknown"
			info.Categories = []int{}
			info.SearchType = "search"
			info.RequiredCaps = []string{}
		}
	}

	return info
}

// normalizeReleaseTypeForContent inspects parsed metadata to correct obvious
// misclassifications (e.g. video torrents parsed as music because of dash-separated
// folder names such as BDMV/STREAM paths).
func normalizeReleaseTypeForContent(release rls.Release) rls.Release {
	if release.Type != rls.Music {
		return release
	}

	if looksLikeVideoRelease(release) {
		// Preserve episode metadata when present so TV content keeps season info.
		if release.Series > 0 || release.Episode > 0 {
			release.Type = rls.Episode
		} else {
			release.Type = rls.Movie
		}
	}

	return release
}

func looksLikeVideoRelease(release rls.Release) bool {
	if release.Resolution != "" {
		return true
	}
	if len(release.HDR) > 0 {
		return true
	}
	if hasVideoCodecHints(release.Codec) {
		return true
	}
	videoTitleHints := []string{
		"2160p", "1080p", "720p", "576p", "480p", "4k", "remux", "rmhd", "hdr", "hdr10",
		"dolby vision", "dv", "uhd", "bluray", "blu-ray", "bdrip", "bdremux", "bd50", "bd25",
		"web-dl", "webdl", "webrip", "hdtv", "cam", "ts", "m2ts", "xvid", "x264", "x265", "hevc",
	}
	if containsVideoTokens(release.Title, videoTitleHints) || containsVideoTokens(release.Group, videoTitleHints) {
		return true
	}
	if release.Source != "" {
		lowerSource := strings.ToLower(release.Source)
		videoSourceHints := []string{"uhd", "hdr", "remux", "stream", "bdmv", "bluray", "blu-ray", "bdrip", "bdremux", "webrip", "web-dl", "webdl", "hdtv", "dvdrip", "m2ts"}
		for _, hint := range videoSourceHints {
			if strings.Contains(lowerSource, hint) {
				return true
			}
		}
	}
	return false
}

func hasVideoCodecHints(codecs []string) bool {
	if len(codecs) == 0 {
		return false
	}
	videoCodecHints := []string{"x264", "x265", "h264", "h265", "hevc", "av1", "xvid", "divx"}
	for _, codec := range codecs {
		lowerCodec := strings.ToLower(codec)
		for _, hint := range videoCodecHints {
			if strings.Contains(lowerCodec, hint) {
				return true
			}
		}
	}
	return false
}

func containsVideoTokens(value string, tokens []string) bool {
	if value == "" {
		return false
	}
	lowerValue := strings.ToLower(value)
	for _, token := range tokens {
		if token == "" {
			continue
		}
		if strings.Contains(lowerValue, token) {
			return true
		}
	}
	return false
}

// OptimizeContentTypeForIndexers optimizes content type information for specific indexers
// This function takes the basic content type and adjusts categories based on indexer capabilities
func OptimizeContentTypeForIndexers(basicInfo ContentTypeInfo, indexerCategories []int) ContentTypeInfo {
	if len(indexerCategories) == 0 || len(basicInfo.Categories) == 0 {
		return basicInfo
	}

	// Create a map of available categories from the indexer
	availableCategories := make(map[int]struct{})
	for _, cat := range indexerCategories {
		availableCategories[cat] = struct{}{}
	}

	// Filter the basic categories to only include those supported by the indexer
	optimizedCategories := make([]int, 0, len(basicInfo.Categories))
	for _, cat := range basicInfo.Categories {
		if _, exists := availableCategories[cat]; exists {
			optimizedCategories = append(optimizedCategories, cat)
		} else {
			// Try parent category
			parent := cat / 100 * 100
			if parent != cat {
				if _, exists := availableCategories[parent]; exists {
					optimizedCategories = append(optimizedCategories, parent)
				}
			}
		}
	}

	// If no categories match, fall back to parent categories
	if len(optimizedCategories) == 0 {
		for _, cat := range basicInfo.Categories {
			parent := cat / 100 * 100
			if _, exists := availableCategories[parent]; exists {
				optimizedCategories = append(optimizedCategories, parent)
			}
		}
	}

	// Create optimized info
	optimizedInfo := basicInfo
	if len(optimizedCategories) > 0 {
		optimizedInfo.Categories = optimizedCategories
	}

	return optimizedInfo
}

// ParseMusicReleaseFromTorrentName extracts music-specific metadata from torrent name
// First tries RLS's built-in parsing, then falls back to manual "Artist - Album" format parsing
func ParseMusicReleaseFromTorrentName(baseRelease rls.Release, torrentName string) rls.Release {
	// First, try RLS's built-in parsing on the torrent name directly
	// This can handle complex release names like "Artist-Album-Edition-Source-Year-GROUP"
	torrentRelease := rls.ParseString(torrentName)

	// If RLS detected it as music and extracted artist/title, use that
	if torrentRelease.Type == rls.Music && torrentRelease.Artist != "" && torrentRelease.Title != "" {
		// Use RLS's parsed results but preserve any content-based detection from baseRelease
		musicRelease := torrentRelease
		// Keep any fields from content detection that might be more accurate
		if baseRelease.Type == rls.Music {
			musicRelease.Type = rls.Music
		}
		return musicRelease
	}

	// Fallback: use our manual parsing approach for simpler names
	musicRelease := baseRelease
	musicRelease.Type = rls.Music // Ensure it's marked as music

	cleanName := torrentName

	// Extract release group if present [GROUP]
	if strings.Contains(cleanName, "[") && strings.Contains(cleanName, "]") {
		groupStart := strings.LastIndex(cleanName, "[")
		groupEnd := strings.LastIndex(cleanName, "]")
		if groupEnd > groupStart {
			musicRelease.Group = strings.TrimSpace(cleanName[groupStart+1 : groupEnd])
			cleanName = strings.TrimSpace(cleanName[:groupStart])
		}
	}

	// Remove year (YYYY) from the end for parsing
	if strings.Contains(cleanName, "(") && strings.Contains(cleanName, ")") {
		yearStart := strings.LastIndex(cleanName, "(")
		yearEnd := strings.LastIndex(cleanName, ")")
		if yearEnd > yearStart {
			cleanName = strings.TrimSpace(cleanName[:yearStart])
		}
	}

	// Parse "Artist - Album" format
	if parts := strings.Split(cleanName, " - "); len(parts) >= 2 {
		musicRelease.Artist = strings.TrimSpace(parts[0])
		// Join remaining parts as album title (in case there are multiple " - " separators)
		musicRelease.Title = strings.TrimSpace(strings.Join(parts[1:], " - "))
	}

	return musicRelease
}

// ParseTorrentName extracts the name and info hash from torrent bytes using anacrolix/torrent
func ParseTorrentName(torrentBytes []byte) (name string, hash string, err error) {
	name, hash, _, err = ParseTorrentMetadata(torrentBytes)
	return name, hash, err
}

// ParseTorrentMetadata extracts comprehensive metadata from torrent bytes
func ParseTorrentMetadata(torrentBytes []byte) (name string, hash string, files qbt.TorrentFiles, err error) {
	mi, err := metainfo.Load(bytes.NewReader(torrentBytes))
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to parse torrent metainfo: %w", err)
	}

	info, err := mi.UnmarshalInfo()
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to unmarshal torrent info: %w", err)
	}

	name = info.Name
	hash = mi.HashInfoBytes().HexString()

	if name == "" {
		return "", "", nil, fmt.Errorf("torrent has no name")
	}

	files = BuildTorrentFilesFromInfo(name, info)

	return name, hash, files, nil
}

// BuildTorrentFilesFromInfo creates qBittorrent-compatible file list from torrent info
func BuildTorrentFilesFromInfo(rootName string, info metainfo.Info) qbt.TorrentFiles {
	var files qbt.TorrentFiles

	if len(info.Files) == 0 {
		// Single file torrent
		files = make(qbt.TorrentFiles, 1)
		files[0] = struct {
			Availability float32 `json:"availability"`
			Index        int     `json:"index"`
			IsSeed       bool    `json:"is_seed,omitempty"`
			Name         string  `json:"name"`
			PieceRange   []int   `json:"piece_range"`
			Priority     int     `json:"priority"`
			Progress     float32 `json:"progress"`
			Size         int64   `json:"size"`
		}{
			Availability: 1,
			Index:        0,
			IsSeed:       true,
			Name:         rootName,
			PieceRange:   []int{0, 0},
			Priority:     0,
			Progress:     1,
			Size:         info.Length,
		}
		return files
	}

	files = make(qbt.TorrentFiles, len(info.Files))
	for i, f := range info.Files {
		displayPath := f.DisplayPath(&info)
		name := rootName
		if info.IsDir() && displayPath != "" {
			name = strings.Join([]string{rootName, displayPath}, "/")
		} else if !info.IsDir() && displayPath != "" {
			name = displayPath
		}

		pieceStart := f.BeginPieceIndex(info.PieceLength)
		pieceEnd := f.EndPieceIndex(info.PieceLength)

		files[i] = struct {
			Availability float32 `json:"availability"`
			Index        int     `json:"index"`
			IsSeed       bool    `json:"is_seed,omitempty"`
			Name         string  `json:"name"`
			PieceRange   []int   `json:"piece_range"`
			Priority     int     `json:"priority"`
			Progress     float32 `json:"progress"`
			Size         int64   `json:"size"`
		}{
			Availability: 1,
			Index:        i,
			IsSeed:       true,
			Name:         name,
			PieceRange:   []int{pieceStart, pieceEnd},
			Priority:     0,
			Progress:     1,
			Size:         f.Length,
		}
	}

	return files
}

// FindLargestFile returns the file with the largest size from a list of torrent files.
// This is useful for content type detection as the largest file usually represents the main content.
func FindLargestFile(files qbt.TorrentFiles) *struct {
	Availability float32 `json:"availability"`
	Index        int     `json:"index"`
	IsSeed       bool    `json:"is_seed,omitempty"`
	Name         string  `json:"name"`
	PieceRange   []int   `json:"piece_range"`
	Priority     int     `json:"priority"`
	Progress     float32 `json:"progress"`
	Size         int64   `json:"size"`
} {
	if len(files) == 0 {
		return nil
	}

	largest := &files[0]
	for i := range files {
		if files[i].Size > largest.Size {
			largest = &files[i]
		}
	}

	return largest
}
