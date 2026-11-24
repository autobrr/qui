// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
)

var walkPathValues = map[string]string{
	"hash":      "aaa",
	"fileIndex": "0",
	"ruleName":  "rule",
	"name":      "qbittorrent",
	"filename":  "qui.log",
}

type walkRequest struct {
	query, body, streamEvent string
}

var walkRequests = map[routeKey]walkRequest{
	{http.MethodGet, "/api/stream"}:                                                       {query: "?streams=" + url.QueryEscape(`[{"key":"view","instanceId":1,"limit":100}]`), streamEvent: "init"},
	{http.MethodGet, "/api/cross-seed/search/runs"}:                                       {query: "?instanceId=1"},
	{http.MethodGet, "/api/instances/{instanceID}/free-space"}:                            {query: "?path=/data"},
	{http.MethodGet, "/api/instances/{instanceID}/getDirectoryContent"}:                   {query: "?dirPath=/data"},
	{http.MethodGet, "/api/instances/{instanceID}/transfer-history"}:                      {query: "?bucket=day&groupBy=tracker"},
	{http.MethodGet, "/api/instances/{instanceID}/rss/items"}:                             {query: "?withData=true"},
	{http.MethodGet, "/api/instances/{instanceID}/rss/events"}:                            {streamEvent: "connected"},
	{http.MethodPost, "/api/torznab/search"}:                                              {body: `{"query":"Show"}`},
	{http.MethodPost, "/api/torznab/cross-seed/search"}:                                   {body: `{"query":"Show"}`},
	{http.MethodPost, "/api/arr/resolve"}:                                                 {body: `{"title":"Show.S01.1080p.WEB-DL-GRP","content_type":"tv"}`},
	{http.MethodPost, "/api/instances/{instanceID}/automations/preview"}:                  {body: `{"name":"rule","trackerPattern":"*","conditions":{"schemaVersion":"1","tag":{"enabled":true,"tags":["seen"],"mode":"add"}}}`},
	{http.MethodPost, "/api/cross-seed/manual/proposals"}:                                 {body: `{"instance_id":1,"torrent_data":"` + walkTorrent + `"}`},
	{http.MethodPost, "/api/cross-seed/manual/assemble/check"}:                            {body: `{"instance_id":1,"torrent_data":"` + walkTorrent + `"}`},
	{http.MethodPost, "/api/instances/{instanceID}/torrents/field"}:                       {body: `{"field":"name","hashes":["aaa"]}`},
	{http.MethodPost, "/api/instances/{instanceID}/torrents/delete-cross-seeds"}:          {body: `{"hashes":["aaa"],"action":"delete","deleteFiles":true}`},
	{http.MethodPost, "/api/instances/{instanceID}/backups/runs/{runID}/restore/preview"}: {body: `{"mode":"overwrite"}`},
}

const walkTorrent = "ZDQ6aW5mb2Q2Omxlbmd0aGkxMDBlNDpuYW1lODpTaG93LlMwMTEyOnBpZWNlIGxlbmd0aGkxNjM4NGU2OnBpZWNlczIwOmFhYWFhYWFhYWFhYWFhYWFhYWFhZWU="

var walkCannotAnswer = map[routeKey]string{
	{http.MethodGet, "/api/auth/me"}:                                                            "answers a browser session only",
	{http.MethodGet, "/api/auth/validate"}:                                                      "answers a browser session only",
	{http.MethodPost, "/api/auth/login"}:                                                        "needs the password of the account",
	{http.MethodPost, "/api/auth/setup"}:                                                        "the account exists",
	{http.MethodPost, "/api/stream/subscriptions"}:                                              "needs the connection of an open stream",
	{http.MethodGet, "/api/themes/custom"}:                                                      "needs a license",
	{http.MethodGet, "/api/peer-client-icons/{name}"}:                                           "the rig has no cached icon",
	{http.MethodGet, "/api/logs/files/{filename}"}:                                              "the rig writes no log file",
	{http.MethodGet, "/api/instances/{instanceID}/cross-seed/status"}:                           "the route is not implemented",
	{http.MethodGet, "/api/instances/{instanceID}/automations/activity/{activityId}"}:           "run details stay in memory, and the rig runs no automation",
	{http.MethodGet, "/api/instances/{instanceID}/mediainfo"}:                                   "needs the content file on disk",
	{http.MethodGet, "/api/instances/{instanceID}/torrents/{hash}/files/{fileIndex}/download"}:  "needs the content file on disk",
	{http.MethodGet, "/api/instances/{instanceID}/torrents/{hash}/files/{fileIndex}/mediainfo"}: "needs the content file on disk",
}

func walkPath(route routeKey) string {
	path := routeParam.ReplaceAllStringFunc(route.Path, func(param string) string {
		if value, ok := walkPathValues[strings.Trim(param, "{}")]; ok {
			return value
		}
		return "1"
	})
	return path + walkRequests[route].query
}

func TestReadKeyGetsNoPlantedSecret(t *testing.T) {
	rig := newSecretsRig(t)
	readKey := rig.key(t, readPermissions()...)
	cookie := rig.sessionCookie(t)

	shownToSession := map[string]bool{}
	reached := map[routeKey]bool{}
	for _, route := range rig.routes {
		status, response := rig.answer(t, route, readKey, nil)
		if status == http.StatusForbidden && (response == browserOnlyBody || strings.HasPrefix(response, needsPermissionPrefix)) {
			continue
		}
		reached[route] = true
		for _, secret := range rig.planted {
			assert.NotContains(t, response, secret.value, "%s %s gives the %s to a read-only key", route.Method, route.Path, secret.name)
		}
		answered := status >= 200 && status < 300
		if reason, listed := walkCannotAnswer[route]; listed {
			assert.False(t, answered, "%s %s answers now, so delete its row (%s) from walkCannotAnswer", route.Method, route.Path, reason)
		} else {
			assert.True(t, answered, "%s %s: %d %s", route.Method, route.Path, status, response)
		}

		_, response = rig.answer(t, route, "", cookie)
		for _, secret := range rig.planted {
			if secret.audience == shownWithTorrentsWrite {
				shownToSession[secret.name] = shownToSession[secret.name] || strings.Contains(response, secret.value)
				continue
			}
			assert.NotContains(t, response, secret.value, "%s %s gives the %s to a session", route.Method, route.Path, secret.name)
		}
	}

	require.GreaterOrEqual(t, len(reached), 120)
	for route := range walkCannotAnswer {
		require.True(t, reached[route], "%s %s in walkCannotAnswer is not a route that a read-only key reaches", route.Method, route.Path)
	}
	for _, secret := range rig.planted {
		if secret.audience == shownWithTorrentsWrite {
			require.True(t, shownToSession[secret.name], "no route gives the %s to a session, so the walk proves nothing about it", secret.name)
		}
	}
}

// regression: #4474
func TestRSSLinksNeedTorrentsWrite(t *testing.T) {
	rig := newSecretsRig(t)
	rssRead := models.Permission{Area: models.AreaRSS, Level: models.LevelRead}
	readKey := rig.key(t, rssRead)
	writeKey := rig.key(t, rssRead, models.Permission{Area: models.AreaTorrents, Level: models.LevelWrite})
	planted := map[string]string{}
	for _, secret := range rig.planted {
		planted[secret.name] = secret.value
	}

	for _, tt := range []struct {
		path  string
		links []string
		shown string
	}{
		// regression: #4474
		{"/api/instances/1/rss/items?withData=true", []string{"RSS feed key", "RSS download passkey", "RSS article link key"}, `"https://rss.example.invalid"`},
		// regression: #4474
		{"/api/instances/1/rss/items?withData=true", []string{"RSS article GUID key", "RSS description passkey", "RSS feed element key"}, `"https://rss.example.invalid"`},
		// regression: #4474
		{"/api/instances/1/rss/items?withData=true", []string{"RSS feed name key"}, `"https://rss.example.invalid#9266f3f2"`},
		// regression: #4474
		{"/api/instances/1/rss/rules", []string{"RSS feed key"}, `"https://rss.example.invalid#7ca629d1"`},
		// regression: #4474
		{"/api/instances/1/rss/rules", []string{"RSS rule feed key"}, `"https://rss.example.invalid#1c54c51a"`},
		// regression: #4474
		{"/api/instances/1/rss/items?withData=true", []string{"RSS feed title key"}, `"title":""`},
		// regression: #4474
		{"/api/instances/1/rss/items?withData=true", []string{"RSS protocol-relative passkey"}, `"source":""`},
		// regression: #4474
		{"/api/instances/1/rss/items?withData=true", []string{"RSS relative download passkey", "RSS relative link key"}, `"torrentURL":"","link":""`},
		// regression: #4474
		{"/api/instances/1/rss/items?withData=true", []string{"RSS article GUID key"}, `"id":"` + rssShortForm("https://rss.example.invalid", "https://rss.example.invalid/dl/1?guid="+planted["RSS article GUID key"]) + `"`},
		// regression: #4474
		{"/api/instances/1/rss/items?withData=true", []string{"RSS folder name key", "RSS folder feed key", "RSS folder article GUID key"}, `"` + rssShortForm("https://rss.example.invalid", "https://rss.example.invalid/folder?key="+planted["RSS folder name key"]) + `"`},
		// regression: #4474
		{"/api/instances/1/rss/rules/rule/preview", []string{"RSS feed name key"}, `"https://rss.example.invalid#9266f3f2"`},
		// regression: #4474
		{"/api/instances/1/rss/rules/rule/preview", []string{"RSS article title key"}, `"Feed":[""]`},
	} {
		status, hidden := rig.send(t, http.MethodGet, tt.path, readKey, nil)
		require.Equal(t, http.StatusOK, status, tt.path)
		status, full := rig.send(t, http.MethodGet, tt.path, writeKey, nil)
		require.Equal(t, http.StatusOK, status, tt.path)
		assert.Contains(t, hidden, tt.shown, tt.path)
		for _, link := range tt.links {
			assert.NotContains(t, hidden, planted[link], "%s gives the %s to a key without Torrents write", tt.path, link)
			assert.Contains(t, full, planted[link], "%s hides the %s from a key with Torrents write", tt.path, link)
		}
	}
}

// regression: #4474
func TestRSSRuleKeepsItsFeedsWhenAKeySavesItBack(t *testing.T) {
	rig := newSecretsRig(t)
	rssRead := models.Permission{Area: models.AreaRSS, Level: models.LevelRead}
	rssWrite := models.Permission{Area: models.AreaRSS, Level: models.LevelWrite}
	editKey := rig.key(t, rssRead, rssWrite)
	writeKey := rig.key(t, rssRead, rssWrite, models.Permission{Area: models.AreaTorrents, Level: models.LevelWrite})
	planted := map[string]string{}
	for _, secret := range rig.planted {
		planted[secret.name] = secret.value
	}
	feedURL := "https://rss.example.invalid/rss/" + planted["RSS feed key"] + "/feed.xml"
	goneFeedURL := "https://rss.example.invalid/rss/" + planted["RSS rule feed key"] + "/gone.xml"
	feedNamedByURL := "https://rss.example.invalid/rss/" + planted["RSS feed name key"] + "/feed.xml"

	status, body := rig.send(t, http.MethodGet, "/api/instances/1/rss/rules", editKey, nil)
	require.Equal(t, http.StatusOK, status)
	var rules qbt.RSSRules
	require.NoError(t, json.Unmarshal([]byte(body), &rules))
	shown := rules["rule"].AffectedFeeds
	require.Equal(t, []string{"https://rss.example.invalid#7ca629d1", "https://rss.example.invalid#1c54c51a"}, shown)

	for _, tt := range []struct {
		name   string
		key    string
		cookie *http.Cookie
		feeds  []string
		status int
		saved  []string
		reads  int
	}{
		// regression: #4474
		{"stand-ins of the rule and the feed list", editKey, nil, append(shown, "https://rss.example.invalid#9266f3f2"), http.StatusCreated, []string{feedURL, goneFeedURL, feedNamedByURL}, 1},
		// regression: #4474
		{"a full URL", editKey, nil, []string{"https://other.example/feed.xml"}, http.StatusCreated, []string{"https://other.example/feed.xml"}, 0},
		// regression: #4474
		{"a full URL with a fragment", editKey, nil, []string{"https://other.example#releases"}, http.StatusCreated, []string{"https://other.example#releases"}, 0},
		// regression: #4474
		{"a stand-in of no feed", editKey, nil, []string{feedURL, "https://rss.example.invalid#00000000"}, http.StatusBadRequest, nil, 1},
		// regression: #4474
		{"a key with Torrents write", writeKey, nil, []string{"https://rss.example.invalid#00000000"}, http.StatusCreated, []string{"https://rss.example.invalid#00000000"}, 0},
		// regression: #4474
		{"a session", "", rig.sessionCookie(t), []string{"https://rss.example.invalid#00000000"}, http.StatusCreated, []string{"https://rss.example.invalid#00000000"}, 0},
	} {
		rule := rules["rule"]
		rule.MustContain = "Show.S02"
		rule.AffectedFeeds = tt.feeds
		request, err := json.Marshal(map[string]any{"name": "rule", "rule": rule})
		require.NoError(t, err)
		savedBefore, readsBefore := rig.stubRecord()
		before := len(savedBefore)
		status, body := rig.sendBody(t, http.MethodPost, "/api/instances/1/rss/rules", tt.key, tt.cookie, string(request))
		require.Equal(t, tt.status, status, "%s: %s", tt.name, body)
		saved, reads := rig.stubRecord()
		assert.Equal(t, tt.reads, reads-readsBefore, "%s reads the feed list %d times", tt.name, reads-readsBefore)
		if tt.saved == nil {
			assert.Len(t, saved, before, tt.name)
			continue
		}
		require.Len(t, saved, before+1, tt.name)
		var savedRule qbt.RSSAutoDownloadRule
		require.NoError(t, json.Unmarshal([]byte(saved[before]), &savedRule))
		assert.Equal(t, tt.saved, savedRule.AffectedFeeds, tt.name)
		assert.Equal(t, "Show.S02", savedRule.MustContain, tt.name)
	}
}

func rssShortForm(origin, full string) string {
	sum := sha256.Sum256([]byte(full))
	return origin + "#" + hex.EncodeToString(sum[:4])
}

// regression: #4474
func TestRSSFeedWritesFindTheNamesAKeyGot(t *testing.T) {
	rig := newSecretsRig(t)
	rssWrite := models.Permission{Area: models.AreaRSS, Level: models.LevelWrite}
	editKey := rig.key(t, rssWrite)
	writeKey := rig.key(t, rssWrite, models.Permission{Area: models.AreaTorrents, Level: models.LevelWrite})
	planted := map[string]string{}
	for _, secret := range rig.planted {
		planted[secret.name] = secret.value
	}
	const origin = "https://rss.example.invalid"
	short := func(full string) string { return rssShortForm(origin, full) }
	feed := origin + "/rss/" + planted["RSS feed name key"] + "/feed.xml"
	folder := origin + "/folder?key=" + planted["RSS folder name key"]
	inFolder := origin + "/rss/" + planted["RSS folder feed key"] + "/feed.xml"
	guid := origin + "/dl/1?guid=" + planted["RSS article GUID key"]
	folderGUID := origin + "/dl/2?guid=" + planted["RSS folder article GUID key"]
	nested := short(folder) + `\` + short(inFolder)
	unknown := origin + "#00000000"
	const (
		refresh = "/api/instances/1/rss/items/refresh"
		items   = "/api/instances/1/rss/items"
		move    = "/api/instances/1/rss/items/move"
		read    = "/api/instances/1/rss/articles/read"
	)

	for _, tt := range []struct {
		name     string
		method   string
		path     string
		key      string
		cookie   *http.Cookie
		body     map[string]string
		status   int
		endpoint string
		form     map[string]string
		reads    int
	}{
		// regression: #4474
		{"refresh a feed named by its URL", http.MethodPost, refresh, editKey, nil, map[string]string{"itemPath": short(feed)}, http.StatusOK, "refreshItem", map[string]string{"itemPath": feed}, 1},
		// regression: #4474
		{"refresh a feed in a folder", http.MethodPost, refresh, editKey, nil, map[string]string{"itemPath": nested}, http.StatusOK, "refreshItem", map[string]string{"itemPath": folder + `\` + inFolder}, 1},
		// regression: #4474
		{"refresh a short form of no feed", http.MethodPost, refresh, editKey, nil, map[string]string{"itemPath": unknown}, http.StatusBadRequest, "", nil, 1},
		// regression: #4474
		{"remove a feed", http.MethodDelete, items, editKey, nil, map[string]string{"path": short(feed)}, http.StatusOK, "removeItem", map[string]string{"path": feed}, 1},
		// regression: #4474
		{"set the URL of a feed", http.MethodPut, "/api/instances/1/rss/feeds/url", editKey, nil, map[string]string{"path": short(feed), "url": origin + "/rss/new.xml"}, http.StatusOK, "setFeedURL", map[string]string{"path": feed, "url": origin + "/rss/new.xml"}, 1},
		// regression: #4474
		{"move a feed into a folder", http.MethodPost, move, editKey, nil, map[string]string{"itemPath": short(feed), "destPath": short(folder) + `\` + short(feed)}, http.StatusOK, "moveItem", map[string]string{"itemPath": feed, "destPath": folder + `\` + feed}, 1},
		// regression: #4474
		{"move a feed to the short form of another name", http.MethodPost, move, editKey, nil, map[string]string{"itemPath": short(feed), "destPath": `Archive\` + short(inFolder)}, http.StatusBadRequest, "", nil, 1},
		// regression: #4474
		{"move with plain names", http.MethodPost, move, editKey, nil, map[string]string{"itemPath": "Feed", "destPath": `Archive\Feed`}, http.StatusOK, "moveItem", map[string]string{"itemPath": "Feed", "destPath": `Archive\Feed`}, 0},
		// regression: #4474
		{"mark an article as read", http.MethodPost, read, editKey, nil, map[string]string{"itemPath": "Feed", "articleId": short(guid)}, http.StatusOK, "markAsRead", map[string]string{"itemPath": "Feed", "articleId": guid}, 1},
		// regression: #4474
		{"mark an article in a folder as read", http.MethodPost, read, editKey, nil, map[string]string{"itemPath": nested, "articleId": short(folderGUID)}, http.StatusOK, "markAsRead", map[string]string{"itemPath": folder + `\` + inFolder, "articleId": folderGUID}, 1},
		// regression: #4474
		{"mark a short form of no article as read", http.MethodPost, read, editKey, nil, map[string]string{"itemPath": "Feed", "articleId": "#00000000"}, http.StatusBadRequest, "", nil, 1},
		// regression: #4474
		{"mark a whole feed as read", http.MethodPost, read, editKey, nil, map[string]string{"itemPath": short(feed)}, http.StatusOK, "markAsRead", map[string]string{"itemPath": feed}, 1},
		// regression: #4474
		{"add a folder in a folder", http.MethodPost, "/api/instances/1/rss/folders", editKey, nil, map[string]string{"path": short(folder) + `\New`}, http.StatusCreated, "addFolder", map[string]string{"path": folder + `\New`}, 1},
		// regression: #4474
		{"add a folder in a short form of no folder", http.MethodPost, "/api/instances/1/rss/folders", editKey, nil, map[string]string{"path": unknown + `\New`}, http.StatusBadRequest, "", nil, 1},
		// regression: #4474
		{"add a feed to a short form of no folder", http.MethodPost, "/api/instances/1/rss/feeds", editKey, nil, map[string]string{"url": origin + "/rss/added.xml", "path": unknown}, http.StatusBadRequest, "", nil, 1},
		// regression: #4474
		{"a key with Torrents write", http.MethodPost, refresh, writeKey, nil, map[string]string{"itemPath": short(feed)}, http.StatusOK, "refreshItem", map[string]string{"itemPath": short(feed)}, 0},
		// regression: #4474
		{"a session", http.MethodPost, refresh, "", rig.sessionCookie(t), map[string]string{"itemPath": short(feed)}, http.StatusOK, "refreshItem", map[string]string{"itemPath": short(feed)}, 0},
	} {
		request, err := json.Marshal(tt.body)
		require.NoError(t, err)
		writesBefore := len(rig.stubRSSWrites())
		_, readsBefore := rig.stubRecord()
		status, body := rig.sendBody(t, tt.method, tt.path, tt.key, tt.cookie, string(request))
		require.Equal(t, tt.status, status, "%s: %s", tt.name, body)
		_, reads := rig.stubRecord()
		assert.Equal(t, tt.reads, reads-readsBefore, "%s reads the feed list %d times", tt.name, reads-readsBefore)
		writes := rig.stubRSSWrites()[writesBefore:]
		if tt.form == nil {
			assert.Empty(t, writes, tt.name)
			continue
		}
		require.NotEmpty(t, writes, tt.name)
		sent := map[string]string{}
		for name := range writes[0].form {
			sent[name] = writes[0].form.Get(name)
		}
		assert.Equal(t, tt.endpoint, writes[0].endpoint, tt.name)
		assert.Equal(t, tt.form, sent, tt.name)
	}
}
