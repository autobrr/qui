// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/systemnotice"
	"github.com/autobrr/qui/internal/services/transferhistory"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func named(t transferhistory.Torrent, name string) transferhistory.Torrent {
	t.Name = name
	return t
}

// torrents asks for the torrents of day 10 as totals, at most limit of them.
func torrents(limit int) transferhistory.TransferQuery {
	q := period(day10, day11, transferhistory.BucketTotal, transferhistory.GroupByTorrent)
	q.Limit = limit
	return q
}

func limited(q transferhistory.TransferQuery, limit int) transferhistory.TransferQuery {
	q.Limit = limit
	return q
}

func within(q transferhistory.TransferQuery, groupOf transferhistory.Grouping, group string) transferhistory.TransferQuery {
	q.GroupOf = groupOf
	return withGroup(q, group)
}

// facts are the facts of a torrent row of a private torrent on tracker.example.
func facts(name string, first24h *int64) *transferhistory.TorrentFacts {
	return &transferhistory.TorrentFacts{Name: name, TrackerGroup: "tracker.example", UploadFirst24h: first24h}
}

func torrentRow(hash string, start time.Time, width transferhistory.Bucket, uploaded, downloaded, gapSeconds int64, f *transferhistory.TorrentFacts) transferhistory.TransferRow {
	r := row(hash, start, width, uploaded, downloaded, 0, gapSeconds)
	if f.StatisticsCrossSeed {
		r.StatisticsCrossSeedUploaded = uploaded
	}
	r.Torrent = f
	return r
}

func TestTheGroupingByTorrentPostgresIntegration(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		steps []step
	}{
		{
			name: "a limit keeps the torrents with the most upload, in the order of their upload",
			steps: []step{
				reading(at(0), named(torrent("a", 0, 0), "Alpha"), named(torrent("b", 0, 0), "Bravo"), named(torrent("c", 0, 0), "Charlie")),
				reading(at(5*time.Minute), named(torrent("a", 300, 10), "Alpha"), named(torrent("b", 500, 20), "Bravo"), named(torrent("c", 100, 30), "Charlie")),
				expect(torrents(2),
					torrentRow("b", day10, transferhistory.BucketTotal, 500, 20, 0, facts("Bravo", new(int64(500)))),
					torrentRow("a", day10, transferhistory.BucketTotal, 300, 10, 0, facts("Alpha", new(int64(300)))),
				),
				expect(torrents(0),
					torrentRow("a", day10, transferhistory.BucketTotal, 300, 10, 0, facts("Alpha", new(int64(300)))),
					torrentRow("b", day10, transferhistory.BucketTotal, 500, 20, 0, facts("Bravo", new(int64(500)))),
					torrentRow("c", day10, transferhistory.BucketTotal, 100, 30, 0, facts("Charlie", new(int64(100)))),
				),
			},
		},
		{
			name: "one group limits the torrents to those of a Tracker group or a category",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), onTracker(torrent("b", 0, 0), "other.example"), inCategory(torrent("c", 0, 0), "movies")),
				reading(at(5*time.Minute), torrent("a", 300, 0), onTracker(torrent("b", 500, 0), "other.example"), inCategory(torrent("c", 100, 0), "movies")),
				expect(within(torrents(1), transferhistory.GroupByTracker, "tracker.example"),
					torrentRow("a", day10, transferhistory.BucketTotal, 300, 0, 0, facts("Some.Show.S01E01.1080p.WEB-DL-GRP", new(int64(300)))),
				),
				expect(within(torrents(0), transferhistory.GroupByCategory, "movies"),
					torrentRow("c", day10, transferhistory.BucketTotal, 100, 0, 0, facts("Some.Show.S01E01.1080p.WEB-DL-GRP", new(int64(100)))),
				),
				expect(within(torrents(0), transferhistory.GroupByTracker, "missing.example")),
			},
		},
		{
			name: "a torrent that qui saw deleted keeps its name and gets the deleted mark",
			steps: []step{
				reading(at(0), named(torrent("a", 0, 0), "Alpha"), torrent("b", 0, 0)),
				reading(at(5*time.Minute), named(torrent("a", 300, 0), "Alpha"), torrent("b", 100, 0)),
				reading(at(10*time.Minute), torrent("b", 100, 0)),
				expect(torrents(1),
					torrentRow("a", day10, transferhistory.BucketTotal, 300, 0, 0, &transferhistory.TorrentFacts{
						Name: "Alpha", TrackerGroup: "tracker.example", Deleted: true, UploadFirst24h: new(int64(300)),
					}),
				),
			},
		},
		{
			name: "a torrent row holds the Statistics cross-seed flag and the upload of the first 24 hours",
			steps: []step{
				reading(at(0), crossSeed(0), addedAt(torrent("old", 0, 0), at(-48*time.Hour))),
				reading(at(5*time.Minute), crossSeed(60), addedAt(torrent("old", 40, 0), at(-48*time.Hour))),
				expect(torrents(0),
					torrentRow("c", day10, transferhistory.BucketTotal, 60, 0, 0, &transferhistory.TorrentFacts{
						Name: "Some.Show.S01E01.1080p.WEB-DL-GRP", TrackerGroup: "tracker.example", StatisticsCrossSeed: true, UploadFirst24h: new(int64(60)),
					}),
					torrentRow("old", day10, transferhistory.BucketTotal, 40, 0, 0, facts("Some.Show.S01E01.1080p.WEB-DL-GRP", nil)),
				),
				expect(torrents(1),
					torrentRow("c", day10, transferhistory.BucketTotal, 60, 0, 0, &transferhistory.TorrentFacts{
						Name: "Some.Show.S01E01.1080p.WEB-DL-GRP", TrackerGroup: "tracker.example", StatisticsCrossSeed: true, UploadFirst24h: new(int64(60)),
					}),
				),
			},
		},
		{
			name: "a limit picks among torrents with the same upload by hash",
			steps: []step{
				reading(at(0), torrent("b", 0, 0), torrent("a", 0, 0), torrent("c", 0, 0)),
				reading(at(5*time.Minute), torrent("b", 300, 0), torrent("a", 300, 0), torrent("c", 100, 0)),
				expect(torrents(1),
					torrentRow("a", day10, transferhistory.BucketTotal, 300, 0, 0, facts("Some.Show.S01E01.1080p.WEB-DL-GRP", new(int64(300)))),
				),
				expect(torrents(2),
					torrentRow("a", day10, transferhistory.BucketTotal, 300, 0, 0, facts("Some.Show.S01E01.1080p.WEB-DL-GRP", new(int64(300)))),
					torrentRow("b", day10, transferhistory.BucketTotal, 300, 0, 0, facts("Some.Show.S01E01.1080p.WEB-DL-GRP", new(int64(300)))),
				),
			},
		},
		{
			name: "a period with no start ranks the torrents by their upload with the Baseline",
			steps: []step{
				reading(at(0), torrent("a", 1000, 0)),
				reading(at(5*time.Minute), torrent("a", 1000, 0), torrent("b", 500, 0)),
				expect(transferhistory.TransferQuery{To: day11, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTorrent, Limit: 1},
					transferhistory.TransferRow{
						Group: "a", Width: transferhistory.BucketTotal,
						Baseline: &transferhistory.Bytes{Uploaded: 1000},
						Torrent:  facts("Some.Show.S01E01.1080p.WEB-DL-GRP", new(int64(1000))),
					}),
			},
		},
		{
			name: "the days of one torrent are UTC days, also for a query for hours",
			steps: concat(twoDaysOfUpload, []step{
				expect(withGroup(period(day10, day12, transferhistory.BucketDay, transferhistory.GroupByTorrent), "a"),
					torrentRow("a", day10, transferhistory.BucketDay, 300, 50, 5400, facts("Some.Show.S01E01.1080p.WEB-DL-GRP", new(int64(300)))),
					torrentRow("a", day11, transferhistory.BucketDay, 100, 0, 0, facts("Some.Show.S01E01.1080p.WEB-DL-GRP", new(int64(300)))),
				),
				expect(withGroup(period(day10, day12, transferhistory.BucketHour, transferhistory.GroupByTorrent), "a"),
					torrentRow("a", day10, transferhistory.BucketDay, 300, 50, 5400, facts("Some.Show.S01E01.1080p.WEB-DL-GRP", new(int64(300)))),
					torrentRow("a", day11, transferhistory.BucketDay, 100, 0, 0, facts("Some.Show.S01E01.1080p.WEB-DL-GRP", new(int64(300)))),
				),
			}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
				runSteps(t, newDB(t), tc.steps)
			})
		})
	}
}

func TestATorrentQueryRefusesTheGroupOfAnotherGroupingOutsideTheGroupingByTorrent(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "transfer-history")
	module := transferhistory.New(db, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
	t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

	for name, q := range map[string]transferhistory.TransferQuery{
		"another grouping":     within(period(day10, day11, transferhistory.BucketTotal, transferhistory.GroupByTracker), transferhistory.GroupByCategory, "tv"),
		"a grouping, no group": {From: day10, To: day11, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTorrent, GroupOf: transferhistory.GroupByTracker},
		"a negative limit":     {From: day10, To: day11, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTorrent, Limit: -1},
	} {
		_, err := module.Transfer(t.Context(), q)
		require.ErrorIs(t, err, transferhistory.ErrInvalidQuery, name)
	}
}
