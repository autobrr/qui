// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

type recordedDelete struct {
	hashes      []string
	deleteFiles bool
	actor       qbittorrent.DeleteActor
}

// recordingDeleter records each delete and passes it on to the sync manager.
type recordingDeleter struct {
	torrentDeleter

	mu      sync.Mutex
	deletes []recordedDelete
}

func (d *recordingDeleter) DeleteTorrents(ctx context.Context, instanceID int, hashes []string, deleteFiles bool, actor qbittorrent.DeleteActor) error {
	d.mu.Lock()
	d.deletes = append(d.deletes, recordedDelete{hashes: slices.Clone(hashes), deleteFiles: deleteFiles, actor: actor})
	d.mu.Unlock()
	return d.torrentDeleter.DeleteTorrents(ctx, instanceID, hashes, deleteFiles, actor)
}

func (d *recordingDeleter) recorded() []recordedDelete {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.deletes)
}

func namedDeleteRule(id int, name, mode, tag string) *models.Automation {
	rule := deleteRule(mode, tagsContain(tag))
	rule.ID = id
	rule.Name = name
	rule.SortOrder = id
	return rule
}

// Each delete of a run names the rule that chose its torrents.
func TestApplyRules_EachDeleteNamesItsRule(t *testing.T) {
	const (
		x = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		y = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		z = "cccccccccccccccccccccccccccccccccccccccc"
	)
	byRule := func(name string) qbittorrent.DeleteActor {
		return qbittorrent.DeleteActor{Kind: qbittorrent.DeleteActorAutomation, RuleName: name}
	}

	tests := []struct {
		name     string
		torrents []stubTorrent
		rules    []*models.Automation
		want     []recordedDelete
	}{
		{
			name: "two rules with one mode send one delete each",
			torrents: []stubTorrent{
				{x, "Example.Show.S01E01.1080p.WEB-DL-GRP", "old", ""},
				{y, "Example.Show.S01E02.1080p.WEB-DL-GRP", "dead", ""},
				{z, "Example.Show.S01E03.1080p.WEB-DL-GRP", "old", ""},
			},
			rules: []*models.Automation{
				namedDeleteRule(1, "remove old", DeleteModeKeepFiles, "old"),
				namedDeleteRule(2, "remove dead", DeleteModeKeepFiles, "dead"),
			},
			want: []recordedDelete{
				{hashes: []string{y}, actor: byRule("remove dead")},
				{hashes: []string{x, z}, actor: byRule("remove old")},
			},
		},
		{
			name: "a cross-seed pulled into the delete carries the rule of its trigger",
			torrents: []stubTorrent{
				{x, "Example.Show.S01E01.1080p.WEB-DL-GRP", "old", ""},
				{y, "Example.Show.S01E01.1080p.WEB-DL-GRP", "", ""},
			},
			rules: []*models.Automation{
				namedDeleteRule(1, "remove old", DeleteModeWithFilesIncludeCrossSeeds, "old"),
			},
			want: []recordedDelete{
				{hashes: []string{x, y}, deleteFiles: true, actor: byRule("remove old")},
			},
		},
		{
			name: "a stored mode that is not a batch mode deletes nothing",
			torrents: []stubTorrent{
				{x, "Example.Show.S01E01.1080p.WEB-DL-GRP", "old", ""},
			},
			rules: []*models.Automation{
				namedDeleteRule(1, "remove old", DeleteModeNone, "old"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, instanceID, stub := newWriteStubService(t, tt.torrents)
			deleter := &recordingDeleter{torrentDeleter: svc.deleter}
			svc.deleter = deleter

			_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, tt.rules, false)
			require.NoError(t, err)

			got := deleter.recorded()
			for i := range got {
				slices.Sort(got[i].hashes)
			}
			slices.SortFunc(got, func(a, b recordedDelete) int { return cmp.Compare(a.actor.RuleName, b.actor.RuleName) })
			require.Equal(t, tt.want, got)
			var sent []string
			for _, d := range tt.want {
				sent = append(sent, d.hashes...)
			}
			slices.Sort(sent)
			require.Equal(t, sent, stub.writtenHashes()["delete"])
		})
	}
}
