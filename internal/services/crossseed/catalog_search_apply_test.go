// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/bencode"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

type catalogReplaySync struct {
	*renameOnlySyncManager
	source      *qbt.Torrent
	sourceBytes []byte
	added       bool
}

func (m *catalogReplaySync) GetTorrentRefs(_ context.Context, _ int, filter qbittorrent.TorrentRefFilter) ([]*qbt.Torrent, error) {
	if len(filter.Hashes) == 0 || slices.Contains(filter.Hashes, m.source.Hash) {
		return []*qbt.Torrent{m.source}, nil
	}
	return nil, nil
}

func (m *catalogReplaySync) ExportTorrent(context.Context, int, string) ([]byte, string, string, error) {
	return m.sourceBytes, "source.torrent", "", nil
}

func (m *catalogReplaySync) ExtractDomainFromURL(raw string) string {
	u, _ := url.Parse(raw)
	return u.Hostname()
}

func (m *catalogReplaySync) AddTorrent(ctx context.Context, id int, data []byte, options map[string]string) (*qbt.TorrentAddResponse, error) {
	m.added = true
	return m.renameOnlySyncManager.AddTorrent(ctx, id, data, options)
}

func (m *catalogReplaySync) RenameTorrentFile(ctx context.Context, id int, hash, oldPath, newPath string) error {
	m.bulkActions = append(m.bulkActions, "rename:"+oldPath+"->"+newPath)
	return m.renameOnlySyncManager.RenameTorrentFile(ctx, id, hash, oldPath, newPath)
}

func (m *catalogReplaySync) RenameTorrentFolder(_ context.Context, _ int, hash, oldPath, newPath string) error {
	m.bulkActions = append(m.bulkActions, "rename:"+oldPath+"->"+newPath)
	for i := range m.files[hash] {
		file := &m.files[hash][i]
		if suffix, ok := strings.CutPrefix(file.Name, oldPath+"/"); ok {
			file.Name = newPath + "/" + suffix
		}
	}
	return nil
}

type catalogReplayBackend struct {
	*local.Backend
	plan *hardlinktree.TreePlan
}

func (b *catalogReplayBackend) HardlinkTree(_ context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	b.plan = plan
	return &fsops.TreeCreateResult{}, nil
}

func (b *catalogReplayBackend) ReflinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	return b.HardlinkTree(ctx, plan)
}

func (*catalogReplayBackend) SupportsReflink(context.Context, string) (bool, string, error) {
	return true, "", nil
}

func catalogTorrent(t *testing.T, name, source string, renamed bool) []byte {
	t.Helper()
	track := "01 - Still Water.flac"
	if renamed {
		track = "01 - Quiet Water.flac"
	}
	content := append([]byte(strings.Repeat("a", 100)), []byte(strings.Repeat("b", 10))...)
	piece := sha1.Sum(content)
	data, err := bencode.Marshal(map[string]any{"announce": "https://tracker.example.invalid/announce", "info": map[string]any{"name": name, "source": source, "piece length": int64(16384), "pieces": string(piece[:]), "files": []map[string]any{{"length": int64(100), "path": []string{track}}, {"length": int64(10), "path": []string{"booklet.pdf"}}}}})
	require.NoError(t, err)
	return data
}

func catalogReplay(t *testing.T, sourceBytes, targetBytes []byte, host, mode string, automatic bool) {
	t.Helper()
	temp := t.TempDir()
	sourceMeta, err := ParseTorrentMetadataWithInfo(sourceBytes)
	require.NoError(t, err)
	targetMeta, err := ParseTorrentMetadataWithInfo(targetBytes)
	require.NoError(t, err)
	wantRecheck := mode == "regular" && !slices.EqualFunc(sourceMeta.Files, targetMeta.Files, func(a, b qbt.TorrentFile) bool { return a.Name == b.Name && a.Size == b.Size })
	total := sourceFilesTotalSize(sourceMeta.Files)
	instance := &models.Instance{ID: 1, Name: "catalog", HasLocalFilesystemAccess: true, HardlinkBaseDir: filepath.Join(temp, "links"), UseHardlinks: mode == "hardlink", UseReflinks: mode == "reflink"}
	svc, base, candidate := newRenameOnlyService(t, instance, sourceMeta.HashV1, sourceMeta.Name, sourceMeta.Files, targetMeta.HashV1, targetMeta.Files)
	source := candidate.Torrents[0]
	source.Size, source.TotalSize = total, total
	source.Tracker = "https://flacsfor.me/announce"
	if host == "redacted.sh" {
		source.Tracker = "https://home.opsfet.ch/announce"
	}
	source.ContentPath = filepath.Join(temp, "data", filepath.FromSlash(sourceMeta.Files[0].Name))
	base.props[sourceMeta.HashV1].SavePath = filepath.Join(temp, "data")
	for _, file := range sourceMeta.Files {
		name := filepath.Join(temp, "data", filepath.FromSlash(file.Name))
		require.NoError(t, os.MkdirAll(filepath.Dir(name), 0o755))
		require.NoError(t, os.WriteFile(name, []byte("metadata replay placeholder"), 0o600))
	}
	sync := &catalogReplaySync{renameOnlySyncManager: base, source: source, sourceBytes: sourceBytes}
	svc.syncManager = sync
	svc.settler = idleSettler(sync)
	svc.searchResultCache = ttlcache.New[string, cachedTorrentSearchResults]()
	settings := models.DefaultCrossSeedAutomationSettings()
	settings.GazelleEnabled = true
	if host == "redacted.sh" {
		settings.RedactedAPIKey = "test-key"
	} else {
		settings.OrpheusAPIKey = "test-key"
	}
	svc.automationSettingsLoader = func(context.Context) (*models.CrossSeedAutomationSettings, error) { return settings, nil }
	backend := &catalogReplayBackend{Backend: local.NewBackend()}
	svc.SetBackendPool(fsops.NewPool(svc.instanceStore, backend))
	withAdder(svc)
	var query string
	var downloads int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch q.Get("action") {
		case "torrent":
			if q.Get("hash") != "" {
				_, _ = w.Write([]byte(`{"status":"failure","error":"bad hash parameter"}`))
				return
			}
			t.Errorf("a filename match must not fetch target metadata: %s", r.URL.RawQuery)
			http.Error(w, "unexpected metadata request", http.StatusBadRequest)
		case "browse":
			query = q.Get("filelist")
			found := false
			for _, file := range targetMeta.Files {
				if strings.Contains(strings.TrimSuffix(path.Base(file.Name), path.Ext(file.Name)), query) {
					found = true
				}
			}
			if !found {
				_, _ = w.Write([]byte(`{"status":"success","response":{"results":[]}}`))
				return
			}
			_, _ = fmt.Fprintf(w, `{"status":"success","response":{"results":[{"groupId":3,"groupName":"Different catalog listing","torrents":[{"torrentId":7,"size":%d}]}]}}`, total)
		case "download":
			downloads++
			_, _ = w.Write(targetBytes)
		default:
			http.Error(w, "unexpected action", http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	previousClient, previousFind := newGazelleClient, findGazelleMatch
	newGazelleClient = func(host, _, key string) (*gazellemusic.Client, error) {
		return gazellemusic.NewClient(host, server.URL, key)
	}
	findGazelleMatch = gazellemusic.FindMatch
	t.Cleanup(func() { newGazelleClient, findGazelleMatch = previousClient, previousFind })
	resp, err := svc.SearchTorrentMatches(t.Context(), 1, source.Hash, TorrentSearchOptions{})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	match := resp.Results[0]
	require.Equal(t, "gazelle:size", match.MatchReason)
	require.Equal(t, searchCandidateClassFileEvidence, match.SearchDecision.Class)
	require.Equal(t, source.Hash, match.SearchDecision.SourceHash)
	require.Empty(t, match.InfoHashV1, "a filename match carries no target hash")
	require.Equal(t, []int{3000}, resp.SourceTorrent.SearchCategories)
	if automatic {
		result, err := svc.executeCrossSeedSearchAttempt(t.Context(), 1, defaultsFor(triggerSeededSearch, settings), source, match, time.Now())
		require.NoError(t, err)
		require.Equal(t, models.CrossSeedSearchResultStatusAdded, result.Status, result.Message)
		mismatchedSource := *source
		mismatchedSource.Hash = adderV1
		failed, err := svc.executeCrossSeedSearchAttempt(t.Context(), 1, defaultsFor(triggerSeededSearch, settings), &mismatchedSource, match, time.Now())
		require.Error(t, err)
		require.Equal(t, models.CrossSeedSearchResultStatusFailed, failed.Status)
		require.Equal(t, 1, downloads, "bound decisions cannot be spent by another automatic source")
	} else {
		result, err := svc.ApplyTorrentSearchResults(t.Context(), 1, source.Hash, &ApplyTorrentSearchRequest{Selections: []TorrentSearchSelection{{IndexerID: match.IndexerID, DownloadURL: match.DownloadURL, Title: "client cannot replace identity"}}})
		require.NoError(t, err)
		require.Len(t, result.Results, 1)
		require.True(t, result.Results[0].Success, "%+v", result.Results[0])
	}
	require.True(t, sync.added)
	require.Equal(t, 1, downloads)
	require.Equal(t, "true", base.addTorrentOpts["skip_checking"])
	if wantRecheck {
		require.Equal(t, "true", base.addTorrentOpts["paused"])
		require.Contains(t, base.bulkActions, "recheck:"+targetMeta.HashV1)
		require.Len(t, svc.settler.queue, 1)
		queued := <-svc.settler.queue
		queued.addedAt = time.Now().Add(-time.Minute)
		full := qbt.Torrent{Hash: targetMeta.HashV1, State: qbt.TorrentStatePausedUp, Progress: 1}
		svc.settler.processPendingRecheckResume(t.Context(), 1, targetMeta.HashV1, queued, full)
		svc.settler.processPendingRecheckResume(t.Context(), 1, targetMeta.HashV1, queued, full)
		require.Contains(t, svc.settler.sync.(*settleScript).actions, "resume:"+targetMeta.HashV1)
	} else {
		require.Empty(t, svc.settler.queue)
		require.NotContains(t, base.bulkActions, "recheck:"+targetMeta.HashV1)
	}
	if mode != "regular" {
		require.NotNil(t, backend.plan)
		require.Len(t, backend.plan.Files, len(sourceMeta.Files))
		for _, edge := range backend.plan.Files {
			from, err := filepath.Rel(base.props[sourceMeta.HashV1].SavePath, edge.SourcePath)
			require.NoError(t, err)
			to, err := filepath.Rel(backend.plan.RootDir, edge.TargetPath)
			require.NoError(t, err)
			left, right := path.Base(filepath.ToSlash(from)), path.Base(filepath.ToSlash(to))
			require.Equal(t, left, right, "private originals use exact relative names")
		}
	}
	old := svc.matcher().matchVerdict(svc.matcher().readName(sourceMeta.Name), svc.matcher().readName(targetMeta.Name), matchEvidence{SourceSize: total, CandidateSize: total}, matchPolicy{})
	old.Relaxed = old.Strict.differences
	pairs, pairErr := completeCatalogPairing(targetMeta.Files, sourceMeta.Files, detectCommonRoot(targetMeta.Files), detectCommonRoot(sourceMeta.Files))
	require.NoError(t, pairErr)
	tiers := make([]catalogPairTier, len(pairs))
	for i, pair := range pairs {
		tiers[i] = pair.tier
	}
	sourceView, err := svc.readCatalogRelease(sourceMeta.Name, sourceMeta.Files, match.SearchDecision.Catalog.Family)
	require.NoError(t, err)
	targetView, err := svc.readCatalogRelease(targetMeta.Name, targetMeta.Files, match.SearchDecision.Catalog.Family)
	require.NoError(t, err)
	report, err := json.Marshal(map[string]any{"case": t.Name(), "filename_query": query, "total_bytes": total, "file_count": len(pairs), "source_hash": sourceMeta.HashV1, "target_hash": targetMeta.HashV1, "source_binding": match.SearchDecision.SourceHash, "listing_title": match.Title, "local_name": sourceMeta.Name, "downloaded_name": targetMeta.Name, "source_parsed": svc.matcher().readName(sourceMeta.Name).release, "target_parsed": svc.matcher().readName(targetMeta.Name).release, "source_view": sourceView.release, "target_view": targetView.release, "old_rejection": old.RejectReason, "observed_differences": old.relaxedFields(), "pair_tiers": tiers, "mode": mode, "automatic": automatic, "recheck_required": wantRecheck, "content_check": "simulated; no private payload proof", "materialization": "simulated; metadata edges only"})
	require.NoError(t, err)
	t.Logf("catalog_receipt=%s", report)
}

// regression: #4181
func TestCatalogSearchApply(t *testing.T) {
	t.Run("artwork larger than audio", func(t *testing.T) {
		piece := sha1.Sum([]byte(strings.Repeat("a", 30) + strings.Repeat("b", 70)))
		torrent := func(flag string) []byte {
			data, err := bencode.Marshal(map[string]any{"announce": "https://tracker.example.invalid/announce", "info": map[string]any{"name": "Mira Calder - Illustrated Album (2001) [FLAC]", "source": flag, "piece length": int64(16384), "pieces": string(piece[:]), "files": []map[string]any{{"length": int64(30), "path": []string{"01 - Still Water.flac"}}, {"length": int64(70), "path": []string{"cover.png"}}}}})
			require.NoError(t, err)
			return data
		}
		catalogReplay(t, torrent("RED"), torrent("OPS"), "orpheus.network", "regular", false)
	})
	for _, pair := range []struct{ source, target string }{
		{"Mira Calder - Morning Is Near (1992) [FLAC]", "Mira Calder - Morning Is Near (1992) [FLAC]"},
		{"Mira Calder - Beneath Quiet Skies - 2001 [WEB 2011 FLAC 24-96] - HDtracks", "Mira Calder - Beneath Quiet Skies (2002) HDTracks 2011 [FLAC 24-96]"},
		{"Mira Calder - Morning Is Near (1992) [FLAC]", "1992 - Morning Is Near"},
	} {
		for _, automatic := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/automatic=%v", pair.target, automatic), func(t *testing.T) {
				catalogReplay(t, catalogTorrent(t, pair.source, "RED", false), catalogTorrent(t, pair.target, "OPS", false), "orpheus.network", "regular", automatic)
			})
		}
	}
}

// regression: #4181
func TestCatalogPrivateFixtures(t *testing.T) {
	manifest := os.Getenv("QUI_GAZELLE_FIXTURES")
	if manifest == "" {
		t.Skip("private fixture manifest not supplied")
	}
	data, err := os.ReadFile(manifest)
	require.NoError(t, err)
	var fixtures struct {
		Fixtures []struct{ ID, Path, SHA256 string }
	}
	require.NoError(t, json.Unmarshal(data, &fixtures))
	byID := make(map[string][]byte)
	for _, f := range fixtures.Fixtures {
		body, err := os.ReadFile(f.Path)
		require.NoError(t, err)
		sum := sha256.Sum256(body)
		require.Equal(t, f.SHA256, hex.EncodeToString(sum[:]))
		byID[f.ID] = body
	}
	for _, pair := range [][2]string{{"A-RED", "A-OPS"}, {"A-OPS", "A-RED"}, {"B-RED", "B-OPS"}, {"B-OPS", "B-RED"}} {
		host := "orpheus.network"
		if strings.HasSuffix(pair[1], "RED") {
			host = "redacted.sh"
		}
		for _, mode := range []string{"regular", "hardlink", "reflink"} {
			t.Run(pair[0]+"-to-"+pair[1]+"/"+mode, func(t *testing.T) { catalogReplay(t, byID[pair[0]], byID[pair[1]], host, mode, false) })
		}
		t.Run(pair[0]+"-to-"+pair[1]+"/automatic", func(t *testing.T) { catalogReplay(t, byID[pair[0]], byID[pair[1]], host, "regular", true) })
	}
}
