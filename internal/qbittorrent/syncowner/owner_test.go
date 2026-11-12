// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package syncowner_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
	"github.com/autobrr/qui/internal/testutil"
)

func TestOwner_WriteToSnapshotTorrentFailsNextSyncWithHash(t *testing.T) {
	const hash = "aaaa000000000000000000000000000000000001"
	qbtClient := qbt.NewClient(qbt.Config{Host: "http://qbt.example.invalid"}).WithHTTPClient(&http.Client{
		Transport: testutil.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			if r.URL.Path != "/api/v2/sync/maindata" {
				rec.WriteHeader(http.StatusNotFound)
				return rec.Result(), nil
			}
			if r.URL.Query().Get("rid") == "0" {
				_, _ = fmt.Fprintf(rec, `{"rid":1,"full_update":true,"torrents":{%q:{"name":"Quiet.Harbor.2024.1080p.WEB-DL-GRP","added_on":1}}}`, hash)
			} else {
				_, _ = rec.WriteString(`{"rid":2}`)
			}
			return rec.Result(), nil
		}),
	})
	owner := syncowner.New(qbtClient, qbt.DefaultSyncOptions())
	require.NoError(t, owner.Sync(t.Context()))

	torrent, ok := owner.SnapshotUnchecked().Torrent(hash)
	require.True(t, ok)
	torrent.Name = "changed by a reader"

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = owner.Sync(t.Context())
	}()
	require.NotNil(t, recovered, "the sync after a write to a shared torrent must fail")
	require.Contains(t, fmt.Sprint(recovered), hash)
}

// No code outside the owner may hold the sync object of go-qbittorrent, so no
// exported method of the owner hands it out or takes it in (ADR 0051).
func TestOwner_NoMethodPassesTheLibrarySyncObject(t *testing.T) {
	owner := reflect.TypeFor[*syncowner.Owner]()
	for method := range owner.Methods() {
		if carriesLibrarySync(method.Type, map[reflect.Type]bool{}) {
			t.Errorf("Owner.%s passes the sync object of go-qbittorrent: %s", method.Name, method.Type)
		}
	}
}

func TestCarriesLibrarySync(t *testing.T) {
	type exported struct{ Sync *qbt.SyncManager }

	for typ, want := range map[reflect.Type]bool{
		reflect.TypeFor[qbt.SyncManager]():                true,
		reflect.TypeFor[*qbt.SyncManager]():               true,
		reflect.TypeFor[[]*qbt.SyncManager]():             true,
		reflect.TypeFor[map[string]qbt.SyncManager]():     true,
		reflect.TypeFor[chan *qbt.SyncManager]():          true,
		reflect.TypeFor[func(func(*qbt.SyncManager))]():   true,
		reflect.TypeFor[func() []exported]():              true,
		reflect.TypeFor[syncowner.Owner]():                false,
		reflect.TypeFor[func(string) *qbt.SyncSnapshot](): false,
	} {
		require.Equal(t, want, carriesLibrarySync(typ, map[reflect.Type]bool{}), typ.String())
	}
}

// carriesLibrarySync reports whether code outside the owner can get the sync
// object of go-qbittorrent from a value of typ: the value, behind a pointer, in
// a list, a map, a channel, or an exported field, or through a callback.
func carriesLibrarySync(typ reflect.Type, seen map[reflect.Type]bool) bool {
	if typ == reflect.TypeFor[qbt.SyncManager]() {
		return true
	}
	if seen[typ] {
		return false
	}
	seen[typ] = true

	var parts []reflect.Type
	switch typ.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Chan:
		parts = []reflect.Type{typ.Elem()}
	case reflect.Map:
		parts = []reflect.Type{typ.Key(), typ.Elem()}
	case reflect.Func:
		parts = slices.AppendSeq(slices.Collect(typ.Ins()), typ.Outs())
	case reflect.Struct:
		for field := range typ.Fields() {
			if field.IsExported() {
				parts = append(parts, field.Type)
			}
		}
	default:
	}
	return slices.ContainsFunc(parts, func(part reflect.Type) bool { return carriesLibrarySync(part, seen) })
}
