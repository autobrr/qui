// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bufio"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompare(t *testing.T) {
	a := match{InstanceID: 1, TorrentHash: "aa", TorrentName: "Quiet.Harbor.S01E01.1080p.WEB-DL.H.264-GRP"}
	b := match{InstanceID: 1, TorrentHash: "bb", TorrentName: a.TorrentName}
	other := match{InstanceID: 2, TorrentHash: "aa", TorrentName: a.TorrentName}
	ready := func(matches ...match) answer {
		return answer{Status: http.StatusOK, CanCrossSeed: true, Recommendation: "download", Matches: matches}
	}
	none := answer{Status: http.StatusNotFound, Recommendation: "skip"}
	pack := func(p seasonPack) answer {
		return answer{Status: http.StatusOK, CanCrossSeed: true, Recommendation: "download", SeasonPack: &p}
	}
	tests := []struct {
		name     string
		old, new answer
		want     verdict
		extra    []match
	}{
		{name: "same match in another order", old: ready(a, b), new: ready(b, a), want: verdictSame},
		{name: "another copy listed", old: ready(a), new: ready(a, b), want: verdictExtraCopies, extra: []match{b}},
		{name: "a match left out", old: ready(a, b), new: ready(a), want: verdictDiffers},
		{name: "another torrent listed", old: ready(a), new: ready(a, match{InstanceID: 1, TorrentHash: "cc", TorrentName: "Lost.Harbor.S01E02.1080p.WEB-DL.H.264-GRP"}), want: verdictDiffers},
		{name: "the same hash on another instance", old: ready(a), new: ready(other), want: verdictDiffers},
		{name: "a miss that matches", old: none, new: ready(a), want: verdictDiffers},
		{name: "a season pack assembly", old: none, new: answer{Status: http.StatusOK, CanCrossSeed: true, Recommendation: "download", SeasonPack: &seasonPack{InstanceID: 1}}, want: verdictSeasonPack},
		{name: "the same torrent by another match type", old: ready(a), new: ready(match{InstanceID: 1, TorrentHash: "aa", TorrentName: a.TorrentName, MatchType: "metadata"}), want: verdictDiffers},
		{name: "another season pack", old: pack(seasonPack{InstanceID: 1, MatchedEpisodes: 10, TotalEpisodes: 10}), new: pack(seasonPack{InstanceID: 1, MatchedEpisodes: 8, TotalEpisodes: 10}), want: verdictDiffers},
		{name: "the same season pack", old: pack(seasonPack{InstanceID: 1, MatchedEpisodes: 10, TotalEpisodes: 10}), new: pack(seasonPack{InstanceID: 1, MatchedEpisodes: 10, TotalEpisodes: 10}), want: verdictSame},
		{name: "a server error", old: none, new: answer{Status: http.StatusInternalServerError, Error: "status 500"}, want: verdictFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compare(announce{TorrentName: "x"}, tt.old, tt.new)
			require.Equal(t, tt.want, got.Verdict)
			require.Equal(t, tt.extra, got.Extra)
		})
	}
}

func TestParseAnnounce(t *testing.T) {
	tests := []struct {
		line    string
		want    announce
		wantErr bool
	}{
		{line: "Quiet.Harbor.S01E01.1080p.WEB-DL.H.264-GRP", want: announce{TorrentName: "Quiet.Harbor.S01E01.1080p.WEB-DL.H.264-GRP"}},
		{line: "Quiet Harbor S01E01\t1500000000", want: announce{TorrentName: "Quiet Harbor S01E01", Size: 1_500_000_000}},
		{line: `{"torrentName":"Quiet.Harbor.S01E01","size":7,"instanceIds":[2],"indexer":"rig"}`, want: announce{TorrentName: "Quiet.Harbor.S01E01", Size: 7, InstanceIDs: []int{2}, Indexer: "rig"}},
		{line: "Quiet.Harbor.S01E01\tabout 1 GB", wantErr: true},
		{line: `{"size":7}`, wantErr: true},
	}
	for _, tt := range tests {
		got, err := parseAnnounce(tt.line)
		if tt.wantErr {
			require.Error(t, err, tt.line)
			continue
		}
		require.NoError(t, err, tt.line)
		require.Equal(t, tt.want, got)
	}
}

type fakeQui struct {
	mu      sync.Mutex
	answers map[string]answer
	keys    []string
	bodies  []announce
}

func (f *fakeQui) serve(w http.ResponseWriter, r *http.Request) {
	var body announce
	if r.URL.Path != "/api/cross-seed/webhook/check" || json.UnmarshalRead(r.Body, &body) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.keys = append(f.keys, r.Header.Get("X-API-Key"))
	f.bodies = append(f.bodies, body)
	got, ok := f.answers[body.TorrentName]
	f.mu.Unlock()
	if !ok {
		got = answer{Status: http.StatusNotFound, Recommendation: "skip"}
	}
	w.WriteHeader(got.Status)
	_ = json.MarshalWrite(w, got)
}

func startFake(t *testing.T, answers map[string]answer) (*fakeQui, string) {
	t.Helper()
	fake := &fakeQui{answers: answers}
	srv := httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(srv.Close)
	return fake, srv.URL
}

func TestRunReportsEachAnswerThatDiffers(t *testing.T) {
	held := match{InstanceID: 1, TorrentHash: "aa", TorrentName: "Quiet.Harbor.S01E01.1080p.WEB-DL.H.264-GRP"}
	copied := match{InstanceID: 1, TorrentHash: "bb", TorrentName: held.TorrentName}
	ready := func(matches ...match) answer {
		return answer{Status: http.StatusOK, CanCrossSeed: true, Recommendation: "download", Matches: matches}
	}
	old, oldURL := startFake(t, map[string]answer{held.TorrentName: ready(held), "Lost.Harbor.S01E02": ready(held)})
	_, newURL := startFake(t, map[string]answer{held.TorrentName: ready(held, copied)})
	dir := t.TempDir()
	announces := filepath.Join(dir, "announces.txt")
	require.NoError(t, os.WriteFile(announces, []byte(held.TorrentName+"\t1500000000\n\nNothing.Held.S01E01\nLost.Harbor.S01E02\n"), 0o600))
	out := filepath.Join(dir, "results.jsonl")
	var stdout, stderr strings.Builder

	err := run(t.Context(), []string{"-old", oldURL, "-old-key", "old-key", "-new", newURL, "-announces", announces, "-out", out, "-instances", "1", "-ready", held.TorrentName}, &stdout, &stderr)

	require.ErrorIs(t, err, errDiffers)
	require.Contains(t, stdout.String(), "announces: 3\n")
	require.Contains(t, stdout.String(), "same          1\n")
	require.Contains(t, stdout.String(), "extra copies  1\n")
	require.Contains(t, stdout.String(), "differs       1\n")
	require.Contains(t, stdout.String(), `differs: "Lost.Harbor.S01E02" size 0: old status 200 with 1 matches, new status 404 with 0 matches`)
	require.Contains(t, stdout.String(), "extra match on instance 1: bb")
	require.Contains(t, stdout.String(), "FAIL: 1 answers differ and 0 checks failed")
	require.Equal(t, "old-key", old.keys[0])
	require.Contains(t, old.bodies, announce{TorrentName: held.TorrentName, Size: 1_500_000_000, InstanceIDs: []int{1}})

	file, err := os.Open(out)
	require.NoError(t, err)
	defer file.Close()
	lines := 0
	for scanner := bufio.NewScanner(file); scanner.Scan(); lines++ {
		var r result
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &r))
	}
	require.Equal(t, 3, lines)
}

func TestRunPassesWhenEveryAnswerAgrees(t *testing.T) {
	_, oldURL := startFake(t, nil)
	_, newURL := startFake(t, nil)
	announces := filepath.Join(t.TempDir(), "announces.txt")
	require.NoError(t, os.WriteFile(announces, []byte("Nothing.Held.S01E01\nNothing.Held.S01E02\n"), 0o600))
	var stdout, stderr strings.Builder

	require.NoError(t, run(t.Context(), []string{"-old", oldURL, "-new", newURL, "-announces", announces}, &stdout, &stderr))

	require.Contains(t, stdout.String(), "same          2\n")
	require.Contains(t, stdout.String(), "PASS: no answer differs")
}
