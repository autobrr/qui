// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/pkg/releases"
	"github.com/autobrr/qui/pkg/stringutils"
)

// explicitSeasonTag matches the original capture of a series tag that names a
// season zero, such as S00 or S00E02. An absolute-numbered anime episode ("01")
// also parses with season 0 but names no season, so it has no pack.
var explicitSeasonTag = regexp.MustCompile(`(?i)^s\d`)

// releaseSeasons returns the sorted seasons the release names, nil when it
// names none. A multi-season pack such as S01-S03 returns its bounds.
func releaseSeasons(r *rls.Release) []int {
	var seasons []int
	for _, tag := range r.Tags() {
		if !tag.Is(rls.TagTypeSeries) {
			continue
		}
		season, _ := tag.Series()
		if season == 0 && !explicitSeasonTag.MatchString(fmt.Sprintf("%o", tag)) {
			continue
		}
		seasons = append(seasons, season)
	}
	slices.Sort(seasons)
	return seasons
}

// seasonPackKey identifies one season of one release in pack form. The field
// list is deliberate: Subtitle stays out because an episode title lands there,
// and REPACK, PROPER, and RERIP stay in through Other, so a repacked episode is
// not covered by a plain pack. The key has to be safe under a delete action.
func seasonPackKey(r *rls.Release, season int) string {
	return strings.Join([]string{
		stringutils.NormalizeForMatching(r.Title),
		strconv.Itoa(season),
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

// addSeasonPack records every season a pack release covers. Episodes, episode
// ranges, and names without a season are ignored.
func addSeasonPack(packs map[string]struct{}, r *rls.Release) {
	if !isSeasonPack(r) {
		return
	}
	seasons := releaseSeasons(r)
	if len(seasons) == 0 {
		return
	}
	for season := seasons[0]; season <= seasons[len(seasons)-1]; season++ {
		packs[seasonPackKey(r, season)] = struct{}{}
	}
}

// seasonPackStatus reports pack, packed, unpacked, or "" for a release without
// a season. An episode range counts as an episode.
func seasonPackStatus(r *rls.Release, packs map[string]struct{}) string {
	if len(releaseSeasons(r)) == 0 {
		return ""
	}
	if isSeasonPack(r) {
		return SeasonPackStatusPack
	}
	if _, ok := packs[seasonPackKey(r, r.Series)]; ok {
		return SeasonPackStatusPacked
	}
	return SeasonPackStatusUnpacked
}

// buildSeasonPackSet indexes the season packs among torrents.
func buildSeasonPackSet(parser *releases.Parser, torrents []qbt.Torrent) map[string]struct{} {
	packs := make(map[string]struct{})
	for i := range torrents {
		addSeasonPack(packs, parser.Parse(torrents[i].Name))
	}
	return packs
}

// buildAnyInstanceSeasonPackSet indexes the season packs cached for every active
// instance. Zero qBittorrent requests. Returns nil when any instance cannot be
// read: a missing instance would report its episodes as unpacked, which is not
// safe under a delete action, so the status stays unknown instead.
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
			log.Warn().Err(err).Int("instanceID", inst.ID).Msg("automations: failed to read cached torrents for season pack status")
			return nil
		}
		for i := range views {
			addSeasonPack(packs, s.releaseParser.Parse(views[i].Name))
		}
	}
	return packs
}

// setupPreviewSeasonPackContext builds the season pack sets a preview needs.
func (s *Service) setupPreviewSeasonPackContext(ctx context.Context, cond *RuleCondition, torrents []qbt.Torrent, evalCtx *EvalContext) {
	if ConditionUsesField(cond, FieldSeasonPackStatus) {
		evalCtx.SeasonPackSet = buildSeasonPackSet(evalCtx.ReleaseParser, torrents)
	}
	if ConditionUsesField(cond, FieldSeasonPackStatusAnyInstance) {
		evalCtx.SeasonPackSetAnyInstance = s.buildAnyInstanceSeasonPackSet(ctx)
	}
}
