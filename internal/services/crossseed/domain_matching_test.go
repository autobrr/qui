// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"errors"
	"testing"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	"github.com/stretchr/testify/require"
)

func TestTrackerDomainsMatchIndexer_SpecificIndexerDomainMatching(t *testing.T) {
	t.Parallel()

	service := &Service{
		jackettService:     newFailingJackettService(errors.New("jackett lookup should not run in this test")),
		indexerDomainCache: ttlcache.New(ttlcache.Options[string, string]{}),
		domainMappings:     trackerDomainAliases,
	}
	_ = service.indexerDomainCache.Set("My Indexer", "example.org", ttlcache.DefaultTTL)

	tests := []struct {
		name     string
		domains  []string
		expected bool
	}{
		{
			name:     "does not match unrelated domain",
			domains:  []string{"unrelated-site.cc"},
			expected: false,
		},
		{
			name:     "matches specific indexer domain directly",
			domains:  []string{"example.org"},
			expected: true,
		},
		{
			name:     "matches specific indexer domain by subdomain",
			domains:  []string{"tracker.example.org"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			matched := service.trackerDomainsMatchIndexerDomain(tt.domains, "My Indexer", "example.org")
			require.Equal(t, tt.expected, matched)
		})
	}
}
