// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"encoding/json/v2"
	"maps"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

const unauthenticatedBody = "Unauthorized\n"

type templateCall struct {
	Method, Path string
	KeyInQuery   bool
}

type unneededCall struct {
	Method, Path, Reason string
}

type templateRoutes struct {
	Calls        []templateCall
	AllInstances []templateCall
	Unneeded     []unneededCall
}

var quiExtensionCalls = []templateCall{
	{http.MethodGet, "/api/instances", false},
	{http.MethodGet, "/api/instances/{instanceID}/categories", false},
	{http.MethodGet, "/api/instances/{instanceID}/capabilities", false},
	{http.MethodGet, "/api/instances/{instanceID}/getDirectoryContent", false},
	{http.MethodPost, "/api/instances/{instanceID}/torrents", false},
}

var routesByTemplate = map[string]templateRoutes{
	"autobrr": {Calls: []templateCall{
		{http.MethodPost, "/api/cross-seed/webhook/check", false},
		{http.MethodPost, "/api/cross-seed/apply", true},
		{http.MethodPost, "/api/cross-seed/season-pack/check", false},
		{http.MethodPost, "/api/cross-seed/season-pack/apply", true},
	}},
	"dashbrr": {
		Calls: []templateCall{
			{http.MethodGet, "/api/instances", false},
			{http.MethodGet, "/api/instances/{instanceID}/transfer-info", false},
		},
		Unneeded: []unneededCall{
			{http.MethodGet, "/api/instances/{instanceID}/torrents", "dashbrr reads the all-time totals here only when transfer-info has none, and qui then has no synced state for the torrent list either"},
			{http.MethodGet, "/api/cross-seed/status", "dashbrr has a client method for this route but no caller"},
		},
	},
	"homepage": {
		Calls:        []templateCall{{http.MethodGet, "/api/instances/{instanceID}/torrents", false}},
		AllInstances: []templateCall{{http.MethodGet, "/api/torrents/cross-instance", false}},
	},
	"arrDirScan": {Calls: []templateCall{
		{http.MethodPost, "/api/dir-scan/webhook/scan", true},
	}},
	"quiExtension": {Calls: quiExtensionCalls},
	"quiExtensionCrossSeed": {Calls: slices.Concat(quiExtensionCalls, []templateCall{
		{http.MethodGet, "/api/instances/{instanceID}/torrents", false},
		{http.MethodPost, "/api/cross-seed/manual/proposals", false},
		{http.MethodPost, "/api/cross-seed/manual/apply", false},
		{http.MethodPost, "/api/cross-seed/manual/assemble/check", false},
		{http.MethodPost, "/api/cross-seed/manual/assemble", false},
	})},
}

type templateRig struct {
	*instanceLimitRig
	instanceID int
}

func (rig *templateRig) keyWith(t *testing.T, perms []models.Permission, instanceIDs []int) string {
	t.Helper()
	raw, _, err := rig.deps.AuthService.CreateAPIKey(t.Context(), "template", slices.Clone(perms), instanceIDs)
	require.NoError(t, err)
	return raw
}

func (rig *templateRig) call(t *testing.T, method, pattern string, keyInQuery bool, key string) (int, string) {
	t.Helper()
	path := routeParam.ReplaceAllString(pattern, strconv.Itoa(rig.instanceID))
	if keyInQuery {
		return rig.send(t, method, path+"?apikey="+url.QueryEscape(key), "", nil)
	}
	return rig.send(t, method, path, key, nil)
}

func (rig *templateRig) requireReachesEveryCall(t *testing.T, calls []templateCall, key string) {
	t.Helper()
	for _, c := range calls {
		status, body := rig.call(t, c.Method, c.Path, c.KeyInQuery, key)
		require.NotEqual(t, http.StatusUnauthorized, status, "%s %s: %q", c.Method, c.Path, body)
		require.NotEqual(t, unauthenticatedBody, body, "%s %s", c.Method, c.Path)
		require.NotEqual(t, browserOnlyBody, body, "%s %s", c.Method, c.Path)
		require.NotEqual(t, handlerPanicBody, body, "%s %s", c.Method, c.Path)
		require.False(t, strings.HasPrefix(body, needsPermissionPrefix), "%s %s: %q", c.Method, c.Path, body)
		require.False(t, isInstanceLimitRefusal(body), "%s %s: %q", c.Method, c.Path, body)
	}
}

func (rig *templateRig) refusals(t *testing.T, calls []templateCall, key string) []string {
	t.Helper()
	var bodies []string
	for _, c := range calls {
		status, body := rig.call(t, c.Method, c.Path, c.KeyInQuery, key)
		if status == http.StatusForbidden {
			bodies = append(bodies, body)
		}
	}
	return bodies
}

func needsAreaPrefix(area models.PermissionArea) string {
	return needsPermissionPrefix + models.Permission{Area: area}.String()
}

func TestAPIKeyTemplates(t *testing.T) {
	limitRig := newInstanceLimitRig(t)
	rig := &templateRig{instanceLimitRig: limitRig, instanceID: limitRig.addInstance(t, "first")}

	router, err := NewServer(rig.deps).Handler()
	require.NoError(t, err)
	registered := make(map[routeKey]bool)
	for _, route := range apiRoutes(t, router) {
		registered[routeKey{route.Method, strings.TrimSuffix(route.Path, "/")}] = true
	}

	ids := make([]string, 0, len(models.PermissionTemplates))
	for _, template := range models.PermissionTemplates {
		ids = append(ids, template.ID)
	}
	require.ElementsMatch(t, ids, slices.Collect(maps.Keys(routesByTemplate)), "each template needs the list of routes its tool calls")

	for _, template := range models.PermissionTemplates {
		routes := routesByTemplate[template.ID]
		t.Run(template.ID, func(t *testing.T) {
			require.NotEmpty(t, routes.Calls)
			require.NoError(t, models.ValidatePermissions(template.Permissions))
			every := slices.Concat(routes.Calls, routes.AllInstances)
			for _, c := range every {
				require.True(t, registered[routeKey{c.Method, c.Path}], "the tool calls %s %s, which qui does not have", c.Method, c.Path)
			}
			for _, u := range routes.Unneeded {
				require.True(t, registered[routeKey{u.Method, u.Path}], "qui does not have the unneeded route %s %s: %s", u.Method, u.Path, u.Reason)
			}

			t.Run("its key reaches each call", func(t *testing.T) {
				rig.requireReachesEveryCall(t, every, rig.keyWith(t, template.Permissions, nil))
			})

			t.Run("each permission is needed", func(t *testing.T) {
				for i, p := range template.Permissions {
					without := slices.Delete(slices.Clone(template.Permissions), i, i+1)
					bodies := rig.refusals(t, every, rig.keyWith(t, without, nil))
					require.True(t, slices.ContainsFunc(bodies, func(body string) bool {
						return strings.HasPrefix(body, needsAreaPrefix(p.Area))
					}), "no call needs %s: %q", p, bodies)
				}
			})

			t.Run("a lower level is not enough", func(t *testing.T) {
				for i, p := range template.Permissions {
					if p.Level != models.LevelWrite {
						continue
					}
					info := models.PermissionAreas[slices.IndexFunc(models.PermissionAreas, func(a models.PermissionAreaInfo) bool { return a.Area == p.Area })]
					for _, lower := range info.Levels {
						if lower == models.LevelWrite {
							continue
						}
						weaker := slices.Clone(template.Permissions)
						weaker[i] = models.Permission{Area: p.Area, Level: lower}
						bodies := rig.refusals(t, every, rig.keyWith(t, weaker, nil))
						require.True(t, slices.ContainsFunc(bodies, func(body string) bool {
							return strings.HasPrefix(body, needsAreaPrefix(p.Area))
						}), "%s is enough in place of %s: %q", weaker[i], p, bodies)
					}
				}
			})

			t.Run("instance limit", func(t *testing.T) {
				limited := rig.keyWith(t, template.Permissions, []int{rig.instanceID})
				if template.AllowsInstanceLimit {
					rig.requireReachesEveryCall(t, routes.Calls, limited)
					for _, c := range routes.AllInstances {
						_, body := rig.call(t, c.Method, c.Path, c.KeyInQuery, limited)
						require.Equal(t, spansInstancesBody, body, "%s %s reaches a limited key, so it belongs in Calls", c.Method, c.Path)
					}
					return
				}
				require.Empty(t, routes.AllInstances, "the template refuses an instance limit, so each call goes in Calls")
				require.Contains(t, rig.refusals(t, routes.Calls, limited), spansInstancesBody, "the template refuses an instance limit that no call refuses")
			})

			t.Run("its key gets no unneeded route", func(t *testing.T) {
				key := rig.keyWith(t, template.Permissions, nil)
				for _, u := range routes.Unneeded {
					status, body := rig.call(t, u.Method, u.Path, false, key)
					require.Equal(t, http.StatusForbidden, status, "%s %s: %s", u.Method, u.Path, u.Reason)
					require.True(t, strings.HasPrefix(body, needsPermissionPrefix), "%s %s: %q", u.Method, u.Path, body)
				}
			})
		})
	}
}

type templateSources struct {
	QuiRoutesOnly []string `json:"quiRoutesOnly"`
	Tools         []struct {
		Templates []string          `json:"templates"`
		Files     map[string]string `json:"files"`
	} `json:"tools"`
}

func TestAPIKeyTemplateSourcesNameEachTemplateOnce(t *testing.T) {
	raw, err := os.ReadFile("testdata/api-key-template-sources.json")
	require.NoError(t, err)
	var sources templateSources
	require.NoError(t, json.Unmarshal(raw, &sources))

	named := slices.Clone(sources.QuiRoutesOnly)
	for _, tool := range sources.Tools {
		require.NotEmpty(t, tool.Files)
		named = append(named, tool.Templates...)
	}
	ids := make([]string, 0, len(models.PermissionTemplates))
	for _, template := range models.PermissionTemplates {
		ids = append(ids, template.ID)
	}
	require.ElementsMatch(t, ids, named)
}
