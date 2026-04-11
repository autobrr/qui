// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"errors"
	"testing"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	"github.com/stretchr/testify/require"
)

func TestTrackerDomainsMatchIndexer_DoesNotCrossMatchSpecificIndexerDomainAcrossTLD(t *testing.T) {
	t.Parallel()

	service := &Service{
		jackettService:     newFailingJackettService(errors.New("jackett lookup should not run in this test")),
		indexerDomainCache: ttlcache.New(ttlcache.Options[string, string]{}),
		domainMappings:     trackerDomainAliases,
	}
	_ = service.indexerDomainCache.Set("My Indexer", "example.org", ttlcache.DefaultTTL)

	matched := service.trackerDomainsMatchIndexer([]string{"example.cc"}, "My Indexer")
	require.False(t, matched)
}

func TestTrackerDomainsMatchIndexer_MatchesSpecificIndexerDomainDirectly(t *testing.T) {
	t.Parallel()

	service := &Service{
		jackettService:     newFailingJackettService(errors.New("jackett lookup should not run in this test")),
		indexerDomainCache: ttlcache.New(ttlcache.Options[string, string]{}),
		domainMappings:     trackerDomainAliases,
	}
	_ = service.indexerDomainCache.Set("My Indexer", "example.org", ttlcache.DefaultTTL)

	matched := service.trackerDomainsMatchIndexer([]string{"example.org"}, "My Indexer")
	require.True(t, matched)
}

func TestTrackerDomainsMatchIndexer_MatchesSpecificIndexerDomainBySubdomain(t *testing.T) {
	t.Parallel()

	service := &Service{
		jackettService:     newFailingJackettService(errors.New("jackett lookup should not run in this test")),
		indexerDomainCache: ttlcache.New(ttlcache.Options[string, string]{}),
		domainMappings:     trackerDomainAliases,
	}
	_ = service.indexerDomainCache.Set("My Indexer", "example.org", ttlcache.DefaultTTL)

	matched := service.trackerDomainsMatchIndexer([]string{"tracker.example.org"}, "My Indexer")
	require.True(t, matched)
}
