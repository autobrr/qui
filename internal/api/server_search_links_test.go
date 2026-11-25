// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/jackett"
)

type searchLinks struct {
	Results []struct {
		IndexerID   int    `json:"indexer_id"`
		DownloadURL string `json:"download_url"`
		InfoURL     string `json:"info_url"`
		GUID        string `json:"guid"`
	} `json:"results"`
}

func (rig *secretsRig) post(t *testing.T, path, key string, cookie *http.Cookie, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, rig.url+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(raw)
}

func (rig *secretsRig) searchLinks(t *testing.T, path, key string, cookie *http.Cookie, body string) searchLinks {
	t.Helper()
	status, response := rig.post(t, path, key, cookie, body)
	require.Equal(t, http.StatusOK, status, response)
	var links searchLinks
	require.NoError(t, json.Unmarshal([]byte(response), &links))
	require.Len(t, links.Results, 1, response)
	return links
}

func TestSearchLinksNeedTorrentsWrite(t *testing.T) {
	rig := newSecretsRig(t)
	indexersRead := models.Permission{Area: models.AreaIndexers, Level: models.LevelRead}
	crossSeedRead := models.Permission{Area: models.AreaCrossSeed, Level: models.LevelRead}
	withoutWrite := rig.key(t, indexersRead, crossSeedRead)
	withWrite := rig.key(t, indexersRead, crossSeedRead, models.Permission{Area: models.AreaTorrents, Level: models.LevelWrite})
	cookie := rig.sessionCookie(t)
	details, err := url.Parse(rig.feedDetails)
	require.NoError(t, err)

	routes := []struct {
		path, body string
	}{
		{"/api/torznab/search", `{"query":"Show"}`},
		{"/api/torznab/cross-seed/search", `{"query":"Show"}`},
		{"/api/cross-seed/torrents/1/aaa/search", `{}`},
	}
	callers := []struct {
		name   string
		key    string
		cookie *http.Cookie
		full   bool
	}{
		{"key without Torrents write", withoutWrite, nil, false},
		{"key with Torrents write", withWrite, nil, true},
		{"browser session", "", cookie, true},
		{"key without Torrents write after a full answer", withoutWrite, nil, false},
	}
	for _, route := range routes {
		t.Run(route.path, func(t *testing.T) {
			for _, caller := range callers {
				got := rig.searchLinks(t, route.path, caller.key, caller.cookie, route.body).Results[0]
				if caller.full {
					require.Equal(t, rig.feedLink, got.DownloadURL, caller.name)
					require.Equal(t, rig.feedDetails, got.InfoURL, caller.name)
					require.Equal(t, rig.feedGUID, got.GUID, caller.name)
					continue
				}
				require.Empty(t, got.DownloadURL, caller.name)
				require.Equal(t, details.Scheme+"://"+details.Host, got.InfoURL, caller.name)
				require.Equal(t, jackett.HiddenGUID(got.IndexerID, rig.feedGUID, rig.feedLink), got.GUID, caller.name)
			}
		})
	}
}

func TestKeyWithoutTorrentsWriteAppliesSearchResultByHiddenGUID(t *testing.T) {
	rig := newSecretsRig(t)
	key := rig.key(t, models.Permission{Area: models.AreaCrossSeed, Level: models.LevelWrite})

	found := rig.searchLinks(t, "/api/cross-seed/torrents/1/aaa/search", key, nil, `{}`).Results[0]
	require.Empty(t, found.DownloadURL)

	selection, err := json.Marshal(crossseed.ApplyTorrentSearchRequest{Selections: []crossseed.TorrentSearchSelection{
		{IndexerID: found.IndexerID, Indexer: "stub", Title: stubReleaseName, GUID: found.GUID},
	}})
	require.NoError(t, err)
	status, response := rig.post(t, "/api/cross-seed/torrents/1/aaa/apply", key, nil, string(selection))
	require.Equal(t, http.StatusOK, status, response)
	require.NotContains(t, response, "does not match cached search results")

	select {
	case passkey := <-rig.downloads:
		link, err := url.Parse(rig.feedLink)
		require.NoError(t, err)
		require.Equal(t, link.Query().Get("passkey"), passkey)
	default:
		t.Fatalf("apply downloaded nothing: %s", response)
	}
}
