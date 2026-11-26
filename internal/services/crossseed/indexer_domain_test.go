// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/infohash"
)

func TestScopeContentPrefilterRejectionsKeepsOnlyRealIndexers(t *testing.T) {
	hash := mustParseHash(t, fakeHash("rejected"))
	rejection := contentPrefilterRejectedTorrent{Hash: hash.String(), Reason: "Size mismatch", trackerDomains: []string{"tracker.example"}}
	svc := &Service{}

	scoped := svc.scopeContentPrefilterRejectionsByIndexer(
		map[infohash.Hash]contentPrefilterRejectedTorrent{hash: rejection},
		[]int{0, 10},
		map[int]jackett.EnabledIndexerInfo{
			0:  {ID: 0, Name: "Tracker", Domain: "tracker.example"},
			10: {ID: 10, Name: "Tracker", Domain: "tracker.example"},
		},
	)

	require.Equal(t, map[string]contentPrefilterRejectedTorrent{"10|" + hash.String(): rejection}, scoped)
}
