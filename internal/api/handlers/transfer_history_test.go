// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/systemnotice"
	"github.com/autobrr/qui/internal/services/transferhistory"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestTransferRejectsAnUnknownTimeZone(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/instances/1/transfer-history?bucket=day&groupBy=tracker&timeZone=Mars/Olympus_Mons", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("instanceID", "1")
	rec := httptest.NewRecorder()

	NewTransferHistoryHandler(nil).Transfer(rec, req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx)))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "timeZone")
}

// A request that does not name the instances, or names a future time, must
// not reach the module: without the check, it deletes everything.
func TestDeleteHistoryRefusesAnUnclearRequest(t *testing.T) {
	for _, query := range []string{"", "?instanceId=", "?instanceId=0", "?instanceId=all&before=2999-01-01T00:00:00Z", "?instanceId=1&before=yesterday", "?instanceId=all&before=0001-01-01T00:00:00Z"} {
		t.Run(query, func(t *testing.T) {
			rec := httptest.NewRecorder()
			NewTransferHistoryHandler(nil).DeleteHistory(rec, httptest.NewRequest(http.MethodDelete, "/api/transfer-history"+query, nil))
			require.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

func TestDeleteHistoryAnswersNoContent(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "transfer-history-delete")
	module := transferhistory.New(db, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
	t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

	for _, query := range []string{"?instanceId=1", "?instanceId=all&before=2026-01-02T00:00:00%2B01:00"} {
		t.Run(query, func(t *testing.T) {
			rec := httptest.NewRecorder()
			NewTransferHistoryHandler(module).DeleteHistory(rec, httptest.NewRequest(http.MethodDelete, "/api/transfer-history"+query, nil))
			require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
		})
	}
}
