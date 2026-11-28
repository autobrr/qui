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

func (s *Service) hasActiveSeasonPackFailCooldown(ctx context.Context, cooldown time.Duration, names ...string) bool {
	for _, name := range names {
		if name != "" && s.seasonPackFailCooldownActive(ctx, name, cooldown) {
			return true
		}
	}
	return false
}

func (s *Service) resolveSeasonPackCooldownInstanceID(ctx context.Context, req *CrossSeedRequest, response *CrossSeedResponse) int {
	if response != nil {
		for i := range response.Results {
			if response.Results[i].InstanceID > 0 {
				return response.Results[i].InstanceID
			}
		}
	}
	if req != nil {
		for _, id := range req.TargetInstanceIDs {
			if id > 0 {
				return id
			}
		}
	}
	if s.instanceStore != nil {
		if instances, err := s.instanceStore.List(ctx); err == nil {
			for _, inst := range instances {
				if inst != nil && inst.ID > 0 {
					return inst.ID
				}
			}
		}
	}
	return 0
}

func (s *Service) recordSeasonPackFailCooldown(ctx context.Context, req *CrossSeedRequest, torrentName string, response *CrossSeedResponse) {
	if s.automationStore == nil {
		return
	}
	instanceID := s.resolveSeasonPackCooldownInstanceID(ctx, req, response)
	if instanceID <= 0 {
		return
	}

	now := time.Now().UTC()
	primaryKey := seasonPackFailKey(torrentName)
	if primaryKey != "" {
		if err := s.automationStore.UpsertSearchHistory(ctx, instanceID, primaryKey, now); err != nil {
			log.Debug().Err(err).Str("torrentName", torrentName).Msg("failed to record season pack failure cooldown")
		}
	}

	if req != nil && req.sourceTitle != "" {
		sourceKey := seasonPackFailKey(req.sourceTitle)
		if sourceKey != "" && sourceKey != primaryKey {
			if err := s.automationStore.UpsertSearchHistory(ctx, instanceID, sourceKey, now); err != nil {
				log.Debug().Err(err).Str("sourceTitle", req.sourceTitle).Msg("failed to record season pack failure cooldown")
			}
		}
	}
}
