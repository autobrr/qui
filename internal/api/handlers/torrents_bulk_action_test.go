// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/qbittorrent"
)

func TestValidateBulkActionRequest(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		req       BulkActionRequest
		shouldErr bool
	}{
		{
			name: "pause has no extra required params",
			req: BulkActionRequest{
				Action: "pause",
			},
			shouldErr: false,
		},
		{
			name: "add tags requires tags",
			req: BulkActionRequest{
				Action: "addTags",
			},
			shouldErr: true,
		},
		{
			name: "set location requires location",
			req: BulkActionRequest{
				Action: "setLocation",
			},
			shouldErr: true,
		},
		{
			name: "edit trackers requires both old and new urls",
			req: BulkActionRequest{
				Action: "editTrackers",
			},
			shouldErr: true,
		},
		{
			name: "add trackers requires payload",
			req: BulkActionRequest{
				Action: "addTrackers",
			},
			shouldErr: true,
		},
		{
			name: "remove trackers accepts payload",
			req: BulkActionRequest{
				Action:      "removeTrackers",
				TrackerURLs: "udp://tracker.example.com:80/announce",
			},
			shouldErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateBulkActionRequest(tc.req)
			if tc.shouldErr && err == nil {
				t.Fatal("expected validation error")
			}
			if !tc.shouldErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestAddBulkTarget_DeduplicatesByInstanceAndHash(t *testing.T) {
	t.Parallel()

	targetsByInstance := make(map[int][]string)
	seen := make(map[int]map[string]struct{})

	addBulkTarget(targetsByInstance, seen, 1, "ABC")
	addBulkTarget(targetsByInstance, seen, 1, "abc")
	addBulkTarget(targetsByInstance, seen, 2, "abc")

	if len(targetsByInstance[1]) != 1 {
		t.Fatalf("expected one hash for instance 1, got %d", len(targetsByInstance[1]))
	}
	if len(targetsByInstance[2]) != 1 {
		t.Fatalf("expected one hash for instance 2, got %d", len(targetsByInstance[2]))
	}
}

func TestAppendTargetsFromCrossInstanceTorrents_RespectsExclusions(t *testing.T) {
	t.Parallel()

	torrents := []qbittorrent.CrossInstanceTorrentView{
		{
			TorrentView: &qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: "aaa"}},
			InstanceID:  1,
		},
		{
			TorrentView: &qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: "bbb"}},
			InstanceID:  1,
		},
		{
			TorrentView: &qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: "ccc"}},
			InstanceID:  2,
		},
	}

	targetsByInstance := make(map[int][]string)
	seen := make(map[int]map[string]struct{})
	excludeHashes := map[string]struct{}{"bbb": {}}
	excludeTargets := buildExcludeTargetSet([]BulkActionTarget{
		{InstanceID: 2, Hash: "ccc"},
	})

	appendTargetsFromCrossInstanceTorrents(targetsByInstance, seen, torrents, excludeHashes, excludeTargets)

	if len(targetsByInstance[1]) != 1 || targetsByInstance[1][0] != "aaa" {
		t.Fatalf("expected only hash aaa for instance 1, got %+v", targetsByInstance[1])
	}
	if len(targetsByInstance[2]) != 0 {
		t.Fatalf("expected no hashes for instance 2, got %+v", targetsByInstance[2])
	}
}
