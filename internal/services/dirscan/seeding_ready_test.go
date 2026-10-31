// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func setUnexportedField(t *testing.T, target any, fieldName string, value any) {
	t.Helper()
	field := reflect.ValueOf(target).Elem().FieldByName(fieldName)
	require.Truef(t, field.IsValid(), "field %s missing on %T", fieldName, target)
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(value))
}

// An instance that has not synced since qui started is not ready, and the
// check asks for a sync, since nothing else may start one. After a sync the
// instance is ready. A client qui cannot get names its own cause.
//
// regression: #3613
func TestSyncManagerSeedingReady(t *testing.T) {
	ctx := t.Context()
	requested := make(chan struct{}, 16)
	qbtClient := qbt.NewClient(qbt.Config{Host: "http://qbt.example.invalid", Timeout: 60}).WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			if r.URL.Path != "/api/v2/sync/maindata" {
				rec.WriteHeader(http.StatusNotFound)
				return rec.Result(), nil
			}
			select {
			case requested <- struct{}{}:
			default:
			}
			_, _ = rec.WriteString(`{"rid":1,"full_update":true,"torrents":{}}`)
			return rec.Result(), nil
		}),
	})
	syncOpts := qbt.DefaultSyncOptions()
	syncOpts.DynamicSync = true
	client := &qbittorrent.Client{Client: qbtClient}
	setUnexportedField(t, client, "isHealthy", true)
	setUnexportedField(t, client, "lastHealthCheck", time.Now())
	setUnexportedField(t, client, "syncManager", qbtClient.NewSyncManager(syncOpts))
	setUnexportedField(t, client, "optimisticUpdates", ttlcache.New[string, *qbittorrent.OptimisticTorrentUpdate]())

	db := testdb.NewMigratedSQLite(t, "dirscan-seeding-ready")
	instanceStore, err := models.NewInstanceStore(db, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "synthetic", "http://qbt.example.invalid", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	pool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), 60*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	setUnexportedField(t, pool, "clients", map[int]*qbittorrent.Client{instance.ID: client})
	files := syncManagerTorrentFiles{qbittorrent.NewSyncManager(pool, nil)}

	require.ErrorContains(t, files.SeedingReady(ctx, instance.ID), "waiting for first sync")
	select {
	case <-requested:
	case <-time.After(10 * time.Second):
		t.Fatal("no sync was requested")
	}

	require.NoError(t, client.GetSyncManager().Sync(ctx))
	require.NoError(t, files.SeedingReady(ctx, instance.ID))

	require.ErrorContains(t, syncManagerTorrentFiles{}.SeedingReady(ctx, instance.ID), "client pool unavailable")
}
