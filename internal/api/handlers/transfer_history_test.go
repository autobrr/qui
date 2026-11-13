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
