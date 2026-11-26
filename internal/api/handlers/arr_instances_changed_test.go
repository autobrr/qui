// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestArrHandlerTellsWhenTheInstancesChange(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "arr-instances-changed")
	store, err := models.NewArrInstanceStore(db, []byte(strings.Repeat("k", 32)))
	require.NoError(t, err)
	service := arr.NewService(store, models.NewArrIDCacheStore(db))
	h := NewArrHandler(store, service)
	existing, err := store.Create(t.Context(), models.ArrInstanceTypeSonarr, "sonarr", "http://sonarr.test", "api-key", nil, nil, true, 1, 15)
	require.NoError(t, err)
	id := strconv.Itoa(existing.ID)

	for _, c := range []struct {
		name    string
		handle  http.HandlerFunc
		body    string
		status  int
		changed bool
	}{
		{"create", h.CreateInstance, `{"type":"radarr","name":"radarr","base_url":"http://radarr.test","api_key":"api-key","enabled":true}`, http.StatusCreated, true},
		{"create rejected", h.CreateInstance, `{"type":"radarr","name":"radarr","base_url":"http://radarr.test"}`, http.StatusBadRequest, false},
		{"update", h.UpdateInstance, `{"enabled":false}`, http.StatusOK, true},
		{"delete", h.DeleteInstance, ``, http.StatusNoContent, true},
		{"delete missing", h.DeleteInstance, ``, http.StatusNotFound, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			changed := service.InstancesChanged()
			routes := chi.NewRouteContext()
			routes.URLParams.Add("id", id)
			r := httptest.NewRequestWithContext(context.WithValue(t.Context(), chi.RouteCtxKey, routes), http.MethodPost, "/", strings.NewReader(c.body))
			w := httptest.NewRecorder()

			c.handle(w, r)

			require.Equal(t, c.status, w.Code, w.Body.String())
			select {
			case <-changed:
				require.True(t, c.changed, "a request that changed nothing")
			default:
				require.False(t, c.changed, "a change that the queue does not hear")
			}
		})
	}
}
