// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"crypto/sha1"
	"encoding/base64"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/bencode"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
)

// regression: #4181
func TestCatalogApplyObligations(t *testing.T) {
	for _, tt := range []struct {
		name        string
		listing     string
		wantCheck   bool
		failedCheck bool
		kind        gazellemusic.MatchKind
		mutate      func(*catalogReplaySync, *CrossSeedRequest)
		wantAdd     bool
		wantErr     string
	}{
		{name: "full size evidence", kind: gazellemusic.MatchSize, wantAdd: true},
		{name: "exact files ignore listing group", listing: "Artist-Album-FLAC-2024-SECOND", kind: gazellemusic.MatchSize, mutate: func(_ *catalogReplaySync, r *CrossSeedRequest) { r.SkipRecheck = true }, wantAdd: true},
		{name: "missing source", kind: gazellemusic.MatchSize, mutate: func(m *catalogReplaySync, _ *CrossSeedRequest) { m.source.Hash = "other" }, wantErr: "bound source unavailable"},
		{name: "incomplete source", kind: gazellemusic.MatchSize, mutate: func(m *catalogReplaySync, _ *CrossSeedRequest) { m.source.Progress = 0.5 }, wantErr: "bound source incomplete or ineligible"},
		{name: "unwanted incomplete file", kind: gazellemusic.MatchSize, mutate: func(m *catalogReplaySync, _ *CrossSeedRequest) { m.files[m.source.Hash][0].Progress = 0.5 }, wantErr: "bound source has incomplete full-torrent files"},
		{name: "source excluded", kind: gazellemusic.MatchSize, mutate: func(_ *catalogReplaySync, r *CrossSeedRequest) {
			r.sourceFilter = sourceFilter{categories: []string{"other"}}
		}, wantErr: "bound source incomplete or ineligible"},
		{name: "source renamed", kind: gazellemusic.MatchSize, mutate: func(m *catalogReplaySync, _ *CrossSeedRequest) { m.files[m.source.Hash][0].Name = "changed/track.flac" }, wantErr: "bound source file table, full size or content family changed"},
		{name: "target changed", kind: gazellemusic.MatchHash, mutate: func(_ *catalogReplaySync, r *CrossSeedRequest) {
			r.SearchDecision.Catalog.TargetHash, r.SearchDecision.Catalog.QueriedHash = adderV1, adderV1
		}, wantErr: "selected target hash or full size changed"},
		{name: "size exact path skips check", kind: gazellemusic.MatchSize, mutate: func(_ *catalogReplaySync, r *CrossSeedRequest) { r.SkipRecheck = true }, wantAdd: true},
		{name: "hash exact path exemption", kind: gazellemusic.MatchHash, mutate: func(_ *catalogReplaySync, r *CrossSeedRequest) { r.SkipRecheck = true }, wantAdd: true},
		{name: "renames honor skip recheck", kind: gazellemusic.MatchSize, mutate: func(_ *catalogReplaySync, r *CrossSeedRequest) { r.SkipRecheck = true }, wantAdd: true},
		{name: "renames require check", kind: gazellemusic.MatchSize, wantCheck: true, wantAdd: true},
		{name: "renames failed check protects reused files", kind: gazellemusic.MatchSize, wantCheck: true, failedCheck: true, wantAdd: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			name := "Mira Calder - Morning Is Near (1992) [FLAC]"
			if tt.listing != "" {
				name = "Artist-Album-FLAC-2024-FIRST"
			}
			data := catalogTorrent(t, name, "OPS", false)
			meta, err := ParseTorrentMetadataWithInfo(data)
			require.NoError(t, err)
			files := append(qbt.TorrentFiles(nil), meta.Files...)
			if strings.HasPrefix(tt.name, "renames") {
				files[0].Name = "Mira Calder - Morning Is Near (1992) [FLAC]/renamed.flac"
			}
			svc, base, candidate := newRenameOnlyService(t, &models.Instance{ID: 1}, "source", meta.Name, files, meta.HashV1, meta.Files)
			source := candidate.Torrents[0]
			source.Size, source.TotalSize = 110, 110
			listing := "Different listing"
			if tt.listing != "" {
				listing = tt.listing
			}
			match := &gazellemusic.Match{Kind: tt.kind, Host: "orpheus.network", TorrentID: 7, Size: 110, Title: listing}
			if tt.kind == gazellemusic.MatchHash {
				match.TargetHash, match.QueriedHash = meta.HashV1, meta.HashV1
			}
			p, err := svc.gazelleDecision(1, source, files, match)
			require.NoError(t, err)
			if tt.listing != "" {
				require.True(t, p.Relaxed.has(differenceGroup), "actual discovery must record the parsed group difference")
			}
			sync := &catalogReplaySync{renameOnlySyncManager: base, source: source}
			svc.syncManager = sync
			withAdder(svc)
			r := &CrossSeedRequest{TorrentData: base64.StdEncoding.EncodeToString(data), TargetInstanceIDs: []int{1}, SearchDecision: p}
			if tt.mutate != nil {
				tt.mutate(sync, r)
			}
			response, err := svc.CrossSeed(t.Context(), r)
			require.Equal(t, tt.wantAdd, sync.added)
			if tt.wantAdd {
				require.NoError(t, err)
				require.True(t, response.Success, "%+v", response.Results)
				if !tt.wantCheck {
					require.Equal(t, "true", sync.addTorrentOpts["skip_checking"])
					require.Empty(t, svc.settler.queue)
					require.NotContains(t, sync.bulkActions, "recheck:"+meta.HashV1)
				}
			}
			if !tt.wantAdd {
				require.ErrorContains(t, err, tt.wantErr)
			}
			if tt.wantCheck {
				require.Len(t, svc.settler.queue, 1)
				require.Contains(t, sync.bulkActions, "recheck:"+meta.HashV1)
				pending := <-svc.settler.queue
				pending.addedAt = time.Now().Add(-time.Minute)
				if tt.failedCheck {
					checking := qbt.Torrent{Hash: meta.HashV1, State: qbt.TorrentStateCheckingDl}
					require.True(t, svc.settler.processPendingRecheckResume(t.Context(), 1, meta.HashV1, pending, checking))
					partial := qbt.Torrent{Hash: meta.HashV1, State: qbt.TorrentStatePausedDl, Progress: 0.99, AmountLeft: 1, Completed: 109}
					require.False(t, svc.settler.processPendingRecheckResume(t.Context(), 1, meta.HashV1, pending, partial))
					require.NotContains(t, svc.settler.sync.(*settleScript).actions, "resume:"+meta.HashV1)
					return
				}
				full := qbt.Torrent{Hash: meta.HashV1, State: qbt.TorrentStatePausedUp, Progress: 1}
				svc.settler.processPendingRecheckResume(t.Context(), 1, meta.HashV1, pending, full)
				svc.settler.processPendingRecheckResume(t.Context(), 1, meta.HashV1, pending, full)
				require.Contains(t, svc.settler.sync.(*settleScript).actions, "resume:"+meta.HashV1)
			}
		})
	}
}

// regression: #4181
func TestCatalogFileOrderUsesPathAndSize(t *testing.T) {
	for _, tt := range []struct {
		name    string
		swap    bool
		skip    bool
		reorder bool
	}{
		{name: "swapped identity honors SkipRecheck", swap: true, skip: true},
		{name: "swapped identity aligns", swap: true},
		{name: "original identity keeps exemption", skip: true},
		{name: "API order is not identity", skip: true, reorder: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			piece := sha1.Sum([]byte("AABBCC"))
			torrent := func(source string) []byte {
				data, err := bencode.Marshal(map[string]any{"announce": "https://tracker.example.invalid/announce", "info": map[string]any{"name": "Original Album", "source": source, "piece length": int64(16384), "pieces": string(piece[:]), "files": []map[string]any{{"length": int64(2), "path": []string{"a.flac"}}, {"length": int64(2), "path": []string{"b.flac"}}, {"length": int64(1), "path": []string{"CD1", "cover.jpg"}}, {"length": int64(1), "path": []string{"CD2", "cover.jpg"}}}}})
				require.NoError(t, err)
				return data
			}
			sourceBytes, targetBytes := torrent("RED"), torrent("OPS")
			sourceMeta, err := ParseTorrentMetadataWithInfo(sourceBytes)
			require.NoError(t, err)
			targetMeta, err := ParseTorrentMetadataWithInfo(targetBytes)
			require.NoError(t, err)
			client, err := gazellemusic.NewClient("orpheus.network", "http://127.0.0.1:1", "test")
			require.NoError(t, err)
			hashes, err := client.TargetHashes(sourceBytes)
			require.NoError(t, err)
			require.Contains(t, hashes, targetMeta.HashV1)
			current := qbt.TorrentFiles{{Index: 0, Name: "Existing Album/b.flac", Size: 2, Progress: 1}, {Index: 1, Name: "Existing Album/a.flac", Size: 2, Progress: 1}, {Index: 2, Name: "Existing Album/CD1/cover.jpg", Size: 1, Progress: 1}, {Index: 3, Name: "Existing Album/CD2/cover.jpg", Size: 1, Progress: 1}}
			if !tt.swap {
				current[0].Name, current[1].Name = current[1].Name, current[0].Name
			}
			if tt.reorder {
				current[0], current[1] = current[1], current[0]
			}
			svc, base, candidate := newRenameOnlyService(t, &models.Instance{ID: 1}, sourceMeta.HashV1, "A completely different local display name", current, targetMeta.HashV1, targetMeta.Files)
			source := candidate.Torrents[0]
			source.Size, source.TotalSize = 6, 6
			p, err := svc.gazelleDecision(1, source, current, &gazellemusic.Match{Kind: gazellemusic.MatchHash, Host: "orpheus.network", TorrentID: 7, Size: 6, Title: "Tracker listing", TargetHash: targetMeta.HashV1, QueriedHash: targetMeta.HashV1})
			require.NoError(t, err)
			sync := &catalogReplaySync{renameOnlySyncManager: base, source: source, sourceBytes: sourceBytes}
			svc.syncManager = sync
			withAdder(svc)
			response, err := svc.CrossSeed(t.Context(), &CrossSeedRequest{TorrentData: base64.StdEncoding.EncodeToString(targetBytes), TargetInstanceIDs: []int{1}, SearchDecision: p, SkipRecheck: tt.skip, StartPaused: new(false)})
			require.NoError(t, err)

			require.True(t, sync.added)
			require.True(t, response.Success, "%+v", response.Results)
			if !tt.skip {
				require.Len(t, svc.settler.queue, 1)
				require.Contains(t, sync.bulkActions, "recheck:"+targetMeta.HashV1)
				require.NotContains(t, sync.bulkActions, "resume:"+targetMeta.HashV1)
			} else {
				require.Empty(t, svc.settler.queue)
			}
		})
	}
}

// regression: #4181
func TestCatalogMappedAlignmentStartsPaused(t *testing.T) {
	for _, shape := range []string{"flattened source", "renamed source root", "rootless incoming"} {
		for _, skip := range []bool{false, true} {
			t.Run(shape+map[bool]string{false: "/recheck", true: "/skip-recheck"}[skip], func(t *testing.T) {
				sourceBytes, targetBytes := catalogTorrent(t, "Original Album", "RED", false), catalogTorrent(t, "Original Album", "OPS", false)
				if shape == "rootless incoming" {
					piece := sha1.Sum([]byte(strings.Repeat("a", 100)))
					single := func(flag string) []byte {
						data, err := bencode.Marshal(map[string]any{"announce": "https://tracker.example.invalid/announce", "info": map[string]any{"name": "track.flac", "source": flag, "length": int64(100), "piece length": int64(16384), "pieces": string(piece[:])}})
						require.NoError(t, err)
						return data
					}
					sourceBytes, targetBytes = single("RED"), single("OPS")
				}
				sourceMeta, err := ParseTorrentMetadataWithInfo(sourceBytes)
				require.NoError(t, err)
				targetMeta, err := ParseTorrentMetadataWithInfo(targetBytes)
				require.NoError(t, err)
				current := slices.Clone(sourceMeta.Files)
				name := "Existing Album"
				for i := range current {
					if shape == "flattened source" {
						current[i].Name = path.Base(current[i].Name)
					} else {
						current[i].Name = name + "/" + path.Base(current[i].Name)
					}
				}
				if shape == "flattened source" {
					name = sourceMeta.Name
				}
				svc, base, candidate := newRenameOnlyService(t, &models.Instance{ID: 1}, sourceMeta.HashV1, name, current, targetMeta.HashV1, targetMeta.Files)
				source := candidate.Torrents[0]
				total := sourceFilesTotalSize(current)
				source.Size, source.TotalSize = total, total
				savePath := filepath.Join(t.TempDir(), "data")
				base.props[source.Hash].SavePath = savePath
				source.ContentPath = filepath.Join(savePath, filepath.FromSlash(current[0].Name))
				if shape == "flattened source" {
					source.ContentPath = savePath
				}
				p, err := svc.gazelleDecision(1, source, current, &gazellemusic.Match{Kind: gazellemusic.MatchHash, Host: "orpheus.network", TorrentID: 7, Size: total, Title: "Listing", TargetHash: targetMeta.HashV1, QueriedHash: targetMeta.HashV1})
				require.NoError(t, err)
				sync := &catalogReplaySync{renameOnlySyncManager: base, source: source}
				svc.syncManager = sync
				withAdder(svc)
				response, err := svc.CrossSeed(t.Context(), &CrossSeedRequest{TorrentData: base64.StdEncoding.EncodeToString(targetBytes), TargetInstanceIDs: []int{1}, SearchDecision: p, SkipRecheck: skip, StartPaused: new(false)})
				require.NoError(t, err)
				require.True(t, response.Success, "%+v", response.Results)
				require.Equal(t, "true", sync.addTorrentOpts["paused"])
				require.Equal(t, savePath, sync.addTorrentOpts["savepath"])
				lastRename := -1
				for i, action := range sync.bulkActions {
					if strings.HasPrefix(action, "rename:") {
						lastRename = i
					}
				}
				require.NotEqual(t, -1, lastRename)
				nextAction := "recheck:" + targetMeta.HashV1
				if skip {
					nextAction = "resume:" + targetMeta.HashV1
				}
				require.Greater(t, slices.Index(sync.bulkActions, nextAction), lastRename)
				for _, pair := range targetMeta.Files {
					index := pair.Index
					require.Equal(t, current[index].Name, sync.files[targetMeta.HashV1][index].Name)
				}
			})
		}
	}
}
