// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/anacrolix/torrent/bencode"
)

func TestTorrentEncodingRoundTrip(t *testing.T) {
	data := loadTorrentFixture(t)
	original := decodeTorrent(t, data)

	encoded := encodeTorrent(t, original)
	decoded := decodeTorrent(t, encoded)

	if !reflect.DeepEqual(normalizeBencode(original), normalizeBencode(decoded)) {
		t.Fatalf("round trip encoding changed torrent structure")
	}
}

func TestPatchTorrentTrackers(t *testing.T) {
	data := loadTorrentFixture(t)
	root := decodeTorrent(t, data)

	// Remove announce metadata to simulate missing tracker data
	delete(root, "announce")
	delete(root, "announce-list")
	encoded := encodeTorrent(t, root)

	trackers := []string{
		"https://tracker.autobrr.com/announce",
		"https://backup.autobrr.com/announce",
	}

	patched, changed, err := patchTorrentTrackers(encoded, trackers)
	if err != nil {
		t.Fatalf("patchTorrentTrackers returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected patchTorrentTrackers to report change")
	}

	patchedRoot := decodeTorrent(t, patched)

	if got := bencodeString(patchedRoot["announce"]); got != trackers[0] {
		t.Fatalf("announce mismatch: got %q want %q", got, trackers[0])
	}

	list := flattenAnnounceList(patchedRoot["announce-list"])
	if !reflect.DeepEqual(list, trackers) {
		t.Fatalf("announce-list mismatch: got %v want %v", list, trackers)
	}
}

func loadTorrentFixture(t *testing.T) []byte {
	t.Helper()
	fixturePath := filepath.Join("testdata", "qbittorrent_4_6.torrent")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func decodeTorrent(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var root map[string]any
	if err := bencode.Unmarshal(data, &root); err != nil {
		t.Fatalf("decode torrent: %v", err)
	}
	return root
}

func encodeTorrent(t *testing.T, root map[string]any) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := bencode.NewEncoder(&buf).Encode(root); err != nil {
		t.Fatalf("encode torrent: %v", err)
	}
	return buf.Bytes()
}

func bencodeString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return ""
	}
}

func normalizeBencode(value any) any {
	switch v := value.(type) {
	case map[string]any:
		normalized := make(map[string]any, len(v))
		for key, val := range v {
			normalized[key] = normalizeBencode(val)
		}
		return normalized
	case []any:
		normalized := make([]any, len(v))
		for i, val := range v {
			normalized[i] = normalizeBencode(val)
		}
		return normalized
	case []byte:
		return string(v)
	default:
		return value
	}
}
