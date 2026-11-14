// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package trackergroup_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/trackergroup"
)

func TestMapper(t *testing.T) {
	t.Parallel()

	merged := &models.TrackerCustomization{
		DisplayName: "Example",
		Domains:     []string{"tracker.example.test", "Announce.Example.test"},
	}
	unnamed := &models.TrackerCustomization{Domains: []string{"unnamed.example.test"}}
	mapper := trackergroup.New([]*models.TrackerCustomization{merged, unnamed})

	private, public := true, false

	tests := []struct {
		name       string
		domains    []string
		private    *bool
		wantGroup  trackergroup.Group
		wantStored string
	}{
		{
			name:       "a customization holds one domain",
			domains:    []string{"zzz.example.test", "announce.example.test"},
			private:    &private,
			wantGroup:  trackergroup.Group{Kind: trackergroup.Customization, Name: "Example"},
			wantStored: "announce.example.test",
		},
		{
			name:       "a customization wins over a lower domain",
			domains:    []string{"a.example.test", "tracker.example.test"},
			private:    &private,
			wantGroup:  trackergroup.Group{Kind: trackergroup.Customization, Name: "Example"},
			wantStored: "tracker.example.test",
		},
		{
			name:       "three domains with no merge give the first domain",
			domains:    []string{"c.example.test", "a.example.test", "b.example.test"},
			private:    &private,
			wantGroup:  trackergroup.Group{Kind: trackergroup.Domain, Name: "a.example.test"},
			wantStored: "a.example.test",
		},
		{
			name:       "a customization with no name shows its domain",
			domains:    []string{"unnamed.example.test"},
			private:    &private,
			wantGroup:  trackergroup.Group{Kind: trackergroup.Customization, Name: "unnamed.example.test"},
			wantStored: "unnamed.example.test",
		},
		{
			name:       "a public torrent is in Public, also with a merged domain",
			domains:    []string{"tracker.example.test", "a.example.test"},
			private:    &public,
			wantGroup:  trackergroup.Group{Kind: trackergroup.Public, Name: "Public"},
			wantStored: "tracker.example.test",
		},
		{
			name:       "an unknown private flag applies the tracker rules",
			domains:    []string{"b.example.test", "a.example.test"},
			private:    nil,
			wantGroup:  trackergroup.Group{Kind: trackergroup.Domain, Name: "a.example.test"},
			wantStored: "a.example.test",
		},
		{
			name:       "no tracker",
			domains:    nil,
			private:    &private,
			wantGroup:  trackergroup.Group{Kind: trackergroup.NoTracker, Name: "No tracker"},
			wantStored: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.wantGroup, mapper.Of(tt.domains, tt.private))

			stored := mapper.StoredDomain(tt.domains)
			assert.Equal(t, tt.wantStored, stored)

			// The stored domain gives the same group when read later. Public
			// comes from the private flag, which the domain does not carry.
			if tt.wantGroup.Kind == trackergroup.Public || stored == "" {
				return
			}
			assert.Equal(t, tt.wantGroup, mapper.Of([]string{stored}, tt.private))
		})
	}
}
