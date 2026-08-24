// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
)

// TestTestIndexerReportsSearchOutcome covers the asynchronous outcome of the
// connectivity search: SearchGeneric returns as soon as the search is
// scheduled, so a failure only shows up on the completion callback.
func TestTestIndexerReportsSearchOutcome(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		closed     bool
		wantCode   int
		wantStatus string
	}{
		{
			name: "search succeeds",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/rss+xml")
				if _, err := w.Write([]byte(`<rss version="2.0"><channel><title>Test</title></channel></rss>`)); err != nil {
					t.Errorf("write RSS response: %v", err)
				}
			},
			wantCode:   http.StatusOK,
			wantStatus: "ok",
		},
		{
			name: "indexer returns http 500",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "boom", http.StatusInternalServerError)
			},
			wantCode:   http.StatusInternalServerError,
			wantStatus: "error",
		},
		{
			name:       "indexer is unreachable",
			handler:    func(http.ResponseWriter, *http.Request) {},
			closed:     true,
			wantCode:   http.StatusInternalServerError,
			wantStatus: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			store := newTorznabStore(t)

			server := httptest.NewServer(tt.handler)
			defer server.Close()
			if tt.closed {
				// Closed listener: the search fails with connection refused.
				server.Close()
			}

			indexer, err := store.CreateWithIndexerID(ctx, "Synthetic Indexer", server.URL, "", "torznab-key", nil, nil, true, 0, 5, models.TorznabBackendNative)
			require.NoError(t, err)

			handler := &JackettHandler{
				service:      jackett.NewService(store),
				indexerStore: store,
			}

			target := "/api/torznab/indexers/" + strconv.Itoa(indexer.ID) + "/test"
			req := httptest.NewRequestWithContext(ctx, http.MethodPost, target, nil)
			routeCtx := chi.NewRouteContext()
			routeCtx.URLParams.Add("indexerID", strconv.Itoa(indexer.ID))
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))

			resp := httptest.NewRecorder()
			handler.TestIndexer(resp, req)

			require.Equal(t, tt.wantCode, resp.Code, "body: %s", resp.Body.String())

			stored, err := store.Get(ctx, indexer.ID)
			require.NoError(t, err)
			require.Equal(t, tt.wantStatus, stored.LastTestStatus)
			if tt.wantStatus == "ok" {
				require.Nil(t, stored.LastTestError)
			} else {
				require.NotNil(t, stored.LastTestError)
				require.NotEmpty(t, *stored.LastTestError)
			}
		})
	}
}

// The service reports timed-out, rate-limited and skipped indexers as a partial
// success: OnAllComplete gets a nil error with the indexer absent from the
// covered set (a deadline-exceeded search even arrives with a nil error). A nil
// error alone must therefore not count as a pass.
func TestIndexerTestOutcome(t *testing.T) {
	searchErr := errors.New("search failed")

	tests := []struct {
		name    string
		resp    *jackett.SearchResponse
		err     error
		wantErr string
	}{
		{name: "search error passes through", err: searchErr, wantErr: "search failed"},
		{name: "nil response is not a pass", wantErr: "did not complete"},
		{
			name:    "uncovered indexer is not a pass",
			resp:    &jackett.SearchResponse{RequestedIndexerIDs: []int{7}, CoveredIndexerIDs: []int{}},
			wantErr: "did not complete",
		},
		{
			name: "covered indexer passes",
			resp: &jackett.SearchResponse{RequestedIndexerIDs: []int{7}, CoveredIndexerIDs: []int{7}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := indexerTestOutcome(tt.resp, tt.err)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
