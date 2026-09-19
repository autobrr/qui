// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package gazellemusic

import (
	"testing"
)

func TestCalculateHashesWithSources_ReturnsDistinctHashes(t *testing.T) {
	torrentDict := map[string]any{
		"announce": "https://tracker.example/announce",
		"info": map[string]any{
			"length": int64(123),
			"name":   "test",
			"source": "RED",
		},
	}

	body, err := encodeBencode(torrentDict)
	if err != nil {
		t.Fatalf("encode torrent: %v", err)
	}

	hashes, err := CalculateHashesWithSources(body, []string{"OPS", "RED"})
	if err != nil {
		t.Fatalf("CalculateHashesWithSources: %v", err)
	}

	base := hashes[""]
	if len(base) != 40 {
		t.Fatalf("expected base hash length 40, got %d", len(base))
	}

	ops := hashes["OPS"]
	red := hashes["RED"]
	if len(ops) != 40 || len(red) != 40 {
		t.Fatalf("expected source hash lengths 40, got ops=%d red=%d", len(ops), len(red))
	}

	if ops == base || red == base {
		t.Fatalf("expected source hash to differ from base")
	}
	if ops == red {
		t.Fatalf("expected OPS hash to differ from RED hash")
	}
}

func TestLooksLikeTorrentPayload_ValidatesInfoDict(t *testing.T) {
	valid, err := encodeBencode(map[string]any{
		"info": map[string]any{
			"name":   "x",
			"length": int64(1),
		},
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !looksLikeTorrentPayload(valid) {
		t.Fatalf("expected valid payload to be accepted")
	}

	missingInfo, err := encodeBencode(map[string]any{
		"announce": "https://tracker.example/announce",
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if looksLikeTorrentPayload(missingInfo) {
		t.Fatalf("expected missing info dict to be rejected")
	}

	listPayload, err := encodeBencode([]any{"x"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if looksLikeTorrentPayload(listPayload) {
		t.Fatalf("expected non-dict payload to be rejected")
	}

	if looksLikeTorrentPayload([]byte(`{"status":"failure","error":"nope"}`)) {
		t.Fatalf("expected ajax error payload to be rejected")
	}
}

// TestTargetHashes_CurrentThenLegacy pins the lookup order: the hash an
// upload gets today, then the hash a pre-rename upload still carries.
func TestTargetHashes_CurrentThenLegacy(t *testing.T) {
	hashes, err := KnownTrackers["redacted.sh"].TargetHashes(legacyFlagTorrent(t))
	if err != nil {
		t.Fatalf("TargetHashes: %v", err)
	}

	want := []string{
		"7ce6f6f7740f75ed5b3a9457ebf04b16931f35cd", // source RED
		"2d95e58ed6b0430e79a00d41c506fe3715b43874", // source PTH
	}
	if len(hashes) != len(want) {
		t.Fatalf("expected %d hashes, got %v", len(want), hashes)
	}
	for i := range want {
		if hashes[i] != want[i] {
			t.Fatalf("hash %d: expected %s, got %s", i, want[i], hashes[i])
		}
	}
}
