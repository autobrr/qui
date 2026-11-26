// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/testutil/qbtstub"
)

const hashRouteTorrentHash = "abcdef0123456789abcdef0123456789abcdef01"

func (r *seasonPackEndpointRig) send(method, path string, body []byte) *httptest.ResponseRecorder {
	r.t.Helper()
	req := httptest.NewRequestWithContext(r.t.Context(), method, path, bytes.NewReader(body))
	resp := httptest.NewRecorder()
	r.router.ServeHTTP(resp, req)
	return resp
}

func newHashRouteRig(t *testing.T) *seasonPackEndpointRig {
	t.Helper()
	return newSeasonPackEndpointRig(t, &seasonPackEndpointCase{setup: func(r *seasonPackEndpointRig) {
		r.servers[1].Add(qbtstub.Torrent{
			Hash: hashRouteTorrentHash, Name: endpointEpisode(1), SavePath: filepath.Join(r.root, "lib"),
			Layout: qbtstub.RootFolder, Files: []string{endpointEpisode(1) + ".mkv"},
		})
	}}, heldTitlesOff)
}

func TestCrossSeedHashRoutesFindATorrentByAnUpperCaseHash(t *testing.T) {
	rig := newHashRouteRig(t)
	apply := []byte(`{"selections":[{"indexer_id":1,"indexer":"a","download_url":"http://127.0.0.1/a.torrent","title":"a"}]}`)
	for _, tc := range []struct {
		method, route string
		body          []byte
		found         func(*testing.T, *httptest.ResponseRecorder)
	}{
		{method: http.MethodGet, route: "local-matches", found: func(t *testing.T, resp *httptest.ResponseRecorder) {
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		}},
		{method: http.MethodGet, route: "analyze", found: func(t *testing.T, resp *httptest.ResponseRecorder) {
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			require.Contains(t, resp.Body.String(), endpointEpisode(1))
		}},
		{method: http.MethodPost, route: "search", body: []byte(`{}`), found: func(t *testing.T, resp *httptest.ResponseRecorder) {
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			require.Contains(t, resp.Body.String(), endpointEpisode(1))
		}},
		{method: http.MethodPost, route: "apply", body: apply, found: func(t *testing.T, resp *httptest.ResponseRecorder) {
			require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
			require.Contains(t, resp.Body.String(), "no cached cross-seed search results")
		}},
	} {
		t.Run(tc.route, func(t *testing.T) {
			lower := rig.send(tc.method, "/api/cross-seed/torrents/1/"+hashRouteTorrentHash+"/"+tc.route, tc.body)
			upper := rig.send(tc.method, "/api/cross-seed/torrents/1/"+strings.ToUpper(hashRouteTorrentHash)+"/"+tc.route, tc.body)
			tc.found(t, upper)
			require.Equal(t, lower.Code, upper.Code)
			require.Equal(t, lower.Body.String(), upper.Body.String())
		})
	}
}

func TestCrossSeedHashRoutesRejectAMissingOrMalformedHash(t *testing.T) {
	rig := newHashRouteRig(t)
	for _, tc := range []struct {
		name, method, path, body, message string
	}{
		{name: "analyze without hash", method: http.MethodGet, path: "/api/cross-seed/torrents/1/%20/analyze", message: "hash is required"},
		{name: "analyze with a malformed hash", method: http.MethodGet, path: "/api/cross-seed/torrents/1/xyz/analyze", message: "hash must be a valid hex infohash"},
		{name: "local matches with a malformed hash", method: http.MethodGet, path: "/api/cross-seed/torrents/1/xyz/local-matches", message: "hash must be a valid hex infohash"},
		{name: "search with a malformed hash", method: http.MethodPost, path: "/api/cross-seed/torrents/1/xyz/search", body: `{}`, message: "hash must be a valid hex infohash"},
		{name: "apply with a malformed hash", method: http.MethodPost, path: "/api/cross-seed/torrents/1/xyz/apply", body: `{}`, message: "hash must be a valid hex infohash"},
		{name: "blocklist add with a malformed hash", method: http.MethodPost, path: "/api/cross-seed/blocklist/", body: `{"instanceId":1,"infoHash":"xyz"}`, message: "infoHash must be a valid hex infohash"},
		{name: "blocklist delete with a malformed hash", method: http.MethodDelete, path: "/api/cross-seed/blocklist/1/xyz", message: "infohash must be a valid hex infohash"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := rig.send(tc.method, tc.path, []byte(tc.body))
			require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
			require.Contains(t, resp.Body.String(), tc.message)
		})
	}
}

func TestBlocklistEntryBlocksAHashInEitherCase(t *testing.T) {
	pack := endpointPackTorrent(t, 1, 4)
	hash := endpointHash(t, pack)
	require.NotEqual(t, hash, strings.ToUpper(hash))
	rig := newSeasonPackEndpointRig(t, &seasonPackEndpointCase{held: endpointEpisodes(4), total: 4, setup: func(r *seasonPackEndpointRig) {
		r.holdPack(false)
	}}, heldTitlesReady)
	added := rig.send(http.MethodPost, "/api/cross-seed/blocklist/", mustJSON(t, CrossSeedBlocklistRequest{InstanceID: 1, InfoHash: strings.ToUpper(hash)}))
	require.Equal(t, http.StatusCreated, added.Code, added.Body.String())

	blocked := rig.webhookApply("", pack)
	require.Len(t, blocked.Results, 1)
	require.Equal(t, "blocked", blocked.Results[0].Status)

	deleted := rig.send(http.MethodDelete, "/api/cross-seed/blocklist/1/"+hash, nil)
	require.Equal(t, http.StatusNoContent, deleted.Code, deleted.Body.String())

	unblocked := rig.webhookApply("", pack)
	require.Len(t, unblocked.Results, 1)
	require.NotEqual(t, "blocked", unblocked.Results[0].Status)
}
