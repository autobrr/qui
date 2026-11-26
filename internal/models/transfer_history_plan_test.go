// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestTheTorrentDaysOfAnInstanceReadInTheOrderOfTheKey(t *testing.T) {
	t.Parallel()
	db := testdb.NewMigratedSQLite(t, "torrent-days-plan")

	for _, total := range []bool{true, false} {
		query, args := models.TorrentTransferQuery("sqlite", 1, nil, 20_000, 20_030, total)
		rows, err := db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+query, args...)
		require.NoError(t, err)
		var plan strings.Builder
		compound, branches := -1, 0
		branchOf := make(map[int]int)
		for rows.Next() {
			var id, parent, notused int
			var detail string
			require.NoError(t, rows.Scan(&id, &parent, &notused, &detail))
			switch {
			case detail == "COMPOUND QUERY":
				compound = id
			case parent == compound:
				branches++
				branchOf[id] = branches
			default:
				branchOf[id] = branchOf[parent]
			}
			if branchOf[id] != 2 {
				plan.WriteString(detail + "\n")
			}
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())

		require.Contains(t, plan.String(), "SEARCH r USING PRIMARY KEY (torrent_id=? AND day>? AND day<?)", "total %v", total)
		require.Contains(t, plan.String(), "SEARCH f EXISTS", "total %v", total)
		for _, slow := range []string{"TEMP B-TREE", "LIST SUBQUERY", "ANY("} {
			require.NotContains(t, plan.String(), slow, "total %v", total)
		}
	}
}
