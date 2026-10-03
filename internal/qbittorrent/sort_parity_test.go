// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"maps"
	"slices"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// TestUnifiedSortMatchesSingleInstance holds view parity: for each sort
// column, the Unified view gives each instance's rows in the order that
// instance's own view gives them. Both fake instances serve the same library
// with different tracker health. So outside the state sort, each row of one
// instance has a twin on the other that compares equal.
func TestUnifiedSortMatchesSingleInstance(t *testing.T) {
	t.Parallel()

	store, err := models.NewInstanceStore(testdb.NewMigratedSQLite(t, "qbittorrent-sort-parity"), make([]byte, 32))
	require.NoError(t, err)
	// Created out of name order, so the instance ID and the name disagree.
	clients := make(map[int]*Client, 2)
	idByName := make(map[string]int, 2)
	trackersByName := map[string]func(int) []qbt.TorrentTracker{
		"a": sortGoldenTrackers,
		"b": func(i int) []qbt.TorrentTracker { return sortGoldenTrackers((i + 6) % 24) },
	}
	for _, name := range []string{"b", "a"} {
		instance, err := store.Create(t.Context(), name, "http://127.0.0.1:8080", "", "", nil, nil, false, nil)
		require.NoError(t, err)
		clients[instance.ID] = newSortTestClient(t, instance.ID, trackersByName[name])
		idByName[name] = instance.ID
	}
	ids := slices.Sorted(maps.Keys(clients))
	sm := NewSyncManager(&ClientPool{instanceStore: store, clients: clients}, nil)

	for _, column := range append(sortGoldenColumns(), "instance") {
		for _, order := range []string{"asc", "desc"} {
			t.Run(column+" "+order, func(t *testing.T) {
				both, err := sm.GetCrossInstanceTorrentsWithFilters(t.Context(), 0, 0, column, order, "", FilterOptions{}, ids)
				require.NoError(t, err)

				// The single-instance view has no instance column. The rows of
				// each instance keep their name order in each direction.
				singleColumn, singleOrder := column, order
				if column == "instance" {
					singleColumn, singleOrder = "name", "asc"
				}
				for _, id := range ids {
					single, err := sm.GetTorrentsWithFilters(t.Context(), id, 0, 0, singleColumn, singleOrder, "", FilterOptions{})
					require.NoError(t, err)
					scoped, err := sm.GetCrossInstanceTorrentsWithFilters(t.Context(), 0, 0, column, order, "", FilterOptions{}, []int{id})
					require.NoError(t, err)

					var scopedRows, bothRows []TorrentView
					for _, row := range scoped.CrossInstanceTorrents {
						scopedRows = append(scopedRows, *row.TorrentView)
					}
					for _, row := range both.CrossInstanceTorrents {
						if row.InstanceID == id {
							bothRows = append(bothRows, *row.TorrentView)
						}
					}

					want := sortParityHashes(single.Torrents)
					require.Equal(t, want, sortParityHashes(scopedRows), "instance %d scoped alone", id)
					require.Equal(t, want, sortParityHashes(bothRows), "instance %d among all instances", id)
					require.Equal(t, single.Torrents, scopedRows)
					require.Equal(t, single.Total, scoped.Total)
					require.Equal(t, single.Stats, scoped.Stats)
					require.Equal(t, single.Counts, scoped.Counts)
					require.ElementsMatch(t, single.Tags, scoped.Tags)
				}

				switch column {
				case "state":
					return
				case "instance":
					first, last := idByName["a"], idByName["b"]
					if order == "desc" {
						first, last = last, first
					}
					half := len(both.CrossInstanceTorrents) / 2
					want := append(slices.Repeat([]int{first}, half), slices.Repeat([]int{last}, half)...)
					got := make([]int, 0, len(both.CrossInstanceTorrents))
					for _, row := range both.CrossInstanceTorrents {
						got = append(got, row.InstanceID)
					}
					require.Equal(t, want, got)
					return
				}
				// Twins compare equal, so the instance breaks the tie as the instance column sorts it.
				for i := 0; i < len(both.CrossInstanceTorrents); i += 2 {
					first, second := both.CrossInstanceTorrents[i], both.CrossInstanceTorrents[i+1]
					require.Equal(t, first.Hash, second.Hash, "row %d", i)
					require.Equal(t, []int{idByName["a"], idByName["b"]}, []int{first.InstanceID, second.InstanceID}, "row %d", i)
				}
			})
		}
	}
}

func sortParityHashes(rows []TorrentView) []string {
	hashes := make([]string, len(rows))
	for i, row := range rows {
		hashes[i] = row.Hash[:2]
	}
	return hashes
}
