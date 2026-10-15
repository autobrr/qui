// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// benchCrossMatchService builds one local instance with pairs of torrents that
// match on name. With extraFile, the second torrent of each pair has an extra
// file that search does not ignore, so its sizes do not pair and the hardlink
// check runs. Its main file is a hardlink of the first torrent's file.
func benchCrossMatchService(b *testing.B, pairs int, extraFile bool) *Service {
	b.Helper()
	root := b.TempDir()
	sm := &tierSyncManager{
		files:      make(map[string]qbt.TorrentFiles),
		candidates: make(map[int][]qbittorrent.CrossInstanceTorrentView),
	}
	for i := range pairs {
		name := fmt.Sprintf("Harbor.Watch.S01E%03d.1080p.WEB.h264-GRP", i)
		fileName := name + ".mkv"
		sourceDir := filepath.Join(root, "a", name)
		candidateDir := filepath.Join(root, "b", name)
		require.NoError(b, os.MkdirAll(sourceDir, 0o755))
		require.NoError(b, os.MkdirAll(candidateDir, 0o755))
		require.NoError(b, os.WriteFile(filepath.Join(sourceDir, fileName), []byte("data"), 0o600))
		require.NoError(b, os.Link(filepath.Join(sourceDir, fileName), filepath.Join(candidateDir, fileName)))

		sourceHash, candidateHash := fmt.Sprintf("a%039d", i), fmt.Sprintf("b%039d", i)
		sm.files[sourceHash] = qbt.TorrentFiles{{Name: fileName, Size: 4}}
		sm.files[candidateHash] = qbt.TorrentFiles{{Name: fileName, Size: 4}}
		if extraFile {
			sm.files[candidateHash] = append(sm.files[candidateHash], qbt.TorrentFile{Name: "Menu.m2ts", Size: 9})
		}
		sm.candidates[1] = append(sm.candidates[1],
			crossMatchTorrent(1, sourceHash, name, sourceDir, filepath.Join(sourceDir, fileName), nil).view,
			crossMatchTorrent(1, candidateHash, name, candidateDir, candidateDir, nil).view,
		)
	}
	svc := hardlinkTestService(nil)
	svc.syncManager = sm
	svc.instanceStore = newOrderedInstanceStore(&models.Instance{ID: 1, Name: "local", IsActive: true, HasLocalFilesystemAccess: true})
	return svc
}

func BenchmarkBuildCrossMatchSets(b *testing.B) {
	for _, tc := range []struct {
		name      string
		extraFile bool
	}{
		{"sizes pair", false},
		{"hardlink check", true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			svc := benchCrossMatchService(b, 500, tc.extraFile)
			needs := CrossMatchNeeds{SameExists: true}
			for b.Loop() {
				result := svc.BuildCrossMatchSets(b.Context(), 1, needs)
				if len(result.SameInstanceExists) != 1000 {
					b.Fatalf("matched %d torrents, want 1000", len(result.SameInstanceExists))
				}
			}
		})
	}
}
