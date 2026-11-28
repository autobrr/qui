// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const (
	floorName      = "Harbor.Signal.2026.1080p.WEB-DL.H.264-LUMA"
	floorIndexerID = 1
	floorPiece     = 16384
	floorPieces    = 4
)

type floorRig struct {
	svc        *Service
	stub       *qbtstub.Server
	completion *models.InstanceCrossSeedCompletionStore
	instanceID int
	sourceHash string
	crossSeed  []byte
	wantHash   string
}

func floorTorrent(t *testing.T, source string, content []byte) []byte {
	t.Helper()
	pieces := make([]byte, floorPieces*metainfo.HashSize)
	data, err := bencode.Marshal(map[string]any{
		"announce": "https://tracker.example.invalid/announce",
		"info": map[string]any{
			"name": floorName, "source": source, "piece length": int64(floorPiece), "pieces": string(pieces),
			"files": []map[string]any{{"length": int64(len(content)), "path": []string{floorName + ".mkv"}}},
		},
	})
	require.NoError(t, err)
	return data
}

func newFloorRig(t *testing.T) *floorRig {
	t.Helper()
	ctx := t.Context()
	savePath := t.TempDir()
	content := bytes.Repeat([]byte{1}, floorPieces*floorPiece)
	file := filepath.Join(savePath, floorName, floorName+".mkv")
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
	require.NoError(t, os.WriteFile(file, content, 0o600))

	stub := qbtstub.New(t, savePath)
	sourceHash := stub.Seed(floorTorrent(t, "SRC", content), savePath)
	stub.Edit(sourceHash, func(source *qbtstub.Torrent) { source.CompletionOn = 1 })
	crossSeed := floorTorrent(t, "XS", content)
	meta, err := ParseTorrentMetadataWithInfo(crossSeed)
	require.NoError(t, err)

	db := testdb.NewMigratedSQLite(t, "crossseed-trigger-floor")
	instances, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instances.Create(ctx, "lib", stub.URL, "user", "pass", nil, nil, false, new(true))
	require.NoError(t, err)
	clients, err := qbittorrent.NewClientPool(instances, models.NewInstanceErrorStore(db), time.Minute)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clients.Close() })
	_, err = clients.GetClient(ctx, instance.ID)
	require.NoError(t, err)
	syncManager := qbittorrent.NewSyncManager(clients, nil)

	store, err := models.NewCrossSeedStore(db, make([]byte, 32))
	require.NoError(t, err)
	settings := models.DefaultCrossSeedAutomationSettings()
	settings.TargetInstanceIDs = []int{instance.ID}
	_, err = store.UpsertSettings(ctx, settings)
	require.NoError(t, err)
	completion := models.NewInstanceCrossSeedCompletionStore(db)

	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/download.torrent":
			w.Header().Set("Content-Type", "application/x-bittorrent")
			_, _ = w.Write(crossSeed)
		case r.URL.Query().Get("t") == "caps":
			w.Header().Set("Content-Type", "application/xml")
			_, _ = fmt.Fprint(w, `<caps><limits default="100" max="100"/><searching><search available="yes" supportedParams="q"/><movie-search available="yes" supportedParams="q"/></searching><categories><category id="2000" name="Movies"/></categories></caps>`)
		default:
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = fmt.Fprintf(w, `<rss version="2.0"><channel><title>Floor</title><item><title>%s</title><guid>floor-item</guid><size>%d</size><enclosure url="%s/download.torrent" length="%d" type="application/x-bittorrent" /></item></channel></rss>`,
				floorName, len(content), "http://"+r.Host, len(content))
		}
	}))
	t.Cleanup(indexer.Close)
	torznab := newJackettServiceWithIndexers([]*models.TorznabIndexer{{
		ID: floorIndexerID, Name: "Floor Indexer", BaseURL: indexer.URL, Backend: models.TorznabBackendNative, TimeoutSeconds: 5, Enabled: true,
		Capabilities: []string{"search", "movie-search"},
		Categories:   []models.TorznabIndexerCategory{{IndexerID: floorIndexerID, CategoryID: 2000, CategoryName: "Movies"}},
	}})

	svc := NewService(instances, syncManager, nil, store, models.NewCrossSeedBlocklistStore(db), torznab, nil, nil, nil, completion, nil, nil, nil, false, nil, NewSettler(syncManager, nil), nil, nil)
	return &floorRig{
		svc: svc, stub: stub, completion: completion, instanceID: instance.ID,
		sourceHash: sourceHash, crossSeed: crossSeed, wantHash: meta.HashV1.String(),
	}
}

func (r *floorRig) encoded() string {
	return base64.StdEncoding.EncodeToString(r.crossSeed)
}

type floorRow func(t *testing.T, r *floorRig)

func floorRows() map[trigger]floorRow {
	return map[trigger]floorRow{
		triggerRSS: func(t *testing.T, r *floorRig) {
			run, err := r.svc.RunAutomation(t.Context(), AutomationRunOptions{})
			require.NoError(t, err)
			require.Equal(t, 1, run.CrossSeedsAdded, "%+v", run)
		},
		triggerSeededSearch: func(t *testing.T, r *floorRig) {
			added := make(chan string, 1)
			r.stub.SetOnAdd(func(hash string) { added <- hash })
			_, err := r.svc.StartSearchRun(t.Context(), SearchRunOptions{InstanceID: r.instanceID})
			require.NoError(t, err)
			t.Cleanup(func() {
				r.svc.CancelSearchRun()
				select {
				case <-r.svc.scheduleWake:
				case <-time.After(10 * time.Second):
					t.Error("the Library Scan did not end")
				}
			})
			select {
			case hash := <-added:
				require.Equal(t, r.wantHash, hash)
			case <-time.After(10 * time.Second):
				t.Fatal("the Library Scan added nothing")
			}
		},
		triggerCompletion: func(t *testing.T, r *floorRig) {
			settings := models.DefaultInstanceCrossSeedCompletionSettings(r.instanceID)
			settings.Enabled = true
			_, err := r.completion.Upsert(t.Context(), settings)
			require.NoError(t, err)
			r.svc.HandleTorrentCompletion(t.Context(), r.instanceID, qbt.Torrent{Hash: r.sourceHash, Name: floorName, Progress: 1, CompletionOn: 1})
		},
		triggerInteractiveApply: func(t *testing.T, r *floorRig) {
			source := mustParseHash(t, r.sourceHash)
			search, err := r.svc.SearchTorrentMatches(t.Context(), r.instanceID, source, TorrentSearchOptions{})
			require.NoError(t, err)
			require.Len(t, search.Results, 1)
			match := search.Results[0]
			resp, err := r.svc.ApplyTorrentSearchResults(t.Context(), r.instanceID, source, &ApplyTorrentSearchRequest{Selections: []TorrentSearchSelection{{
				IndexerID: match.IndexerID, Indexer: match.Indexer, DownloadURL: match.DownloadURL, Title: match.Title, GUID: match.GUID,
			}}})
			require.NoError(t, err)
			require.Len(t, resp.Results, 1)
			require.True(t, resp.Results[0].Success, "%+v", resp.Results[0].InstanceResults)
		},
		triggerWebhook: func(t *testing.T, r *floorRig) {
			resp, err := r.svc.AutobrrApply(t.Context(), &AutobrrApplyRequest{TorrentData: r.encoded(), InstanceIDs: []int{r.instanceID}})
			require.NoError(t, err)
			require.True(t, resp.Success, "%+v", resp.Results)
		},
	}
}

func TestEachTriggerAddsThroughTheRealInjection(t *testing.T) {
	rows := floorRows()
	for _, constant := range triggerConstants(t) {
		t.Run(constant.name, func(t *testing.T) {
			row, ok := rows[constant.value]
			require.True(t, ok, "Trigger %s has no row that runs the real Injection", constant.name)
			r := newFloorRig(t)
			require.False(t, r.stub.Has(r.wantHash))
			row(t, r)
			require.True(t, r.stub.Has(r.wantHash), "the qBittorrent stub holds no cross-seed")
		})
	}
}

type triggerConstant struct {
	name  string
	value trigger
}

func triggerConstants(t *testing.T) []triggerConstant {
	t.Helper()
	fset := token.NewFileSet()
	paths, err := filepath.Glob("*.go")
	require.NoError(t, err)
	var decls []ast.Decl
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		require.NoError(t, err)
		for _, decl := range file.Decls {
			if gen, ok := decl.(*ast.GenDecl); ok && declaresTrigger(gen) {
				decls = append(decls, gen)
			}
		}
	}
	pkg, err := new(types.Config).Check("triggers", fset, []*ast.File{{Name: ast.NewIdent("triggers"), Decls: decls}}, nil)
	require.NoError(t, err)
	triggerType := pkg.Scope().Lookup("trigger").Type()
	var constants []triggerConstant
	for _, name := range pkg.Scope().Names() {
		c, ok := pkg.Scope().Lookup(name).(*types.Const)
		if !ok || !types.Identical(c.Type(), triggerType) {
			continue
		}
		value, exact := constant.Int64Val(c.Val())
		require.True(t, exact, name)
		constants = append(constants, triggerConstant{name, trigger(value)})
	}
	require.NotEmpty(t, constants)
	return constants
}

func declaresTrigger(gen *ast.GenDecl) bool {
	for _, spec := range gen.Specs {
		switch spec := spec.(type) {
		case *ast.TypeSpec:
			if spec.Name.Name == "trigger" {
				return true
			}
		case *ast.ValueSpec:
			if ident, ok := spec.Type.(*ast.Ident); ok && ident.Name == "trigger" {
				return true
			}
		}
	}
	return false
}
