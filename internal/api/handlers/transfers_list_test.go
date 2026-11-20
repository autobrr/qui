// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// The list of the Transfers page answers one page of rows, with the counts
// of all the rows.
func TestTheTransferListAnswersOnePage(t *testing.T) {
	store, err := transfer.NewStore(t.Context(), testdb.NewMigratedSQLite(t, "transfer-list-page"))
	require.NoError(t, err)
	hashes := []string{"aaaa", "bbbb", "cccc"}
	ids := make([]int64, 0, len(hashes))
	for _, hash := range hashes {
		record := &transfer.Record{
			Kind: transfer.KindTransfer, State: transfer.StateWaiting, StartedBy: transfer.StartedByUser,
			SourceInstanceID: 1, TargetInstanceID: 2, Size: 4 << 30,
			Members: []transfer.Member{{Hash: hash, Name: "Name " + hash, SourceSavePath: "/src", TargetInstanceID: 2}},
		}
		require.NoError(t, store.Create(t.Context(), record))
		ids = append(ids, record.ID)
	}
	// A Service with no run: no row has progress.
	h := NewTransfersHandler(&transfer.Service{}, store, nil)

	list := func(query string) (int, transferListResponse) {
		t.Helper()
		rec := httptest.NewRecorder()
		h.List(rec, httptest.NewRequest(http.MethodGet, "/api/transfers"+query, nil))
		var res transferListResponse
		if rec.Code == http.StatusOK {
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		}
		return rec.Code, res
	}
	rowIDs := func(res transferListResponse) []int64 {
		out := make([]int64, 0, len(res.Rows))
		for _, row := range res.Rows {
			out = append(out, row.ID)
		}
		return out
	}

	code, res := list("?limit=2&offset=1")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, []int64{ids[1], ids[0]}, rowIDs(res), "the page after the newest row")
	require.Equal(t, 3, res.Counts.Waiting, "the counts hold the rows of every page")
	require.Equal(t, int64(4<<30), res.Rows[0].Size, "a row has the size of its torrent")

	code, res = list("")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, []int64{ids[2], ids[1], ids[0]}, rowIDs(res), "without a limit, the first page of the default size")

	for _, query := range []string{"?limit=0", "?limit=501", "?limit=many", "?offset=-1", "?offset=next"} {
		code, _ := list(query)
		require.Equal(t, http.StatusBadRequest, code, query)
	}
}
