// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlink"
)

type failedIdentityBackend struct{ fsops.Backend }

func (b failedIdentityBackend) Lstat(ctx context.Context, path string) (*fsops.LstatInfo, error) {
	info, err := b.Backend.Lstat(ctx, path)
	if err == nil && info.Mode.IsRegular() {
		info.FileID, info.Nlinks, info.FileIDErr = hardlink.FileID{}, 0, fmt.Errorf("statx %s: %w", path, syscall.EIO)
	}
	return info, err
}

func TestGetHardlinkIndex_FailedIdentityReadLeavesNoScope(t *testing.T) {
	const hash = "dddddddddddddddddddddddddddddddddddddddd"
	const name = "Film.2021.1080p.BluRay.x264-GRP.mkv"
	dir := t.TempDir()
	savePath := filepath.Join(dir, "torrents")
	createFile(t, filepath.Join(savePath, name))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "library"), 0o700))
	require.NoError(t, os.Link(filepath.Join(savePath, name), filepath.Join(dir, "library", name)))
	torrent := qbt.Torrent{Hash: hash, SavePath: savePath}
	files := qbt.TorrentFiles{{Name: name, Priority: 1}}

	readable := newHardlinkIndexService(t, 904, localbackend.NewBackend(), files)
	require.Equal(t, HardlinkScopeOutsideQBitTorrent, readable.GetHardlinkIndex(t.Context(), 904, torrentRefs([]qbt.Torrent{torrent})).ScopeByHash[hash])

	failed := newHardlinkIndexService(t, 905, failedIdentityBackend{localbackend.NewBackend()}, files)
	index := failed.GetHardlinkIndex(t.Context(), 905, torrentRefs([]qbt.Torrent{torrent}))
	require.NotContains(t, index.ScopeByHash, hash)

	scope := &RuleCondition{Field: FieldHardlinkScope, Operator: OperatorEqual, Value: HardlinkScopeOutsideQBitTorrent, Negate: true}
	rules := map[int]*models.Automation{1: {Conditions: &ActionConditions{Delete: &DeleteAction{Enabled: true, Mode: DeleteModeWithFiles, Condition: scope}}}}
	pending := &deletionSelection{byHash: map[string]selectedDeletion{hash: {rule: ruleRef{id: 1}}}}
	blocked := failed.blockedDeleteCandidates(t.Context(), 905, index, torrentRefMap(map[string]qbt.Torrent{hash: torrent}), pending, rules)
	require.Contains(t, blocked, hash)
}
