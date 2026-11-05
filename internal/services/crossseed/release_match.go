// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"fmt"
	"strings"

	"github.com/autobrr/rls"
	"github.com/rs/zerolog/log"
)

func evaluateReleaseMatch(source, candidate *rls.Release) (float64, string) {
	score := 1.0
	reasons := make([]string, 0, 6)

	if source.Group != "" && candidate.Group != "" && strings.EqualFold(source.Group, candidate.Group) {
		score += 0.35
		reasons = append(reasons, "group "+candidate.Group)
	}
	if source.Resolution != "" && candidate.Resolution != "" && strings.EqualFold(source.Resolution, candidate.Resolution) {
		score += 0.15
		reasons = append(reasons, "resolution "+candidate.Resolution)
	}
	if source.Source != "" && candidate.Source != "" && strings.EqualFold(source.Source, candidate.Source) {
		score += 0.1
		reasons = append(reasons, "source "+candidate.Source)
	}
	if source.Series > 0 && candidate.Series == source.Series {
		reasons = append(reasons, fmt.Sprintf("season %d", source.Series))
		score += 0.1
		if source.Episode > 0 && candidate.Episode == source.Episode {
			reasons = append(reasons, fmt.Sprintf("episode %d", source.Episode))
			score += 0.1
		}
	}
	if source.Year > 0 && candidate.Year == source.Year {
		score += 0.05
		reasons = append(reasons, fmt.Sprintf("year %d", source.Year))
	}
	if len(source.Codec) > 0 && len(candidate.Codec) > 0 && strings.EqualFold(source.Codec[0], candidate.Codec[0]) {
		score += 0.05
		reasons = append(reasons, "codec "+candidate.Codec[0])
	}
	if len(source.Audio) > 0 && len(candidate.Audio) > 0 && strings.EqualFold(source.Audio[0], candidate.Audio[0]) {
		score += 0.05
		reasons = append(reasons, "audio "+candidate.Audio[0])
	}

	if len(reasons) == 0 {
		reasons = append(reasons, "title match")
	}

	return score, strings.Join(reasons, ", ")
}

type releaseFilterDebugInfo struct {
	Type       string   `json:"type,omitempty"`
	Title      string   `json:"title,omitempty"`
	Subtitle   string   `json:"subtitle,omitempty"`
	Alt        string   `json:"alt,omitempty"`
	Collection string   `json:"collection,omitempty"`
	Year       int      `json:"year,omitempty"`
	Month      int      `json:"month,omitempty"`
	Day        int      `json:"day,omitempty"`
	Series     int      `json:"series,omitempty"`
	Episode    int      `json:"episode,omitempty"`
	Group      string   `json:"group,omitempty"`
	Site       string   `json:"site,omitempty"`
	Sum        string   `json:"sum,omitempty"`
	Source     string   `json:"source,omitempty"`
	Resolution string   `json:"resolution,omitempty"`
	Codec      []string `json:"codec,omitempty"`
	HDR        []string `json:"hdr,omitempty"`
	BitDepth   string   `json:"bit_depth,omitempty"`
	Audio      []string `json:"audio,omitempty"`
	Cut        []string `json:"cut,omitempty"`
	Edition    []string `json:"edition,omitempty"`
	Language   []string `json:"language,omitempty"`
	Version    string   `json:"version,omitempty"`
	Disc       string   `json:"disc,omitempty"`
	Platform   string   `json:"platform,omitempty"`
	Arch       string   `json:"arch,omitempty"`
}

func recordReleaseRejection(
	releaseFilterReasons map[string]int,
	reason string,
	sourceTitle string,
	candidateTitle string,
	findIndividualEpisodes bool,
	sourceRelease releaseFilterDebugInfo,
	candidateRelease releaseFilterDebugInfo,
	message string,
) {
	if reason == "" {
		reason = "release mismatch"
	}
	releaseFilterReasons[reason]++
	// The parsed release pair is developer detail. The caller logs the reason.
	if evt := log.Trace(); evt.Enabled() {
		evt.
			Str("sourceTitle", sourceTitle).
			Str("candidateTitle", candidateTitle).
			Str("reason", reason).
			Bool("findIndividualEpisodes", findIndividualEpisodes).
			Interface("sourceRelease", sourceRelease).
			Interface("candidateRelease", candidateRelease).
			Msg(message)
	}
}

func logReleaseMatchDecision(
	sourceTitle string,
	candidateTitle string,
	findIndividualEpisodes bool,
	sourceRelease *rls.Release,
	candidateRelease *rls.Release,
	matched bool,
	reason string,
	message string,
) {
	if reason == "" {
		if matched {
			reason = "matched"
		} else {
			reason = "release mismatch"
		}
	}
	// TRACE, not DEBUG: the only caller runs this for each pair that the
	// cross-match index selects, on every cross-match build.
	if evt := log.Trace(); evt.Enabled() {
		evt.
			Str("sourceTitle", sourceTitle).
			Str("candidateTitle", candidateTitle).
			Bool("matched", matched).
			Str("reason", reason).
			Bool("findIndividualEpisodes", findIndividualEpisodes).
			Interface("sourceRelease", releaseFilterDebugInfoFrom(sourceRelease)).
			Interface("candidateRelease", releaseFilterDebugInfoFrom(candidateRelease)).
			Msg(message)
	}
}

func releaseFilterDebugInfoFrom(release *rls.Release) releaseFilterDebugInfo {
	if release == nil {
		return releaseFilterDebugInfo{}
	}

	return releaseFilterDebugInfo{
		Type:       release.Type.String(),
		Title:      release.Title,
		Subtitle:   release.Subtitle,
		Alt:        release.Alt,
		Collection: release.Collection,
		Year:       release.Year,
		Month:      release.Month,
		Day:        release.Day,
		Series:     release.Series,
		Episode:    release.Episode,
		Group:      release.Group,
		Site:       release.Site,
		Sum:        release.Sum,
		Source:     release.Source,
		Resolution: release.Resolution,
		Codec:      append([]string(nil), release.Codec...),
		HDR:        append([]string(nil), release.HDR...),
		BitDepth:   release.BitDepth,
		Audio:      append([]string(nil), release.Audio...),
		Cut:        append([]string(nil), release.Cut...),
		Edition:    append([]string(nil), release.Edition...),
		Language:   append([]string(nil), release.Language...),
		Version:    release.Version,
		Disc:       release.Disc,
		Platform:   release.Platform,
		Arch:       release.Arch,
	}
}
