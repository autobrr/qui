// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/moistari/rls"

	"github.com/autobrr/qui/pkg/pathutil"
	"github.com/autobrr/qui/pkg/stringutils"
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

// audioFileExtensions lists the extensions of standalone audio content for the
// byte-weighted signal in DetermineContentTypeWithFiles. It is deliberately a
// separate list from gazellePlausibleExtensions: that one encodes what RED/OPS
// host, this one encodes what is audio.
var audioFileExtensions = map[string]bool{
	".flac": true,
	".mp3":  true,
	".m4a":  true,
	".m4b":  true,
	".aac":  true,
	".ac3":  true,
	".dts":  true,
	".ogg":  true,
	".opus": true,
	".wav":  true,
	".aiff": true,
	".dsf":  true,
	".dff":  true,
	".dsd":  true,
	".wma":  true,
	".ape":  true,
	".alac": true,
	".wv":   true,
	".mka":  true,
	".tta":  true,
	".aob":  true,
}

// dominantFileContent reports which class of content holds the majority of a
// torrent's bytes: rls.Music for audio, rls.Movie for video, or rls.Unknown
// when neither dominates. It weighs total bytes per class instead of picking
// the largest file, because booklet scans and cover art outweigh individual
// tracks in many music releases.
//
// ponytail: videoExtensions is the season-pack list and misses .vob, .mpg,
// .webm, and .m4v; those torrents fall through to name-based classification.
// Widen the list if field reports show it.
func dominantFileContent(files qbt.TorrentFiles) rls.Type {
	var audioBytes, videoBytes, totalBytes int64
	for _, file := range files {
		ext := strings.ToLower(path.Ext(file.Name))
		if audioFileExtensions[ext] {
			audioBytes += file.Size
		} else if _, ok := videoExtensions[ext]; ok {
			videoBytes += file.Size
		}
		totalBytes += file.Size
	}
	switch {
	case audioBytes > 0 && audioBytes >= totalBytes-audioBytes:
		return rls.Music
	case videoBytes > 0 && videoBytes >= totalBytes-videoBytes:
		return rls.Movie
	}
	return rls.Unknown
}

// DetermineContentTypeWithFiles classifies a release like DetermineContentType,
// but first corrects the parsed type with the byte-weighted extension signal
// from the torrent's files (discussion #1734). Release names often defeat the
// rls parser, music names most of all, but file extensions are ground truth:
// audio bytes force the music classification, and video bytes pull a music
// parse back to tv or movie. The tv/movie split stays with the name-based
// parse. Without files, or when neither class dominates, the name decides,
// except for a disc layout, which classifies as a movie when the name gives
// nothing.
func DetermineContentTypeWithFiles(release *rls.Release, files qbt.TorrentFiles) ContentTypeInfo {
	dominant := dominantFileContent(files)
	if dominant == rls.Music {
		if release.Type != rls.Music && release.Type != rls.Audiobook {
			adjusted := *release
			adjusted.Type = rls.Music
			release = &adjusted
		}
		// Skip the music-to-video name rescue: the bytes are audio, so a
		// video-looking token in the name must not flip the type back.
		return classifyRelease(release)
	}
	if dominant == rls.Movie && release.Type == rls.Music {
		return classifyRelease(demoteMusicToVideo(release))
	}

	info := DetermineContentType(release)
	if info.ContentType == "unknown" {
		// A disc rip holds no release name in its files (VIDEO_TS/VTS_01_1.VOB),
		// and its folder often carries no year or resolution either, so the name
		// alone classifies as unknown and the search goes out with no categories
		// (#2336). The layout itself proves the content is video.
		if isDisc, _ := isDiscLayoutTorrent(files); isDisc {
			if movieInfo, ok := RuleContentTypeInfo("movie"); ok {
				return movieInfo
			}
		}
	}
	return info
}

// DetermineContentType analyzes a release and returns comprehensive content type information
func DetermineContentType(release *rls.Release) ContentTypeInfo {
	return classifyRelease(normalizeReleaseTypeForContent(release))
}

// RuleContentTypeInfo builds the classification a category mapping rule forces.
// It classifies a synthetic release of the rule's content type, so the result
// stays identical to a release that parsed as that type. Returns false for
// content types a rule cannot force. The API handler also uses it to validate
// incoming rules, so the list of valid content types lives here only.
func RuleContentTypeInfo(contentType string) (ContentTypeInfo, bool) {
	var releaseType rls.Type
	switch contentType {
	case "movie":
		releaseType = rls.Movie
	case "tv":
		releaseType = rls.Series
	case "music":
		releaseType = rls.Music
	case "audiobook":
		releaseType = rls.Audiobook
	case "book":
		releaseType = rls.Book
	case "comic":
		releaseType = rls.Comic
	case "game":
		releaseType = rls.Game
	case "app":
		releaseType = rls.App
	default:
		return ContentTypeInfo{}, false
	}
	return classifyRelease(&rls.Release{Type: releaseType}), true
}

func classifyRelease(release *rls.Release) ContentTypeInfo {
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
		info.Categories = []int{2000, 2010, 2020, 2030, 2040, 2045, 2050, 2060, 2070, 2080} // Movies
		info.SearchType = "movie"
		info.RequiredCaps = []string{"movie-search"}
	case rls.Episode, rls.Series:
		info.ContentType = "tv"
		info.Categories = []int{5000, 5010, 5020, 5030, 5040, 5045, 5070, 5080} // TV
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
		// Books is 7000, not 8000. 8000 is Other, and an indexer that advertises
		// its caps drops a request for a category it does not carry, so books were
		// skipped at every book indexer. Torznab derives the search mode from these
		// categories, so the 7000 range is also what makes the request t=book.
		info.ContentType = "book"
		info.Categories = []int{7000, 7010, 7020, 7040, 7050, 7060} // Books, minus comics
		info.SearchType = "book"
		info.RequiredCaps = []string{"book-search"}
	case rls.Comic:
		info.ContentType = "comic"
		info.Categories = []int{7000, 7030} // Books, Books/Comics
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
			info.Categories = []int{5000, 5010, 5020, 5030, 5040, 5045, 5070, 5080}
			info.SearchType = "tvsearch"
			info.RequiredCaps = []string{"tv-search"}
		} else if release.Year > 0 {
			info.ContentType = "movie"
			info.Categories = []int{2000, 2010, 2020, 2030, 2040, 2045, 2050, 2060, 2070, 2080}
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
				info.Categories = []int{2000, 2010, 2020, 2030, 2040, 2045, 2050, 2060, 2070, 2080}
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
	if release.Type == rls.Music && looksLikeVideoRelease(release) {
		return demoteMusicToVideo(release)
	}

	normalized := *release
	return &normalized
}

// demoteMusicToVideo returns a copy of a music-typed release retyped as video.
// Episode metadata is preserved when present so TV content keeps season info.
func demoteMusicToVideo(release *rls.Release) *rls.Release {
	demoted := *release
	if demoted.Series > 0 || demoted.Episode > 0 {
		demoted.Type = rls.Episode
	} else {
		demoted.Type = rls.Movie
	}
	return &demoted
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

	name := stringutils.SanitizeUTF8(infoVal.Name)
	hashV1 := strings.ToLower(mi.HashInfoBytes().HexString())
	var hashV2 string
	if infoVal.HasV2() {
		h := metainfo.HashV2Bytes([]byte(mi.InfoBytes))
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
		displayPath := stringutils.SanitizeUTF8(torrentDisplayPath(&info, &f))
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

// torrentDisplayPath is metainfo.FileInfo.DisplayPath with libtorrent's empty-component
// rule applied, so the path lines up with the one qBittorrent stores for the file.
// The non-directory branch is kept so this stays a drop-in for DisplayPath.
func torrentDisplayPath(info *metainfo.Info, f *metainfo.FileInfo) string {
	if !info.IsDir() {
		return info.BestName()
	}

	parts := f.BestPath()
	components := make([]string, len(parts))
	for i, part := range parts {
		components[i] = pathutil.TorrentPathComponent(part)
	}
	return strings.Join(components, "/")
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
func FindLargestFile(files qbt.TorrentFiles) *qbt.TorrentFile {
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
