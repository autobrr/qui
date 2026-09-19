// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/moistari/rls"

	"github.com/autobrr/qui/pkg/pathutil"
	"github.com/autobrr/qui/pkg/releases"
	"github.com/autobrr/qui/pkg/stringutils"
)

// ContentTypeInfo contains all information about a torrent's detected content type
type ContentTypeInfo struct {
	ContentType  string   // one of releases.ContentTypes
	Categories   []int    // Torznab category IDs
	SearchType   string   // "search", "movie", "tvsearch", "music", "book"
	RequiredCaps []string // Required indexer capabilities
	IsMusic      bool     // Helper flag for music-related content
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
// rls parser, music names most of all. A recognized disc layout is authoritative
// video content and preserves explicit TV structure. Otherwise, audio bytes
// force the music classification, and video bytes pull a music parse back to tv
// or movie. The tv/movie split stays with the name-based parse. Without files,
// or when neither class dominates, the name decides.
func DetermineContentTypeWithFiles(release *rls.Release, files qbt.TorrentFiles) ContentTypeInfo {
	if isDisc, _ := isDiscLayoutTorrent(files); isDisc {
		if isTVRelease(release) {
			return classifyReleaseAs(release, rls.Series)
		}
		return contentTypeInfo(releases.ContentTypeMovie)
	}

	dominant := dominantFileContent(files)
	if dominant == rls.Music {
		if release.Type != rls.Music && release.Type != rls.Audiobook {
			return classifyReleaseAs(release, rls.Music)
		}
		// Skip the music-to-video name rescue: the bytes are audio, so a
		// video-looking token in the name must not flip the type back.
		return contentTypeInfo(releases.ClassifyRelease(release).ContentType)
	}
	if dominant == rls.Movie && release.Type == rls.Music {
		return contentTypeInfo(releases.ClassifyRelease(releases.DemoteMusicToVideo(release)).ContentType)
	}

	return DetermineContentType(release)
}

// educationContentType is what cross-seed searches for when rls types a release
// as Education. pkg/releases calls it a book, but video courses are common and
// a book category filter would skip them, while an unfiltered search only
// widens the results.
const educationContentType = releases.ContentTypeUnknown

// DetermineContentType analyzes a release and returns comprehensive content type information
func DetermineContentType(release *rls.Release) ContentTypeInfo {
	contentType := releases.DetermineContentType(release).ContentType
	if release.Type == rls.Education && contentType == releases.ContentTypeBook {
		contentType = educationContentType
	}
	return contentTypeInfo(contentType)
}

// RuleContentTypeInfo builds the classification a category mapping rule forces.
// Returns false for content types a rule cannot force. The API handler also uses
// it to validate incoming rules, so the list of valid content types lives here only.
func RuleContentTypeInfo(contentType string) (ContentTypeInfo, bool) {
	switch releases.ContentType(contentType) {
	case releases.ContentTypeMovie, releases.ContentTypeTV, releases.ContentTypeMusic,
		releases.ContentTypeAudiobook, releases.ContentTypeBook, releases.ContentTypeComic,
		releases.ContentTypeGame, releases.ContentTypeApp:
		return contentTypeInfo(releases.ContentType(contentType)), true
	default:
		return ContentTypeInfo{}, false
	}
}

// classifyReleaseAs classifies a copy of release retyped as releaseType. Adult
// detection still runs on the name, so the file correction cannot unmask adult content.
func classifyReleaseAs(release *rls.Release, releaseType rls.Type) ContentTypeInfo {
	retyped := *release
	retyped.Type = releaseType
	return contentTypeInfo(releases.ClassifyRelease(&retyped).ContentType)
}

// contentTypeInfo maps a content type to the Torznab categories, search mode and
// indexer capabilities cross-seed searches with.
func contentTypeInfo(contentType releases.ContentType) ContentTypeInfo {
	info := ContentTypeInfo{
		ContentType:  string(contentType),
		Categories:   []int{},
		SearchType:   "search",
		RequiredCaps: []string{},
	}

	switch contentType {
	case releases.ContentTypeMovie:
		info.Categories = []int{2000, 2010, 2020, 2030, 2040, 2045, 2050, 2060, 2070, 2080} // Movies
		info.SearchType = "movie"
		info.RequiredCaps = []string{"movie-search"}
	case releases.ContentTypeTV:
		info.Categories = []int{5000, 5010, 5020, 5030, 5040, 5045, 5070, 5080} // TV
		info.SearchType = "tvsearch"
		info.RequiredCaps = []string{"tv-search"}
	case releases.ContentTypeMusic, releases.ContentTypeAudiobook:
		info.Categories = []int{3000} // Audio
		info.SearchType = "music"
		info.RequiredCaps = []string{"music-search", "audio-search"}
		info.IsMusic = true
	case releases.ContentTypeBook:
		// Books is 7000, not 8000. 8000 is Other, and an indexer that advertises
		// its caps drops a request for a category it does not carry, so books were
		// skipped at every book indexer. Torznab derives the search mode from these
		// categories, so the 7000 range is also what makes the request t=book.
		info.Categories = []int{7000, 7010, 7020, 7040, 7050, 7060} // Books, minus comics
		info.SearchType = "book"
		info.RequiredCaps = []string{"book-search"}
	case releases.ContentTypeComic:
		info.Categories = []int{7000, 7030} // Books, Books/Comics
		info.SearchType = "book"
		info.RequiredCaps = []string{"book-search"}
	case releases.ContentTypeGame, releases.ContentTypeApp:
		info.Categories = []int{4000} // PC
	case releases.ContentTypeAdult:
		info.Categories = []int{6000} // XXX
	default:
		// Unknown keeps the unfiltered search set above.
	}

	return info
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
