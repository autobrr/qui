// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"context"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
)

const bigPiece = 16 << 10

func newBigShow(t *testing.T) show {
	t.Helper()
	random := rand.New(rand.NewChaCha8([32]byte{1}))
	fill := func(n int) []byte {
		data := make([]byte, n)
		for i := range data {
			data[i] = byte(random.Uint32())
		}
		return data
	}
	files := map[string][]byte{
		"Show.S01E01.mkv": fill(600<<10 + 5),
		"Show.S01E02.mkv": fill(400<<10 + 7),
	}
	data, hash := qbtstub.NewContentTorrent(t, showName, bigPiece,
		qbtstub.ContentFile{Path: "Show.S01E01.mkv", Data: files["Show.S01E01.mkv"]},
		qbtstub.ContentFile{Path: "Show.S01E02.mkv", Data: files["Show.S01E02.mkv"]})
	return show{data: data, hash: hash, files: files}
}

type span struct{ start, end int64 }

func spans(t *testing.T, s show, id int64, writes []diskWrite) []span {
	t.Helper()
	first := int64(len(s.files["Show.S01E01.mkv"]))
	out := make([]span, 0, len(writes))
	for _, w := range writes {
		var offset int64
		switch strings.TrimSuffix(w.name, ".!qB") {
		case showName + "/Show.S01E01.mkv.qui-transfer-" + strconv.FormatInt(id, 10):
		case showName + "/Show.S01E02.mkv.qui-transfer-" + strconv.FormatInt(id, 10):
			offset = first
		default:
			t.Fatalf("a write to %s", w.name)
		}
		out = append(out, span{start: offset + w.at, end: offset + w.at + w.n})
	}
	return out
}

func proven(bits []byte, piece int64) bool {
	return int(piece/8) < len(bits) && bits[piece/8]&(0x80>>(piece%8)) != 0
}

func requireWholePieces(t *testing.T, total int64, writes []span) {
	t.Helper()
	for i := 0; i < len(writes); {
		start := writes[i].start
		require.Zero(t, start%bigPiece, "write %d starts piece %d inside it", i, start/bigPiece)
		end := min(start+bigPiece, total)
		at := start
		for ; i < len(writes) && at < end; i++ {
			require.Equal(t, at, writes[i].start, "write %d continues piece %d", i, start/bigPiece)
			at = writes[i].end
		}
		require.Equal(t, end, at, "piece %d is written whole", start/bigPiece)
	}
}

func TestTransferResumesAfterADroppedConnection(t *testing.T) {
	r := newRig(t, func(c *transfer.Config) { c.FlushBytes = 64 << 10 })
	s := newBigShow(t)
	r.seed(s)
	ids := make(chan int64, 1)
	var (
		once  sync.Once
		id    int64
		count int
		drops []int
		at    [][]byte
	)
	r.disk.onWrite = func(string) {
		once.Do(func() { id = <-ids })
		count++
		if count == 10 || count == 30 || count == 50 {
			record, err := r.store.Get(t.Context(), id)
			require.NoError(t, err)
			require.NotEmpty(t, record.Members[0].ProvenPieces, "a flush came before the drop")
			drops = append(drops, count-1)
			at = append(at, slices.Clone(record.Members[0].ProvenPieces))
			r.ssh.DropConnections()
		}
	}

	started, err := r.svc.Start(t.Context(), r.request(s))
	require.NoError(t, err)
	ids <- started
	r.svc.Wait()

	record, err := r.store.Get(t.Context(), started)
	require.NoError(t, err)
	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, contents(s, showName), tree(t, r.dstDir))
	require.Contains(t, logOf(record), "The connection to seedbox dropped.")
	require.Len(t, drops, 3)

	writes, creates := r.disk.log()
	for name, n := range creates {
		require.Equal(t, 1, n, "qui made %s once, and wrote into it after each drop", name)
	}
	total := int64(len(s.files["Show.S01E01.mkv"]) + len(s.files["Show.S01E02.mkv"]))
	written := spans(t, s, started, writes)
	requireWholePieces(t, total, written)
	for d, index := range drops {
		for i := index; i < len(written); i++ {
			piece := written[i].start / bigPiece
			require.False(t, proven(at[d], piece), "write %d copies piece %d again after drop %d", i, piece, d+1)
		}
	}
}

func TestTransferResumesAfterARestart(t *testing.T) {
	r := newRig(t, func(c *transfer.Config) { c.FlushBytes = 64 << 10 })
	s := newBigShow(t)
	r.seed(s)
	ids := make(chan int64, 1)
	var (
		once     sync.Once
		id       int64
		count    int
		stops    []int
		at       [][]byte
		resumed  bool
		progress transfer.Progress
		tracked  bool
	)
	restarted := make(chan struct{})
	r.disk.onWrite = func(string) {
		once.Do(func() { id = <-ids })
		count++
		if resumed {
			resumed = false
			<-restarted
			progress, tracked = r.svc.Progress(id)
		}
		if count == 10 || count == 30 || count == 50 {
			record, err := r.store.Get(t.Context(), id)
			require.NoError(t, err)
			require.NotEmpty(t, record.Members[0].ProvenPieces, "a flush came before the stop")
			stops = append(stops, count-1)
			at = append(at, slices.Clone(record.Members[0].ProvenPieces))
			r.cancel()
		}
	}

	started, err := r.svc.Start(t.Context(), r.request(s))
	require.NoError(t, err)
	ids <- started
	for i := range 3 {
		r.svc.Wait()
		resumed = i == 0
		r.restart()
		if i == 0 {
			close(restarted)
		}
	}
	r.svc.Wait()

	record, err := r.store.Get(t.Context(), started)
	require.NoError(t, err)
	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, contents(s, showName), tree(t, r.dstDir))
	require.Len(t, stops, 3)
	require.True(t, tracked)
	require.Positive(t, progress.Done, "the Proven pieces count as passed at the first write after the restart")
	require.Equal(t, 1, r.dst.Writes("torrents/add"))

	writes, creates := r.disk.log()
	for name, n := range creates {
		require.Equal(t, 1, n, "qui made %s once, and wrote into it after each restart", name)
	}
	total := int64(len(s.files["Show.S01E01.mkv"]) + len(s.files["Show.S01E02.mkv"]))
	written := spans(t, s, started, writes)
	requireWholePieces(t, total, written)
	for d, index := range stops {
		for i := index; i < len(written); i++ {
			piece := written[i].start / bigPiece
			require.False(t, proven(at[d], piece), "write %d copies piece %d again after stop %d", i, piece, d+1)
		}
	}
}

func TestTransferProvesNoPieceAfterAnUnconfirmedFlush(t *testing.T) {
	r := newRig(t, func(c *transfer.Config) { c.FlushBytes = 64 << 10 })
	s := newBigShow(t)
	r.seed(s)
	r.disk.unconfirmed = true
	count := 0
	r.disk.onWrite = func(string) {
		if count++; count == 10 || count == 30 {
			r.cancel()
		}
	}

	id, err := r.svc.Start(t.Context(), r.request(s))
	require.NoError(t, err)
	r.svc.Wait()
	first, _ := r.disk.log()
	r.disk.unconfirmed = false
	r.restart()
	r.svc.Wait()
	require.Equal(t, transfer.FlushUnconfirmed, r.get(id).FlushState)
	writes, _ := r.disk.log()
	require.Greater(t, len(writes), len(first)+10, "the second run flushed pieces")
	at := spans(t, s, id, writes)[len(first)].start + 50
	name, size := "Show.S01E01.mkv", int64(len(s.files["Show.S01E01.mkv"]))
	if at >= size {
		name, at = "Show.S01E02.mkv", at-size
	}
	lose(t, filepath.Join(r.dstDir, showName, name+".qui-transfer-"+strconv.FormatInt(id, 10)), at, 100)

	r.restart()
	r.svc.Wait()

	record := r.get(id)
	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, contents(s, showName), tree(t, r.dstDir))
}

func sent(s *qbtstub.Server) int { return s.RequestsWithout("torrents/info") }

func sentWithoutResync(s *qbtstub.Server) int {
	return s.RequestsWithout("torrents/info", "sync/maindata", "app/webapiVersion")
}

func (r *rig) requests() int { return sent(r.src) + sent(r.dst) }

func TestTransferRetriesATemporaryFailureAfterAnHour(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	r.dst.Reject = http.StatusInternalServerError

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateFailed, record.State)
	require.Equal(t, transfer.FailureTemporary, record.FailureClass, record.FailureReason)
	require.Equal(t, clock.Add(time.Hour), record.RetryAt)
	require.True(t, record.FinishedAt.IsZero())
	require.True(t, r.store.IsClaimed(r.source.ID, s.hash))
	require.Equal(t, contents(s, showName), tree(t, r.dstDir), "the copy stays")
	require.True(t, r.src.Has(s.hash))
	require.False(t, r.src.Stopped(s.hash))

	r.dst.Reject = 0
	before := r.requests()
	r.setClock(clock.Add(59 * time.Minute))
	r.svc.Wake()
	r.svc.Wait()
	require.Equal(t, before, r.requests(), "no retry before the hour")

	r.setClock(clock.Add(time.Hour))
	r.svc.Wake()
	r.svc.Wait()
	record, err := r.store.Get(t.Context(), record.ID)
	require.NoError(t, err)
	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, 2, r.dst.Writes("torrents/add"))
	require.False(t, r.src.Has(s.hash))
	require.Equal(t, contents(s, showName), tree(t, r.dstDir))
}

func TestTransferNeverRetriesAPermanentFailure(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	r.disk.unconfirmed = true
	r.dst.IncompleteFilesExt = true
	r.disk.damage = map[string]int64{showName + "/Show.S01E02.mkv": 20}

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateFailed, record.State)
	require.Equal(t, transfer.FailurePermanent, record.FailureClass, record.FailureReason)
	require.Equal(t, "The check on homelab failed 1 of 5 pieces: 3.", record.FailureReason, "pieces 0, 1, 2 and 4 passed")
	require.True(t, record.RetryAt.IsZero())
	require.False(t, record.FinishedAt.IsZero())
	require.False(t, r.store.IsClaimed(r.source.ID, s.hash))
	require.Empty(t, tree(t, r.dstDir))
	r.requireNoHelpers(record)

	before := r.requests()
	r.setClock(clock.Add(48 * time.Hour))
	r.svc.Wake()
	r.restart()
	r.svc.Wait()
	require.Equal(t, before, r.requests())
}

func (r *rig) startOnWrite(s show, change func(id int64)) int64 {
	r.t.Helper()
	ids := make(chan int64, 1)
	var once sync.Once
	r.disk.onWrite = func(string) { once.Do(func() { change(<-ids) }) }
	id, err := r.svc.Start(r.t.Context(), r.request(s))
	require.NoError(r.t, err)
	ids <- id
	r.svc.Wait()
	return id
}

func (r *rig) get(id int64) *transfer.Record {
	r.t.Helper()
	record, err := r.store.Get(r.t.Context(), id)
	require.NoError(r.t, err)
	return record
}

func TestTransferWaitsForATurnedOffInstance(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)

	id := r.startOnWrite(s, func(int64) {
		_, err := r.instances.SetActiveState(t.Context(), r.target.ID, false)
		require.NoError(t, err)
	})

	record := r.get(id)
	require.Equal(t, transfer.StateWaiting, record.State, record.FailureReason)
	require.True(t, record.FinishedAt.IsZero())
	require.Contains(t, logOf(record), "Waits until homelab is turned on again.")
	require.NotEmpty(t, tree(t, r.dstDir), "qui keeps what it copied")
	require.Zero(t, r.dst.Writes("torrents/add"))

	before := r.requests()
	r.setClock(clock.Add(2 * time.Hour))
	r.svc.Wake()
	r.svc.Wait()
	require.Equal(t, before, r.requests(), "the Transfer waits while homelab is off")

	_, err := r.instances.SetActiveState(t.Context(), r.target.ID, true)
	require.NoError(t, err)
	r.svc.Wake()
	r.svc.Wait()
	record = r.get(id)
	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, contents(s, showName), tree(t, r.dstDir))
	require.False(t, r.src.Has(s.hash))
}

func TestTransferFailsWhenAnInstanceIsDeleted(t *testing.T) {
	t.Run("the source", func(t *testing.T) {
		r := newRig(t)
		s := newShow(t, showName)
		r.seed(s)
		var before int
		id := r.startOnWrite(s, func(int64) {
			require.NoError(t, r.instances.Delete(t.Context(), r.source.ID))
			before = sent(r.src)
		})

		record := r.get(id)
		require.Equal(t, transfer.StateFailed, record.State)
		require.Equal(t, transfer.FailurePermanent, record.FailureClass)
		require.Contains(t, record.FailureReason, "was deleted")
		require.Empty(t, tree(t, r.dstDir), "qui removes what it wrote on the target")
		require.Equal(t, before, sent(r.src))
		require.False(t, r.store.IsClaimed(r.source.ID, s.hash))
	})
	t.Run("the target", func(t *testing.T) {
		r := newRig(t)
		s := newShow(t, showName)
		r.seed(s)
		var before int
		id := r.startOnWrite(s, func(int64) {
			require.NoError(t, r.instances.Delete(t.Context(), r.target.ID))
			before = sent(r.dst)
		})

		record := r.get(id)
		require.Equal(t, transfer.StateFailed, record.State)
		require.Equal(t, transfer.FailurePermanent, record.FailureClass)
		require.Contains(t, record.FailureReason, "was deleted")
		require.Equal(t, before, sent(r.dst))
		require.True(t, r.src.Has(s.hash))
		require.False(t, r.src.Stopped(s.hash))
	})
}

func TestTransferStopsWhenAnEndpointChanges(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	moved := qbtstub.New(t, r.dstDir)

	id := r.startOnWrite(s, func(int64) {
		_, err := r.instances.Update(t.Context(), r.target.ID, "homelab", moved.URL, "", "", nil, nil, nil)
		require.NoError(t, err)
	})

	record := r.get(id)
	require.Equal(t, transfer.StateFailed, record.State)
	require.Equal(t, transfer.FailurePermanent, record.FailureClass)
	require.Contains(t, record.FailureReason, "homelab changed its address")
	require.Zero(t, moved.Requests(), "qui sends nothing to the new address")
	require.NotEmpty(t, tree(t, r.dstDir), "qui removes nothing there")
	require.True(t, r.src.Has(s.hash))
	require.False(t, r.store.IsClaimed(r.source.ID, s.hash))
}

func (r *rig) moveTheCrossSeedInstance() (show, int64, *qbtstub.Server) {
	r.t.Helper()
	cs := r.withCrossSeedInstance()
	cs.strand = "alignment_failed"
	s := r.seedRouted()
	record := r.run(r.request(s))
	require.Equal(r.t, transfer.FailureTemporary, record.FailureClass, record.FailureReason)
	require.True(r.t, cs.stub.Has(s.hash), "the add left the member on the Cross-seed instance")
	return s, record.ID, r.moveCrossSeed(cs, s)
}

func (r *rig) seedRouted() show {
	r.t.Helper()
	s := newShow(r.t, showName)
	r.seed(s)
	crossSeed, crossSeedHash := crossSeedOf(r.t, s)
	r.src.Seed(crossSeed, r.srcPath())
	r.src.Edit(crossSeedHash, func(t *qbtstub.Torrent) { t.AddedOn = 100 })
	r.src.Edit(s.hash, func(t *qbtstub.Torrent) { t.AddedOn = 200 })
	return s
}

func (r *rig) moveCrossSeed(cs *crossSeeds, s show) *qbtstub.Server {
	r.t.Helper()
	other := r.t.TempDir()
	moved := qbtstub.New(r.t, other)
	moved.Seed(s.data, other)
	_, err := r.instances.Update(r.t.Context(), cs.inst.ID, cs.inst.Name, moved.URL, "", "", nil, nil, nil)
	require.NoError(r.t, err)
	r.restartWithNewClients()
	return moved
}

func (r *rig) restartWithNewClients(edit ...func(*transfer.Deps)) {
	r.t.Helper()
	deps, _, sync := wire(r.t, r.db, r.disk, r.now)
	deps.Routes, deps.CrossSeeds, deps.Config = r.deps.Routes, r.deps.CrossSeeds, r.deps.Config
	for _, e := range edit {
		e(&deps)
	}
	r.deps, r.sync = deps, sync
	r.restart()
}

func TestTransferStopsWhenTheCrossSeedInstanceGetsANewAddress(t *testing.T) {
	r := newRig(t)
	s, id, moved := r.moveTheCrossSeedInstance()

	r.setClock(clock.Add(time.Hour))
	r.svc.Wake()
	r.svc.Wait()

	record := r.get(id)
	const reason = "The Cross-seed instance homelab cross-seed changed its address, its SSH server, or its file access " +
		"since the Transfer started, so the Transfer stopped. qui cleans up nothing on homelab cross-seed."
	require.Equal(t, transfer.StateFailed, record.State, record.FailureReason)
	require.Equal(t, transfer.FailurePermanent, record.FailureClass, record.FailureReason)
	require.Equal(t, reason, record.FailureReason)
	require.Contains(t, logOf(record), "Failed: "+reason)
	require.Zero(t, sent(moved), "qui sends nothing to the new address")
	require.True(t, moved.Has(s.hash))
}

func TestTransferCancelRemovesNothingFromACrossSeedInstanceWithANewAddress(t *testing.T) {
	r := newRig(t)
	s, id, moved := r.moveTheCrossSeedInstance()

	require.NoError(t, r.svc.Cancel(t.Context(), id))
	r.svc.Wait()

	require.Equal(t, transfer.StateCancelled, r.get(id).State)
	require.Zero(t, sent(moved), "qui sends nothing to the new address")
	require.True(t, moved.Has(s.hash))
}

func TestTransferCancelRemovesNothingFromACrossSeedInstanceThatMovedDuringTheRun(t *testing.T) {
	r := newRig(t)
	cs := r.withCrossSeedInstance()
	s := r.seedRouted()
	moved := qbtstub.New(t, t.TempDir())
	ids := make(chan int64, 1)
	moveAndCancel := sync.OnceFunc(func() {
		_, err := r.instances.Update(t.Context(), cs.inst.ID, cs.inst.Name, moved.URL, "", "", nil, nil, nil)
		require.NoError(t, err)
		require.NoError(t, r.svc.Cancel(t.Context(), <-ids))
	})
	cs.stub.OnWrite = func(endpoint string) {
		if endpoint == "torrents/start" {
			moveAndCancel()
		}
	}

	id, err := r.svc.Start(t.Context(), r.request(s))
	require.NoError(t, err)
	ids <- id
	r.svc.Wait()

	require.Equal(t, transfer.StateCancelled, r.get(id).State, r.get(id).FailureReason)
	require.Zero(t, cs.stub.Writes("torrents/delete"), "qui removes nothing from the Cross-seed instance")
	require.True(t, cs.stub.Has(s.hash))
}

func TestTransferRemovesItsCopyWhenTheCrossSeedInstanceGetsANewAddress(t *testing.T) {
	r := newRig(t)
	cs := r.withCrossSeedInstance()
	s := r.seedRouted()
	writes := 0
	r.disk.onWrite = func(string) {
		if writes++; writes == 5 {
			r.cancel()
		}
	}
	id, err := r.svc.Start(t.Context(), r.request(s))
	require.NoError(t, err)
	r.svc.Wait()
	require.NotEmpty(t, tree(t, r.dstDir), "the stop leaves part of the copy")

	moved := r.moveCrossSeed(cs, s)
	r.svc.Wait()

	record := r.get(id)
	require.Equal(t, transfer.FailurePermanent, record.FailureClass, record.FailureReason)
	require.Contains(t, record.FailureReason, "The Cross-seed instance homelab cross-seed changed its address")
	require.Empty(t, tree(t, r.dstDir), "the cleanup removes the partial copy")
	require.Zero(t, sent(moved), "qui sends nothing to the new address")
}

func TestTransferRollbackStartsOnlyWhatQuiStopped(t *testing.T) {
	for _, tc := range []struct {
		name                string
		reject              bool
		restart, killAtStop bool
		class               transfer.FailureClass
	}{
		{name: "a permanent failure", class: transfer.FailurePermanent},
		{name: "a temporary failure", reject: true, class: transfer.FailureTemporary},
		{name: "a cleanup after a restart", restart: true, class: transfer.FailurePermanent},
		{name: "a failure after a restart at the stop", killAtStop: true, class: transfer.FailurePermanent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			s := newShow(t, showName)
			r.seed(s)
			episode := r.seedEpisode(s)
			require.NoError(t, r.sync.BulkAction(t.Context(), r.source.ID, []string{episode}, "pause"))
			r.dst.OnWrite = func(endpoint string) {
				r.dst.Reject = 0
				switch {
				case endpoint != "torrents/start":
				case tc.reject:
					r.dst.Reject = http.StatusInternalServerError
				default:
					_ = os.Remove(filepath.Join(r.dstDir, showName, episode2))
				}
			}

			var id int64
			if tc.restart || tc.killAtStop {
				plan := crash{DB: r.dbPath, Request: r.request(s), At: "after:clean up"}
				child := make(chan *os.Process, 1)
				if tc.killAtStop {
					plan.At = ""
					r.src.OnWrite = func(endpoint string) {
						if endpoint == "torrents/stop" {
							require.NoError(t, (<-child).Kill())
						}
					}
				}
				runCrashChild(t, plan, child, tc.killAtStop)
				r.src.OnWrite = nil
				r.restart()
				r.svc.Wait()
				require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT id FROM torrent_transfers`).Scan(&id))
			} else {
				id = r.run(r.request(s)).ID
			}

			record := r.get(id)
			require.Equal(t, transfer.StateFailed, record.State)
			require.Equal(t, tc.class, record.FailureClass, record.FailureReason)
			require.Len(t, record.Members, 2)
			require.False(t, r.src.Stopped(s.hash), "qui starts the torrent that it stopped")
			require.True(t, r.src.Stopped(episode), "the torrent that was stopped before the Transfer stays stopped")
		})
	}
}

func TestTransferCancel(t *testing.T) {
	t.Run("during the copy", func(t *testing.T) {
		r := newRig(t)
		s := newShow(t, showName)
		r.seed(s)

		id := r.startOnWrite(s, func(id int64) { require.NoError(t, r.svc.Cancel(t.Context(), id)) })

		record := r.get(id)
		require.Equal(t, transfer.StateCancelled, record.State)
		require.False(t, record.FinishedAt.IsZero())
		require.False(t, r.store.IsClaimed(r.source.ID, s.hash))
		require.Empty(t, tree(t, r.dstDir))
		require.True(t, r.src.Has(s.hash))
		require.False(t, r.src.Stopped(s.hash))
		require.Zero(t, r.dst.Writes("torrents/add"))
	})
	t.Run("before the removal from the source", func(t *testing.T) {
		r := newRig(t)
		s := newShow(t, showName)
		r.seed(s)
		ids := make(chan int64, 1)
		r.src.OnWrite = func(endpoint string) {
			if endpoint == "torrents/stop" {
				require.NoError(t, r.svc.Cancel(t.Context(), <-ids))
			}
		}
		id, err := r.svc.Start(t.Context(), r.request(s))
		require.NoError(t, err)
		ids <- id
		r.svc.Wait()

		record := r.get(id)
		require.Equal(t, transfer.StateCancelled, record.State, record.FailureReason)
		require.False(t, r.dst.Has(s.hash), "qui removes the torrent that it added")
		require.Empty(t, tree(t, r.dstDir))
		require.True(t, r.src.Has(s.hash))
		require.False(t, r.src.Stopped(s.hash), "qui starts the source again")
		require.Equal(t, contents(s, showName), tree(t, r.srcDir))
	})
	t.Run("after the removal from the source started", func(t *testing.T) {
		r := newRig(t)
		s := newShow(t, showName)
		r.seed(s)
		ids := make(chan int64, 1)
		var cancelErr error
		r.src.OnWrite = func(endpoint string) {
			if endpoint == "torrents/delete" {
				cancelErr = r.svc.Cancel(t.Context(), <-ids)
			}
		}
		id, err := r.svc.Start(t.Context(), r.request(s))
		require.NoError(t, err)
		ids <- id
		r.svc.Wait()

		require.ErrorIs(t, cancelErr, transfer.ErrCancelTooLate)
		record := r.get(id)
		require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
		require.Equal(t, contents(s, showName), tree(t, r.dstDir))
		require.False(t, r.src.Has(s.hash))
		require.ErrorIs(t, r.svc.Cancel(t.Context(), id), transfer.ErrFinished)
	})
	t.Run("once qui decided to remove the torrent from the source", func(t *testing.T) {
		r := newRig(t)
		s := newShow(t, showName)
		r.seed(s)
		ids := make(chan int64, 1)
		var cancelErr error
		r.deps.Groups = onFind{r.deps.Groups, func() {
			if r.src.Stopped(s.hash) {
				cancelErr = r.svc.Cancel(t.Context(), <-ids)
			}
		}}
		r.restart()
		req := r.request(s)
		req.Options.After = transfer.AfterRemoveWithFiles
		id, err := r.svc.Start(t.Context(), req)
		require.NoError(t, err)
		ids <- id
		r.svc.Wait()

		require.ErrorIs(t, cancelErr, transfer.ErrCancelTooLate)
		require.Equal(t, transfer.StateDone, r.get(id).State)
		require.False(t, r.src.Has(s.hash))
	})
	t.Run("while the run records a temporary failure", func(t *testing.T) {
		r := newRig(t)
		s := newShow(t, showName)
		r.seed(s)
		r.dst.OnWrite = func(endpoint string) {
			r.dst.Reject = 0
			if endpoint == "torrents/start" {
				r.dst.Reject = http.StatusInternalServerError
			}
		}
		ids := make(chan int64, 1)
		cancel := sync.OnceFunc(func() { require.NoError(t, r.svc.Cancel(t.Context(), <-ids)) })
		r.src.OnWrite = func(endpoint string) {
			if endpoint == "torrents/start" {
				cancel()
			}
		}
		id, err := r.svc.Start(t.Context(), r.request(s))
		require.NoError(t, err)
		ids <- id
		r.svc.Wait()

		record := r.get(id)
		require.Equal(t, transfer.StateCancelled, record.State, record.FailureReason)
		require.False(t, record.FinishedAt.IsZero())
		require.False(t, r.dst.Has(s.hash), "qui removes the torrent that it added")
		require.Empty(t, tree(t, r.dstDir))
		require.True(t, r.src.Has(s.hash))
		require.False(t, r.src.Stopped(s.hash))
		require.False(t, r.store.IsClaimed(r.source.ID, s.hash))
	})
	t.Run("a Transfer that waits for its retry", func(t *testing.T) {
		r := newRig(t)
		s := newShow(t, showName)
		r.seed(s)
		r.dst.Reject = http.StatusInternalServerError
		record := r.run(r.request(s))
		require.Equal(t, transfer.FailureTemporary, record.FailureClass)
		r.dst.Reject = 0

		require.NoError(t, r.svc.Cancel(t.Context(), record.ID))
		r.svc.Wait()

		record = r.get(record.ID)
		require.Equal(t, transfer.StateCancelled, record.State)
		require.Empty(t, tree(t, r.dstDir), "qui removes the files that it placed")
		require.False(t, r.dst.Has(s.hash))
		require.True(t, r.src.Has(s.hash))
		require.False(t, r.store.IsClaimed(r.source.ID, s.hash))
	})
}

func TestTransferKeepsATorrentWhoseAddLostItsAnswer(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	r.dst.DropAddAnswers = 1

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateFailed, record.State)
	require.Equal(t, transfer.FailurePermanent, record.FailureClass)
	require.Contains(t, record.FailureReason, "Ownership unknown: homelab has the torrent")
	require.True(t, r.dst.Has(s.hash), "qui never removes a torrent that it may not have added")
	require.Equal(t, contents(s, showName), tree(t, r.dstDir))
	require.True(t, r.src.Has(s.hash))
	require.False(t, r.src.Stopped(s.hash))
	require.Equal(t, contents(s, showName), tree(t, r.srcDir))
}

type onFind struct {
	transfer.GroupFinder
	find func()
}

func (f onFind) FindDeleteMatches(ctx context.Context, selection map[int][]string) (*crossseed.DeleteMatchesResponse, error) {
	f.find()
	return f.GroupFinder.FindDeleteMatches(ctx, selection)
}
