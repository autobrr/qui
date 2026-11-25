// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
)

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
		{"/api/instances/1/rss/items?withData=true", []string{"RSS feed key", "RSS download passkey", "RSS article link key"}, `"https://rss.example.invalid"`},
		{"/api/instances/1/rss/items?withData=true", []string{"RSS article GUID key", "RSS description passkey", "RSS feed element key"}, `"https://rss.example.invalid"`},
		{"/api/instances/1/rss/items?withData=true", []string{"RSS feed name key"}, `"https://rss.example.invalid#9266f3f2"`},
		{"/api/instances/1/rss/rules", []string{"RSS feed key"}, `"https://rss.example.invalid#7ca629d1"`},
		{"/api/instances/1/rss/rules", []string{"RSS rule feed key"}, `"https://rss.example.invalid#1c54c51a"`},
		{"/api/instances/1/rss/items?withData=true", []string{"RSS feed title key"}, `"title":""`},
		{"/api/instances/1/rss/items?withData=true", []string{"RSS protocol-relative passkey"}, `"source":""`},
		{"/api/instances/1/rss/items?withData=true", []string{"RSS relative download passkey", "RSS relative link key"}, `"torrentURL":"","link":""`},
		{"/api/instances/1/rss/items?withData=true", []string{"RSS article GUID key"}, `"id":"` + rssShortForm("https://rss.example.invalid", "https://rss.example.invalid/dl/1?guid="+planted["RSS article GUID key"]) + `"`},
		{"/api/instances/1/rss/items?withData=true", []string{"RSS folder name key", "RSS folder feed key", "RSS folder article GUID key"}, `"` + rssShortForm("https://rss.example.invalid", "https://rss.example.invalid/folder?key="+planted["RSS folder name key"]) + `"`},
		{"/api/instances/1/rss/rules/rule/preview", []string{"RSS feed name key"}, `"https://rss.example.invalid#9266f3f2"`},
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
		{"stand-ins of the rule and the feed list", editKey, nil, append(shown, "https://rss.example.invalid#9266f3f2"), http.StatusCreated, []string{feedURL, goneFeedURL, feedNamedByURL}, 1},
		{"a full URL", editKey, nil, []string{"https://other.example/feed.xml"}, http.StatusCreated, []string{"https://other.example/feed.xml"}, 0},
		{"a full URL with a fragment", editKey, nil, []string{"https://other.example#releases"}, http.StatusCreated, []string{"https://other.example#releases"}, 0},
		{"a stand-in of no feed", editKey, nil, []string{feedURL, "https://rss.example.invalid#00000000"}, http.StatusBadRequest, nil, 1},
		{"a short form of two feeds", editKey, nil, []string{"https://rss.example.invalid#757efa96"}, http.StatusBadRequest, nil, 1},
		{"a key with Torrents write", writeKey, nil, []string{"https://rss.example.invalid#00000000"}, http.StatusCreated, []string{"https://rss.example.invalid#00000000"}, 0},
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

func TestRSSRuleMadeFromAFeedURLWatchesThatFeed(t *testing.T) {
	rig := newSecretsRig(t)
	editKey := rig.key(t, models.Permission{Area: models.AreaRSS, Level: models.LevelRead}, models.Permission{Area: models.AreaRSS, Level: models.LevelWrite})
	planted := map[string]string{}
	for _, secret := range rig.planted {
		planted[secret.name] = secret.value
	}
	feedURL := "https://rss.example.invalid/rss/" + planted["RSS feed key"] + "/feed.xml"

	status, body := rig.send(t, http.MethodGet, "/api/instances/1/rss/items", editKey, nil)
	require.Equal(t, http.StatusOK, status, body)
	var items map[string]struct {
		URL string `json:"url"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &items))
	shown := items["Feed"].URL
	assert.NotContains(t, shown, planted["RSS feed key"])

	status, body = rig.send(t, http.MethodGet, "/api/instances/1/rss/rules", editKey, nil)
	require.Equal(t, http.StatusOK, status, body)
	var rules qbt.RSSRules
	require.NoError(t, json.Unmarshal([]byte(body), &rules))
	assert.Equal(t, rules["rule"].AffectedFeeds[0], shown, "the feed list and the rule show one feed by different values")

	request, err := json.Marshal(map[string]any{"name": "new rule", "rule": qbt.RSSAutoDownloadRule{Enabled: true, MustContain: "Show", AffectedFeeds: []string{shown}}})
	require.NoError(t, err)
	savedBefore, _ := rig.stubRecord()
	status, body = rig.sendBody(t, http.MethodPost, "/api/instances/1/rss/rules", editKey, nil, string(request))
	require.Equal(t, http.StatusCreated, status, body)
	saved, _ := rig.stubRecord()
	require.Len(t, saved, len(savedBefore)+1)
	var savedRule qbt.RSSAutoDownloadRule
	require.NoError(t, json.Unmarshal([]byte(saved[len(savedBefore)]), &savedRule))
	assert.Equal(t, []string{feedURL}, savedRule.AffectedFeeds)
}

func rssShortForm(origin, full string) string {
	sum := sha256.Sum256([]byte(full))
	return origin + "#" + hex.EncodeToString(sum[:4])
}

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
	feedURL := origin + "/rss/" + planted["RSS feed key"] + "/feed.xml"
	folder := origin + "/folder?key=" + planted["RSS folder name key"]
	inFolder := origin + "/rss/" + planted["RSS folder feed key"] + "/feed.xml"
	guid := origin + "/dl/1?guid=" + planted["RSS article GUID key"]
	folderGUID := origin + "/dl/2?guid=" + planted["RSS folder article GUID key"]
	nested := short(folder) + `\` + short(inFolder)
	unknown := origin + "#00000000"
	const (
		refresh      = "/api/instances/1/rss/items/refresh"
		feedURLRoute = "/api/instances/1/rss/feeds/url"
		items        = "/api/instances/1/rss/items"
		move         = "/api/instances/1/rss/items/move"
		read         = "/api/instances/1/rss/articles/read"
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
		{"refresh a feed named by its URL", http.MethodPost, refresh, editKey, nil, map[string]string{"itemPath": short(feed)}, http.StatusOK, "refreshItem", map[string]string{"itemPath": feed}, 1},
		{"refresh a feed in a folder", http.MethodPost, refresh, editKey, nil, map[string]string{"itemPath": nested}, http.StatusOK, "refreshItem", map[string]string{"itemPath": folder + `\` + inFolder}, 1},
		{"refresh a short form of no feed", http.MethodPost, refresh, editKey, nil, map[string]string{"itemPath": unknown}, http.StatusBadRequest, "", nil, 1},
		{"remove a feed", http.MethodDelete, items, editKey, nil, map[string]string{"path": short(feed)}, http.StatusOK, "removeItem", map[string]string{"path": feed}, 1},
		{"set the URL of a feed", http.MethodPut, "/api/instances/1/rss/feeds/url", editKey, nil, map[string]string{"path": short(feed), "url": origin + "/rss/new.xml"}, http.StatusOK, "setFeedURL", map[string]string{"path": feed, "url": origin + "/rss/new.xml"}, 1},
		{"set the URL of a feed to the short form of a URL", http.MethodPut, feedURLRoute, editKey, nil, map[string]string{"path": "Feed", "url": short(feedURL)}, http.StatusOK, "setFeedURL", map[string]string{"path": "Feed", "url": feedURL}, 1},
		{"set the URL and name a feed by short forms", http.MethodPut, feedURLRoute, editKey, nil, map[string]string{"path": short(feed), "url": short(feed)}, http.StatusOK, "setFeedURL", map[string]string{"path": feed, "url": feed}, 1},
		{"set the URL of a feed to a short form of no feed", http.MethodPut, feedURLRoute, editKey, nil, map[string]string{"path": "Feed", "url": unknown}, http.StatusBadRequest, "", nil, 1},
		{"set the URL of a feed to a short form of two feeds", http.MethodPut, feedURLRoute, editKey, nil, map[string]string{"path": "Feed", "url": origin + "#757efa96"}, http.StatusBadRequest, "", nil, 1},
		{"set the URL of a feed with plain values", http.MethodPut, feedURLRoute, editKey, nil, map[string]string{"path": "Feed", "url": origin + "/rss/new.xml"}, http.StatusOK, "setFeedURL", map[string]string{"path": "Feed", "url": origin + "/rss/new.xml"}, 0},
		{"set the URL with a key with Torrents write", http.MethodPut, feedURLRoute, writeKey, nil, map[string]string{"path": "Feed", "url": unknown}, http.StatusOK, "setFeedURL", map[string]string{"path": "Feed", "url": unknown}, 0},
		{"move a feed into a folder", http.MethodPost, move, editKey, nil, map[string]string{"itemPath": short(feed), "destPath": short(folder) + `\` + short(feed)}, http.StatusOK, "moveItem", map[string]string{"itemPath": feed, "destPath": folder + `\` + feed}, 1},
		{"move a feed to the short form of another name", http.MethodPost, move, editKey, nil, map[string]string{"itemPath": short(feed), "destPath": `Archive\` + short(inFolder)}, http.StatusBadRequest, "", nil, 1},
		{"move with plain names", http.MethodPost, move, editKey, nil, map[string]string{"itemPath": "Feed", "destPath": `Archive\Feed`}, http.StatusOK, "moveItem", map[string]string{"itemPath": "Feed", "destPath": `Archive\Feed`}, 0},
		{"mark an article as read", http.MethodPost, read, editKey, nil, map[string]string{"itemPath": "Feed", "articleId": short(guid)}, http.StatusOK, "markAsRead", map[string]string{"itemPath": "Feed", "articleId": guid}, 1},
		{"mark an article in a folder as read", http.MethodPost, read, editKey, nil, map[string]string{"itemPath": nested, "articleId": short(folderGUID)}, http.StatusOK, "markAsRead", map[string]string{"itemPath": folder + `\` + inFolder, "articleId": folderGUID}, 1},
		{"mark a short form of no article as read", http.MethodPost, read, editKey, nil, map[string]string{"itemPath": "Feed", "articleId": "#00000000"}, http.StatusBadRequest, "", nil, 1},
		{"mark a whole feed as read", http.MethodPost, read, editKey, nil, map[string]string{"itemPath": short(feed)}, http.StatusOK, "markAsRead", map[string]string{"itemPath": feed}, 1},
		{"add a folder in a folder", http.MethodPost, "/api/instances/1/rss/folders", editKey, nil, map[string]string{"path": short(folder) + `\New`}, http.StatusCreated, "addFolder", map[string]string{"path": folder + `\New`}, 1},
		{"add a folder in a short form of no folder", http.MethodPost, "/api/instances/1/rss/folders", editKey, nil, map[string]string{"path": unknown + `\New`}, http.StatusBadRequest, "", nil, 1},
		{"add a feed to a short form of no folder", http.MethodPost, "/api/instances/1/rss/feeds", editKey, nil, map[string]string{"url": origin + "/rss/added.xml", "path": unknown}, http.StatusBadRequest, "", nil, 1},
		{"a key with Torrents write", http.MethodPost, refresh, writeKey, nil, map[string]string{"itemPath": short(feed)}, http.StatusOK, "refreshItem", map[string]string{"itemPath": short(feed)}, 0},
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
