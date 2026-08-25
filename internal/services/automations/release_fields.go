// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"

	"github.com/autobrr/qui/pkg/releases"
	"github.com/autobrr/qui/pkg/stringutils"
)

func parsedTorrentRelease(t qbt.Torrent, ctx *EvalContext) *rls.Release {
	if ctx == nil || ctx.ReleaseParser == nil {
		empty := &rls.Release{}
		return empty
	}
	return ctx.ReleaseParser.Parse(t.Name)
}

func torrentContentType(t qbt.Torrent, ctx *EvalContext) string {
	r := parsedTorrentRelease(t, ctx)
	info := releases.DetermineContentType(r)
	return info.ContentType
}

// torrentEffectiveName returns a stable-ish "item" identifier derived from the parsed title
// (not including tracker-specific garbage). Used for grouping and filtering.
func torrentEffectiveName(t qbt.Torrent, ctx *EvalContext) string {
	r := parsedTorrentRelease(t, ctx)

	// Prefer parsed title, fall back to normalized raw name.
	title := strings.TrimSpace(r.Title)
	if title == "" {
		return stringutils.NormalizeForMatching(t.Name)
	}

	base := stringutils.NormalizeForMatching(title)
	if base == "" {
		base = strings.ToLower(title)
	}

	// Episode == 0 means "season pack" in moistari/rls conventions.
	if r.Series > 0 {
		if r.Episode > 0 {
			return fmt.Sprintf("%s|s%02de%02d", base, r.Series, r.Episode)
		}
		// A multi-episode range ("S01E05E06") also parses with Episode 0, but it
		// is its own item: it must not share the season pack's group.
		if releases.IsEpisodeRange(r) {
			eps := make([]int, 0, 4)
			for _, se := range r.SeriesEpisodes() {
				if len(se) == 2 {
					eps = append(eps, se[1])
				}
			}
			slices.Sort(eps)
			var key strings.Builder
			fmt.Fprintf(&key, "%s|s%02d", base, r.Series)
			for _, e := range slices.Compact(eps) {
				fmt.Fprintf(&key, "e%02d", e)
			}
			return key.String()
		}
		return fmt.Sprintf("%s|s%02d", base, r.Series)
	}

	if r.Year > 0 {
		return fmt.Sprintf("%s|%d", base, r.Year)
	}

	return base
}

func torrentRlsSource(t qbt.Torrent, ctx *EvalContext) string {
	r := parsedTorrentRelease(t, ctx)
	return releases.NormalizeSource(r.Source)
}

func torrentRlsResolution(t qbt.Torrent, ctx *EvalContext) string {
	r := parsedTorrentRelease(t, ctx)
	return strings.ToUpper(strings.TrimSpace(r.Resolution))
}

func torrentRlsCodec(t qbt.Torrent, ctx *EvalContext) string {
	r := parsedTorrentRelease(t, ctx)
	return releases.JoinNormalizedCodecSlice(r.Codec)
}

func torrentRlsHDR(t qbt.Torrent, ctx *EvalContext) string {
	r := parsedTorrentRelease(t, ctx)
	return joinUpperSortedUnique(r.HDR)
}

func torrentRlsAudio(t qbt.Torrent, ctx *EvalContext) string {
	r := parsedTorrentRelease(t, ctx)
	return joinUpperSortedUnique(r.Audio)
}

func torrentRlsChannels(t qbt.Torrent, ctx *EvalContext) string {
	r := parsedTorrentRelease(t, ctx)
	return strings.ToUpper(strings.TrimSpace(r.Channels))
}

func torrentRlsGroup(t qbt.Torrent, ctx *EvalContext) string {
	r := parsedTorrentRelease(t, ctx)
	return strings.ToUpper(strings.TrimSpace(r.Group))
}

// torrentRlsYear returns the release year parsed from the torrent name.
// Returns 0 when no year could be parsed; the evaluator treats 0 as unknown (no match).
func torrentRlsYear(t qbt.Torrent, ctx *EvalContext) int64 {
	return int64(parsedTorrentRelease(t, ctx).Year)
}

func joinUpperSortedUnique(slice []string) string {
	if len(slice) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(slice))
	out := make([]string, 0, len(slice))
	for _, s := range slice {
		n := strings.ToUpper(strings.TrimSpace(s))
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}
