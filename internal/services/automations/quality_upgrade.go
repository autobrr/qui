// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"fmt"
	"math"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/releases"
	"github.com/autobrr/qui/pkg/stringutils"
)

// noValueRank is the rank assigned when a field value is absent from a tier's value_order.
// It must be larger than any valid index so absent values sort last (worst quality).
const noValueRank = math.MaxInt32

// qualityGroupKey builds the group key for a torrent given a profile's group_fields list.
// Torrents with the same key are treated as "the same content" and compared by quality.
func qualityGroupKey(t qbt.Torrent, profile *models.QualityProfile, parser *releases.Parser) string {
	r := parser.Parse(t.Name)

	parts := make([]string, 0, len(profile.GroupFields))
	for _, field := range profile.GroupFields {
		var part string
		switch field {
		case models.QualityGroupFieldTitle:
			raw := strings.TrimSpace(r.Title)
			if raw == "" {
				raw = t.Name
			}
			part = stringutils.NormalizeForMatching(raw)
		case models.QualityGroupFieldSubtitle:
			part = stringutils.NormalizeForMatching(r.Subtitle)
		case models.QualityGroupFieldArtist:
			part = stringutils.NormalizeForMatching(r.Artist)
		case models.QualityGroupFieldPlatform:
			part = stringutils.NormalizeForMatching(r.Platform)
		case models.QualityGroupFieldCollection:
			part = stringutils.NormalizeForMatching(r.Collection)
		case models.QualityGroupFieldYear:
			if r.Year > 0 {
				part = fmt.Sprintf("%d", r.Year)
			}
		case models.QualityGroupFieldMonth:
			if r.Month > 0 {
				part = fmt.Sprintf("%02d", r.Month)
			}
		case models.QualityGroupFieldDay:
			if r.Day > 0 {
				part = fmt.Sprintf("%02d", r.Day)
			}
		case models.QualityGroupFieldSeries:
			if r.Series > 0 {
				part = fmt.Sprintf("s%02d", r.Series)
			}
		case models.QualityGroupFieldEpisode:
			if r.Episode > 0 {
				part = fmt.Sprintf("e%02d", r.Episode)
			}
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "|")
}

// rankFieldValue returns the rank of a single string value within a value_order slice.
// Comparison is case-insensitive. Returns noValueRank when the value is absent.
func rankFieldValue(raw string, valueOrder []string) int {
	upper := strings.ToUpper(strings.TrimSpace(raw))
	for i, v := range valueOrder {
		if strings.ToUpper(strings.TrimSpace(v)) == upper {
			return i
		}
	}
	return noValueRank
}

// rankSliceField returns the best (minimum) rank across all values in a slice field.
// Returns noValueRank when no value matches.
func rankSliceField(vals []string, valueOrder []string) int {
	best := noValueRank
	for _, v := range vals {
		if r := rankFieldValue(v, valueOrder); r < best {
			best = r
		}
	}
	return best
}

// qualityRankVector computes the quality rank vector for a torrent name using the profile's
// ranking tiers. Each element corresponds to one tier; lower values indicate higher quality.
func qualityRankVector(name string, profile *models.QualityProfile, parser *releases.Parser) []int {
	rel := parser.Parse(name)
	ranks := make([]int, len(profile.RankingTiers))

	for i, tier := range profile.RankingTiers {
		switch tier.Field {
		case models.QualityRankFieldResolution:
			ranks[i] = rankFieldValue(rel.Resolution, tier.ValueOrder)

		case models.QualityRankFieldSource:
			normSource := releases.NormalizeSource(rel.Source)
			normOrder := make([]string, len(tier.ValueOrder))
			for j, v := range tier.ValueOrder {
				normOrder[j] = releases.NormalizeSource(v)
			}
			ranks[i] = rankFieldValue(normSource, normOrder)

		case models.QualityRankFieldCodec:
			normCodecs := make([]string, len(rel.Codec))
			for j, c := range rel.Codec {
				normCodecs[j] = releases.NormalizeVideoCodec(c)
			}
			normOrder := make([]string, len(tier.ValueOrder))
			for j, v := range tier.ValueOrder {
				normOrder[j] = releases.NormalizeVideoCodec(v)
			}
			ranks[i] = rankSliceField(normCodecs, normOrder)

		case models.QualityRankFieldHDR:
			ranks[i] = rankSliceField(rel.HDR, tier.ValueOrder)

		case models.QualityRankFieldAudio:
			ranks[i] = rankSliceField(rel.Audio, tier.ValueOrder)

		case models.QualityRankFieldChannels:
			ranks[i] = rankFieldValue(rel.Channels, tier.ValueOrder)

		case models.QualityRankFieldContainer:
			ranks[i] = rankFieldValue(rel.Container, tier.ValueOrder)

		case models.QualityRankFieldOther:
			ranks[i] = rankSliceField(rel.Other, tier.ValueOrder)

		case models.QualityRankFieldCut:
			ranks[i] = rankSliceField(rel.Cut, tier.ValueOrder)

		case models.QualityRankFieldEdition:
			ranks[i] = rankSliceField(rel.Edition, tier.ValueOrder)

		case models.QualityRankFieldLanguage:
			ranks[i] = rankSliceField(rel.Language, tier.ValueOrder)

		case models.QualityRankFieldRegion:
			ranks[i] = rankFieldValue(rel.Region, tier.ValueOrder)

		case models.QualityRankFieldGroup:
			ranks[i] = rankFieldValue(rel.Group, tier.ValueOrder)

		default:
			ranks[i] = noValueRank
		}
	}
	return ranks
}

// rankVectorLess returns true when rank vector a is strictly better than b
// (lexicographically smaller = higher quality).
func rankVectorLess(a, b []int) bool {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := range n {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return false
}

// torrentQualityInfo bundles a torrent hash with its quality group key and rank vector.
type torrentQualityInfo struct {
	hash     string
	groupKey string
	ranks    []int
}

// buildQualityBestAndInferiorSets returns two sets for the given profile and torrent list.
//   - bestSet: hashes of torrents that are tied for best quality in their content group
//   - inferiorSet: hashes of torrents that have at least one better-quality peer in the same group
//
// Only torrents that share a content group with at least one other torrent are included.
// Lone members of a group (no peers) are excluded from both sets.
func buildQualityBestAndInferiorSets(
	torrents []qbt.Torrent,
	profile *models.QualityProfile,
	parser *releases.Parser,
) (bestSet, inferiorSet map[string]struct{}) {
	if len(torrents) == 0 || len(profile.GroupFields) == 0 || len(profile.RankingTiers) == 0 {
		return nil, nil
	}

	infos := make([]torrentQualityInfo, 0, len(torrents))
	for _, t := range torrents {
		key := qualityGroupKey(t, profile, parser)
		if key == "" {
			continue
		}
		infos = append(infos, torrentQualityInfo{
			hash:     t.Hash,
			groupKey: key,
			ranks:    qualityRankVector(t.Name, profile, parser),
		})
	}

	byGroup := make(map[string][]torrentQualityInfo, len(infos))
	for _, info := range infos {
		byGroup[info.groupKey] = append(byGroup[info.groupKey], info)
	}

	bestSet = make(map[string]struct{})
	inferiorSet = make(map[string]struct{})

	for _, members := range byGroup {
		if len(members) < 2 {
			// No peers to compare against — exclude from both sets.
			continue
		}

		// Find the best (lexicographically smallest) rank vector in this group.
		bestRanks := members[0].ranks
		for _, m := range members[1:] {
			if rankVectorLess(m.ranks, bestRanks) {
				bestRanks = m.ranks
			}
		}

		for _, m := range members {
			if rankVectorLess(bestRanks, m.ranks) {
				// Strictly worse than the best member → inferior.
				inferiorSet[m.hash] = struct{}{}
			} else {
				// Ties for best in this group.
				bestSet[m.hash] = struct{}{}
			}
		}
	}

	if len(bestSet) == 0 {
		bestSet = nil
	}
	if len(inferiorSet) == 0 {
		inferiorSet = nil
	}
	return bestSet, inferiorSet
}

// collectQualityProfileIDsFromCondition recursively walks a condition tree and
// adds any referenced quality profile IDs to the seen set.
func collectQualityProfileIDsFromCondition(cond *models.RuleCondition, seen map[int]struct{}) {
	if cond == nil {
		return
	}
	if (cond.Field == models.FieldQualityIsBest || cond.Field == models.FieldQualityIsInferior) && cond.QualityProfileID > 0 {
		seen[cond.QualityProfileID] = struct{}{}
	}
	for _, child := range cond.Conditions {
		collectQualityProfileIDsFromCondition(child, seen)
	}
}

// collectQualityProfileIDsFromActions collects all quality profile IDs referenced
// in any action condition within an ActionConditions struct.
func collectQualityProfileIDsFromActions(ac *models.ActionConditions, seen map[int]struct{}) {
	if ac == nil {
		return
	}
	if ac.SpeedLimits != nil {
		collectQualityProfileIDsFromCondition(ac.SpeedLimits.Condition, seen)
	}
	if ac.ShareLimits != nil {
		collectQualityProfileIDsFromCondition(ac.ShareLimits.Condition, seen)
	}
	if ac.Pause != nil {
		collectQualityProfileIDsFromCondition(ac.Pause.Condition, seen)
	}
	if ac.Resume != nil {
		collectQualityProfileIDsFromCondition(ac.Resume.Condition, seen)
	}
	if ac.Recheck != nil {
		collectQualityProfileIDsFromCondition(ac.Recheck.Condition, seen)
	}
	if ac.Reannounce != nil {
		collectQualityProfileIDsFromCondition(ac.Reannounce.Condition, seen)
	}
	if ac.Delete != nil {
		collectQualityProfileIDsFromCondition(ac.Delete.Condition, seen)
	}
	if ac.Tag != nil {
		collectQualityProfileIDsFromCondition(ac.Tag.Condition, seen)
	}
	for _, tag := range ac.Tags {
		if tag != nil {
			collectQualityProfileIDsFromCondition(tag.Condition, seen)
		}
	}
	if ac.Category != nil {
		collectQualityProfileIDsFromCondition(ac.Category.Condition, seen)
	}
	if ac.Move != nil {
		collectQualityProfileIDsFromCondition(ac.Move.Condition, seen)
	}
	if ac.ExternalProgram != nil {
		collectQualityProfileIDsFromCondition(ac.ExternalProgram.Condition, seen)
	}
}

// PreComputeQualitySets scans all enabled rule conditions for QUALITY_IS_BEST /
// QUALITY_IS_INFERIOR references, then pre-computes per-profile best and inferior
// hash sets for use by the condition evaluator.
//
// Returns nil maps when no rules reference quality profiles.
func PreComputeQualitySets(
	ctx context.Context,
	rules []*models.Automation,
	torrents []qbt.Torrent,
	profileStore *models.QualityProfileStore,
	parser *releases.Parser,
) (bestByProfile, inferiorByProfile map[int]map[string]struct{}) {
	// Collect all unique quality profile IDs referenced in any rule condition.
	seenIDs := make(map[int]struct{})
	for _, rule := range rules {
		if rule.Conditions == nil {
			continue
		}
		collectQualityProfileIDsFromActions(rule.Conditions, seenIDs)
	}
	if len(seenIDs) == 0 {
		return nil, nil
	}
	if profileStore == nil {
		log.Warn().Msg("quality: profile store unavailable; skipping quality condition pre-computation")
		return nil, nil
	}

	for profileID := range seenIDs {
		profile, err := profileStore.Get(ctx, profileID)
		if err != nil {
			log.Error().Err(err).Int("profileID", profileID).
				Msg("quality: failed to load quality profile; conditions referencing it will not match")
			continue
		}
		best, inferior := buildQualityBestAndInferiorSets(torrents, profile, parser)
		if best != nil {
			if bestByProfile == nil {
				bestByProfile = make(map[int]map[string]struct{})
			}
			bestByProfile[profileID] = best
		}
		if inferior != nil {
			if inferiorByProfile == nil {
				inferiorByProfile = make(map[int]map[string]struct{})
			}
			inferiorByProfile[profileID] = inferior
		}
	}
	return bestByProfile, inferiorByProfile
}
