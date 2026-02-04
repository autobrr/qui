// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/anacrolix/torrent/metainfo"
	infohash_v2 "github.com/anacrolix/torrent/types/infohash-v2"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
)

// ContentTypeInfo contains all information about a torrent's detected content type
type ContentTypeInfo struct {
	ContentType  string   // "movie", "tv", "music", "audiobook", "book", "comic", "game", "app", "adult", "unknown"
	Categories   []int    // Torznab category IDs
	SearchType   string   // "search", "movie", "tvsearch", "music", "book"
	RequiredCaps []string // Required indexer capabilities
	IsMusic      bool     // Helper flag for music-related content
	MediaType    string   // Detected media format (e.g., "cd", "dvd-video", "bluray")
}

// isAdultContent checks if a release appears to be adult/pornographic content
func isAdultContent(release *rls.Release) bool {
	titleLower := strings.ToLower(release.Title)
	subtitleLower := strings.ToLower(release.Subtitle)
	collectionLower := strings.ToLower(release.Collection)

	// Check for explicit adult indicators that are unlikely to appear in legitimate TV/movies
	if (reAdultXXX.MatchString(release.Title) || reAdultXXX.MatchString(release.Subtitle) || reAdultXXX.MatchString(release.Collection)) &&
		!isBenignXXXContent(release, titleLower, subtitleLower, collectionLower) {
		return true
	}

	// Check for JAV code patterns (4 letters - 3-4 digits), but exclude if it's a valid RIAJ media code
	if reJAV.MatchString(release.Title) {
		// exclude if the same-looking token is a valid RIAJ media code in Title
		if detectRIAJMediaType(release.Title) == "" {
			return true
		}
	}

	// Check for date patterns common in adult content (MMDDYY_XXX or similar)
	// This is more specific than the previous broad terms
	if reAdultDate.MatchString(titleLower) || reAdultDate.MatchString(subtitleLower) || reAdultDate.MatchString(collectionLower) {
		return true
	}

	// Check for bracketed date patterns common in Japanese adult content
	if reBracketDate.MatchString(titleLower) || reBracketDate.MatchString(subtitleLower) || reBracketDate.MatchString(collectionLower) {
		return true
	}

	return false
}

func isBenignXXXContent(release *rls.Release, titleLower, subtitleLower, collectionLower string) bool {
	// Avoid flagging the mainstream xXx film franchise
	if strings.HasPrefix(titleLower, "xxx") || strings.HasPrefix(subtitleLower, "xxx") || strings.HasPrefix(collectionLower, "xxx") {
		if release.Year == 2002 || release.Year == 2005 || release.Year == 2017 {
			return true
		}
		if strings.Contains(titleLower, "xander cage") || strings.Contains(subtitleLower, "xander cage") || strings.Contains(collectionLower, "xander cage") {
			return true
		}
		if strings.Contains(titleLower, "state of the union") || strings.Contains(subtitleLower, "state of the union") || strings.Contains(collectionLower, "state of the union") {
			return true
		}
	}
	return false
}

// RIAJ media type mapping based on the 3rd character of the 4-letter manufacturer code
var riajMediaTypes = map[byte]string{
	'A': "dvd-audio",
	'B': "dvd-video",
	'C': "cd",
	'D': "cd-single",
	'F': "cd-video",
	'G': "sacd",
	'H': "hd-dvd",
	'I': "video-cd",
	'J': "vinyl-lp",
	'K': "vinyl-ep",
	'L': "ld-30cm",
	'M': "ld-20cm",
	'N': "cd-g",
	'P': "ps-game",
	'R': "cd-rom",
	'S': "cassette-single",
	'T': "cassette-album",
	'U': "umd-video",
	'V': "vhs",
	'W': "dvd-music",
	'X': "bluray",
	'Y': "md",
	'Z': "multi-format",
}

// Precompiled regexes — these functions run hot, so keep regexes compiled once
var (
	// RIAJ codes are manufacturer codes in the form ABCD-12345, case-insensitive
	reRIAJ = regexp.MustCompile(`(?i)\b[A-Z]{4}-?\d{3,5}\b`)

	// JAV codes typically are 3- or 4-letter alphanumeric groups followed by dash and 3-4 digits
	// e.g. IPX-123, ABP-1234, AAEJ-123
	reJAV = regexp.MustCompile(`(?i)\b(?:[A-Z0-9]{3,4})-\d{3,4}\b`)

	// Date patterns often used in adult filenames (MMDDYY_001 etc.)
	reAdultDate = regexp.MustCompile(`\b\d{6}[_-]\d{3}\b`)

	// Bracketed date pattern [YYYY.MM.DD]
	reBracketDate = regexp.MustCompile(`\[[12]\d{3}\.\d{2}\.\d{2}\]`)

	// explicit 'xxx' indicator (case-insensitive word boundary)
	reAdultXXX = regexp.MustCompile(`(?i)\bxxx\b`)
)

func detectRIAJMediaType(title string) string {
	// find the first RIAJ-like code
	match := reRIAJ.FindString(title)
	if match == "" {
		return ""
	}
	// Remove hyphen if present and normalize case
	code := strings.ReplaceAll(match, "-", "")
	code = strings.ToUpper(code)
	if len(code) < 4 {
		return ""
	}
	mediaChar := code[2] // 3rd character (0-indexed)
	if mediaType, exists := riajMediaTypes[mediaChar]; exists {
		return mediaType
	}
	return ""
}

// DetermineContentType analyzes a release and returns comprehensive content type information
func DetermineContentType(release *rls.Release) ContentTypeInfo {
	release = normalizeReleaseTypeForContent(release)
	var info ContentTypeInfo

	// Apply stacked parsing for clarity and to avoid false-positives
	// Order: adult indicators -> JAV (sanity-checked against RIAJ) -> mark adult

	// If adult-looking release, check JAV code and attempt to re-parse without it
	if isAdultContent(release) {
		// Look for JAV-style code (3 or 4 letters, compiled globally)
		if reJAV.MatchString(release.Title) {
			// Sanity-check: don't treat valid RIAJ codes as JAV
			if detectRIAJMediaType(release.Title) == "" {
				// Remove JAV code and try to see what the title becomes
				newTitle := reJAV.ReplaceAllString(release.Title, "")
				newTitle = strings.TrimSpace(newTitle)
				if newTitle != "" {
					newRelease := rls.ParseString(newTitle)
					altInfo := DetermineContentType(&newRelease)
					if altInfo.ContentType != "adult" {
						return altInfo
					}
				}
			}
		}

		// No alternate non-adult classification found; mark as adult
		info.ContentType = "adult"
		info.Categories = []int{6000} // XXX
		info.SearchType = "search"
		info.RequiredCaps = []string{}
		return info
	}

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

	// If content type is still unknown, try to infer from RIAJ media type as last resort
	if info.ContentType == "unknown" {
		info.MediaType = detectRIAJMediaType(release.Title)
		if info.MediaType != "" {
			switch info.MediaType {
			case "cd", "cd-single", "sacd", "md", "cassette-single", "cassette-album", "cd-g":
				info.ContentType = "music"
				info.Categories = []int{3000}
				info.SearchType = "music"
				info.RequiredCaps = []string{"music-search", "audio-search"}
				info.IsMusic = true
			case "dvd-video", "bluray", "hd-dvd", "ld-30cm", "ld-20cm", "vhs", "umd-video", "video-cd":
				// Assume movie unless we have better detection
				info.ContentType = "movie"
				info.Categories = []int{2000}
				info.SearchType = "movie"
				info.RequiredCaps = []string{"movie-search"}
			case "dvd-audio":
				info.ContentType = "music"
				info.Categories = []int{3000}
				info.SearchType = "music"
				info.RequiredCaps = []string{"music-search", "audio-search"}
				info.IsMusic = true
			case "cd-rom", "dvd-music":
				// Could be music or software, but lean towards music if dvd-music
				if info.MediaType == "dvd-music" {
					info.ContentType = "music"
					info.Categories = []int{3000}
					info.SearchType = "music"
					info.RequiredCaps = []string{"music-search", "audio-search"}
					info.IsMusic = true
				} else {
					info.ContentType = "app"
					info.Categories = []int{4000}
					info.SearchType = "search"
					info.RequiredCaps = []string{}
				}
			case "ps-game":
				info.ContentType = "game"
				info.Categories = []int{4000}
				info.SearchType = "search"
				info.RequiredCaps = []string{}
			case "vinyl-lp", "vinyl-ep":
				info.ContentType = "music"
				info.Categories = []int{3000}
				info.SearchType = "music"
				info.RequiredCaps = []string{"music-search", "audio-search"}
				info.IsMusic = true
			case "cd-video":
				// Could be music video or movie
				info.ContentType = "music"
				info.Categories = []int{3000}
				info.SearchType = "music"
				info.RequiredCaps = []string{"music-search", "audio-search"}
				info.IsMusic = true
			}
		}
	}

	return info
}

// normalizeReleaseTypeForContent inspects parsed metadata to correct obvious
// misclassifications (e.g. video torrents parsed as music because of dash-separated
// folder names such as BDMV/STREAM paths).
func normalizeReleaseTypeForContent(release *rls.Release) *rls.Release {
	normalized := *release
	if normalized.Type != rls.Music {
		return &normalized
	}

	if looksLikeVideoRelease(&normalized) {
		// Preserve episode metadata when present so TV content keeps season info.
		if normalized.Series > 0 || normalized.Episode > 0 {
			normalized.Type = rls.Episode
		} else {
			normalized.Type = rls.Movie
		}
	}

	return &normalized
}

func looksLikeVideoRelease(release *rls.Release) bool {
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
func ParseMusicReleaseFromTorrentName(baseRelease *rls.Release, torrentName string) *rls.Release {
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
		return &musicRelease
	}

	// Fallback: use our manual parsing approach for simpler names
	musicRelease := *baseRelease
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

	return &musicRelease
}

type TorrentMetadata struct {
	Name   string
	HashV1 string
	HashV2 string
	Files  qbt.TorrentFiles
	Info   *metainfo.Info
}

// ParseTorrentName extracts the name and info hash from torrent bytes using anacrolix/torrent
func ParseTorrentName(torrentBytes []byte) (name string, hash string, err error) {
	name, hash, _, err = ParseTorrentMetadata(torrentBytes)
	return name, hash, err
}

// ParseTorrentMetadata extracts comprehensive metadata from torrent bytes
func ParseTorrentMetadata(torrentBytes []byte) (name string, hash string, files qbt.TorrentFiles, err error) {
	meta, err := ParseTorrentMetadataWithInfo(torrentBytes)
	if err != nil {
		return "", "", nil, err
	}

	return meta.Name, meta.HashV1, meta.Files, nil
}

// ParseTorrentMetadataWithInfo extracts comprehensive metadata from torrent bytes,
// including the raw metainfo.Info for piece-level operations.
func ParseTorrentMetadataWithInfo(torrentBytes []byte) (TorrentMetadata, error) {
	mi, err := metainfo.Load(bytes.NewReader(torrentBytes))
	if err != nil {
		return TorrentMetadata{}, fmt.Errorf("failed to parse torrent metainfo: %w", err)
	}

	infoVal, err := mi.UnmarshalInfo()
	if err != nil {
		return TorrentMetadata{}, fmt.Errorf("failed to unmarshal torrent info: %w", err)
	}

	name := infoVal.Name
	hashV1 := strings.ToLower(mi.HashInfoBytes().HexString())
	var hashV2 string
	if infoVal.HasV2() {
		h := infohash_v2.HashBytes([]byte(mi.InfoBytes))
		hashV2 = strings.ToLower(h.HexString())
	}

	if name == "" {
		return TorrentMetadata{}, errors.New("torrent has no name")
	}

	files := BuildTorrentFilesFromInfo(name, infoVal)

	return TorrentMetadata{
		Name:   name,
		HashV1: hashV1,
		HashV2: hashV2,
		Files:  files,
		Info:   &infoVal,
	}, nil
}

// BuildTorrentFilesFromInfo creates qBittorrent-compatible file list from torrent info
func BuildTorrentFilesFromInfo(rootName string, info metainfo.Info) qbt.TorrentFiles {
	var files qbt.TorrentFiles
	pieceLength := info.PieceLength
	if pieceLength <= 0 {
		pieceLength = 1
	}

	if len(info.Files) == 0 {
		// Single file torrent
		pieceStart := 0
		pieceEnd := 0
		if info.Length > 0 {
			pieceEnd = int((info.Length - 1) / pieceLength)
		}
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
			PieceRange:   []int{pieceStart, pieceEnd},
			Priority:     0,
			Progress:     1,
			Size:         info.Length,
		}
		return files
	}

	files = make(qbt.TorrentFiles, len(info.Files))
	var offset int64
	for i, f := range info.Files {
		displayPath := f.DisplayPath(&info)
		name := rootName
		if info.IsDir() && displayPath != "" {
			name = rootName + "/" + displayPath
		} else if !info.IsDir() && displayPath != "" {
			name = displayPath
		}

		pieceStart := 0
		pieceEnd := 0
		if f.Length > 0 {
			pieceStart = int(offset / pieceLength)
			pieceEnd = int((offset + f.Length - 1) / pieceLength)
		} else {
			pieceStart = int(offset / pieceLength)
			pieceEnd = pieceStart
		}

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

		offset += f.Length
	}

	return files
}

// ParseTorrentAnnounceDomain extracts the primary announce URL's domain from torrent bytes.
// Prefers the first entry in announce-list if present; falls back to announce.
// Returns an empty string if no announce URL is found.
func ParseTorrentAnnounceDomain(torrentBytes []byte) string {
	mi, err := metainfo.Load(bytes.NewReader(torrentBytes))
	if err != nil {
		return ""
	}

	var announceURL string
	// Prefer first tier of announce-list
	if len(mi.AnnounceList) > 0 && len(mi.AnnounceList[0]) > 0 {
		announceURL = mi.AnnounceList[0][0]
	}
	// Fall back to announce
	if announceURL == "" {
		announceURL = mi.Announce
	}
	if announceURL == "" {
		return ""
	}

	// Extract domain from URL
	return extractDomainFromAnnounce(announceURL)
}

// extractDomainFromAnnounce extracts and normalizes the domain from an announce URL.
func extractDomainFromAnnounce(announceURL string) string {
	// Handle various URL schemes (http, https, udp, etc.)
	url := announceURL
	// Remove scheme
	if idx := strings.Index(url, "://"); idx != -1 {
		url = url[idx+3:]
	}
	// Remove path
	if idx := strings.Index(url, "/"); idx != -1 {
		url = url[:idx]
	}
	// Remove port
	if idx := strings.LastIndex(url, ":"); idx != -1 {
		// Make sure this is a port, not part of IPv6
		if !strings.Contains(url[idx:], "]") {
			url = url[:idx]
		}
	}
	// Remove userinfo if present
	if idx := strings.Index(url, "@"); idx != -1 {
		url = url[idx+1:]
	}
	return strings.ToLower(url)
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
