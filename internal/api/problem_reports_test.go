// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/experimental"
	"github.com/autobrr/qui/internal/logstream"
	"github.com/autobrr/qui/internal/models"
	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/problemreport"
	"github.com/autobrr/qui/internal/services/systemnotice"
	"github.com/autobrr/qui/internal/testutil/fakebrrbin"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/sshtest"
	"github.com/autobrr/qui/pkg/version"
)

const (
	reportPasskey      = "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6"
	reportInstanceUser = "seedboxuser"
	reportBasicUser    = "proxyuser"
	reportIndexerKey   = "indexer-key-0123456789"
	reportSSHUser      = "remoteshell"
	reportSSHHost      = "storage.example.invalid"
	reportREDKey       = "red-key-0123456789abcdef"
	reportSubmission   = "0b7c1a52-6f43-4d6e-9c7e-2f8a1d3b4c5e"
)

var reportStartedAt = time.Date(2026, 11, 19, 8, 30, 0, 0, time.UTC)

type stubReleases struct{ release *version.Release }

func (s *stubReleases) GetLatestRelease(context.Context) *version.Release { return s.release }

type reportRig struct {
	t        *testing.T
	sshKey   string
	srv      *httptest.Server
	cookie   *http.Cookie
	fake     *fakebrrbin.Fake
	hub      *logstream.Hub
	store    *models.ProblemReportStore
	db       *database.DB
	stub     *qbtstub.Server
	releases *stubReleases
	notices  *systemnotice.Module
	apiKey   string
}

type reportRigOptions struct {
	experimentalOff   bool
	problemReportsOff bool
	client            *http.Client
}

// newReportRig starts qui's API with the real problem report service and
// store against a fake brrbin, a stub qBittorrent instance with two torrents,
// and a second instance that never connected.
func newReportRig(t *testing.T, opts reportRigOptions) *reportRig {
	t.Helper()
	deps, db := newTestDependenciesWithDB(t)
	rig := &reportRig{t: t, fake: fakebrrbin.New(), hub: logstream.NewHub(0), db: db, releases: &stubReleases{}, notices: deps.SystemNotices}
	brrbin := httptest.NewServer(rig.fake)
	t.Cleanup(brrbin.Close)

	rig.stub = qbtstub.New(t, t.TempDir())
	rig.stub.Libtorrent = "2.0.11.0"
	rig.stub.Add(qbtstub.Torrent{Hash: "1111111111111111111111111111111111111111", Name: "Example.Show.S01.1080p.WEB-DL-GRP"})
	rig.stub.Add(qbtstub.Torrent{Hash: "2222222222222222222222222222222222222222", Name: "Example.Film.2024.1080p.BluRay-GRP"})
	basicUser, basicPassword := reportBasicUser, "basic-password-123"
	instance, err := deps.InstanceStore.Create(t.Context(), "seedbox", rig.stub.URL, reportInstanceUser, "instance-password-123", &basicUser, &basicPassword, false, new(true))
	require.NoError(t, err)
	rig.sshKey = sshtest.PrivateKey("")
	require.NoError(t, deps.InstanceStore.SetSSHCredentials(t.Context(), instance.ID, reportSSHHost, 22, reportSSHUser, rig.sshKey))
	_, err = deps.InstanceStore.Create(t.Context(), "home", "http://qbittorrent:8080", "admin", "", nil, nil, false, new(true))
	require.NoError(t, err)
	_, err = deps.InstanceStore.Create(t.Context(), "local", "http://localhost/", "admin", "", nil, nil, false, new(true))
	require.NoError(t, err)
	client, err := deps.ClientPool.GetClient(t.Context(), instance.ID)
	require.NoError(t, err)
	_, err = client.GetAppInfo(t.Context())
	require.NoError(t, err)
	_, err = deps.SyncManager.GetTorrentRefsFresh(t.Context(), instance.ID, internalqbittorrent.TorrentRefFilter{})
	require.NoError(t, err)

	indexer, err := deps.TorznabIndexerStore.Create(t.Context(), "Example Indexer", "http://indexer.example.invalid", reportIndexerKey, nil, nil, true, 1, 30)
	require.NoError(t, err)
	require.NotZero(t, indexer.ID)

	rig.store, err = models.NewProblemReportStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	crossSeed, err := models.NewCrossSeedStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	_, err = crossSeed.UpsertSettings(t.Context(), &models.CrossSeedAutomationSettings{GazelleEnabled: false, RedactedAPIKey: reportREDKey})
	require.NoError(t, err)

	featureNames := []string{string(experimental.ProblemReports)}
	if opts.experimentalOff {
		featureNames = nil
	}
	deps.Experimental = experimental.New(featureNames)
	deps.Config.Config.ProblemReports = !opts.problemReportsOff
	client2 := opts.client
	if client2 == nil {
		client2 = &http.Client{}
	}
	deps.ProblemReports = problemreport.NewService(brrbin.URL, client2, rig.store, problemreport.Sources{
		Qui: func() problemreport.QuiFacts {
			return problemreport.QuiFacts{Version: "v1.30.0", StartedAt: reportStartedAt, OS: "linux", Arch: "amd64", Database: "sqlite", LogLevel: "DEBUG", ExperimentalFeatures: deps.Experimental.Names()}
		},
		Instances: deps.InstanceStore,
		Pool:      deps.ClientPool,
		Notices:   deps.SystemNotices,
		Logs:      rig.hub,
		Releases:  rig.releases,
		Search:    deps.CrossSeedService,
		Secrets: problemreport.StoreSecrets{
			Instances:     deps.InstanceStore,
			Indexers:      deps.TorznabIndexerStore,
			Arr:           deps.ArrInstanceStore,
			CrossSeed:     crossSeed,
			APIKeys:       models.NewAPIKeyStore(db),
			ClientAPIKeys: deps.ClientAPIKeyStore,
			Notifications: deps.NotificationTargetStore,
			Config:        func() []string { return []string{"session-secret-value"} },
		},
	})

	rig.apiKey, _, err = deps.AuthService.CreateAPIKey(t.Context(), "script", models.AllPermissions(), nil)
	require.NoError(t, err)

	router, err := NewServer(deps).Handler()
	require.NoError(t, err)
	rig.srv = httptest.NewServer(router)
	t.Cleanup(rig.srv.Close)

	sessions := deps.SessionManager
	signIn := sessions.LoadAndSave(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		sessions.Put(r.Context(), "authenticated", true)
		sessions.Put(r.Context(), "username", "test-user")
	}))
	rec := httptest.NewRecorder()
	signIn.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	require.Len(t, rec.Result().Cookies(), 1)
	rig.cookie = rec.Result().Cookies()[0]
	return rig
}

func (rig *reportRig) do(req *http.Request) (int, []byte) {
	rig.t.Helper()
	if len(req.Cookies()) == 0 {
		req.AddCookie(rig.cookie)
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(rig.t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(rig.t, err)
	return resp.StatusCode, body
}

func (rig *reportRig) facts(query string) (int, problemreport.Facts) {
	rig.t.Helper()
	req, err := http.NewRequestWithContext(rig.t.Context(), http.MethodGet, rig.srv.URL+"/api/problem-reports/facts?"+query, nil)
	require.NoError(rig.t, err)
	status, body := rig.do(req)
	var facts problemreport.Facts
	if status == http.StatusOK {
		require.NoError(rig.t, json.Unmarshal(body, &facts), string(body))
	}
	return status, facts
}

// send posts a report JSON, and an image when image is not nil.
func (rig *reportRig) send(report map[string]any, image []byte) (int, map[string]any) {
	rig.t.Helper()
	status, raw := rig.do(rig.sendRequest(rig.t.Context(), report, image))
	var answer map[string]any
	require.NoError(rig.t, json.Unmarshal(raw, &answer), string(raw))
	return status, answer
}

func (rig *reportRig) sendRequest(ctx context.Context, report map[string]any, image []byte) *http.Request {
	rig.t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreatePart(textproto.MIMEHeader{"Content-Disposition": {`form-data; name="report"`}, "Content-Type": {"application/json"}})
	require.NoError(rig.t, err)
	require.NoError(rig.t, json.NewEncoder(part).Encode(report))
	if image != nil {
		part, err := writer.CreateFormFile("image", "screen.png")
		require.NoError(rig.t, err)
		_, err = part.Write(image)
		require.NoError(rig.t, err)
	}
	require.NoError(rig.t, writer.Close())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rig.srv.URL+"/api/problem-reports", &body)
	require.NoError(rig.t, err)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(rig.cookie)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	return req
}

// sentReport returns the report JSON of the only report the fake received.
func (rig *reportRig) sentReport() map[string]any {
	rig.t.Helper()
	requests := rig.fake.Requests()
	require.Len(rig.t, requests, 1)
	var report map[string]any
	require.NoError(rig.t, json.Unmarshal(requests[0].Report, &report))
	return report
}

func menuReport(text string, facts problemreport.Facts, scope string, lines []string) map[string]any {
	return map[string]any{
		"submissionId":   reportSubmission,
		"text":           text,
		"startedFrom":    "menu",
		"qui":            facts.Qui,
		"instances":      facts.Instances,
		"notices":        facts.Notices,
		"browser":        map[string]any{"route": "/dashboard", "userAgent": "test", "viewportWidth": 1440, "viewportHeight": 900, "devicePixelRatio": 2},
		"failedRequests": []any{},
		"logs":           map[string]any{"scope": scope, "lines": lines},
	}
}

func TestProblemReportFacts(t *testing.T) {
	rig := newReportRig(t, reportRigOptions{})
	rig.notices.Open(systemnotice.Kind("low-disk"), "disk of http://"+strings.TrimPrefix(rig.stub.URL, "http://")+" is full")
	for i := range 600 {
		rig.hub.Write(fmt.Sprintf(`{"level":"warn","message":"warning %d"}`, i))
		rig.hub.Write(fmt.Sprintf(`{"level":"debug","message":"debug %d"}`, i))
	}

	status, facts := rig.facts("startedFrom=menu")
	require.Equal(t, http.StatusOK, status)

	assert.Equal(t, reportStartedAt, facts.Qui.StartedAt)
	assert.Nil(t, facts.Qui.NewerRelease)
	assert.Equal(t, []string{"problemReports"}, facts.Qui.ExperimentalFeatures)

	require.Len(t, facts.Instances, 3)
	connected := facts.Instances[0]
	assert.Equal(t, "v5.1.0", connected.QBittorrentVersion)
	assert.Equal(t, "2.0.11.0", connected.LibtorrentVersion)
	assert.Equal(t, "connected", connected.State)
	require.NotNil(t, connected.TorrentCount)
	assert.Equal(t, 2, *connected.TorrentCount)
	assert.Equal(t, "offline", facts.Instances[1].State)
	assert.Nil(t, facts.Instances[1].TorrentCount, "no torrent list in memory")

	require.Len(t, facts.Logs.Warnings, 200)
	assert.Contains(t, facts.Logs.Warnings[199], "warning 599")
	for _, line := range facts.Logs.Warnings {
		assert.NotContains(t, line, "debug")
	}
	assert.Len(t, facts.Logs.All, logstream.DefaultBufferSize)

	require.Len(t, facts.Notices, 1)
	assert.NotContains(t, facts.Notices[0].Cause, "127.0.0.1")

	raw, err := json.Marshal(facts)
	require.NoError(t, err)
	for _, private := range []string{"127.0.0.1", "qbittorrent:8080", reportInstanceUser, rig.stub.DefaultSavePath()} {
		assert.NotContains(t, string(raw), private)
	}

	rig.releases.release = &version.Release{TagName: "v1.36.0", HTMLURL: "https://github.com/autobrr/qui/releases/tag/v1.36.0"}
	_, facts = rig.facts("startedFrom=menu")
	require.NotNil(t, facts.Qui.NewerRelease)
	assert.Equal(t, "v1.36.0", facts.Qui.NewerRelease.Version)

	status, _ = rig.facts("startedFrom=elsewhere")
	assert.Equal(t, http.StatusBadRequest, status)
	for _, path := range []string{"/api/version", "/api/instances"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, rig.srv.URL+path, nil)
		require.NoError(t, err)
		status, _ := rig.do(req)
		assert.Equal(t, http.StatusOK, status, path)
	}
	assert.Empty(t, rig.fake.Requests(), "qui contacts brrbin only on Send")
}

func TestProblemReportSendCleansEveryTextPart(t *testing.T) {
	rig := newReportRig(t, reportRigOptions{})
	stubHost := strings.TrimPrefix(rig.stub.URL, "http://")
	rig.hub.Write(`{"level":"error","message":"announce failed: http://tracker.example.invalid/announce/` + reportPasskey + `"}`)
	rig.hub.Write(`{"level":"error","message":"login to ` + rig.stub.URL + ` as ` + reportInstanceUser + ` via ` + reportBasicUser + ` failed"}`)
	rig.hub.Write(`{"level":"error","message":"dial ` + stubHost + ` and 127.0.0.1 failed"}`)
	rig.hub.Write(`{"level":"error","message":"indexer answered: key=[` + reportIndexerKey + `]"}`)
	rig.hub.Write(`{"level":"error","message":"connect to qbittorrent refused"}`)
	_, facts := rig.facts("startedFrom=menu")

	text := "Uploads stall.\nMy announce is udp://tracker.example.invalid:6969/" + reportPasskey + "/announce and my key " + rig.apiKey +
		"\nqui reaches " + rig.stub.URL + " as " + reportInstanceUser + ", and http://localhost/ answers, and so does localhost." +
		"\nThe files sit on " + reportSSHHost + " for " + reportSSHUser + " with this key:\n" + rig.sshKey +
		"\nRED/OPS is off, its key was " + reportREDKey
	status, answer := rig.send(menuReport(text, facts, "warnings", facts.Logs.Warnings), nil)
	require.Equal(t, http.StatusCreated, status, answer)
	assert.Equal(t, "received", answer["status"])
	assert.Equal(t, "Uploads stall.", answer["title"])
	assert.NotContains(t, answer["text"], reportPasskey)
	assert.NotContains(t, answer["text"], rig.apiKey)

	request := rig.fake.Requests()[0]
	assert.NotEmpty(t, request.Header.Get("X-Install-Key"))
	assert.Empty(t, request.Header.Get("Authorization"), "an anonymous report carries no token")
	raw := string(request.Report)
	for _, private := range []string{reportPasskey, rig.apiKey, reportInstanceUser, reportBasicUser, stubHost, "127.0.0.1", reportIndexerKey,
		reportSSHHost, reportSSHUser, strings.TrimSpace(rig.sshKey), reportREDKey} {
		assert.NotContains(t, raw, private)
	}
	assert.Contains(t, raw, "connect to qbittorrent refused", "a host name with no dot stays")
	assert.Contains(t, raw, "http://localhost/ answers, and so does localhost.", "a bare URL of a host name with no dot stays")
	assert.NotContains(t, answer["text"], stubHost)
	assert.NotContains(t, answer["text"], reportInstanceUser)

	report := rig.sentReport()
	assert.Equal(t, "menu", report["startedFrom"])
	assert.Equal(t, "v1.30.0", report["qui"].(map[string]any)["version"])
	instances := report["instances"].([]any)
	require.Len(t, instances, 3)
	assert.Equal(t, "v5.1.0", instances[0].(map[string]any)["qbittorrentVersion"])
	assert.NotContains(t, report, "search")
	assert.NotContains(t, report, "matchPolicy")
	assert.NotContains(t, report, "searchScope")
}

func TestProblemReportSendLogScopes(t *testing.T) {
	rig := newReportRig(t, reportRigOptions{})
	_, facts := rig.facts("startedFrom=menu")
	lines := make([]string, 250)
	for i := range lines {
		lines[i] = fmt.Sprintf(`{"level":"warn","message":"line %d"}`, i)
	}

	status, _ := rig.send(menuReport("text", facts, "none", lines), nil)
	require.Equal(t, http.StatusCreated, status)
	logs := rig.sentReport()["logs"].(map[string]any)
	assert.Equal(t, "none", logs["scope"])
	assert.Empty(t, logs["lines"])

	rig = newReportRig(t, reportRigOptions{})
	status, _ = rig.send(menuReport("text", facts, "warnings", lines), nil)
	require.Equal(t, http.StatusCreated, status)
	sentLines := rig.sentReport()["logs"].(map[string]any)["lines"].([]any)
	require.Len(t, sentLines, 200)
	assert.Contains(t, sentLines[199], "line 249")

	// A buffer larger than the limit loses its oldest lines.
	rig = newReportRig(t, reportRigOptions{})
	big := make([]string, 6000)
	for i := range big {
		big[i] = fmt.Sprintf("%04d %s", i, strings.Repeat("x", 1000))
	}
	status, _ = rig.send(menuReport("text", facts, "all", big), nil)
	require.Equal(t, http.StatusCreated, status)
	request := rig.fake.Requests()[0]
	assert.LessOrEqual(t, len(request.Report), problemreport.MaxReportSize)
	sentLines = rig.sentReport()["logs"].(map[string]any)["lines"].([]any)
	assert.Less(t, len(sentLines), len(big))
	assert.Equal(t, big[len(big)-1], sentLines[len(sentLines)-1], "the newest line stays")
}

func TestProblemReportSearch(t *testing.T) {
	rig := newReportRig(t, reportRigOptions{})
	instances, err := models.NewInstanceStore(rig.db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	list, err := instances.List(t.Context())
	require.NoError(t, err)

	status, facts := rig.facts(url.Values{
		"startedFrom": {"search"},
		"instanceId":  {strconv.Itoa(list[0].ID)},
		"hash":        {"1111111111111111111111111111111111111111"},
	}.Encode())
	require.Equal(t, http.StatusOK, status)
	require.NotNil(t, facts.MatchPolicy)
	require.NotNil(t, facts.SearchScope)
	assert.InDelta(t, 5.0, facts.MatchPolicy.SizeTolerancePercent, 0.001)
	assert.NotEmpty(t, facts.SearchScope.TorznabIndexers, "the indexer qui asks")
	assert.Equal(t, "Example Indexer", facts.SearchScope.TorznabIndexers[0].Name)

	searchReport := func(search map[string]any) map[string]any {
		report := menuReport("No match found", facts, "all", nil)
		report["startedFrom"] = "search"
		report["search"] = search
		report["matchPolicy"] = facts.MatchPolicy
		report["searchScope"] = facts.SearchScope
		return report
	}
	status, _ = rig.send(searchReport(map[string]any{
		"trace":          map[string]any{"releaseFiltered": 3},
		"torrentName":    "Example.Show.S01.1080p.WEB-DL-GRP",
		"indexerNames":   []string{"Example Indexer"},
		"gazelleLookups": []any{},
	}), nil)
	require.Equal(t, http.StatusCreated, status)
	report := rig.sentReport()
	search := report["search"].(map[string]any)
	assert.Equal(t, "Example.Show.S01.1080p.WEB-DL-GRP", search["torrentName"])
	assert.Contains(t, search, "trace")
	assert.Equal(t, []any{"Example Indexer"}, search["indexerNames"])
	assert.Contains(t, report, "matchPolicy")
	assert.Contains(t, report, "searchScope")
	assert.NotContains(t, string(rig.fake.Requests()[0].Report), reportIndexerKey)

	// A RED/OPS-only search has lookups and no trace.
	rig = newReportRig(t, reportRigOptions{})
	status, _ = rig.send(searchReport(map[string]any{
		"torrentName":    "Example.Album.2024.FLAC-GRP",
		"indexerNames":   []string{},
		"gazelleLookups": []any{map[string]any{"site": "redacted.sh", "status": "searched"}},
	}), nil)
	require.Equal(t, http.StatusCreated, status)
	search = rig.sentReport()["search"].(map[string]any)
	assert.NotContains(t, search, "trace")
	assert.Len(t, search["gazelleLookups"], 1)
}

func TestProblemReportSendOneReportPerSubmission(t *testing.T) {
	rig := newReportRig(t, reportRigOptions{})
	_, facts := rig.facts("startedFrom=menu")

	status, first := rig.send(menuReport("text", facts, "none", nil), nil)
	require.Equal(t, http.StatusCreated, status)
	status, second := rig.send(menuReport("text", facts, "none", nil), nil)
	require.Equal(t, http.StatusCreated, status)
	assert.Equal(t, first["id"], second["id"])

	requests := rig.fake.Requests()
	require.Len(t, requests, 2)
	assert.Equal(t, requests[0].Header.Get("X-Install-Key"), requests[1].Header.Get("X-Install-Key"), "one Install key")

	reports, err := rig.store.SentReports(t.Context())
	require.NoError(t, err)
	require.Len(t, reports, 1)
	assert.Equal(t, reportSubmission, reports[0].SubmissionID)
	assert.Equal(t, "received", reports[0].Status)
	assert.Equal(t, reports[0].SentAt, reports[0].StatusChangedAt)

	var stored string
	require.NoError(t, rig.db.QueryRowContext(t.Context(), `SELECT install_key_encrypted FROM problem_report_install WHERE id = 1`).Scan(&stored))
	assert.NotContains(t, stored, requests[0].Header.Get("X-Install-Key"), "the Install key is stored encrypted")
}

func TestProblemReportSendRefusals(t *testing.T) {
	for _, tc := range []struct {
		fakeStatus int
		fakeReason string
		status     int
		reason     string
	}{
		{http.StatusTooManyRequests, "anonymous_full", http.StatusTooManyRequests, "anonymous_full"},
		{http.StatusTooManyRequests, "account_limit", http.StatusTooManyRequests, "account_limit"},
		{http.StatusForbidden, "sign_in_required", http.StatusForbidden, "sign_in_required"},
		{http.StatusConflict, "reporter_full", http.StatusConflict, "reporter_full"},
		{http.StatusRequestEntityTooLarge, "", http.StatusRequestEntityTooLarge, "too_large"},
		{http.StatusServiceUnavailable, "", http.StatusBadGateway, "unavailable"},
	} {
		t.Run(fmt.Sprintf("%d %s", tc.fakeStatus, tc.fakeReason), func(t *testing.T) {
			rig := newReportRig(t, reportRigOptions{})
			_, facts := rig.facts("startedFrom=menu")
			rig.fake.Refuse(tc.fakeStatus, tc.fakeReason)

			status, answer := rig.send(menuReport("text", facts, "none", nil), nil)
			assert.Equal(t, tc.status, status)
			assert.Equal(t, tc.reason, answer["error"])
			reports, err := rig.store.SentReports(t.Context())
			require.NoError(t, err)
			assert.Empty(t, reports)
		})
	}
}

func TestProblemReportSendTimeoutIsUnavailable(t *testing.T) {
	rig := newReportRig(t, reportRigOptions{client: &http.Client{Timeout: 200 * time.Millisecond}})
	_, facts := rig.facts("startedFrom=menu")
	rig.fake.Hold()

	status, answer := rig.send(menuReport("text", facts, "none", nil), nil)
	assert.Equal(t, http.StatusBadGateway, status)
	assert.Equal(t, "unavailable", answer["error"])
}

func TestProblemReportSendSurvivesAClosedTab(t *testing.T) {
	rig := newReportRig(t, reportRigOptions{})
	_, facts := rig.facts("startedFrom=menu")
	received, release := rig.fake.Hold()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		resp, err := http.DefaultClient.Do(rig.sendRequest(ctx, menuReport("text", facts, "none", nil), nil))
		if err == nil {
			resp.Body.Close()
		}
		done <- err
	}()
	<-received
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	release()
	rig.srv.Close() // waits for the Send handler to finish

	reports, err := rig.store.SentReports(t.Context())
	require.NoError(t, err)
	require.Len(t, reports, 1, "brrbin holds the report, so qui keeps its row")
	assert.Equal(t, "r1", reports[0].BrrbinID)
}

func TestProblemReportSignedInSend(t *testing.T) {
	rig := newReportRig(t, reportRigOptions{})
	_, facts := rig.facts("startedFrom=menu")
	require.NoError(t, rig.store.SaveSignIn(t.Context(), models.ProblemReportSignIn{Token: "brrbin-token-123", Provider: "github", Name: "someone"}))

	status, _ := rig.send(menuReport("text", facts, "none", nil), nil)
	require.Equal(t, http.StatusCreated, status)
	assert.Equal(t, "Bearer brrbin-token-123", rig.fake.Requests()[0].Header.Get("Authorization"))

	rig.fake.Refuse(http.StatusUnauthorized, "token_rejected")
	report := menuReport("text", facts, "none", nil)
	report["submissionId"] = "7d1e0c4b-2a39-4f5e-8b6c-9a0d1e2f3a4b"
	status, answer := rig.send(report, nil)
	assert.Equal(t, http.StatusConflict, status)
	assert.Equal(t, "token_rejected", answer["error"])
	signIn, err := rig.store.SignIn(t.Context())
	require.NoError(t, err)
	assert.Nil(t, signIn, "the Sender is anonymous again")
}

func TestProblemReportImageRefusedBeforeBrrbin(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	rig := newReportRig(t, reportRigOptions{})
	_, facts := rig.facts("startedFrom=menu")

	status, answer := rig.send(menuReport("text", facts, "none", nil), png)
	assert.Equal(t, http.StatusBadRequest, status, answer)
	assert.Empty(t, rig.fake.Requests(), "an anonymous image never reaches brrbin")

	require.NoError(t, rig.store.SaveSignIn(t.Context(), models.ProblemReportSignIn{Token: "brrbin-token-123", Provider: "discord", Name: "someone"}))
	tooLarge := append(slices.Clone(png), make([]byte, problemreport.MaxImageSize)...)
	status, answer = rig.send(menuReport("text", facts, "none", nil), tooLarge)
	assert.Equal(t, http.StatusRequestEntityTooLarge, status)
	assert.Equal(t, "too_large", answer["error"])
	assert.Empty(t, rig.fake.Requests())

	status, _ = rig.send(menuReport("text", facts, "none", nil), png)
	require.Equal(t, http.StatusCreated, status)
	assert.Equal(t, png, rig.fake.Requests()[0].Image)
}

func TestProblemReportOversizedReportPartIsTooLarge(t *testing.T) {
	rig := newReportRig(t, reportRigOptions{})
	_, facts := rig.facts("startedFrom=menu")

	status, answer := rig.send(menuReport(strings.Repeat("x", 16<<20), facts, "none", nil), nil)
	assert.Equal(t, http.StatusRequestEntityTooLarge, status)
	assert.Equal(t, "too_large", answer["error"])
	assert.Empty(t, rig.fake.Requests())
}

func TestProblemReportTVDBReadFailure(t *testing.T) {
	// A TVDB key that does not decrypt cannot reach a log line, so the report still sends.
	rig := newReportRig(t, reportRigOptions{})
	_, facts := rig.facts("startedFrom=menu")
	_, err := rig.db.ExecContext(t.Context(), `UPDATE cross_seed_settings SET season_pack_tvdb_api_key_encrypted = 'not-a-ciphertext' WHERE id = 1`)
	require.NoError(t, err)
	status, _ := rig.send(menuReport("text", facts, "none", nil), nil)
	assert.Equal(t, http.StatusCreated, status)

	// A failed read could leave the TVDB key in the text, so nothing leaves qui.
	rig = newReportRig(t, reportRigOptions{})
	_, facts = rig.facts("startedFrom=menu")
	_, err = rig.db.ExecContext(t.Context(), `ALTER TABLE cross_seed_settings DROP COLUMN season_pack_tvdb_pin_encrypted`)
	require.NoError(t, err)
	status, _ = rig.send(menuReport("text", facts, "none", nil), nil)
	assert.Equal(t, http.StatusInternalServerError, status)
	assert.Empty(t, rig.fake.Requests())
}

func TestProblemReportRoutesWhenOff(t *testing.T) {
	for _, tc := range []struct {
		name   string
		opts   reportRigOptions
		status int
	}{
		{"problemReports = false", reportRigOptions{problemReportsOff: true}, http.StatusForbidden},
		{"Experimental feature off and problemReports = false", reportRigOptions{experimentalOff: true, problemReportsOff: true}, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newReportRig(t, tc.opts)
			status, _ := rig.facts("startedFrom=menu")
			assert.Equal(t, tc.status, status)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, rig.srv.URL+"/api/problem-reports", strings.NewReader("{}"))
			require.NoError(t, err)
			status, body := rig.do(req)
			assert.Equal(t, tc.status, status)
			if tc.status == http.StatusForbidden {
				assert.Contains(t, string(body), "Problem reports are off in the qui config.")
			}
			assert.Empty(t, rig.fake.Requests())
		})
	}
}
