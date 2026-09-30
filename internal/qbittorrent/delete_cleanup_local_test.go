// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// remoteRowStore answers every instance read with a row in remote mode.
type remoteRowStore struct{}

func (remoteRowStore) Get(_ context.Context, id int) (*models.Instance, error) {
	return &models.Instance{ID: id, SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-hostkey"}, nil
}

// statSpy stands in for the SSH host and counts the stats that reached it.
type statSpy struct {
	fsops.Backend
	stats atomic.Int32
}

func (s *statSpy) Stat(_ context.Context, p string) (*fsops.LstatInfo, error) {
	s.stats.Add(1)
	return nil, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
}

// The ClientPool's instance store still reads the row as local, the backend
// pool's store reads it as remote. The cleanup must decide from the backend
// pool's row alone and skip, rather than stat and later remove the local
// hardlink base dir's paths on the SSH host.
func TestManagedDeleteCleanupSkipsAnInstanceThatLeftLocalMode(t *testing.T) {
	const hash = "cccccccccccccccccccccccccccccccccccccccc"
	baseDir := filepath.Join(t.TempDir(), "links")
	savePath := filepath.Join(baseDir, "Some.Release")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = io.WriteString(w, "Ok.")
		case "/api/v2/app/webapiVersion":
			_, _ = io.WriteString(w, "2.11.4")
		case "/api/v2/sync/maindata":
			_, _ = fmt.Fprintf(w, `{"rid":1,"full_update":true,"torrents":{%q:{"name":"Some.Release","save_path":%q,"content_path":%q}}}`,
				hash, filepath.ToSlash(savePath), filepath.ToSlash(filepath.Join(savePath, "a.mkv")))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	db := testdb.NewMigratedSQLite(t, "qbittorrent-delete-cleanup-local")
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), "test", srv.URL, "", "", nil, nil, false, new(true))
	require.NoError(t, err)
	_, err = instanceStore.Update(t.Context(), instance.ID, instance.Name, srv.URL, "", "", nil, nil,
		&models.InstanceUpdateParams{HardlinkBaseDir: &baseDir})
	require.NoError(t, err)

	client, err := NewClientWithTimeout(instance.ID, srv.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	t.Cleanup(client.optimisticUpdates.Close)
	require.NoError(t, client.GetSyncManager().Sync(t.Context()))

	sm := NewSyncManager(&ClientPool{instanceStore: instanceStore, clients: map[int]*Client{instance.ID: client}}, nil)
	spy := &statSpy{}
	sm.SetBackendPool(fsops.NewPoolWithRemote(remoteRowStore{}, local.NewBackend(),
		func(*models.Instance) fsops.Backend { return spy }))

	targets, backend := sm.buildManagedDeleteCleanupTargets(t.Context(), instance.ID, client.GetSyncManager(), []string{hash})

	require.Empty(t, targets)
	require.Nil(t, backend)
	require.Zero(t, spy.stats.Load(), "the cleanup must not read the SSH host")
}
