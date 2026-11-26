// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory_test

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/systemnotice"
	"github.com/autobrr/qui/internal/services/transferhistory"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const (
	filmName  = "Another.Film.2024.2160p.BluRay.x265-OTHER"
	plainName = "holiday photos"
)

func releaseTorrents(show, film, plain, crossSeedUploaded int64) []transferhistory.Torrent {
	return []transferhistory.Torrent{
		torrent("a", show, min(show, 50)),
		named(torrent("b", film, 0), filmName),
		public(named(torrent("p", plain, plain/30), plainName)),
		crossSeed(crossSeedUploaded),
		named(torrent("d", 1000, 0), filmName),
	}
}

var releaseUpload = []step{
	reading(at(0), releaseTorrents(0, 0, 0, 0)...),
	reading(at(5*time.Minute), releaseTorrents(100, 200, 300, 30)...),
	reading(at(25*time.Hour), releaseTorrents(150, 200, 300, 30)...),
}

func TestReleaseAttributeGroupingsPostgresIntegration(t *testing.T) {
	t.Parallel()

	total := func(groupBy transferhistory.Grouping) transferhistory.TransferQuery {
		return period(day10, day12, transferhistory.BucketTotal, groupBy)
	}
	totals := func(groupBy transferhistory.Grouping, show, film string) step {
		rows := []transferhistory.TransferRow{
			row("", day10, transferhistory.BucketTotal, 300, 10, 0, 0),
			row(show, day10, transferhistory.BucketTotal, 180, 50, 30, 0),
			row(film, day10, transferhistory.BucketTotal, 200, 0, 0, 0),
		}
		slices.SortFunc(rows, func(x, y transferhistory.TransferRow) int { return strings.Compare(x.Group, y.Group) })
		return expect(total(groupBy), rows...)
	}
	steps := concat(releaseUpload, []step{
		expect(total(transferhistory.GroupByTracker),
			row("Public", day10, transferhistory.BucketTotal, 300, 10, 0, 0),
			row("tracker.example", day10, transferhistory.BucketTotal, 380, 50, 30, 0),
		),
		totals(transferhistory.GroupByReleaseGroup, "GRP", "OTHER"),
		totals(transferhistory.GroupByResolution, "1080p", "2160p"),
		totals(transferhistory.GroupBySource, "WEBDL", "BLURAY"),
		totals(transferhistory.GroupByContentType, "tv", "movie"),
		expect(period(day10, day12, transferhistory.BucketHour, transferhistory.GroupByReleaseGroup),
			row("", day10, transferhistory.BucketDay, 300, 10, 0, 0),
			row("GRP", day10, transferhistory.BucketDay, 130, 50, 30, 0),
			row("OTHER", day10, transferhistory.BucketDay, 200, 0, 0, 0),
			row("GRP", day11, transferhistory.BucketDay, 50, 0, 0, 0),
		),
		expect(withLimit(total(transferhistory.GroupByReleaseGroup), 2),
			row("", day10, transferhistory.BucketTotal, 300, 10, 0, 0),
			row("OTHER", day10, transferhistory.BucketTotal, 200, 0, 0, 0),
		),
		expect(withGroup(total(transferhistory.GroupByResolution), ""),
			row("", day10, transferhistory.BucketTotal, 300, 10, 0, 0),
		),
		expect(withGroup(period(day10, day12, transferhistory.BucketDay, transferhistory.GroupBySource), "WEBDL"),
			row("WEBDL", day10, transferhistory.BucketDay, 130, 50, 30, 0),
			row("WEBDL", day11, transferhistory.BucketDay, 50, 0, 0, 0),
		),
		expect(transferhistory.TransferQuery{To: day12, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByReleaseGroup},
			row("", time.Time{}, transferhistory.BucketTotal, 300, 10, 0, 0),
			row("GRP", time.Time{}, transferhistory.BucketTotal, 180, 50, 30, 0),
			withBaseline(row("OTHER", time.Time{}, transferhistory.BucketTotal, 200, 0, 0, 0), transferhistory.Bytes{Uploaded: 1000}),
		),
	})
	engines(t, func(t *testing.T, db func(t *testing.T) *database.DB) {
		runSteps(t, db(t), steps)
	})
}

func withLimit(q transferhistory.TransferQuery, limit int) transferhistory.TransferQuery {
	q.Limit = limit
	return q
}

func withBaseline(r transferhistory.TransferRow, baseline transferhistory.Bytes) transferhistory.TransferRow {
	r.Baseline = &baseline
	return r
}

func TestAReleaseAttributeGroupsTheYearsOfAFoldPostgresIntegration(t *testing.T) {
	t.Parallel()
	year := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	steps := concat(releaseUpload, []step{
		settings(true, 90),
		reading(day10.Add(91*24*time.Hour+10*time.Hour), releaseTorrents(150, 200, 300, 30)...),
		{foldsDone: true},
		expect(period(day10, day12, transferhistory.BucketDay, transferhistory.GroupByReleaseGroup),
			row("", year, transferhistory.BucketYear, 300, 10, 0, 0),
			row("GRP", year, transferhistory.BucketYear, 130, 50, 30, 0),
			row("OTHER", year, transferhistory.BucketYear, 200, 0, 0, 0),
			row("GRP", day11, transferhistory.BucketDay, 50, 0, 0, 0),
		),
		expect(period(day10, day12, transferhistory.BucketTotal, transferhistory.GroupByReleaseGroup),
			row("", year, transferhistory.BucketTotal, 300, 10, 0, 0),
			row("GRP", year, transferhistory.BucketTotal, 180, 50, 30, 0),
			row("OTHER", year, transferhistory.BucketTotal, 200, 0, 0, 0),
		),
	})
	engines(t, func(t *testing.T, db func(t *testing.T) *database.DB) {
		runSteps(t, db(t), steps)
	})
}

type torrentReads struct {
	*database.DB
	reads atomic.Int64
}

func (r *torrentReads) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "transfer_history_torrent_days") {
		r.reads.Add(1)
	}
	return r.DB.QueryContext(ctx, query, args...)
}

func TestTheAnswerOfAReleaseAttributeStaysUntilTheNextWritePostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := &torrentReads{DB: newDB(t)}
		module := transferhistory.New(db, models.NewTrackerCustomizationStore(db.DB), systemnotice.New(nil))
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
		record := func(r transferhistory.Reading) {
			r.InstanceID = instanceID
			module.Record(r)
		}
		query := transferhistory.TransferQuery{InstanceID: instanceID, From: day10, To: day12, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByReleaseGroup}
		uploadIn := func(query transferhistory.TransferQuery, group string) int64 {
			rows, err := module.Transfer(t.Context(), query)
			require.NoError(t, err)
			for _, r := range rows {
				if r.Group == group {
					return r.Uploaded
				}
			}
			return 0
		}
		uploadOf := func(group string) int64 { return uploadIn(query, group) }

		record(transferhistory.Reading{Time: at(0), Torrents: releaseTorrents(0, 0, 0, 0)})
		record(transferhistory.Reading{Time: at(5 * time.Minute), Torrents: releaseTorrents(100, 200, 300, 30)})
		_, err := module.Status(t.Context())
		require.NoError(t, err)
		transferhistory.WaitForFolds(module)
		require.Equal(t, int64(130), uploadOf("GRP"))
		reads := db.reads.Load()
		require.Positive(t, reads)

		require.Equal(t, int64(130), uploadOf("GRP"))
		require.Equal(t, reads, db.reads.Load())
		moved := query
		moved.To = day12.Add(-time.Minute)
		require.Equal(t, int64(130), uploadIn(moved, "GRP"))
		require.Equal(t, reads, db.reads.Load())
		record(transferhistory.Reading{Time: at(6 * time.Minute), Torrents: releaseTorrents(150, 200, 300, 30)})
		require.Equal(t, int64(130), uploadOf("GRP"))
		require.Equal(t, reads, db.reads.Load())
		module.Record(transferhistory.Reading{InstanceID: instanceID + 1, Time: at(7 * time.Minute), Torrents: releaseTorrents(0, 0, 0, 0)})
		require.Equal(t, int64(130), uploadOf("GRP"))
		require.Equal(t, reads, db.reads.Load())

		record(transferhistory.Reading{Time: at(10 * time.Minute), Torrents: releaseTorrents(150, 200, 300, 30)})
		require.Equal(t, int64(180), uploadOf("GRP"))
		require.Greater(t, db.reads.Load(), reads)

		require.NoError(t, module.DeleteHistory(t.Context(), transferhistory.HistoryDelete{InstanceID: instanceID}))
		require.Zero(t, uploadOf("GRP"))
	})
}

func TestNoAnswerOfAReleaseAttributeIsKeptWhileAFoldRuns(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "transfer-history")
	q, folding := newBlockedFold(db)
	module := transferhistory.New(q, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
	t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
	t.Cleanup(q.release)

	module.Record(transferhistory.Reading{InstanceID: instanceID, Time: at(0), Torrents: releaseTorrents(0, 0, 0, 0)})
	<-folding
	module.Record(transferhistory.Reading{InstanceID: instanceID, Time: at(5 * time.Minute), Torrents: releaseTorrents(100, 200, 300, 30)})
	upload := func() int64 {
		rows, err := module.Transfer(t.Context(), transferhistory.TransferQuery{
			InstanceID: instanceID, From: day10, To: day12, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByReleaseGroup,
		})
		require.NoError(t, err)
		for _, r := range rows {
			if r.Group == "GRP" {
				return r.Uploaded
			}
		}
		return 0
	}
	require.Equal(t, int64(130), upload())

	_, err := db.ExecContext(t.Context(), `UPDATE transfer_history_recent_torrent_days SET uploaded = uploaded + 1000`)
	require.NoError(t, err)
	require.Equal(t, int64(2130), upload())
}
