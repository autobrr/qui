// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/autobrr/qui/internal/backups"
	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/experimental"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/automations"
	"github.com/autobrr/qui/internal/services/orphanscan"
)

type secretAudience int

const (
	shownToNobody secretAudience = iota
	shownWithTorrentsWrite
	shownWithNotificationsWrite
)

type plantedSecret struct {
	name     string
	value    string
	audience secretAudience
}

type secretsRig struct {
	*permissionRig
	routes  []routeKey
	planted []plantedSecret
}

func (rig *secretsRig) plant(name string, audience secretAudience) string {
	value := "QUIPLANTED" + strings.ReplaceAll(name, " ", "")
	rig.planted = append(rig.planted, plantedSecret{name: name, value: value, audience: audience})
	return value
}

const (
	stubTorrentJSON = `{"name":"NAME","hash":"NAME","state":"uploading","added_on":1,"size":100,"total_size":100,"progress":1,` +
		`"save_path":"/data","content_path":"/data/Show.S01","tracker":"TRACKER","magnet_uri":"magnet:?xt=urn:btih:NAME&tr=ESCAPED",` +
		`"trackers":[{"url":"TRACKER"}]}`
	stubFeedURL = "https://rss.example.invalid/feed"
)

func stubTorrent(name, tracker string) string {
	return strings.NewReplacer("NAME", name, "TRACKER", tracker, "ESCAPED", url.QueryEscape(tracker)).Replace(stubTorrentJSON)
}

func (rig *secretsRig) qBittorrentResponses() map[string]string {
	pathTracker := "https://tracker.example.invalid/" + rig.plant("announce path passkey", shownWithTorrentsWrite) + "/announce"
	queryTracker := "https://query.example.invalid/announce?passkey=" + rig.plant("announce query passkey", shownWithTorrentsWrite)
	first, second := stubTorrent("aaa", pathTracker), stubTorrent("bbb", queryTracker)
	trackerRow := func(tracker string) string {
		return `[{"url":"** [DHT] **","status":0},{"url":"` + tracker + `","status":2}]`
	}
	return map[string]string{
		"/api/v2/app/webapiVersion": "2.16.0",
		"/api/v2/app/version":       "v5.2.0",
		"/api/v2/auth/login":        "Ok.",
		"/api/v2/sync/maindata": `{"rid":1,"full_update":true,"torrents":{"aaa":` + first + `,"bbb":` + second + `},` +
			`"trackers":{"` + pathTracker + `":["aaa"],"` + queryTracker + `":["bbb"]},"server_state":{"connection_status":"connected"}}`,
		"/api/v2/torrents/info":                   `[` + first + `,` + second + `]`,
		"/api/v2/torrents/trackers?aaa":           trackerRow(pathTracker),
		"/api/v2/torrents/trackers?bbb":           trackerRow(queryTracker),
		"/api/v2/app/preferences":                 `{"add_trackers_enabled":true,"add_trackers":"` + pathTracker + `\n\n` + queryTracker + `","save_path":"/data"}`,
		"/api/v2/app/buildInfo":                   `{}`,
		"/api/v2/app/defaultSavePath":             `/data`,
		"/api/v2/app/getDirectoryContent":         `[]`,
		"/api/v2/app/networkInterfaceList":        `[]`,
		"/api/v2/app/networkInterfaceAddressList": `[]`,
		"/api/v2/app/getFreeSpaceAtPath":          `1000`,
		"/api/v2/torrents/files":                  `[{"index":0,"name":"Show.S01/file.mkv","size":100}]`,
		"/api/v2/torrents/categories":             `{}`,
		"/api/v2/torrents/tags":                   `[]`,
		"/api/v2/torrents/properties":             `{"save_path":"/data","comment":"Show"}`,
		"/api/v2/torrents/webseeds":               `[{"url":"https://seed.example.invalid/` + rig.plant("web seed passkey", shownWithTorrentsWrite) + `/file"}]`,
		"/api/v2/torrents/pieceStates":            `[2]`,
		"/api/v2/sync/torrentPeers":               `{"rid":1,"full_update":true,"peers":{}}`,
		"/api/v2/transfer/info":                   `{"connection_status":"connected"}`,
		"/api/v2/transfer/speedLimitsMode":        `0`,
		"/api/v2/torrentcreator/status":           `[{"taskID":"task","status":"Finished","trackers":["` + pathTracker + `","` + queryTracker + `"]}]`,
		"/api/v2/rss/items": `{"Feed":{"uid":"{1}","url":"` + stubFeedURL + `","title":"Feed","articles":[{"id":"1","title":"Show.S01",` +
			`"date":"24 Nov 2026 08:00:00 +0000","torrentURL":"https://rss.example.invalid/dl/1","link":"https://rss.example.invalid/t/1"}]}}`,
		"/api/v2/rss/rules":            `{"rule":{"enabled":true,"mustContain":"Show","affectedFeeds":["` + stubFeedURL + `"]}}`,
		"/api/v2/rss/matchingArticles": `{"Feed":["Show.S01"]}`,
	}
}

func (rig *secretsRig) startQBittorrent(t *testing.T) *httptest.Server {
	t.Helper()
	responses := rig.qBittorrentResponses()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/api/v2/torrents/trackers" {
			path += "?" + strings.ToLower(r.URL.Query().Get("hash"))
		}
		response, ok := responses[path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(srv.Close)
	return srv
}

const (
	stubCaps = `<?xml version="1.0" encoding="UTF-8"?><caps><server title="stub"/><limits default="100" max="100"/>` +
		`<searching><search available="yes" supportedParams="q"/><tv-search available="yes" supportedParams="q,season,ep"/>` +
		`<movie-search available="yes" supportedParams="q"/></searching>` +
		`<categories><category id="5000" name="TV"/><category id="2000" name="Movies"/></categories></caps>`
	stubFeed = `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>` +
		`<title>stub</title><item><title>Show.S01.1080p.WEB-DL-GRP</title><guid>https://indexer.example.invalid/t/1</guid>` +
		`<link>https://indexer.example.invalid/dl/1</link><comments>https://indexer.example.invalid/t/1</comments>` +
		`<pubDate>Tue, 24 Nov 2026 08:00:00 +0000</pubDate><size>100</size><category>5000</category>` +
		`<enclosure url="https://indexer.example.invalid/dl/1" length="100" type="application/x-bittorrent"/>` +
		`<torznab:attr name="seeders" value="5"/></item></channel></rss>`
)

func startStub(t *testing.T, answer func(*http.Request) string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, answer(r))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newSecretsRig(t *testing.T) *secretsRig {
	t.Helper()
	ctx := t.Context()
	deps, db := newTestDependenciesWithDB(t)
	rig := &secretsRig{permissionRig: &permissionRig{deps: deps}}

	var err error
	deps.Config, err = config.New(t.TempDir(), "test")
	require.NoError(t, err)
	deps.Config.Config.SessionSecret = rig.plant("session secret", shownToNobody)
	deps.Config.Config.OIDCClientSecret = rig.plant("OIDC client secret", shownToNobody)
	deps.Config.Config.DatabasePassword = rig.plant("database password", shownToNobody)
	deps.Config.Config.DatabaseEngine = "postgres"
	deps.Config.Config.DatabaseDSN = "host=db.example.invalid user=qui dbname=qui password=" + rig.plant("database DSN password", shownToNobody)
	deps.Experimental = experimental.New([]string{string(experimental.ProblemReports)})
	deps.InstanceCrossSeedCompletionStore = models.NewInstanceCrossSeedCompletionStore(db)
	deps.SeasonPackRunStore = models.NewSeasonPackRunStore(db)
	deps.OrphanScanStore = models.NewOrphanScanStore(db)
	deps.OrphanScanService = orphanscan.NewService(orphanscan.DefaultConfig(), deps.InstanceStore, deps.OrphanScanStore, deps.SyncManager, nil, deps.BackendPool)
	deps.AutomationActivityStore = models.NewAutomationActivityStore(db)
	deps.AutomationService = automations.NewService(automations.Config{}, deps.InstanceStore, deps.AutomationStore, deps.AutomationActivityStore,
		deps.TrackerCustomizationStore, deps.SyncManager, nil, nil, nil, deps.BackendPool)

	basicUser := "basic"
	basicPassword := rig.plant("instance basic password", shownToNobody)
	instance, err := deps.InstanceStore.Create(ctx, "stub", rig.startQBittorrent(t).URL, "user",
		rig.plant("instance password", shownToNobody), &basicUser, &basicPassword, false, new(true), rig.plant("instance API key", shownToNobody))
	require.NoError(t, err)
	require.Equal(t, 1, instance.ID)

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	block, err := ssh.MarshalPrivateKey(privateKey, "")
	require.NoError(t, err)
	sshKey := string(pem.EncodeToMemory(block))
	rig.planted = append(rig.planted, plantedSecret{name: "instance SSH key", value: strings.Split(sshKey, "\n")[2]})
	require.NoError(t, deps.InstanceStore.SetSSHCredentials(ctx, instance.ID, "ssh.example.invalid", 22, "user", sshKey))

	arrStub := startStub(t, func(*http.Request) string { return "[]" })
	arrPassword := rig.plant("arr basic password", shownToNobody)
	_, err = deps.ArrInstanceStore.Create(ctx, models.ArrInstanceTypeSonarr, "sonarr", arrStub.URL,
		rig.plant("arr API key", shownToNobody), &basicUser, &arrPassword, true, 1, 5)
	require.NoError(t, err)

	torznab := startStub(t, func(r *http.Request) string {
		if r.URL.Query().Get("t") == "caps" {
			return stubCaps
		}
		return stubFeed
	})
	indexerPassword := rig.plant("indexer basic password", shownToNobody)
	_, err = deps.TorznabIndexerStore.CreateWithIndexerID(ctx, "indexer", torznab.URL, "stub",
		rig.plant("indexer API key", shownToNobody), &basicUser, &indexerPassword, true, 1, 5, models.TorznabBackendJackett)
	require.NoError(t, err)

	settings, err := deps.CrossSeedService.GetAutomationSettings(ctx)
	require.NoError(t, err)
	settings.RedactedAPIKey = rig.plant("RED API key", shownToNobody)
	settings.OrpheusAPIKey = rig.plant("OPS API key", shownToNobody)
	settings.SeasonPackTVDBAPIKey = rig.plant("TVDB API key", shownToNobody)
	settings.SeasonPackTVDBPIN = rig.plant("TVDB PIN", shownToNobody)
	_, err = deps.CrossSeedService.UpdateAutomationSettings(ctx, settings)
	require.NoError(t, err)

	_, err = deps.NotificationTargetStore.Create(ctx, &models.NotificationTargetCreate{
		Name: "ntfy", URL: "ntfy://ntfy.example.invalid/" + rig.plant("notification URL token", shownWithNotificationsWrite), Enabled: true,
	})
	require.NoError(t, err)

	proxyKey, _, err := deps.ClientAPIKeyStore.Create(ctx, "sonarr", instance.ID)
	require.NoError(t, err)
	rig.planted = append(rig.planted, plantedSecret{name: "Client Proxy API key", value: proxyKey})

	_, err = models.NewDirScanStore(db).CreateDirectory(ctx, &models.DirScanDirectory{
		Path: "/data/scan", Enabled: true, TargetInstanceID: instance.ID, ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)

	_, err = deps.DiscScanStore.Create(ctx, instance.ID, "aaa", "/data/Show.S01", "/data/Show.S01")
	require.NoError(t, err)

	_, err = deps.OrphanScanStore.CreateRunIfNoActive(ctx, instance.ID, "manual")
	require.NoError(t, err)

	backupStore := models.NewBackupStore(db)
	run := &models.BackupRun{InstanceID: instance.ID, Kind: models.BackupRunKindManual, Status: models.BackupRunStatusSuccess, RequestedBy: "test", RequestedAt: time.Now()}
	require.NoError(t, backupStore.CreateRun(ctx, run))
	require.EqualValues(t, 1, run.ID)
	require.NoError(t, backupStore.InsertItems(ctx, run.ID, []models.BackupItem{
		{TorrentHash: "aaa", Name: "aaa", Category: new("snapshot"), SizeBytes: 100},
		{TorrentHash: "bbb", Name: "bbb", Category: new("snapshot"), SizeBytes: 100},
	}))
	deps.BackupService = backups.NewService(backupStore, deps.SyncManager, backups.Config{DataDir: t.TempDir()}, nil)

	otherKey, _, err := deps.AuthService.CreateAPIKey(ctx, "other", []models.Permission{{Area: models.AreaTorrents, Level: models.LevelRead}}, nil)
	require.NoError(t, err)
	rig.planted = append(rig.planted, plantedSecret{name: "another API key", value: otherKey})

	router, err := NewServer(deps).Handler()
	require.NoError(t, err)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	rig.url = srv.URL
	rig.routes = apiRoutes(t, router)
	return rig
}

const (
	unlistedStreamWait = 500 * time.Millisecond
	streamEventWait    = 30 * time.Second
)

func (rig *secretsRig) answer(t *testing.T, route routeKey, key string, cookie *http.Cookie) (int, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request := walkRequests[route]
	if request.body == "" {
		request.body = "{}"
	}
	req, err := http.NewRequestWithContext(ctx, route.Method, rig.url+walkPath(route), strings.NewReader(request.body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "%s %s", route.Method, route.Path)
	defer resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		raw, err := io.ReadAll(resp.Body)
		require.NoError(t, err, "%s %s", route.Method, route.Path)
		return resp.StatusCode, string(raw)
	}
	wait := unlistedStreamWait
	if request.streamEvent != "" {
		wait = streamEventWait
	}
	stop := time.AfterFunc(wait, cancel)
	defer stop.Stop()
	events, complete := readStream(resp.Body, request.streamEvent)
	require.True(t, complete || request.streamEvent == "", "%s %s sent no %q event: %s", route.Method, route.Path, request.streamEvent, events)
	return resp.StatusCode, events
}

func readStream(stream io.Reader, lastEvent string) (events string, complete bool) {
	var read strings.Builder
	reader := bufio.NewReader(stream)
	inLastEvent := false
	for {
		line, err := reader.ReadString('\n')
		read.WriteString(line)
		if err != nil {
			return read.String(), false
		}
		if lastEvent != "" && strings.TrimSpace(line) == "event: "+lastEvent {
			inLastEvent = true
		}
		if inLastEvent && line == "\n" {
			return read.String(), true
		}
	}
}

func readPermissions() []models.Permission {
	var perms []models.Permission
	for _, info := range models.PermissionAreas {
		for _, level := range info.Levels {
			if level == models.LevelRead {
				perms = append(perms, models.Permission{Area: info.Area, Level: level})
			}
		}
	}
	return perms
}
