// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sharetracker

import (
	"slices"
	"testing"
)

func FuzzParseAnnounce(f *testing.F) {
	for _, seed := range []string{
		"info_hash=share-torrent-hash-7&peer_id=-qB5100-abcdefghijkl&port=6881&uploaded=0&downloaded=0&left=1000&compact=1&event=started",
		"info_hash=%00%01%02%03%04%05%06%07%08%09%0A%0B%0C%0D%0E%0F%10%11%12%13&port=65535&uploaded=1&downloaded=2&left=0&numwant=0&event=stopped",
		"info_hash=share-torrent-hash-7&port=1&uploaded=0&downloaded=0&left=0&numwant=200&ip=192.0.2.99&event=paused",
		"info_hash=share-torrent-hash-7&port=0&uploaded=0&downloaded=0&left=0",
		"info_hash=short&port=6881&uploaded=0&downloaded=0&left=0",
		"info_hash=share-torrent-hash-7&port=6881&uploaded=-1&downloaded=0&left=0",
		"info_hash=share-torrent-hash-7&port=6881&uploaded=0&downloaded=0&left=0&numwant=-5",
		"info_hash=share-torrent-hash-7&port=6881&uploaded=0&downloaded=0&left=0&compact=0",
		"%zz=1;a=b",
		"",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, query string) {
		req, err := parseAnnounce(query)
		if err != nil {
			if req != (announceRequest{}) {
				t.Fatalf("a refused announce returned %+v", req)
			}
			return
		}
		if req.port == 0 {
			t.Fatalf("port 0 passed in %q", query)
		}
		if req.uploaded < 0 || req.downloaded < 0 || req.left < 0 || req.numwant < 0 {
			t.Fatalf("a negative count passed in %q: %+v", query, req)
		}
		if !slices.Contains([]Event{EventNone, EventStarted, EventCompleted, EventStopped}, req.event) {
			t.Fatalf("unknown event %q passed in %q", req.event, query)
		}
	})
}
