// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
)

func TestAMoveOfTheRecentTorrentDaysKeepsEachByteOfTheWritesBesideItPostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		store := models.NewTransferHistoryStore(newDB(t))
		const instances, torrents, writes = 8, 300, 6
		const yesterday, today = int64(20400), int64(20401)
		at := time.Unix(today*86400, 0).UTC()

		records := make([][]*models.TransferHistoryRecord, instances)
		for i := range records {
			w := &models.TransferHistoryWrite{InstanceID: i + 1, FirstRecordedAt: at, LastReadingAt: at, Number: 1}
			for j := range torrents {
				r := &models.TransferHistoryRecord{Hash: fmt.Sprintf("%040x", i*torrents+j), Name: "Some.Show.S01E01.1080p.WEB-DL-GRP", TrackerDomain: "tracker.example"}
				w.Records = append(w.Records, r)
				w.Counters = append(w.Counters, r)
			}
			ids, err := store.Write(t.Context(), w)
			require.NoError(t, err)
			for _, r := range w.Records {
				r.ID = ids[r.Hash]
			}
			records[i] = w.Records
		}

		start, written := make(chan struct{}), make(chan struct{})
		var writers, mover sync.WaitGroup
		for i := range instances {
			writers.Go(func() {
				<-start
				for n := range writes {
					w := &models.TransferHistoryWrite{InstanceID: i + 1, FirstRecordedAt: at, LastReadingAt: at, Number: int64(n + 2)}
					for _, r := range records[i] {
						w.TorrentDays = append(w.TorrentDays, models.TransferHistoryTorrentDay{Record: r, Day: yesterday, Uploaded: 1})
					}
					_, err := store.Write(t.Context(), w)
					assert.NoError(t, err)
				}
			})
		}
		mover.Go(func() {
			<-start
			for {
				select {
				case <-written:
					return
				default:
				}
				assert.NoError(t, store.MoveRecentTorrentDays(t.Context(), today))
			}
		})
		close(start)
		writers.Wait()
		close(written)
		mover.Wait()
		require.NoError(t, store.MoveRecentTorrentDays(t.Context(), today))

		for i := range instances {
			rows, err := store.TorrentTransfer(t.Context(), i+1, nil, yesterday, today, true)
			require.NoError(t, err)
			uploaded := make(map[int64]int64)
			for _, r := range rows {
				uploaded[r.TorrentID] += r.Uploaded
			}
			require.Len(t, uploaded, torrents)
			for id, bytes := range uploaded {
				require.Equal(t, int64(writes), bytes, "torrent %d", id)
			}
		}
	})
}
