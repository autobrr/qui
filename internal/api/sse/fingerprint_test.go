// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"reflect"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
)

// volatileTorrentFields are the per-tick jitter fields fpBuf.torrent deliberately
// skips. Keep this set in sync with the skip list documented on that method.
var volatileTorrentFields = map[string]bool{
	"Reannounce":    true,
	"TimeActive":    true,
	"SeedingTime":   true,
	"ETA":           true,
	"LastActivity":  true,
	"SeenComplete":  true,
	"Popularity":    true,
	"Availability":  true,
	"NumComplete":   true,
	"NumIncomplete": true,
	"NumLeechs":     true,
	"NumSeeds":      true,
}

// setSampleValue writes a non-zero value of the field's kind so a hashed field
// must move the fingerprint. New field kinds in go-qbittorrent fail loudly here.
func setSampleValue(t *testing.T, f reflect.Value) {
	t.Helper()
	//exhaustive:ignore -- default fails loudly on any kind not yet used by qbt.Torrent
	switch f.Kind() {
	case reflect.String:
		f.SetString("x")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		f.SetInt(1)
	case reflect.Float32, reflect.Float64:
		f.SetFloat(1)
	case reflect.Bool:
		f.SetBool(true)
	case reflect.Slice:
		f.Set(reflect.Append(f, reflect.New(f.Type().Elem()).Elem()))
	default:
		t.Fatalf("field kind %s not handled; extend setSampleValue", f.Kind())
	}
}

// TestTorrentFingerprintCoversEveryField proves the fingerprint reacts to every
// non-volatile qbt.Torrent field and ignores every volatile one. When a
// go-qbittorrent bump adds a field, this fails until the field is added to
// fpBuf.torrent or to volatileTorrentFields.
func TestTorrentFingerprintCoversEveryField(t *testing.T) {
	base := singleRowFingerprint(qbittorrent.TorrentView{Torrent: &qbt.Torrent{}})
	typ := reflect.TypeFor[qbt.Torrent]()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		var mutated qbt.Torrent
		setSampleValue(t, reflect.ValueOf(&mutated).Elem().Field(i))
		fp := singleRowFingerprint(qbittorrent.TorrentView{Torrent: &mutated})
		if volatileTorrentFields[field.Name] {
			require.Equal(t, base, fp, "volatile field %s must not affect the fingerprint", field.Name)
		} else {
			require.NotEqual(t, base, fp, "field %s must change the fingerprint; add it to fpBuf.torrent or volatileTorrentFields", field.Name)
		}
	}
}

// TestTrackerFingerprintCoversEveryField does the same for the per-tracker rows
// inside Torrent.Trackers.
func TestTrackerFingerprintCoversEveryField(t *testing.T) {
	row := func(tr qbt.TorrentTracker) uint64 {
		return singleRowFingerprint(qbittorrent.TorrentView{Torrent: &qbt.Torrent{Trackers: []qbt.TorrentTracker{tr}}})
	}
	base := row(qbt.TorrentTracker{})
	typ := reflect.TypeFor[qbt.TorrentTracker]()
	for i := 0; i < typ.NumField(); i++ {
		var mutated qbt.TorrentTracker
		setSampleValue(t, reflect.ValueOf(&mutated).Elem().Field(i))
		require.NotEqual(t, base, row(mutated), "tracker field %s must change the fingerprint; add it to the Trackers loop in fpBuf.torrent", typ.Field(i).Name)
	}
}

func BenchmarkSingleRowFingerprint(b *testing.B) {
	row := qbittorrent.TorrentView{
		Torrent: &qbt.Torrent{
			AddedOn:     1700000000,
			Category:    "tv-sonarr",
			ContentPath: "/data/torrents/tv/Some.Show.S01.1080p.WEB-DL.DDP5.1.H.264-GRP",
			SavePath:    "/data/torrents/tv",
			DlSpeed:     1234567,
			UpSpeed:     7654321,
			Downloaded:  123456789012,
			Uploaded:    98765432101,
			Hash:        "0123456789abcdef0123456789abcdef01234567",
			InfohashV1:  "0123456789abcdef0123456789abcdef01234567",
			Name:        "Some.Show.S01.1080p.WEB-DL.DDP5.1.H.264-GRP",
			Progress:    0.75,
			Ratio:       1.234,
			Size:        56712345678,
			State:       qbt.TorrentStateUploading,
			Tags:        "cross-seed, keep",
			Tracker:     "https://tracker.example.invalid/announce",
			TotalSize:   56712345678,
		},
		TrackerHealth: "ok",
	}
	b.ReportAllocs()
	for b.Loop() {
		singleRowFingerprint(row)
	}
}
