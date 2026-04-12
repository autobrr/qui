// Copyright (c) 2026, s0up and the autobrr contributors.
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

func TestCrossSeedRoutes_DoNotRegisterIndexerCategoriesEndpoints(t *testing.T) {
	t.Parallel()

	router := chi.NewRouter()
	handler := &CrossSeedHandler{}

	identity := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}

	handler.Routes(router, identity, identity)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/cross-seed/indexer-categories/1", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}
