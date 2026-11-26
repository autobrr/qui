// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/pkg/releases"
	"github.com/autobrr/qui/pkg/stringutils"
)

var explicitSeasonTag = regexp.MustCompile(`(?i)^s\d`)

func hasSingleSeason(r *rls.Release) bool {
	count := 0
	for _, tag := range r.Tags() {
		if !tag.Is(rls.TagTypeSeries) {
			continue
		}
		season, _ := tag.Series()
		if season == 0 && !explicitSeasonTag.MatchString(fmt.Sprintf("%o", tag)) {
			continue
		}
		count++
	}
	return count == 1
}

func seasonPackKey(r *rls.Release) string {
	return strings.Join([]string{
		stringutils.NormalizeForMatching(r.Title),
		strconv.Itoa(r.Series),
		joinUpperSortedUnique(r.Cut),
		joinUpperSortedUnique(r.Other),
		joinUpperSortedUnique(r.Language),
		strings.ToUpper(strings.TrimSpace(r.Resolution)),
		releases.NormalizeSource(r.Source),
		releases.JoinNormalizedCodecSlice(r.Codec),
		joinUpperSortedUnique(r.Audio),
		strings.ToUpper(strings.TrimSpace(r.Channels)),
		joinUpperSortedUnique(r.HDR),
		strings.ToUpper(strings.TrimSpace(r.Group)),
	}, "|")
}

func isSeasonPack(r *rls.Release) bool {
	return r.Episode == 0 && !releases.IsEpisodeRange(r)
}

func addSeasonPack(packs map[string]struct{}, r *rls.Release) {
	if !isSeasonPack(r) || !hasSingleSeason(r) {
		return
	}
	packs[seasonPackKey(r)] = struct{}{}
}

func seasonPackStatus(r *rls.Release, packs map[string]struct{}) string {
	if !hasSingleSeason(r) {
		return ""
	}
	if isSeasonPack(r) {
		return SeasonPackStatusPack
	}
	if _, ok := packs[seasonPackKey(r)]; ok {
		return SeasonPackStatusPacked
	}
	return SeasonPackStatusUnpacked
}

func buildSeasonPackSet(parser *releases.Parser, torrents []*qbt.Torrent) map[string]struct{} {
	packs := make(map[string]struct{})
	for i := range torrents {
		addSeasonPack(packs, parser.Parse(torrents[i].Name))
	}
	return packs
}

func (s *Service) buildAnyInstanceSeasonPackSet(ctx context.Context) map[string]struct{} {
	instances, err := s.instanceStore.List(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("automations: failed to list instances for season pack status")
		return nil
	}
	packs := make(map[string]struct{})
	for _, inst := range instances {
		if !inst.IsActive {
			continue
		}
		views, err := s.filesReader.GetCachedInstanceTorrents(ctx, inst.ID)
		if err != nil {
			log.Debug().Err(err).Int("instanceID", inst.ID).Msg("automations: failed to read cached torrents for season pack status, skipping instance")
			continue
		}
		for i := range views {
			addSeasonPack(packs, s.releaseParser.Parse(views[i].Name))
		}
	}
	return packs
}
