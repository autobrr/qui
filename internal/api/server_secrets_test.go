// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			if secret.audience != shownToNobody {
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
		if secret.audience != shownToNobody {
			require.True(t, shownToSession[secret.name], "no route gives the %s to a session, so the walk proves nothing about it", secret.name)
		}
	}
}
