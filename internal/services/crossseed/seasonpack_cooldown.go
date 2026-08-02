// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// Season-pack diversion failures cool down by normalized release name, not by
// result GUID: the same pack circulates under one name with a distinct GUID
// per indexer, and a per-GUID key would re-download it from every other
// indexer. Rows reuse cross_seed_search_history under a "packfail:" pseudo-key
// (the same trick as the "season:" ensemble pseudo-hashes, zero migrations).
// The verdict is global — coverage is computed across all eligible instances —
// so reads take the latest row for the key regardless of which instance owns
// it; the instance column only satisfies the table's FK.
const seasonPackFailHashPrefix = "packfail:"

var releaseNameKeyRe = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// canonicalReleaseNameKey strips case and separator differences so a feed
// title ("Show Title S01 ... DDP 5.1 ...") and the torrent's internal name
// ("Show.Title.S01...DDP5.1...") key identically: cooldowns are recorded
// under the latter but checked against the former, and indexers restyle
// separators inside tokens too (BeyondHD's "DDP 5.1"), so separators are
// removed rather than collapsed.
func canonicalReleaseNameKey(name string) string {
	return releaseNameKeyRe.ReplaceAllString(strings.ToLower(name), "")
}

func seasonPackFailKey(torrentName string) string {
	return seasonPackFailHashPrefix + canonicalReleaseNameKey(torrentName)
}

// seasonPackFailCooldownActive reports whether a season pack release name
// failed diversion within the cooldown window. Callers check this before
// downloading a .torrent whose only purpose is another diversion attempt.
func (s *Service) seasonPackFailCooldownActive(ctx context.Context, torrentName string, cooldown time.Duration) bool {
	if s.automationStore == nil || cooldown <= 0 {
		return false
	}
	last, found, err := s.automationStore.GetLatestSearchHistory(ctx, seasonPackFailKey(torrentName))
	if err != nil {
		log.Debug().Err(err).Str("torrentName", torrentName).Msg("failed to read season pack failure cooldown")
		return false
	}
	return found && time.Since(last) < cooldown
}

// recordSeasonPackFailCooldown stamps a failed diversion for a release name.
// The first valid instance in the response owns the row (the verdict itself is
// not tied to one instance). invalid_torrent deliberately never lands here:
// the same name from another indexer can serve a valid payload.
func (s *Service) recordSeasonPackFailCooldown(ctx context.Context, torrentName string, response *CrossSeedResponse) {
	if s.automationStore == nil {
		return
	}
	for i := range response.Results {
		if response.Results[i].InstanceID <= 0 {
			continue
		}
		if err := s.automationStore.UpsertSearchHistory(ctx, response.Results[i].InstanceID, seasonPackFailKey(torrentName), time.Now().UTC()); err != nil {
			log.Debug().Err(err).Str("torrentName", torrentName).Msg("failed to record season pack failure cooldown")
		}
		return
	}
}
