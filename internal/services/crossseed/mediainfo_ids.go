// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"context"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	mediainfo "github.com/autobrr/go-mediainfo"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/stringutils"
)

// mediaIDExtractorVersion invalidates cached rows when the tag parsing or
// selection rules below change. Bump it on any behavioral change here.
const mediaIDExtractorVersion = 1

// mediaIDAnalyzeTimeout bounds one MKV metadata read so a slow network
// mount degrades to "no ID" instead of holding the search slot. Local
// reads finish in milliseconds. The context is observed between reads, so
// a single read blocked in the OS still returns only when the OS completes it.
const mediaIDAnalyzeTimeout = 30 * time.Second

// mediaIDCache captures the media_id_cache store methods the service uses.
type mediaIDCache interface {
	Get(ctx context.Context, torrentKey, contentType string) (*models.MediaIDCacheEntry, error)
	Set(ctx context.Context, entry *models.MediaIDCacheEntry) error
}

// lookupMediaFileIDs resolves external IDs from Matroska metadata embedded in
// the torrent's largest .mkv file, backed by the media_id_cache table. It
// returns nil when no usable ID exists. Transient failures (no local access,
// unreadable file, parse error) return nil without caching so a later search
// can retry; a successful scan is cached even when it finds no ID.
func (s *Service) lookupMediaFileIDs(ctx context.Context, instance *models.Instance, torrent *qbt.Torrent, files qbt.TorrentFiles, contentType string) *models.ExternalIDs {
	if s.mediaIDCacheStore == nil {
		return nil
	}
	switch contentType {
	case "movie", "tv", "anime":
	default:
		return nil
	}

	// qBittorrent's Hash is the v1 infohash for v1 and hybrid torrents, so
	// the same content shares one row across instances.
	key := normalizeHash(torrent.Hash)
	if key == "" {
		return nil
	}

	entry, err := s.mediaIDCacheStore.Get(ctx, key, contentType)
	if err != nil {
		log.Debug().Err(err).Str("torrentKey", key).Msg("[CROSSSEED-SEARCH] Media ID cache read failed")
	} else if entry != nil && entry.ExtractorVersion == mediaIDExtractorVersion {
		return mediaIDsToExternalIDs(entry.IDType, entry.IDValue)
	}

	if !instance.HasLocalFilesystemAccess {
		return nil
	}

	filePath := largestMKVPath(torrent.SavePath, files)
	if filePath == "" {
		return nil
	}

	analyze := s.analyzeMediaFile
	if analyze == nil {
		analyze = func(ctx context.Context, path string) (mediainfo.Report, error) {
			// Speed 0 keeps codec/cluster probing bounded to a few packets per
			// track; the Matroska Tags reads are ParseSpeed-independent.
			// #nosec G304 -- path is constructed from the instance save path via resolveLocalTorrentFile.
			return mediainfo.AnalyzeFileContext(ctx, path, mediainfo.WithParseSpeed(0))
		}
	}

	analyzeCtx, cancel := context.WithTimeout(ctx, mediaIDAnalyzeTimeout)
	defer cancel()
	report, err := analyze(analyzeCtx, filePath)
	if err != nil {
		log.Debug().Err(err).Str("torrentName", torrent.Name).Str("file", filePath).Msg("[CROSSSEED-SEARCH] MediaInfo analysis failed; not caching")
		return nil
	}

	idType, idValue := extractMediaExternalIDs(report, contentType)

	writeErr := s.mediaIDCacheStore.Set(ctx, &models.MediaIDCacheEntry{
		TorrentKey:       key,
		ContentType:      contentType,
		ExtractorVersion: mediaIDExtractorVersion,
		IDType:           idType,
		IDValue:          idValue,
	})
	if writeErr != nil {
		log.Warn().Err(writeErr).Str("torrentKey", key).Msg("[CROSSSEED-SEARCH] Media ID cache write failed")
	}

	if idType == "" {
		return nil
	}
	log.Debug().Str("torrentName", torrent.Name).Str("idType", idType).Str("idValue", idValue).Msg("[CROSSSEED-SEARCH] Extracted external ID from MKV metadata")
	return mediaIDsToExternalIDs(idType, idValue)
}

func mediaIDsToExternalIDs(idType, idValue string) *models.ExternalIDs {
	switch idType {
	case "imdb":
		return &models.ExternalIDs{IMDbID: idValue}
	case "tmdb":
		if n, err := strconv.Atoi(idValue); err == nil && n > 0 {
			return &models.ExternalIDs{TMDbID: n}
		}
	case "tvdb":
		if n, err := strconv.Atoi(idValue); err == nil && n > 0 {
			return &models.ExternalIDs{TVDbID: n}
		}
	}
	return nil
}

// largestMKVPath returns the resolved local path of the torrent's largest
// .mkv file, or "" when none resolves. qBittorrent returns files in stable
// index order, so the pick is deterministic.
func largestMKVPath(savePath string, files qbt.TorrentFiles) string {
	// Filter first: forEachLocalTorrentFile stats every file it visits.
	// Skip deselected/partial files: a stub can sit on disk at the declared
	// size and would cache a wrong "no tags" row over the complete file.
	var mkvs qbt.TorrentFiles
	for _, file := range files {
		if file.Progress >= 1 && file.Size > 0 && stringutils.HasSuffixFold(file.Name, ".mkv") {
			mkvs = append(mkvs, file)
		}
	}
	slices.SortStableFunc(mkvs, func(a, b qbt.TorrentFile) int {
		return cmp.Compare(b.Size, a.Size)
	})

	var bestPath string
	forEachLocalTorrentFile(savePath, mkvs, func(_ qbt.TorrentFile, fullPath string, _ os.FileInfo) bool {
		bestPath = fullPath
		return false
	})
	return bestPath
}

// extractMediaExternalIDs selects one external ID from the Matroska file-level
// tags in a MediaInfo report. Namespaces are tried in a fixed,
// content-type-dependent order; a namespace whose tags disagree is skipped.
// Only explicitly namespaced TMDb/TVDb forms that match the content type are
// accepted, because muxer tags carry no library-level trust.
func extractMediaExternalIDs(report mediainfo.Report, contentType string) (idType, idValue string) {
	imdb := map[string]struct{}{}
	tmdb := map[string]struct{}{}
	tvdb := map[string]struct{}{}

	for _, field := range report.General.Fields {
		value := strings.ToLower(strings.TrimSpace(field.Value))
		if value == "" {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(field.Name)) {
		case "IMDB":
			if v, ok := normalizeIMDbTag(value); ok {
				imdb[v] = struct{}{}
			}
		case "TMDB":
			if v, ok := normalizeTMDbTag(value, contentType); ok {
				tmdb[v] = struct{}{}
			}
		case "TVDB", "TVDB2":
			if v, ok := normalizeTVDbTag(value, contentType); ok {
				tvdb[v] = struct{}{}
			}
		}
	}

	single := func(set map[string]struct{}) string {
		if len(set) != 1 {
			return ""
		}
		for v := range set {
			return v
		}
		return ""
	}

	values := map[string]string{"imdb": single(imdb), "tmdb": single(tmdb), "tvdb": single(tvdb)}
	order := []string{"imdb", "tmdb", "tvdb"}
	if contentType != "movie" {
		order = []string{"tvdb", "imdb", "tmdb"}
	}
	for _, ns := range order {
		if values[ns] != "" {
			return ns, values[ns]
		}
	}
	return "", ""
}

func normalizeIMDbTag(value string) (string, bool) {
	if !strings.HasPrefix(value, "tt") || !isDecimalID(value[2:]) {
		return "", false
	}
	return value, true
}

// normalizeTMDbTag accepts mkvmerge-style namespaced TMDb tags ("movie/278",
// "tv/1399"). Bare numbers are ambiguous between the movie and TV databases
// and collection IDs are a different object entirely, so both are rejected.
func normalizeTMDbTag(value, contentType string) (string, bool) {
	prefix := "movie/"
	if contentType != "movie" {
		prefix = "tv/"
	}
	digits, ok := strings.CutPrefix(value, prefix)
	if !ok || !isDecimalID(digits) {
		return "", false
	}
	return digits, true
}

// normalizeTVDbTag accepts the classic bare series ID and the TheTVDB v4
// namespaced forms ("series/431162", "movies/19799"). A bare number is only
// meaningful for series content; movie content requires the explicit
// "movies/" namespace.
func normalizeTVDbTag(value, contentType string) (string, bool) {
	if contentType == "movie" {
		digits, ok := strings.CutPrefix(value, "movies/")
		if !ok || !isDecimalID(digits) {
			return "", false
		}
		return digits, true
	}
	digits := strings.TrimPrefix(value, "series/")
	if !isDecimalID(digits) {
		return "", false
	}
	return digits, true
}

// isDecimalID reports whether s is a plain non-empty decimal number
// (ParseUint rejects signs, underscores, and the empty string).
func isDecimalID(s string) bool {
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}
