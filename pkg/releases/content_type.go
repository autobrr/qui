// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package releases

import (
	"regexp"
	"strings"

	"github.com/moistari/rls"
)

// ContentTypeInfo contains all information about a torrent's detected content type.
type ContentTypeInfo struct {
	// ContentType is one of: movie, tv, music, audiobook, book, comic, game, app, adult, unknown.
	ContentType string
	// MediaType is an optional detected media format (e.g. "cd", "dvd-video", "bluray").
	MediaType string
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

// isAdultContent checks if a release appears to be adult/pornographic content.
func isAdultContent(release *rls.Release) bool {
	titleLower := strings.ToLower(release.Title)
	subtitleLower := strings.ToLower(release.Subtitle)
	collectionLower := strings.ToLower(release.Collection)

	if (reAdultXXX.MatchString(release.Title) || reAdultXXX.MatchString(release.Subtitle) || reAdultXXX.MatchString(release.Collection)) &&
		!isBenignXXXContent(release, titleLower, subtitleLower, collectionLower) {
		return true
	}

	// JAV code patterns (4 letters - 3-4 digits), but exclude if it's a valid RIAJ media code.
	if reJAV.MatchString(release.Title) {
		if detectRIAJMediaType(release.Title) == "" {
			return true
		}
	}

	if reAdultDate.MatchString(titleLower) || reAdultDate.MatchString(subtitleLower) || reAdultDate.MatchString(collectionLower) {
		return true
	}
	if reBracketDate.MatchString(titleLower) || reBracketDate.MatchString(subtitleLower) || reBracketDate.MatchString(collectionLower) {
		return true
	}

	return false
}

func isBenignXXXContent(release *rls.Release, titleLower, subtitleLower, collectionLower string) bool {
	// Avoid flagging the mainstream xXx film franchise.
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

// RIAJ media type mapping based on the 3rd character of the 4-letter manufacturer code.
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

var (
	reRIAJ = regexp.MustCompile(`(?i)\b[A-Z]{4}-?\d{3,5}\b`)
	reJAV  = regexp.MustCompile(`(?i)\b(?:[A-Z0-9]{3,4})-\d{3,4}\b`)

	reAdultDate   = regexp.MustCompile(`\b\d{6}[_-]\d{3}\b`)
	reBracketDate = regexp.MustCompile(`\[[12]\d{3}\.\d{2}\.\d{2}\]`)
	reAdultXXX    = regexp.MustCompile(`(?i)\bxxx\b`)
)

func detectRIAJMediaType(title string) string {
	match := reRIAJ.FindString(title)
	if match == "" {
		return ""
	}
	code := strings.ReplaceAll(match, "-", "")
	code = strings.ToUpper(code)
	if len(code) < 4 {
		return ""
	}
	mediaChar := code[2]
	if mediaType, exists := riajMediaTypes[mediaChar]; exists {
		return mediaType
	}
	return ""
}

// DetermineContentType analyzes a parsed release and returns a best-effort content type.
func DetermineContentType(release *rls.Release) ContentTypeInfo {
	release = normalizeReleaseTypeForContent(release)
	var info ContentTypeInfo

	if release == nil {
		info.ContentType = "unknown"
		return info
	}

	// Adult detection first; if JAV-like token appears, attempt re-parse without it.
	if isAdultContent(release) {
		if reJAV.MatchString(release.Title) && detectRIAJMediaType(release.Title) == "" {
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
		info.ContentType = "adult"
		return info
	}

	switch release.Type {
	case rls.Movie:
		info.ContentType = "movie"
	case rls.Episode, rls.Series:
		info.ContentType = "tv"
	case rls.Music:
		info.ContentType = "music"
	case rls.Audiobook:
		info.ContentType = "audiobook"
	case rls.Book, rls.Education, rls.Magazine:
		info.ContentType = "book"
	case rls.Comic:
		info.ContentType = "comic"
	case rls.Game:
		info.ContentType = "game"
	case rls.App:
		info.ContentType = "app"
	case rls.Unknown:
		// Fall back below.
	}

	// Fallback logic based on series/episode/year detection for unknown types.
	if info.ContentType == "" {
		switch {
		case release.Series > 0 || release.Episode > 0:
			info.ContentType = "tv"
		case release.Year > 0:
			info.ContentType = "movie"
		default:
			info.ContentType = "unknown"
		}
	}

	// Last resort: infer from RIAJ media type.
	if info.ContentType == "unknown" {
		info.MediaType = detectRIAJMediaType(release.Title)
		if info.MediaType != "" {
			switch info.MediaType {
			case "cd", "cd-single", "sacd", "md", "cassette-single", "cassette-album", "cd-g", "vinyl-lp", "vinyl-ep", "cd-video", "dvd-audio":
				info.ContentType = "music"
			case "dvd-video", "bluray", "hd-dvd", "ld-30cm", "ld-20cm", "vhs", "umd-video", "video-cd":
				info.ContentType = "movie"
			case "cd-rom", "dvd-music":
				if info.MediaType == "dvd-music" {
					info.ContentType = "music"
				} else {
					info.ContentType = "app"
				}
			case "ps-game":
				info.ContentType = "game"
			}
		}
	}

	return info
}
