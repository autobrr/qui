// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

// fakeHeld is the Transfer store: the reason that qui holds each torrent.
type fakeHeld map[string]error

func (f fakeHeld) HoldCheck(int) func(hash string) bool {
	if len(f) == 0 {
		return nil
	}
	return func(hash string) bool { return f[hash] != nil }
}

// No rule deletes or moves a torrent that a Transfer holds or a helper
// torrent. A torrent of the user with the helper tag and no registry row is a
// normal torrent.
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
	held := fakeHeld{claimed: errors.New("a Transfer holds this torrent"), helper: errors.New("qui added this torrent")}

	tests := map[string]struct {
		rule *models.Automation
		want map[string][]string
	}{
		"delete with files": {deleteRule(DeleteModeWithFilesIncludeCrossSeeds, tagsContain("del")), map[string][]string{"delete": {own}}},
		"move":              {moveRule(""), map[string][]string{"move": {own}}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			svc, instanceID, stub := newWriteStubService(t, torrents)
			svc.SetHeldTorrents(held)
			_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{tt.rule}, false)
			require.NoError(t, err)
			require.Equal(t, tt.want, stub.writtenHashes())

			svc, instanceID, stub = newWriteStubService(t, torrents)
			svc.SetHeldTorrents(held)
			rule := *tt.rule
			rule.DryRun = true
			activities, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{&rule}, true)
			require.NoError(t, err)
			require.Equal(t, tt.want, dryRunActivityHashes(t, svc, instanceID, activities), "dry run")
			require.Empty(t, stub.writtenHashes(), "dry run")
		})
	}

	t.Run("delete preview", func(t *testing.T) {
		svc, instanceID, _ := newWriteStubService(t, torrents)
		svc.SetHeldTorrents(held)
		preview, err := svc.PreviewDeleteRule(t.Context(), instanceID, deleteRule(DeleteModeWithFilesIncludeCrossSeeds, tagsContain("del")), 10, 0, "")
		require.NoError(t, err)
		hashes := make([]string, 0, len(preview.Examples))
		for _, example := range preview.Examples {
			hashes = append(hashes, example.Hash)
		}
		require.Equal(t, []string{own}, hashes)
	})
}
