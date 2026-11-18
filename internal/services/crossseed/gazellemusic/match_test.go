package gazellemusic

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/time/rate"
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
			var browseCalls, metadataCalls atomic.Int32
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
				case "torrent":
					metadataCalls.Add(1)
					if r.URL.Query().Get("id") != "7" {
						t.Errorf("metadata fetched for an unequal-total result: %s", r.URL.RawQuery)
					}
					_, _ = w.Write([]byte(`{"status":"success","response":{"torrent":{"id":7,"size":300,"infoHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}`))
				default:
					t.Errorf("unexpected request: %s", r.URL.RawQuery)
				}
			}))
			t.Cleanup(server.Close)
			client, err := NewClient("orpheus.network", server.URL, "key")
			if err != nil {
				t.Fatal(err)
			}
			client.limiter = rate.NewLimiter(rate.Inf, 1)
			match, err := FindMatch(t.Context(), client, nil, map[string]int64{"Album/01 - A distinctive longer song.flac": 100, "Album/02 - Brief.flac": 200}, 300)
			if err != nil || (match != nil) != tt.wantMatch {
				t.Fatalf("match=%+v err=%v, wantMatch=%v", match, err, tt.wantMatch)
			}
			if match != nil && (match.TorrentID != 7 || match.Kind != MatchSize || match.TargetHash != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
				t.Fatalf("unexpected match: %+v", match)
			}
			var wantMetadata int32
			if tt.wantMatch {
				wantMetadata = 1
			}
			if browseCalls.Load() != tt.wantBrowse || metadataCalls.Load() != wantMetadata {
				t.Fatalf("browse=%d metadata=%d, want browse=%d match=%v", browseCalls.Load(), metadataCalls.Load(), tt.wantBrowse, tt.wantMatch)
			}
		})
	}
}

// regression: #4181
func TestFindMatchSelectedIdentity(t *testing.T) {
	for _, tt := range []struct {
		name, metadata       string
		total                int64
		wantMatch, wantError bool
		browseSize           int64
		wantKind             MatchKind
	}{
		{"enriched size", `{"id":7,"size":123,"infoHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, 123, true, false, 0, MatchSize},
		{"zero size", `{"id":7,"size":0,"infoHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, 0, false, false, 0, MatchSize},
		{"missing hash", `{"id":7,"size":123}`, 123, false, true, 0, MatchSize},
		{"changed id", `{"id":8,"size":123,"infoHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, 123, false, true, 0, MatchSize},
		{"changed size", `{"id":7,"size":124,"infoHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, 123, false, true, 0, MatchSize},
		{"verified selected file list", `{"id":7,"size":123,"infoHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","fileList":"01 - Synthetic Track.flac{{{123}}}"}`, 123, true, false, 0, MatchFileList},
		{"unequal total skips metadata", `{"id":7,"size":124,"infoHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, 123, false, false, 124, MatchSize},
	} {
		t.Run(tt.name, func(t *testing.T) {
			browseSize := tt.total
			if tt.browseSize > 0 {
				browseSize = tt.browseSize
			}
			var metadataCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("action") == "browse" {
					_, _ = fmt.Fprintf(w, `{"status":"success","response":{"results":[{"groupId":3,"groupName":"Album","torrents":[{"torrentId":7,"size":%d}]}]}}`, browseSize)
					return
				}
				metadataCalls.Add(1)
				_, _ = fmt.Fprintf(w, `{"status":"success","response":{"torrent":%s}}`, tt.metadata)
			}))
			defer server.Close()
			client, err := NewClient("orpheus.network", server.URL, "key")
			if err != nil {
				t.Fatal(err)
			}
			match, err := FindMatch(t.Context(), client, nil, map[string]int64{"01 - Synthetic Track.flac": tt.total}, tt.total)
			if (err != nil) != tt.wantError || (match != nil) != tt.wantMatch {
				t.Fatalf("match=%+v err=%v", match, err)
			}
			expectedCalls := int32(0)
			if tt.total > 0 && browseSize == tt.total {
				expectedCalls = 1
			}
			if metadataCalls.Load() != expectedCalls {
				t.Fatalf("selected metadata fetched %d times, want %d", metadataCalls.Load(), expectedCalls)
			}
			if match != nil && (match.Kind != tt.wantKind || match.TargetHash != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || match.QueriedHash != "") {
				t.Fatalf("wrong evidence: %+v", match)
			}
			if match != nil && tt.wantKind == MatchFileList && match.RemoteFiles["01 - Synthetic Track.flac"] != 123 {
				t.Fatalf("compared remote snapshot missing: %+v", match)
			}
		})
	}
}

// regression: #4181
func TestFileListSnapshotPreservesCompleteEvidence(t *testing.T) {
	for _, bad := range []string{"", "track.flac{{{123}}}|||broken", "track.flac{{{123}}}|||track.flac{{{123}}}", "track.flac{{{-1}}}"} {
		if len(parseFileList(bad)) != 0 {
			t.Fatalf("malformed list accepted: %q", bad)
		}
	}
	left := map[string]int64{"Album/CD1/A.flac": 100, "Album/CD2/B.flac": 200}
	right := map[string]int64{"Other/CD1/a.flac": 100, "Other/CD2/b.flac": 200}
	if FileListFingerprint(left) != FileListFingerprint(right) {
		t.Fatal("root/case normalization changed")
	}
	right["Other/CD1/a.flac"], right["Other/CD2/b.flac"] = 200, 100
	if FileListFingerprint(left) == FileListFingerprint(right) {
		t.Fatal("individual sizes lost")
	}
}

func TestFilesConflict_SizeOnlyNotEnough(t *testing.T) {
	local := map[string]int64{
		"01 - Alpha.flac": 100,
		"02 - Beta.flac":  200,
	}
	remote := map[string]int64{
		"a.flac": 100,
		"b.flac": 200,
	}
	if !filesConflict(local, remote) {
		t.Fatalf("expected conflict when names differ but sizes match")
	}
}

func TestFilesConflict_IgnoresRootFolderAndFormatting(t *testing.T) {
	local := map[string]int64{
		"Some Album/01 - Track_Name.FLAC":  100,
		"Some Album/02 - Other.Track.flac": 200,
	}
	remote := map[string]int64{
		"01-Track Name.flac":  100,
		"02 Other Track.flac": 200,
	}
	if filesConflict(local, remote) {
		t.Fatalf("expected no conflict when names match after normalization and root folder is ignored")
	}
}

func TestFilesConflict_PreservesSubdirectories(t *testing.T) {
	local := map[string]int64{
		"CD1/01 - Alpha.flac": 100,
		"CD2/02 - Beta.flac":  200,
	}
	remote := map[string]int64{
		"CD2/01 - Alpha.flac": 100,
		"CD1/02 - Beta.flac":  200,
	}
	if !filesConflict(local, remote) {
		t.Fatalf("expected conflict when subdirectories differ")
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
// search must not run.
func TestFindMatch_HashLookupFindsLegacyFlagUpload(t *testing.T) {
	const pthHash = "2d95e58ed6b0430e79a00d41c506fe3715b43874"
	var hashCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("action") != "torrent" {
			t.Errorf("unexpected %s call", r.URL.Query().Get("action"))
			return
		}
		hashCalls.Add(1)
		if strings.EqualFold(r.URL.Query().Get("hash"), pthHash) {
			_, _ = w.Write([]byte(`{"status":"success","response":{"group":{"id":3,"name":"Album"},"torrent":{"id":7,"size":123,"infoHash":"2d95e58ed6b0430e79a00d41c506fe3715b43874"}}}`))
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
	if err != nil {
		t.Fatalf("FindMatch: %v", err)
	}
	if match == nil || match.Kind != MatchHash || match.TargetHash != pthHash || match.QueriedHash != pthHash || match.TorrentID != 7 {
		t.Fatalf("expected the PTH upload as a hash match, got %+v", match)
	}
	if got := hashCalls.Load(); got != 2 {
		t.Fatalf("expected the RED then PTH hash lookups, got %d calls", got)
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
