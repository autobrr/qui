// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"fmt"
	"math"
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
	case reflect.Map:
		// One entry with a zero value: fpSortedMap hashes the length and key, so
		// the entry alone must move the fingerprint.
		m := reflect.MakeMap(f.Type())
		m.SetMapIndex(reflect.ValueOf("x"), reflect.New(f.Type().Elem()).Elem())
		f.Set(m)
	default:
		t.Fatalf("field kind %s not handled; extend setSampleValue", f.Kind())
	}
}

// TestTorrentFingerprintCoversEveryField proves the fingerprint reacts to every
// non-volatile qbt.Torrent field and ignores every volatile one. When a
// go-qbittorrent bump adds a field, this fails until the field is added to
// fpBuf.torrent or to volatileTorrentFields.
func TestTorrentFingerprintCoversEveryField(t *testing.T) {
	base := singleRowFingerprint(new(fpBuf), qbittorrent.TorrentView{Torrent: &qbt.Torrent{}})
	typ := reflect.TypeFor[qbt.Torrent]()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		var mutated qbt.Torrent
		setSampleValue(t, reflect.ValueOf(&mutated).Elem().Field(i))
		fp := singleRowFingerprint(new(fpBuf), qbittorrent.TorrentView{Torrent: &mutated})
		if volatileTorrentFields[field.Name] {
			require.Equal(t, base, fp, "volatile field %s must not affect the fingerprint", field.Name)
		} else {
			require.NotEqual(t, base, fp, "field %s must change the fingerprint; add it to fpBuf.torrent or volatileTorrentFields", field.Name)
		}
	}
}

// assertEveryFieldHashed proves fp reacts to every field of T: a fingerprint
// with no coverage guard fails silently, because the delta stream drops a field
// whose hash holds and no periodic keyframe corrects the client.
func assertEveryFieldHashed[T any](t *testing.T, hint string, fp func(T) uint64) {
	var zero T
	base := fp(zero)
	typ := reflect.TypeFor[T]()
	for i := 0; i < typ.NumField(); i++ {
		var mutated T
		setSampleValue(t, reflect.ValueOf(&mutated).Elem().Field(i))
		require.NotEqual(t, base, fp(mutated), "%s.%s must change the fingerprint; %s", typ.Name(), typ.Field(i).Name, hint)
	}
}

// TestTrackerFingerprintCoversEveryField does the same for the per-tracker rows
// inside Torrent.Trackers.
func TestTrackerFingerprintCoversEveryField(t *testing.T) {
	assertEveryFieldHashed(t, "add it to the Trackers loop in fpBuf.torrent", func(tr qbt.TorrentTracker) uint64 {
		return singleRowFingerprint(new(fpBuf), qbittorrent.TorrentView{Torrent: &qbt.Torrent{Trackers: []qbt.TorrentTracker{tr}}})
	})
}

// TestCountsFingerprintCoversEveryField proves the counts fingerprint reacts to
// every TorrentCounts field, the way the row fingerprint is already guarded.
func TestCountsFingerprintCoversEveryField(t *testing.T) {
	assertEveryFieldHashed(t, "add it to countsFingerprint", func(c qbittorrent.TorrentCounts) uint64 {
		return countsFingerprint(&c)
	})
}

// TestTrackerTransferStatsFingerprintCoversEveryField does the same for the
// per-domain transfer stats nested inside TorrentCounts.TrackerTransfers. The
// map sample in the counts test only proves the map key is hashed, not the
// struct fields of its values.
func TestTrackerTransferStatsFingerprintCoversEveryField(t *testing.T) {
	assertEveryFieldHashed(t, "add it to countsFingerprint's TrackerTransfers loop", func(s qbittorrent.TrackerTransferStats) uint64 {
		return countsFingerprint(&qbittorrent.TorrentCounts{
			TrackerTransfers: map[string]qbittorrent.TrackerTransferStats{"x": s},
		})
	})
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
		singleRowFingerprint(new(fpBuf), row)
	}
}

// BenchmarkComputeRowDeltaPage measures a whole page the way a tick does, which
// is where the fingerprint buffer is reused across rows.
func BenchmarkComputeRowDeltaPage(b *testing.B) {
	const pageRows = 300
	rows := make([]qbittorrent.TorrentView, pageRows)
	for i := range rows {
		rows[i] = qbittorrent.TorrentView{
			Torrent: &qbt.Torrent{
				AddedOn:     1700000000 + int64(i),
				Category:    "tv-sonarr",
				ContentPath: fmt.Sprintf("/data/torrents/tv/Some.Show.S%02d.1080p.WEB-DL.DDP5.1.H.264-GRP", i%40),
				SavePath:    "/data/torrents/tv",
				Hash:        fmt.Sprintf("%040x", i),
				Name:        fmt.Sprintf("Some.Show.S%02d.1080p.WEB-DL.DDP5.1.H.264-GRP", i%40),
				Progress:    0.75,
				Size:        56712345678,
				State:       qbt.TorrentStateUploading,
				Tags:        "cross-seed, keep",
				Tracker:     "https://tracker.example.invalid/announce",
			},
			TrackerHealth: "ok",
		}
	}

	_, _, base := computeRowDelta(rows, singleRowKey, singleRowFingerprint, nil)

	b.ReportAllocs()
	for b.Loop() {
		computeRowDelta(rows, singleRowKey, singleRowFingerprint, base)
	}
}

// TestFingerprintDistinguishesNaNRows guards the fix for the old JSON path,
// where a NaN ratio made json.Marshal fail and every such row hashed as empty.
func TestFingerprintDistinguishesNaNRows(t *testing.T) {
	a := singleRowFingerprint(new(fpBuf), qbittorrent.TorrentView{Torrent: &qbt.Torrent{Name: "A", Ratio: math.NaN()}})
	b := singleRowFingerprint(new(fpBuf), qbittorrent.TorrentView{Torrent: &qbt.Torrent{Name: "B", Ratio: math.NaN()}})
	require.NotEqual(t, a, b)
}
