// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"encoding/json/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const spansInstancesBody = "API key with an instance limit cannot use this route\n"

func isInstanceLimitRefusal(body string) bool {
	return body == spansInstancesBody || strings.HasPrefix(body, "API key cannot reach instance ")
}

func cannotReach(id int) string {
	return "API key cannot reach instance " + strconv.Itoa(id) + "\n"
}

type instanceLimitRig struct {
	*permissionRig
	instances *models.InstanceStore
}

func newInstanceLimitRig(t *testing.T) *instanceLimitRig {
	t.Helper()
	db := testdb.NewMigratedSQLite(t, "instance-limit")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	rig := newPermissionRig(t, func(deps *Dependencies) {
		deps.InstanceStore = instances
		deps.AutomationStore = models.NewAutomationStore(db)
	})
	return &instanceLimitRig{permissionRig: rig, instances: instances}
}

func (rig *instanceLimitRig) addInstance(t *testing.T, name string) int {
	t.Helper()
	inst, err := rig.instances.Create(t.Context(), name, "http://127.0.0.1:1", "admin", "secret", nil, nil, false, nil)
	require.NoError(t, err)
	return inst.ID
}

func (rig *instanceLimitRig) limitedKey(t *testing.T, instanceIDs ...int) string {
	t.Helper()
	raw, _, err := rig.deps.AuthService.CreateAPIKey(t.Context(), "limited", models.AllPermissions(), instanceIDs)
	require.NoError(t, err)
	return raw
}

func (rig *instanceLimitRig) listedInstances(t *testing.T, key string) []int {
	t.Helper()
	status, body := rig.send(t, http.MethodGet, "/api/instances", key, nil)
	require.Equal(t, http.StatusOK, status, body)
	var listed []struct {
		ID int `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &listed))
	ids := make([]int, 0, len(listed))
	for _, inst := range listed {
		ids = append(ids, inst.ID)
	}
	return ids
}

func TestInstanceLimit(t *testing.T) {
	rig := newInstanceLimitRig(t)
	first, second := rig.addInstance(t, "first"), rig.addInstance(t, "second")
	key := rig.limitedKey(t, first)

	t.Run("a route under an instance in the limit passes", func(t *testing.T) {
		status, body := rig.send(t, http.MethodGet, "/api/instances/"+strconv.Itoa(first)+"/torrents", key, nil)
		require.NotEqual(t, http.StatusForbidden, status, body)
	})

	for _, tc := range []struct{ name, method, path, body string }{
		{"a route under another instance", http.MethodGet, "/api/instances/%d/torrents", "{}"},
		{"content file download", http.MethodGet, "/api/instances/%d/torrents/abc/files/0/download", "{}"},
		{"RSS events", http.MethodGet, "/api/instances/%d/rss/events", "{}"},
		{"external program run", http.MethodPost, "/api/external-programs/execute", `{"program_id":1,"instance_id":%d,"hashes":["abc"]}`},
		{"torrent stream", http.MethodGet, "/api/stream?streams=" + url.QueryEscape(`[{"key":"a","instanceId":`) + "%d" + url.QueryEscape(`}]`), "{}"},
		{"stream subscription change", http.MethodPost, "/api/stream/subscriptions", `{"connectionId":"x","add":[{"key":"a","instanceIds":[1,%d]}]}`},
	} {
		t.Run(tc.name+" refuses another instance", func(t *testing.T) {
			path := strings.Replace(tc.path, "%d", strconv.Itoa(second), 1)
			reqBody := strings.Replace(tc.body, "%d", strconv.Itoa(second), 1)
			status, body := rig.sendBody(t, tc.method, path, key, nil, reqBody)
			require.Equal(t, http.StatusForbidden, status, body)
			require.Equal(t, cannotReach(second), body)
		})
	}

	t.Run("the all-instances scope is refused", func(t *testing.T) {
		status, body := rig.send(t, http.MethodGet, "/api/instances/0/torrents", key, nil)
		require.Equal(t, http.StatusForbidden, status, body)
		require.Equal(t, cannotReach(0), body)
	})

	t.Run("the instance list shows only the instances in the limit", func(t *testing.T) {
		require.Equal(t, []int{first}, rig.listedInstances(t, key))
	})

	t.Run("a new instance does not join the limit", func(t *testing.T) {
		added := rig.addInstance(t, "added")
		require.Equal(t, []int{first}, rig.listedInstances(t, key))
		status, body := rig.send(t, http.MethodGet, "/api/instances/"+strconv.Itoa(added)+"/torrents", key, nil)
		require.Equal(t, http.StatusForbidden, status, body)
		require.Equal(t, cannotReach(added), body)
	})

	t.Run("an automation cannot export to another instance", func(t *testing.T) {
		rules := "/api/instances/" + strconv.Itoa(first) + "/automations"
		exportTo := func(target int) string {
			return `{"name":"export","trackerPattern":"*","conditions":{"schemaVersion":"1","exportToInstance":{"enabled":true,"targetInstanceId":` + strconv.Itoa(target) + `,"savePath":"/data"}}}`
		}
		refused := `{"error":"` + strings.TrimSuffix(cannotReach(second), "\n") + `"}`

		status, body := rig.sendBody(t, http.MethodPost, rules, rig.key(t, models.AllPermissions()...), nil, exportTo(second))
		require.Equal(t, http.StatusCreated, status, "a key with no limit can export to any instance: %s", body)
		for _, path := range []string{rules, rules + "/dry-run"} {
			status, body = rig.sendBody(t, http.MethodPost, path, key, nil, exportTo(second))
			require.Equal(t, http.StatusForbidden, status, path)
			require.JSONEq(t, refused, body, path)
		}

		stored, err := rig.deps.AutomationStore.Create(t.Context(), &models.Automation{
			InstanceID: first, Name: "saved export", TrackerPattern: "*", Enabled: true,
			Conditions: &models.ActionConditions{SchemaVersion: "1", ExportToInstance: &models.ExportToInstanceAction{Enabled: true, TargetInstanceID: second, SavePath: "/data"}},
		})
		require.NoError(t, err)
		rule := rules + "/" + strconv.Itoa(stored.ID)
		for _, tc := range []struct{ method, path, body string }{
			{http.MethodPut, rule, exportTo(second)},
			{http.MethodPost, rule + "/apply", "{}"},
			{http.MethodPost, rules + "/apply", "{}"},
		} {
			status, body = rig.sendBody(t, tc.method, tc.path, key, nil, tc.body)
			require.Equal(t, http.StatusForbidden, status, tc.path)
			require.JSONEq(t, refused, body, tc.path)
		}

		saved, err := rig.deps.AutomationStore.ListByInstance(t.Context(), first)
		require.NoError(t, err)
		for _, rule := range saved {
			rule.Enabled = false
			_, err = rig.deps.AutomationStore.Update(t.Context(), rule)
			require.NoError(t, err)
		}
		status, body = rig.sendBody(t, http.MethodPost, rules+"/apply", key, nil, "{}")
		require.NotEqual(t, http.StatusForbidden, status, "a run of all rules skips a disabled rule: %s", body)
		status, body = rig.sendBody(t, http.MethodPost, rule+"/apply", key, nil, "{}")
		require.Equal(t, http.StatusForbidden, status, "a run of one rule also runs a disabled rule: %s", body)
	})

	t.Run("after its last instance is deleted the key reaches no instance", func(t *testing.T) {
		require.NoError(t, rig.instances.Delete(t.Context(), first))
		require.Empty(t, rig.listedInstances(t, key))
		status, body := rig.send(t, http.MethodGet, "/api/instances/"+strconv.Itoa(second)+"/torrents", key, nil)
		require.Equal(t, http.StatusForbidden, status, body)
		require.Equal(t, cannotReach(second), body)
	})
}

// ADR 0040.
func TestRoutesThatSpanInstancesRefuseInstanceLimit(t *testing.T) {
	rig := newInstanceLimitRig(t)
	// walkAPIRoutes puts 1 in each path parameter.
	require.Equal(t, 1, rig.addInstance(t, "first"))
	key := rig.limitedKey(t, 1)

	spansInstances := map[routeKey]struct{}{
		{http.MethodGet, "/api/instances/{instanceID}/cross-seed/status"}:            {},
		{http.MethodGet, "/api/torrents/cross-instance"}:                             {},
		{http.MethodPost, "/api/torrents/export"}:                                    {},
		{http.MethodPut, "/api/instances/order"}:                                     {},
		{http.MethodPost, "/api/instances/{instanceID}/torrents/delete-cross-seeds"}: {},
		{http.MethodGet, "/api/transfer-history/status"}:                             {},
		{http.MethodPut, "/api/transfer-history/settings"}:                           {},
		{http.MethodDelete, "/api/transfer-history"}:                                 {},
		{http.MethodGet, "/api/database/shrink"}:                                     {},
		{http.MethodDelete, "/api/database/shrink/result"}:                           {},
	}
	var refused int
	walkAPIRoutes(t, rig.permissionRig, func(route routeKey, path string) {
		_, spans := spansInstances[route]
		spans = spans || strings.HasPrefix(route.Path, "/api/cross-seed/") || strings.HasPrefix(route.Path, "/api/dir-scan/")
		status, body := rig.send(t, route.Method, path, key, nil)
		if spans {
			refused++
			require.Equal(t, http.StatusForbidden, status, "%s %s: %s", route.Method, route.Path, body)
			require.Equal(t, spansInstancesBody, body, "%s %s", route.Method, route.Path)
			return
		}
		require.False(t, isInstanceLimitRefusal(body), "%s %s: %d %q", route.Method, route.Path, status, body)
	})
	// The cross-seed and Dir Scan areas have about 60 routes.
	require.GreaterOrEqual(t, refused, 50)
}
