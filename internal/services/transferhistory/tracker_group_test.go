// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory

import (
	"context"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/systemnotice"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// regression: #4100
func TestTheFirstReadingAfterARestartKeepsTheTrackerGroup(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "transfer-history")
	customizations := models.NewTrackerCustomizationStore(db)
	start := time.Date(2026, 11, 10, 10, 0, 30, 0, time.UTC)

	// A private torrent on three domains of one tracker, with the primary
	// tracker on the last domain in sort order. Before the restart, the tracker
	// mapping holds the torrent.
	const hash = "aaaa000000000000000000000000000000000002"
	module := New(db, customizations, systemnotice.New(nil))
	before := Torrent{
		Hash: hash, Name: "Quiet.Harbor.2024.1080p.WEB-DL-GRP",
		TrackerDomains: []string{"alpha.example", "bravo.example", "charlie.example"},
		Private:        new(true), Size: 1000, AddedAt: time.Unix(1, 0).UTC(),
	}
	module.Record(Reading{InstanceID: 1, Time: start, Torrents: []Torrent{before}})
	before.Uploaded = 100
	module.Record(Reading{InstanceID: 1, Time: start.Add(5 * time.Minute), Torrents: []Torrent{before}})
	require.NoError(t, module.Shutdown(t.Context()))

	module = New(db, customizations, systemnotice.New(nil))
	t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
	owner, syncManager := newStubSync(t, func() string {
		return `{"rid":1,"full_update":true,` +
			`"torrents":{"` + hash + `":{"name":"Quiet.Harbor.2024.1080p.WEB-DL-GRP","added_on":1,"size":1000,"state":"stalledUP","uploaded":400,"private":true,"has_metadata":true,"tracker":"https://charlie.example/announce","trackers_count":3}},` +
			`"trackers":{"https://alpha.example/announce":["` + hash + `"],"https://bravo.example/announce":["` + hash + `"],"https://charlie.example/announce":["` + hash + `"]}}`
	})
	require.NoError(t, owner.Sync(t.Context()))
	update := qbt.SyncUpdate{Snapshot: owner.SnapshotUnchecked(), Trackers: owner.GetTrackersUnchecked()}
	NewAdapter(module, syncManager).record(1, update, start.Add(10*time.Minute), time.Time{}, peakSpeeds{})

	rows, err := module.Transfer(t.Context(), TransferQuery{
		InstanceID: 1, From: start.Truncate(time.Hour), To: start.Truncate(time.Hour).Add(time.Hour),
		Bucket: BucketTotal, GroupBy: GroupByTracker,
	})
	require.NoError(t, err)
	uploaded := make(map[string]int64)
	for _, row := range rows {
		uploaded[row.Group] += row.Uploaded
	}
	require.Equal(t, map[string]int64{"alpha.example": 400}, uploaded)
}
