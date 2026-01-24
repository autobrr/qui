// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anacrolix/torrent/bencode"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/qbittorrent"
)

var trackerPatchWebAPIVersions = map[string]struct{}{
	"2.9.1": {},
	"2.9.2": {},
	"2.9.3": {},
}

// patchTorrentTrackers ensures the exported .torrent includes tracker metadata.
// Some qBittorrent 4.6.x builds omit tracker information when exporting a
// torrent. When detected, we inject the trackers retrieved from the API into
// both the announce field and the announce-list. The function returns the
// (possibly mutated) payload along with a flag indicating whether changes were
// applied.
func patchTorrentTrackers(data []byte, trackers []string) ([]byte, bool, error) {
	if len(trackers) == 0 {
		return data, false, nil
	}

	var root map[string]any
	if err := bencode.Unmarshal(data, &root); err != nil {
		return data, false, fmt.Errorf("decode torrent: %w", err)
	}

	announce := firstTracker(trackers)
	changed := false

	if announce != "" {
		if !trackerMatches(root["announce"], trackers) {
			root["announce"] = announce
			changed = true
		}
	}

	if needAnnounceListUpdate(root["announce-list"], trackers) {
		root["announce-list"] = buildAnnounceList(trackers)
		changed = true
	}

	if !changed {
		return data, false, nil
	}

	var buf bytes.Buffer
	if err := bencode.NewEncoder(&buf).Encode(root); err != nil {
		return data, false, fmt.Errorf("encode torrent: %w", err)
	}

	return buf.Bytes(), true, nil
}

func shouldInjectTrackerMetadata(apiVersion string) bool {
	apiVersion = strings.TrimSpace(apiVersion)
	if apiVersion == "" {
		return false
	}

	_, ok := trackerPatchWebAPIVersions[apiVersion]
	return ok
}

func needAnnounceListUpdate(existing any, trackers []string) bool {
	if len(trackers) == 0 {
		return false
	}

	current := flattenAnnounceList(existing)
	if len(current) != len(trackers) {
		return true
	}

	for i, tr := range trackers {
		if strings.TrimSpace(current[i]) != strings.TrimSpace(tr) {
			return true
		}
	}

	return false
}

func firstTracker(trackers []string) string {
	for _, tr := range trackers {
		if tr != "" {
			return tr
		}
	}
	return ""
}

func gatherTrackerURLs(ctx context.Context, sm *qbittorrent.SyncManager, instanceID int, torrent qbt.Torrent) []string {
	seen := make(map[string]struct{})
	var trackers []string

	appendTracker := func(url string) {
		url = strings.TrimSpace(url)
		if url == "" {
			return
		}
		if _, ok := seen[url]; ok {
			return
		}
		seen[url] = struct{}{}
		trackers = append(trackers, url)
	}

	for _, tr := range torrent.Trackers {
		appendTracker(tr.Url)
	}

	if len(trackers) == 0 {
		extra, err := sm.GetTorrentTrackers(ctx, instanceID, torrent.Hash)
		if err == nil {
			for _, tr := range extra {
				appendTracker(tr.Url)
			}
		} else if !errors.Is(err, qbt.ErrTorrentNotFound) {
			log.Debug().Err(err).Str("hash", torrent.Hash).Int("instanceID", instanceID).Msg("Failed to fetch trackers for patching export")
		}
	}

	if len(trackers) == 0 {
		appendTracker(torrent.Tracker)
	}

	return trackers
}

func trackerMatches(value any, trackers []string) bool {
	s := ""
	switch v := value.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return false
	}

	for _, tr := range trackers {
		if strings.TrimSpace(tr) == strings.TrimSpace(s) {
			return true
		}
	}
	return false
}

func buildAnnounceList(trackers []string) []any {
	result := make([]any, 0, len(trackers))
	for _, tr := range trackers {
		result = append(result, []any{tr})
	}
	return result
}

func flattenAnnounceList(v any) []string {
	switch val := v.(type) {
	case nil:
		return nil
	case string:
		return []string{val}
	case []byte:
		return []string{string(val)}
	case []any:
		var out []string
		for _, entry := range val {
			switch inner := entry.(type) {
			case string:
				out = append(out, inner)
			case []byte:
				out = append(out, string(inner))
			case []any:
				flattened := flattenAnnounceList(inner)
				if len(flattened) > 0 {
					out = append(out, flattened[0])
				}
			}
		}
		return out
	default:
		return nil
	}
}
