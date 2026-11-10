// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// benchCrossMatchService builds one local instance with pairs of torrents that
// match on name. With extraFile, the second torrent of each pair has an extra
// file that search does not ignore, so its sizes do not pair and the hardlink
// check runs. Its main file is a hardlink of the first torrent's file. With
// sharedFolder, both torrents of a pair sit in the first torrent's folder and
// read the same files, so addSharedPathPicks links them.
func benchCrossMatchService(b *testing.B, pairs int, extraFile, sharedFolder bool) *Service {
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
		if sharedFolder {
			candidateDir = sourceDir
			sm.files[sourceHash] = append(sm.files[sourceHash], qbt.TorrentFile{Name: name + ".nfo", Size: 1})
			sm.files[candidateHash] = sm.files[sourceHash]
		}
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
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	b.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })
	for _, tc := range []struct {
		name         string
		extraFile    bool
		sharedFolder bool
	}{
		{"sizes pair", false, false},
		{"hardlink check", true, false},
		{"shared folder", false, true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			svc := benchCrossMatchService(b, 500, tc.extraFile, tc.sharedFolder)
			needs := CrossMatchNeeds{SameExists: true, SameContent: tc.sharedFolder}
			b.ReportAllocs()
			for b.Loop() {
				result := svc.BuildCrossMatchSets(b.Context(), 1, needs)
				if len(result.SameInstanceExists) != 1000 {
					b.Fatalf("matched %d torrents, want 1000", len(result.SameInstanceExists))
				}
				if tc.sharedFolder && len(result.SameInstanceContentByHash) != 1000 {
					b.Fatalf("linked %d torrents by path, want 1000", len(result.SameInstanceContentByHash))
				}
			}
		})
	}
}

// benchCopyGroupInput builds about 6,000 torrents and 40,000 files: releases
// with three copies each, two in separate folders on instance 1 and one on
// instance 2. A third of the releases are season packs of 15 episodes.
func benchCopyGroupInput() (map[int][]qbittorrent.CrossInstanceTorrentView, map[int]map[string]qbt.TorrentFiles) {
	torrents := make(map[int][]qbittorrent.CrossInstanceTorrentView)
	files := map[int]map[string]qbt.TorrentFiles{1: {}, 2: {}}
	for r := range 2000 {
		var name string
		var list qbt.TorrentFiles
		if r%3 == 0 {
			name = fmt.Sprintf("Harbor.Watch.%04d.S01.2160p.WEB-DL.DV.HDR.H.265-GRP", r)
			for e := range 15 {
				list = append(list, qbt.TorrentFile{Name: fmt.Sprintf("%s/Harbor.Watch.%04d.S01E%02d.2160p.WEB-DL.DV.HDR.H.265-GRP.mkv", name, r, e+1), Size: int64(1_000_000 + r*100 + e)})
			}
		} else {
			name = fmt.Sprintf("Quiet.Harbor.%04d.2019.1080p.BluRay.x264-GRP", r)
			list = qbt.TorrentFiles{{Name: name + "/" + name + ".mkv", Size: int64(5000 + r)}}
		}
		list = append(list, qbt.TorrentFile{Name: name + "/" + name + ".nfo", Size: 10})
		for c, instanceID := range []int{1, 1, 2} {
			hash := fmt.Sprintf("%c%039d", 'a'+c, r)
			savePath := fmt.Sprintf("/data/%c", 'a'+c)
			torrents[instanceID] = append(torrents[instanceID], qbittorrent.CrossInstanceTorrentView{
				TorrentView: &qbittorrent.TorrentView{Torrent: &qbt.Torrent{
					Hash: hash, Name: name, SavePath: savePath, ContentPath: savePath + "/" + name,
					Size: sourceFilesTotalSize(list), AddedOn: int64(r*3 + c),
				}},
				InstanceID: instanceID,
			})
			files[instanceID][hash] = list
		}
	}
	return torrents, files
}

func BenchmarkFindCopyGroups(b *testing.B) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	b.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })
	torrents, files := benchCopyGroupInput()
	key := make([]byte, 32)
	run := func(b *testing.B, svc *Service) {
		groups, err := svc.FindCopyGroups(b.Context(), torrents, files, nil, key)
		if err != nil || len(groups) != 2000 {
			b.Fatalf("found %d groups, err %v, want 2000", len(groups), err)
		}
	}
	b.Run("warm parser", func(b *testing.B) {
		svc := newTestService()
		run(b, svc)
		b.ReportAllocs()
		for b.Loop() {
			run(b, svc)
		}
	})
	b.Run("cold parser", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			run(b, newTestService())
		}
	})
}
