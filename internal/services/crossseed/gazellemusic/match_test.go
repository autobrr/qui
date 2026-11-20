package gazellemusic

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// regression: #4219
func TestFindMatchBroadFilenameFallback(t *testing.T) {
	for _, tt := range []struct {
		name       string
		count      int
		firstExact bool
		wantMatch  bool
		wantBrowse int32
	}{
		{"20 unrelated results stop", 20, false, false, 1},
		{"21 unrelated results try another filename", 21, false, true, 2},
		{"21 results with an exact total stop", 21, true, true, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var browseCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Query().Get("action") {
				case "browse":
					browseCalls.Add(1)
					results := make([]string, tt.count)
					for i := range results {
						results[i] = fmt.Sprintf(`{"torrentId":%d,"size":1000}`, 100+i)
					}
					if tt.firstExact {
						results[len(results)-1] = `{"torrentId":7,"size":300}`
					} else if r.URL.Query().Get("filelist") == "02 - Brief" {
						results = []string{`{"torrentId":7,"size":300}`}
					}
					_, _ = fmt.Fprintf(w, `{"status":"success","response":{"results":[{"groupId":3,"groupName":"Synthetic Album","torrents":[%s]}]}}`, strings.Join(results, ","))
				default:
					t.Errorf("unexpected request: %s", r.URL.RawQuery)
				}
			}))
			t.Cleanup(server.Close)
			client, err := NewClient("orpheus.network", server.URL, "key")
			if err != nil {
				t.Fatal(err)
			}
			match, err := FindMatch(t.Context(), client, nil, map[string]int64{"Album/01 - A distinctive longer song.flac": 100, "Album/02 - Brief.flac": 200}, 300)
			if err != nil || (match != nil) != tt.wantMatch {
				t.Fatalf("match=%+v err=%v, wantMatch=%v", match, err, tt.wantMatch)
			}
			if match != nil && (match.TorrentID != 7 || match.Kind != MatchSize || match.TargetHash != "") {
				t.Fatalf("unexpected match: %+v", match)
			}
			if browseCalls.Load() != tt.wantBrowse {
				t.Fatalf("browse=%d, want %d", browseCalls.Load(), tt.wantBrowse)
			}
		})
	}
}

// regression: #4181
func TestFindMatchEqualTotalNeedsNoMetadata(t *testing.T) {
	for _, tt := range []struct {
		name              string
		total, browseSize int64
		wantMatch         bool
	}{
		{"equal total", 123, 123, true},
		{"unknown local total", 0, 0, false},
		{"unequal total", 123, 124, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("action") != "browse" {
					t.Errorf("unexpected request: %s", r.URL.RawQuery)
					return
				}
				_, _ = fmt.Fprintf(w, `{"status":"success","response":{"results":[{"groupId":3,"groupName":"Album","torrents":[{"torrentId":7,"size":%d}]}]}}`, tt.browseSize)
			}))
			defer server.Close()
			client, err := NewClient("orpheus.network", server.URL, "key")
			if err != nil {
				t.Fatal(err)
			}
			match, err := FindMatch(t.Context(), client, nil, map[string]int64{"01 - Synthetic Track.flac": tt.total}, tt.total)
			if err != nil || (match != nil) != tt.wantMatch {
				t.Fatalf("match=%+v err=%v", match, err)
			}
			if match != nil && (match.Kind != MatchSize || match.TorrentID != 7 || match.Size != 123 || match.TargetHash != "" || match.QueriedHash != "") {
				t.Fatalf("wrong evidence: %+v", match)
			}
		})
	}
}

func legacyFlagTorrent(t *testing.T) []byte {
	t.Helper()
	body, err := encodeBencode(map[string]any{
		"announce": "https://tracker.example/announce",
		"info": map[string]any{
			"length": int64(123),
			"name":   "test",
			"source": "OPS",
		},
	})
	if err != nil {
		t.Fatalf("encode torrent: %v", err)
	}
	return body
}

// TestFindMatch_HashLookupFindsLegacyFlagUpload: the RED copy was uploaded as
// PTH, so the RED-flag hash misses and the PTH-flag hash hits. The filename
// search must not run. OPS leaves infoHash out of the answer unless the user
// snatched or uploaded the torrent, so a hit without it is still a hash match.
// regression: #4213
func TestFindMatch_HashLookupFindsLegacyFlagUpload(t *testing.T) {
	const pthHash = "2d95e58ed6b0430e79a00d41c506fe3715b43874"
	for _, tc := range []struct {
		name, infoHash string
		wantErr        bool
	}{
		{name: "answer names the hash", infoHash: `,"infoHash":"2D95E58ED6B0430E79A00D41C506FE3715B43874"`},
		{name: "answer leaves the hash out", infoHash: ""},
		{name: "answer names another hash", infoHash: `,"infoHash":"0000000000000000000000000000000000000001"`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hashCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("action") != "torrent" {
					t.Errorf("unexpected %s call", r.URL.Query().Get("action"))
					return
				}
				hashCalls.Add(1)
				if strings.EqualFold(r.URL.Query().Get("hash"), pthHash) {
					_, _ = w.Write([]byte(`{"status":"success","response":{"group":{"id":3,"name":"Album"},"torrent":{"id":7,"size":123` + tc.infoHash + `}}}`))
					return
				}
				_, _ = w.Write([]byte(`{"status":"failure","error":"bad hash parameter"}`))
			}))
			defer server.Close()

			client, err := NewClient("redacted.sh", server.URL, "key")
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}

			match, err := FindMatch(t.Context(), client, legacyFlagTorrent(t), map[string]int64{"test.flac": 123}, 123)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an identity error, got %+v", match)
				}
				return
			}
			if err != nil {
				t.Fatalf("FindMatch: %v", err)
			}
			if match == nil || match.Kind != MatchHash || match.TargetHash != pthHash || match.QueriedHash != pthHash || match.TorrentID != 7 {
				t.Fatalf("expected the PTH upload as a hash match, got %+v", match)
			}
			if got := hashCalls.Load(); got != 2 {
				t.Fatalf("expected the RED then PTH hash lookups, got %d calls", got)
			}
		})
	}
}

// TestFindMatch_FailedLookupIsAnErrorNotAMiss: a tracker that cannot answer
// must surface as an error so the caller leaves the torrent eligible, instead
// of a nil match that stamps its cooldown.
func TestFindMatch_FailedLookupIsAnErrorNotAMiss(t *testing.T) {
	tests := []struct {
		name         string
		torrentBytes []byte
		body         string
		status       int
	}{
		{
			name:         "hash lookup rate limited",
			torrentBytes: legacyFlagTorrent(t),
			body:         `{"status":"failure","error":"rate limit exceeded"}`,
			status:       http.StatusOK,
		},
		{
			name:         "filename search server error",
			torrentBytes: nil,
			body:         "boom",
			status:       http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client, err := NewClient("redacted.sh", server.URL, "key")
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}

			match, err := FindMatch(t.Context(), client, tt.torrentBytes, map[string]int64{"test.flac": 123}, 123)
			if err == nil {
				t.Fatalf("expected an error, got match %+v", match)
			}
			if match != nil {
				t.Fatalf("expected no match, got %+v", match)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("expected the first failed call to end the lookup, got %d calls", got)
			}
		})
	}
}
