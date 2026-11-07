// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// benchRunService serves pairs cross-seed pairs that share their file and as
// many torrents without a cross-seed from a stub qBittorrent, and runs the
// real cross-seed service. fileReads counts the torrents/files requests.
func benchRunService(b *testing.B, pairs int) (*Service, *models.Instance, *atomic.Int64) {
	b.Helper()
	var entries []string
	files := make(map[string]string)
	add := func(hash, name, file string, size int) {
		entries = append(entries, fmt.Sprintf(`"%s":{"name":"%s","ratio":2,"progress":1,"size":%d,"state":"stalledUP","save_path":"/data","content_path":"/data/%s","tags":""}`, hash, name, size, file))
		files[hash] = fmt.Sprintf(`[{"index":0,"name":"%s","size":%d,"progress":1,"priority":1}]`, file, size)
	}
	for i := range pairs {
		name := fmt.Sprintf("Harbor.Watch.S01E%03d.1080p.WEB.h264-GRP", i)
		add(fmt.Sprintf("a%039d", i), name, name+".mkv", 1000+i)
		add(fmt.Sprintf("b%039d", i), name, name+".mkv", 1000+i)
		lone := fmt.Sprintf("Quiet.Coast.%04d.1080p.BluRay.x264-GRP", i)
		add(fmt.Sprintf("c%039d", i), lone, lone+".mkv", 5000+i)
	}
	maindata := `{"rid":1,"full_update":true,"server_state":{"free_space_on_disk":1000},"torrents":{` + strings.Join(entries, ",") + `}}`
	fileReads := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.10.0"))
		case "/api/v2/sync/maindata":
			_, _ = w.Write([]byte(maindata))
		case "/api/v2/torrents/files":
			fileReads.Add(1)
			_, _ = w.Write([]byte(files[r.URL.Query().Get("hash")]))
		default:
			http.NotFound(w, r)
		}
	}))
	b.Cleanup(server.Close)

	db := testdb.NewMigratedSQLite(b, b.Name())
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(b, err)
	instance, err := instanceStore.Create(b.Context(), "bench", server.URL, "", "", nil, nil, false, new(false))
	require.NoError(b, err)
	clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Second)
	require.NoError(b, err)
	b.Cleanup(func() { _ = clientPool.Close() })
	syncManager := qbittorrent.NewSyncManager(clientPool, nil)
	matcher := crossseed.NewService(instanceStore, syncManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, nil)
	svc := NewService(DefaultConfig(), instanceStore, nil, nil, nil, syncManager, nil, nil, matcher, fsops.NewPool(instanceStore, localbackend.NewBackend()))
	return svc, instance, fileReads
}

// BenchmarkRunSessionDeletionSelection measures what a run reads before its
// actions: buildEvalContext, the load step and the Deletion selection. The
// delete rule matches no torrent. With a move rule, the run reads the groups
// of the whole library.
func BenchmarkRunSessionDeletionSelection(b *testing.B) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	b.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	include := deleteRule(DeleteModeWithFilesIncludeCrossSeeds, tagsContain("del"))
	move := &models.Automation{
		ID: 2, Name: "move", TrackerPattern: "*", Enabled: true, SortOrder: 2,
		Conditions: &models.ActionConditions{Move: &models.MoveAction{Enabled: true, Path: "/data/new", Condition: tagsContain("mv")}},
	}
	for _, tc := range []struct {
		name  string
		rules []*models.Automation
	}{
		{"include rule matches nothing", []*models.Automation{include}},
		{"whole library with a move rule", []*models.Automation{include, move}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			svc, instance, fileReads := benchRunService(b, 1000)
			ctx := b.Context()
			needs := NeedsFor(tc.rules)
			fileReads.Store(0)
			b.ReportAllocs()
			for b.Loop() {
				torrents, err := svc.syncManager.GetAllTorrents(ctx, instance.ID)
				if err != nil || len(torrents) != 3000 {
					b.Fatalf("got %d torrents: %v", len(torrents), err)
				}
				evalCtx, _, err := svc.buildEvalContext(ctx, instance.ID, instance, torrents, needs)
				if err != nil {
					b.Fatal(err)
				}
				groups := buildCrossSeedGroups(torrents, evalCtx)
				svc.loadCrossSeedFiles(ctx, instance.ID, groups, evalCtx, nil)
				selection := svc.selectDeletions(ctx, instance.ID, deletionSelectionInput{rules: tc.rules, torrents: torrents, evalCtx: evalCtx, crossSeeds: groups})
				if len(selection.byHash) != 0 {
					b.Fatalf("selected %d torrents, want none", len(selection.byHash))
				}
			}
			b.ReportMetric(float64(fileReads.Load())/float64(b.N), "fileReads/op")
		})
	}
}
