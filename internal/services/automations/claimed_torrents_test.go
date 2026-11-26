// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

type fakeTransfers struct{ claimed, helper string }

func (f fakeTransfers) ClaimCheck(int) func(hash string) bool {
	return func(hash string) bool { return hash == f.claimed }
}

func (f fakeTransfers) HelperKinds(int) map[string]string {
	return map[string]string{f.helper: "transfer"}
}

func TestRulesLeaveOutTorrentsThatQuiHolds(t *testing.T) {
	const (
		claimed = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		helper  = "ffffffffffffffffffffffffffffffffffffffff"
		own     = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	torrents := []stubTorrent{
		{claimed, "Example.Show.S01E01.1080p.WEB-DL-GRP", "del,mv", ""},
		{helper, "Example.Show.S01E02.1080p.WEB-DL-GRP", "del,mv,qui-transfer", ""},
		{own, "Example.Show.S01E03.1080p.WEB-DL-GRP", "del,mv,qui-transfer", ""},
	}
	transfers := fakeTransfers{claimed: claimed, helper: helper}
	newService := func(t *testing.T) (*Service, int, *stubQbt) {
		svc, instanceID, stub := newWriteStubService(t, torrents)
		svc.syncManager.SetHelperTorrents(transfers)
		svc.SetClaimedTorrents(transfers)
		return svc, instanceID, stub
	}

	tests := map[string]struct {
		rule *models.Automation
		want map[string][]string
	}{
		"delete with files": {deleteRule(DeleteModeWithFilesIncludeCrossSeeds, tagsContain("del")), map[string][]string{"delete": {own}}},
		"move":              {moveRule(""), map[string][]string{"move": {own}}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			svc, instanceID, stub := newService(t)
			_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{tt.rule}, false)
			require.NoError(t, err)
			require.Equal(t, tt.want, stub.writtenHashes())

			svc, instanceID, stub = newService(t)
			rule := *tt.rule
			rule.DryRun = true
			activities, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{&rule}, true)
			require.NoError(t, err)
			require.Equal(t, tt.want, dryRunActivityHashes(t, svc, instanceID, activities), "dry run")
			require.Empty(t, stub.writtenHashes(), "dry run")
		})
	}

	t.Run("delete preview", func(t *testing.T) {
		svc, instanceID, _ := newService(t)
		preview, err := svc.PreviewDeleteRule(t.Context(), instanceID, deleteRule(DeleteModeWithFilesIncludeCrossSeeds, tagsContain("del")), 10, 0, "")
		require.NoError(t, err)
		hashes := make([]string, 0, len(preview.Examples))
		for _, example := range preview.Examples {
			hashes = append(hashes, example.Hash)
		}
		require.Equal(t, []string{own}, hashes)
	})
}
