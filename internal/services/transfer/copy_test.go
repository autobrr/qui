// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/sshtest"
)

const packPiece = 16 << 10

func newSeason(t *testing.T, sizes ...int) show {
	t.Helper()
	random := rand.New(rand.NewChaCha8([32]byte{byte(len(sizes))}))
	files := map[string][]byte{}
	content := make([]qbtstub.ContentFile, 0, len(sizes))
	for i, size := range sizes {
		data := make([]byte, size)
		for j := range data {
			data[j] = byte(random.Uint32())
		}
		name := fmt.Sprintf("Show.S01E%02d.mkv", i+1)
		files[name] = data
		content = append(content, qbtstub.ContentFile{Path: name, Data: data})
	}
	data, hash := qbtstub.NewContentTorrent(t, showName, packPiece, content...)
	return show{data: data, hash: hash, files: files}
}

func allPieces(n int) []byte {
	out := make([]byte, (n+7)/8)
	for piece := range n {
		out[piece/8] |= 0x80 >> (piece % 8)
	}
	return out
}

func fastCopy(c *transfer.Config) {
	c.ReadBytes = transfer.DefaultConfig().ReadBytes
	c.FlushBytes = 256 << 10
}

func readers(ranges []sshtest.ReadRange) int {
	conns := map[int]bool{}
	for _, read := range ranges {
		conns[read.Conn] = true
	}
	return len(conns)
}

func TestTransferCopiesABigFileInFourRanges(t *testing.T) {
	r := newRig(t, fastCopy)
	const size = 4<<20 + 3
	s := newSeason(t, size)
	r.seed(s)
	r.ssh.SetLatency(time.Millisecond)

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, contents(s, showName), tree(t, r.dstDir))
	require.Equal(t, allPieces(size/packPiece+1), record.Members[0].ProvenPieces)
	ranges := r.ssh.ReadRanges()
	require.Len(t, ranges, 4)
	require.Equal(t, 4, readers(ranges))
	slices.SortFunc(ranges, func(a, b sshtest.ReadRange) int { return int(a.From - b.From) })
	require.Zero(t, ranges[0].From)
	for i := 1; i < len(ranges); i++ {
		require.Equal(t, ranges[i-1].To, ranges[i].From, "range %d starts where range %d ends", i, i-1)
		require.Zero(t, ranges[i].From%packPiece, "range %d starts at a piece", i)
	}
	require.Equal(t, int64(size), ranges[3].To)
	require.GreaterOrEqual(t, r.ssh.MaxInFlight(), 16, "the ranges keep reads in flight")
}

func TestTransferCopiesManySmallFilesAtOnce(t *testing.T) {
	r := newRig(t, fastCopy)
	sizes := make([]int, 400)
	for i := range sizes {
		sizes[i] = 10<<10 + i
	}
	s := newSeason(t, sizes...)
	r.seed(s)
	r.ssh.SetLatency(2 * time.Millisecond)

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, contents(s, showName), tree(t, r.dstDir))
	total := 0
	for _, size := range sizes {
		total += size
	}
	require.Equal(t, allPieces((total+packPiece-1)/packPiece), record.Members[0].ProvenPieces)
	require.Equal(t, 4, readers(r.ssh.ReadRanges()))
	require.GreaterOrEqual(t, r.ssh.MaxInFlight(), 64, "many files are read at once")
	require.LessOrEqual(t, r.ssh.MaxInFlight(), 128, "at most 128 files are read at once")
}

func TestTransferResumesACopyInSeveralRanges(t *testing.T) {
	r := newRig(t, fastCopy)
	const size = 4<<20 + 3
	s := newSeason(t, size)
	r.seed(s)
	ids := make(chan int64, 1)
	var (
		mu            sync.Mutex
		once          sync.Once
		id            int64
		stopped       bool
		at            []byte
		writes, reads int
	)
	r.disk.onWrite = func(string) {
		mu.Lock()
		defer mu.Unlock()
		once.Do(func() { id = <-ids })
		if stopped {
			return
		}
		record, err := r.store.Get(t.Context(), id)
		require.NoError(t, err)
		if bits := record.Members[0].ProvenPieces; provenRanges(bits, size/packPiece+1) >= 2 {
			stopped, at = true, slices.Clone(bits)
			logged, _ := r.disk.log()
			writes, reads = len(logged), len(r.ssh.ReadRanges())
			r.cancel()
		}
	}

	started, err := r.svc.Start(t.Context(), r.request(s))
	require.NoError(t, err)
	ids <- started
	r.svc.Wait()
	require.True(t, stopped, "qui stopped the copy")
	r.restart()
	r.svc.Wait()

	record := r.get(started)
	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, contents(s, showName), tree(t, r.dstDir))
	require.Equal(t, allPieces(size/packPiece+1), record.Members[0].ProvenPieces)
	logged, creates := r.disk.log()
	for name, n := range creates {
		require.Equal(t, 1, n, "qui made %s once, and wrote into it after the restart", name)
	}
	for _, w := range logged[writes:] {
		for piece := w.at / packPiece; piece <= (w.at+w.n-1)/packPiece; piece++ {
			require.False(t, proven(at, piece), "a write after the stop copies piece %d again", piece)
		}
	}
	for _, read := range r.ssh.ReadRanges()[reads:] {
		for piece := read.From / packPiece; piece <= (read.To-1)/packPiece; piece++ {
			require.False(t, proven(at, piece), "a read after the stop reads piece %d again", piece)
		}
	}
}

func TestTransferResumeClosesTheFilesOfEachTask(t *testing.T) {
	r := newRig(t)
	sizes := make([]int, 40)
	for i := range sizes {
		sizes[i] = 2*packPiece + 100
	}
	s := newSeason(t, sizes...)
	r.seed(s)
	r.disk.unconfirmed = true
	cancel := sync.OnceFunc(r.cancel)
	r.dst.OnAdd = func(string) { cancel() }

	id, err := r.svc.Start(t.Context(), r.request(s))
	require.NoError(t, err)
	r.svc.Wait()
	for name := range s.files {
		lose(t, filepath.Join(r.dstDir, showName, name+".qui-transfer-"+strconv.FormatInt(id, 10)), 20000, 50)
	}
	r.dst.OnAdd = nil
	r.disk.mostWriters = 0

	r.restart()
	r.svc.Wait()

	record := r.get(id)
	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, contents(s, showName), tree(t, r.dstDir))
	require.Contains(t, logOf(record), "The check on homelab passed 41 of 81 pieces of the partial files.")
	require.Equal(t, 1, r.disk.mostWriters, "qui writes one file at a time")
}

func provenRanges(bits []byte, n int) int {
	quarters := map[int]bool{}
	for piece := range n {
		if proven(bits, int64(piece)) {
			quarters[piece*4/n] = true
		}
	}
	return len(quarters)
}

func TestTransferGoesOnWithFewerConnections(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit int
	}{
		{name: "a host that allows two connections", limit: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, fastCopy)
			const size = 4<<20 + 3
			s := newSeason(t, size)
			r.seed(s)
			r.ssh.SetMaxConnections(tc.limit)

			record := r.run(r.request(s))

			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Equal(t, contents(s, showName), tree(t, r.dstDir))
			require.Equal(t, allPieces(size/packPiece+1), record.Members[0].ProvenPieces)
			ranges := r.ssh.ReadRanges()
			require.Len(t, ranges, tc.limit, "one range for each connection")
			require.Equal(t, tc.limit, readers(ranges))
			require.Empty(t, tree(t, r.srcDir), "qui still reaches the source")
		})
	}
}
