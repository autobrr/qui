// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"context"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/bencode"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func multiTrackerTorrentData(t *testing.T, announce string, announceList [][]string) []byte {
	t.Helper()

	root := map[string]any{
		"info": map[string]any{
			"name":         "Synthetic.Show.S01.1080p.WEB-DL-GRP",
			"piece length": 16384,
			"pieces":       string(make([]byte, 20)),
			"length":       1,
		},
	}
	if announce != "" {
		root["announce"] = announce
	}
	if announceList != nil {
		root["announce-list"] = announceList
	}
	data, err := bencode.Marshal(root)
	require.NoError(t, err)
	return data
}

// qBittorrent reports its first working tracker in Tracker, so a torrent with
// several announce hosts changes it between runs without anything else changing.
func TestExecuteBackupArchivePathIgnoresWorkingTrackerFlip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// persistRuns stores each run's items so later runs read the cached
		// blob instead of exporting again.
		persistRuns bool
	}{
		{name: "live export", persistRuns: false},
		{name: "cached blob", persistRuns: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := setupTestBackupDB(t)
			ctx := context.Background()
			instanceID := insertTestInstance(t, db, "tracker-flip")
			store := models.NewBackupStore(db)
			require.NoError(t, store.UpsertSettings(ctx, &models.BackupSettings{
				InstanceID:    instanceID,
				Enabled:       true,
				HourlyEnabled: true,
				KeepHourly:    1,
			}))

			data := multiTrackerTorrentData(t, "https://alpha.example/announce", [][]string{
				{"https://alpha.example/announce"},
				{"udp://beta.example:6969/announce"},
			})
			sm := &stubBackupSyncManager{
				torrents:   []qbt.Torrent{{Hash: "abcdef0123", Name: "Synthetic.Show.S01.1080p.WEB-DL-GRP", TotalSize: 1}},
				exportData: data,
				exportName: "Synthetic.Show.S01.1080p.WEB-DL-GRP",
			}

			now := time.Unix(0, 0).UTC()
			svc := NewService(store, sm, Config{WorkerCount: 1, DataDir: t.TempDir()}, nil)
			svc.now = func() time.Time { return now }

			workingTrackers := []string{"beta.example", "alpha.example", "beta.example"}
			paths := make([]string, 0, len(workingTrackers))
			for _, working := range workingTrackers {
				sm.torrents[0].Tracker = "udp://" + working + ":6969/announce"
				// SyncManager.ExportTorrent derives its domain from the same field.
				sm.exportTrack = working

				run := &models.BackupRun{InstanceID: instanceID, Kind: models.BackupRunKindManual, Status: models.BackupRunStatusSuccess, RequestedBy: "tester", RequestedAt: now}
				require.NoError(t, store.CreateRun(ctx, run))

				result, err := svc.executeBackup(ctx, job{runID: run.ID, instanceID: instanceID, kind: models.BackupRunKindManual})
				require.NoError(t, err)
				require.Len(t, result.items, 1)
				require.NotNil(t, result.items[0].ArchiveRelPath)
				paths = append(paths, *result.items[0].ArchiveRelPath)

				if tt.persistRuns {
					require.NoError(t, store.InsertItems(ctx, run.ID, result.items))
				}
			}

			require.Equal(t, []string{paths[0], paths[0], paths[0]}, paths)
			require.Equal(t, "[alpha] Synthetic.Show.S01.1080p.WEB-DL-GRP - abcde.torrent", paths[0])
			if tt.persistRuns {
				require.Equal(t, 1, sm.exportCalls, "later runs must read the cached blob")
			} else {
				require.Equal(t, 3, sm.exportCalls)
			}
		})
	}
}

func TestAnnounceDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		data         []byte
		announceList [][]string
		announce     string
		want         string
	}{
		{
			name:         "first tier wins over announce",
			announce:     "https://beta.example/announce",
			announceList: [][]string{{"https://alpha.example/announce"}, {"https://beta.example/announce"}},
			want:         "alpha.example",
		},
		{
			name:         "order inside a tier does not matter",
			announceList: [][]string{{"udp://gamma.example:6969/announce", "https://alpha.example/announce"}, {"https://beta.example/announce"}},
			want:         "alpha.example",
		},
		{
			name:         "tier without a host is skipped",
			announceList: [][]string{{"", "   "}, {"https://beta.example/announce"}},
			want:         "beta.example",
		},
		{
			name:     "announce only",
			announce: "udp://beta.example:6969/announce",
			want:     "beta.example",
		},
		{
			name: "trackerless torrent",
			want: "",
		},
		{
			name: "payload is not bencode",
			data: []byte("payload-abcdef"),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data := tt.data
			if data == nil {
				data = multiTrackerTorrentData(t, tt.announce, tt.announceList)
			}
			require.Equal(t, tt.want, announceDomain(data))
		})
	}
}
