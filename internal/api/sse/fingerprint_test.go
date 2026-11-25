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

	"ConnectionsCount": true,
	"PiecesHave":       true,
}

var payloadOmittedTorrentFields = map[string]bool{
	"MagnetURI":             true,
	"HasMetadata":           true,
	"HasTrackerWarning":     true,
	"HasTrackerError":       true,
	"HasOtherAnnounceError": true,
}

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
		m := reflect.MakeMap(f.Type())
		m.SetMapIndex(reflect.ValueOf("x"), reflect.New(f.Type().Elem()).Elem())
		f.Set(m)
	case reflect.Pointer:
		f.Set(reflect.New(f.Type().Elem()))
	default:
		t.Fatalf("field kind %s not handled; extend setSampleValue", f.Kind())
	}
}

func TestTorrentFingerprintCoversEveryField(t *testing.T) {
	base := singleRowFingerprint(new(fpBuf), qbittorrent.TorrentView{Torrent: &qbt.Torrent{}})
	typ := reflect.TypeFor[qbt.Torrent]()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		var mutated qbt.Torrent
		setSampleValue(t, reflect.ValueOf(&mutated).Elem().Field(i))
		fp := singleRowFingerprint(new(fpBuf), qbittorrent.TorrentView{Torrent: &mutated})
		switch {
		case volatileTorrentFields[field.Name]:
			require.Equal(t, base, fp, "volatile field %s must not affect the fingerprint", field.Name)
		case payloadOmittedTorrentFields[field.Name]:
			require.Equal(t, base, fp, "payload-omitted field %s must not affect the fingerprint", field.Name)
		default:
			require.NotEqual(t, base, fp, "field %s must change the fingerprint; add it to fpBuf.torrent, volatileTorrentFields, or payloadOmittedTorrentFields", field.Name)
		}
	}
}

func TestMagnetOnlyChangeDoesNotResendRow(t *testing.T) {
	before := []qbittorrent.TorrentView{{Torrent: &qbt.Torrent{Hash: "aaa", Name: "Alpha"}}}
	after := []qbittorrent.TorrentView{{Torrent: &qbt.Torrent{Hash: "aaa", Name: "Alpha", MagnetURI: "magnet:?xt=urn:btih:aaa"}}}

	_, _, baseFP := computeRowDelta(before, singleRowKey, singleRowFingerprint, nil)
	_, changed, _ := computeRowDelta(after, singleRowKey, singleRowFingerprint, baseFP)
	require.Empty(t, changed, "magnet-only change must not resend the row")
}

func TestHasMetadataOnlyChangeDoesNotResendRow(t *testing.T) {
	metadataUnavailable := false
	metadataAvailable := true
	before := []qbittorrent.TorrentView{{Torrent: &qbt.Torrent{Hash: "aaa", Name: "Alpha", HasMetadata: &metadataUnavailable}}}
	after := []qbittorrent.TorrentView{{Torrent: &qbt.Torrent{Hash: "aaa", Name: "Alpha", HasMetadata: &metadataAvailable}}}

	_, _, baseFP := computeRowDelta(before, singleRowKey, singleRowFingerprint, nil)
	_, changed, _ := computeRowDelta(after, singleRowKey, singleRowFingerprint, baseFP)
	require.Empty(t, changed, "has_metadata-only change must not resend the row")
}

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

func TestTrackerFingerprintCoversEveryField(t *testing.T) {
	assertEveryFieldHashed(t, "add it to the Trackers loop in fpBuf.torrent", func(tr qbt.TorrentTracker) uint64 {
		return singleRowFingerprint(new(fpBuf), qbittorrent.TorrentView{Torrent: &qbt.Torrent{Trackers: []qbt.TorrentTracker{tr}}})
	})
}

func TestCountsEqualCoversEveryField(t *testing.T) {
	var base qbittorrent.TorrentCounts
	typ := reflect.TypeFor[qbittorrent.TorrentCounts]()
	for i := range typ.NumField() {
		var changed qbittorrent.TorrentCounts
		setSampleValue(t, reflect.ValueOf(&changed).Elem().Field(i))
		require.False(t, countsEqual(&base, &changed), "%s must affect countsEqual", typ.Field(i).Name)
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
		singleRowFingerprint(new(fpBuf), row)
	}
}

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

func TestFingerprintDistinguishesNaNRows(t *testing.T) {
	a := singleRowFingerprint(new(fpBuf), qbittorrent.TorrentView{Torrent: &qbt.Torrent{Name: "A", Ratio: math.NaN()}})
	b := singleRowFingerprint(new(fpBuf), qbittorrent.TorrentView{Torrent: &qbt.Torrent{Name: "B", Ratio: math.NaN()}})
	require.NotEqual(t, a, b)
}

func TestStreamResendsARowThatGainsAnOptionalValue(t *testing.T) {
	gains := map[string]func(*qbittorrent.TorrentView){
		"Reannounce": func(tv *qbittorrent.TorrentView) { tv.Reannounce = new(int64(30)) },
		"Popularity": func(tv *qbittorrent.TorrentView) { tv.Popularity = new(0.0) },
		"Private":    func(tv *qbittorrent.TorrentView) { tv.Private = new(false) },
	}
	for name, gain := range gains {
		row := func(known bool) qbittorrent.TorrentView {
			tv := qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: "a", Name: "A", Reannounce: 30}}
			if known {
				gain(&tv)
			}
			return tv
		}
		for view, limit := range map[string]int{"list": 300, "details row": 1} {
			t.Run(name+"/"+view, func(t *testing.T) {
				g := &subscriptionGroup{}
				opts := StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 1, Limit: limit}}
				g.buildUpdatePayload(opts, singleResp(row(false)), &StreamMeta{})
				payload := g.buildUpdatePayload(opts, singleResp(row(true)), &StreamMeta{})
				require.Equal(t, []string{"a"}, changedHashes(payload))
			})
		}
		t.Run(name+"/cross-instance", func(t *testing.T) {
			g := &subscriptionGroup{}
			opts := StreamOptions{View: qbittorrent.TorrentListView{Unified: true, InstanceIDs: []int{1}}}
			cross := func(known bool) qbittorrent.CrossInstanceTorrentView {
				tv := row(known)
				return qbittorrent.CrossInstanceTorrentView{TorrentView: &tv, InstanceID: 1}
			}
			g.buildUpdatePayload(opts, crossResp(cross(false)), &StreamMeta{})
			payload := g.buildUpdatePayload(opts, crossResp(cross(true)), &StreamMeta{})
			require.Len(t, payload.Data.CrossInstanceTorrents, 1)
		})
	}
}
