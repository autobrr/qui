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
	"github.com/autobrr/rls"

	"github.com/autobrr/qui/pkg/pathutil"
	"github.com/autobrr/qui/pkg/releases"
)

type ContentTypeInfo struct {
	ContentType  string
	Categories   []int
	SearchType   string
	RequiredCaps []string
	IsMusic      bool
}

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

func DetermineContentTypeWithFiles(release *rls.Release, files qbt.TorrentFiles) ContentTypeInfo {
	if isDisc, _ := isDiscLayoutTorrent(files); isDisc {
		if isTVRelease(release) {
			return classifyReleaseAs(release, rls.Series)
		}
		if movieInfo, ok := RuleContentTypeInfo("movie"); ok {
			return movieInfo
		}
	}

	dominant := dominantFileContent(files)
	if dominant == rls.Music {
		if release.Type != rls.Music && release.Type != rls.Audiobook {
			adjusted := *release
			adjusted.Type = rls.Music
			release = &adjusted
		}
		return classifyRelease(release)
	}
	if dominant == rls.Movie && release.Type == rls.Music {
		return classifyRelease(demoteMusicToVideo(release))
	}

	return DetermineContentType(release)
}

func DetermineContentType(release *rls.Release) ContentTypeInfo {
	return torznabContentTypeInfo(releases.DetermineContentType(release))
}

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
	return classifyReleaseAs(release, release.Type)
}

func classifyReleaseAs(release *rls.Release, releaseType rls.Type) ContentTypeInfo {
	return torznabContentTypeInfo(releases.ClassifyAs(release, releaseType))
}

func torznabContentTypeInfo(classified releases.ContentTypeInfo) ContentTypeInfo {
	info := ContentTypeInfo{
		ContentType: string(classified.ContentType),
	}
	switch classified.ContentType {
	case releases.ContentTypeMovie:
		info.Categories = []int{2000, 2010, 2020, 2030, 2040, 2045, 2050, 2060, 2070, 2080}
		info.SearchType = "movie"
		info.RequiredCaps = []string{"movie-search"}
	case releases.ContentTypeTV:
		info.Categories = []int{5000, 5010, 5020, 5030, 5040, 5045, 5070, 5080}
		info.SearchType = "tvsearch"
		info.RequiredCaps = []string{"tv-search"}
	case releases.ContentTypeMusic, releases.ContentTypeAudiobook:
		info.Categories = []int{3000}
		info.SearchType = "music"
		info.RequiredCaps = []string{"music-search", "audio-search"}
		info.IsMusic = true
	case releases.ContentTypeBook:
		info.Categories = []int{7000, 7010, 7020, 7040, 7050, 7060}
		info.SearchType = "book"
		info.RequiredCaps = []string{"book-search"}
	case releases.ContentTypeComic:
		info.Categories = []int{7000, 7030}
		info.SearchType = "book"
		info.RequiredCaps = []string{"book-search"}
	case releases.ContentTypeGame, releases.ContentTypeApp:
		info.Categories = []int{4000}
		info.SearchType = "search"
		info.RequiredCaps = []string{}
	case releases.ContentTypeAdult:
		info.Categories = []int{6000}
		info.SearchType = "search"
		info.RequiredCaps = []string{}
	default:
		info.Categories = []int{}
		info.SearchType = "search"
		info.RequiredCaps = []string{}
	}
	return info
}

func demoteMusicToVideo(release *rls.Release) *rls.Release {
	demoted := *release
	if demoted.Series > 0 || demoted.Episode > 0 {
		demoted.Type = rls.Episode
	} else {
		demoted.Type = rls.Movie
	}
	return &demoted
}

func (m matcher) parseMusicReleaseFromTorrentName(baseRelease *rls.Release, torrentName string) *rls.Release {
	torrentRelease := *m.parseReleaseName(torrentName)

	if torrentRelease.Artist != "" && torrentRelease.Title != "" {
		musicRelease := torrentRelease
		musicRelease.Type = rls.Music
		return &musicRelease
	}

	musicRelease := *baseRelease
	musicRelease.Type = rls.Music

	cleanName := torrentName

	if strings.Contains(cleanName, "[") && strings.Contains(cleanName, "]") {
		groupStart := strings.LastIndex(cleanName, "[")
		groupEnd := strings.LastIndex(cleanName, "]")
		if groupEnd > groupStart {
			musicRelease.Group = strings.TrimSpace(cleanName[groupStart+1 : groupEnd])
			cleanName = strings.TrimSpace(cleanName[:groupStart])
		}
	}

	if strings.Contains(cleanName, "(") && strings.Contains(cleanName, ")") {
		yearStart := strings.LastIndex(cleanName, "(")
		yearEnd := strings.LastIndex(cleanName, ")")
		if yearEnd > yearStart {
			cleanName = strings.TrimSpace(cleanName[:yearStart])
		}
	}

	if parts := strings.Split(cleanName, " - "); len(parts) >= 2 {
		musicRelease.Artist = strings.TrimSpace(parts[0])
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

func ParseTorrentMetadataWithInfo(torrentBytes []byte) (TorrentMetadata, error) {
	mi, err := metainfo.Load(bytes.NewReader(torrentBytes))
	if err != nil {
		return TorrentMetadata{}, fmt.Errorf("failed to parse torrent metainfo: %w", err)
	}

	infoVal, err := mi.UnmarshalInfo()
	if err != nil {
		return TorrentMetadata{}, fmt.Errorf("failed to unmarshal torrent info: %w", err)
	}

	name := pathutil.TorrentPathName(infoVal.Name)
	hashV1 := strings.ToLower(mi.HashInfoBytes().HexString())
	var hashV2 string
	if infoVal.HasV2() {
		h := metainfo.HashV2Bytes([]byte(mi.InfoBytes))
		hashV2 = strings.ToLower(h.HexString())
	}

	if name == "" {
		return TorrentMetadata{}, errors.New("torrent has no name")
	}

	files, err := BuildTorrentFilesFromInfo(name, infoVal)
	if err != nil {
		return TorrentMetadata{}, err
	}

	return TorrentMetadata{
		Name:   name,
		HashV1: hashV1,
		HashV2: hashV2,
		Files:  files,
		Info:   &infoVal,
	}, nil
}

func BuildTorrentFilesFromInfo(rootName string, info metainfo.Info) (qbt.TorrentFiles, error) {
	rootName = pathutil.TorrentPathName(rootName)
	var files qbt.TorrentFiles
	pieceLength := info.PieceLength
	if pieceLength <= 0 {
		pieceLength = 1
	}

	if len(info.Files) == 0 {
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
		return files, nil
	}

	files = make(qbt.TorrentFiles, len(info.Files))
	var offset int64
	for i, f := range info.Files {
		name, err := torrentFilePath(&info, &f, rootName)
		if err != nil {
			return nil, err
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

	return files, nil
}

func torrentFilePath(info *metainfo.Info, f *metainfo.FileInfo, rootName string) (string, error) {
	if !info.IsDir() {
		if name := pathutil.TorrentPathName(info.BestName()); name != "" {
			return name, nil
		}
		return rootName, nil
	}
	return pathutil.TorrentFilePath(rootName, f.BestPath())
}

func ParseTorrentAnnounceDomain(torrentBytes []byte) string {
	mi, err := metainfo.Load(bytes.NewReader(torrentBytes))
	if err != nil {
		return ""
	}

	var announceURL string
	if len(mi.AnnounceList) > 0 && len(mi.AnnounceList[0]) > 0 {
		announceURL = mi.AnnounceList[0][0]
	}
	if announceURL == "" {
		announceURL = mi.Announce
	}
	if announceURL == "" {
		return ""
	}

	return extractDomainFromAnnounce(announceURL)
}

func extractDomainFromAnnounce(announceURL string) string {
	url := announceURL
	if idx := strings.Index(url, "://"); idx != -1 {
		url = url[idx+3:]
	}
	if idx := strings.Index(url, "/"); idx != -1 {
		url = url[:idx]
	}
	if idx := strings.LastIndex(url, ":"); idx != -1 {
		if !strings.Contains(url[idx:], "]") {
			url = url[:idx]
		}
	}
	if idx := strings.Index(url, "@"); idx != -1 {
		url = url[idx+1:]
	}
	return strings.ToLower(url)
}

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
