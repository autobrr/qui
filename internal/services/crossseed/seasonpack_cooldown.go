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

const seasonPackFailHashPrefix = "packfail:"

var releaseNameKeyRe = regexp.MustCompile(`[^\p{L}\p{N}]+`)

func canonicalReleaseNameKey(name string) string {
	return releaseNameKeyRe.ReplaceAllString(strings.ToLower(name), "")
}

func seasonPackFailKey(torrentName string) string {
	key := canonicalReleaseNameKey(torrentName)
	if key == "" {
		return ""
	}
	return seasonPackFailHashPrefix + key
}

func (s *Service) seasonPackFailCooldownActive(ctx context.Context, torrentName string, cooldown time.Duration) bool {
	key := seasonPackFailKey(torrentName)
	if s.automationStore == nil || cooldown <= 0 || key == "" {
		return false
	}
	last, found, err := s.automationStore.GetLatestSearchHistory(ctx, key)
	if err != nil {
		log.Debug().Err(err).Str("torrentName", torrentName).Msg("failed to read season pack failure cooldown")
		return false
	}
	return found && time.Since(last) < cooldown
}

func (s *Service) recordSeasonPackFailCooldown(ctx context.Context, torrentName string, response *CrossSeedResponse) {
	key := seasonPackFailKey(torrentName)
	if s.automationStore == nil || key == "" {
		return
	}
	for i := range response.Results {
		if response.Results[i].InstanceID <= 0 {
			continue
		}
		if err := s.automationStore.UpsertSearchHistory(ctx, response.Results[i].InstanceID, key, time.Now().UTC()); err != nil {
			log.Debug().Err(err).Str("torrentName", torrentName).Msg("failed to record season pack failure cooldown")
		}
		return
	}
}
