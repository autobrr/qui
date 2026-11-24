// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
)

func TestNoteProxiedAddSendsOneSync(t *testing.T) {
	t.Parallel()
	qbtClient := qbt.NewClient(qbt.Config{Host: "http://qbt.example.invalid"})
	client := &Client{Client: qbtClient, instanceID: 1, syncManager: syncowner.New(qbtClient, qbt.DefaultSyncOptions())}
	sm := NewSyncManager(&ClientPool{clients: map[int]*Client{1: client}}, nil)

	synctest.Test(t, func(t *testing.T) {
		var syncs atomic.Int32
		serveInProcess(qbtClient, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			syncs.Add(1)
			_, _ = io.WriteString(w, `{"rid":1,"full_update":true}`)
		}))

		sm.NoteProxiedAdd(1)
		sm.NoteProxiedAdd(2)

		time.Sleep(200 * time.Millisecond)
		synctest.Wait()
		require.EqualValues(t, 1, syncs.Load())
		time.Sleep(time.Minute)
		synctest.Wait()
		require.EqualValues(t, 1, syncs.Load(), "a proxied add sends no second sync")
	})
}
