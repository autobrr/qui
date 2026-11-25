// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
)

func TestTransfersOfOneSourceRunSideBySideUpToTheLimit(t *testing.T) {
	r := newRig(t, func(c *transfer.Config) { c.PerSource = 2 })
	names := []string{showName, "Other.Show.S02.720p-GRP", "Third.Show.S03.720p-GRP"}
	shows := make([]show, len(names))
	for i, name := range names {
		shows[i] = newShow(t, name)
		write(t, filepath.Join(r.srcDir, name), shows[i].files)
		r.src.Seed(shows[i].data, r.srcPath())
	}
	copying, held := make(chan string), make(chan struct{})
	release := sync.OnceFunc(func() { close(held) })
	t.Cleanup(release)
	r.disk.onWrite = func(name string) {
		top, _, _ := strings.Cut(filepath.ToSlash(name), "/")
		select {
		case copying <- top:
		case <-held:
		}
		<-held
	}

	ids := make([]int64, len(shows))
	for i, s := range shows[:2] {
		var err error
		ids[i], err = r.svc.Start(t.Context(), r.request(s))
		require.NoError(t, err)
	}
	inCopy := map[string]bool{}
	for len(inCopy) < 2 {
		select {
		case top := <-copying:
			inCopy[top] = true
		case <-time.After(5 * time.Second):
			require.FailNow(t, "the second Transfer waits for the first", "in the copy: %v", inCopy)
		}
	}
	var err error
	ids[2], err = r.svc.Start(t.Context(), r.request(shows[2]))
	require.NoError(t, err)
	require.Equal(t, transfer.StateWaiting, r.get(ids[2]).State, "the third Transfer waits for a free place")
	release()
	r.svc.Wait()

	for _, id := range ids {
		record := r.get(id)
		require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	}
}

func TestTransfersIntoOneFolderRunOneAfterTheOther(t *testing.T) {
	r := newRig(t, func(c *transfer.Config) { c.PerSource = 2 })
	first := newShow(t, showName)
	r.seed(first)
	data, hash := qbtstub.NewContentTorrent(t, showName, 32,
		qbtstub.ContentFile{Path: "Show.S01E01.mkv", Data: first.files["Show.S01E01.mkv"]},
		qbtstub.ContentFile{Path: "Show.S01E02.mkv", Data: first.files["Show.S01E02.mkv"]})
	second := show{data: data, hash: hash, files: first.files}
	write(t, filepath.Join(r.srcDir, "cross", showName), second.files)
	r.src.Seed(second.data, r.srcPath("cross"))

	var mu sync.Mutex
	writers := map[string]struct{}{}
	secondCopied := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(writers) > 1
	}
	copying, held := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(held) })
	t.Cleanup(release)
	startCopy := sync.OnceFunc(func() { close(copying) })
	r.disk.onWrite = func(name string) {
		_, writer, _ := strings.Cut(name, ".qui-transfer-")
		mu.Lock()
		writers[writer] = struct{}{}
		mu.Unlock()
		startCopy()
		<-held
	}

	firstID, err := r.svc.Start(t.Context(), r.request(first))
	require.NoError(t, err)
	select {
	case <-copying:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the first Transfer does not copy", r.get(firstID).FailureReason)
	}
	id, err := r.svc.Start(t.Context(), r.request(second))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		if secondCopied() {
			return true
		}
		record, err := r.store.Get(t.Context(), id)
		if err != nil {
			return false
		}
		for _, line := range record.Log {
			if strings.Contains(line.Message, "Waits for Transfer") {
				return true
			}
		}
		return false
	}, 5*time.Second, time.Millisecond, "the second Transfer neither waits nor copies")
	require.False(t, secondCopied(), "the second Transfer copies into the folder that the first one writes")
	require.Equal(t, transfer.StateWaiting, r.get(id).State)
	release()
	r.svc.Wait()

	for _, id := range []int64{firstID, id} {
		record := r.get(id)
		require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	}
	require.False(t, secondCopied(), "the second Transfer uses the files of the first one")
	require.Equal(t, contents(first, showName), tree(t, r.dstDir))
	require.True(t, r.dst.Has(first.hash))
	require.True(t, r.dst.Has(second.hash))
}

func TestCancelOfATransferThatWaitsForItsPathLeavesTheTarget(t *testing.T) {
	r := newRig(t, func(c *transfer.Config) { c.PerSource = 2 })
	first := newShow(t, showName)
	r.seed(first)
	extra := first.files["Show.S01E01.mkv"]
	data, hash := qbtstub.NewContentTorrent(t, showName, 32, qbtstub.ContentFile{Path: "Extras/Show.Extras.mkv", Data: extra})
	write(t, filepath.Join(r.srcDir, "cross", showName, "Extras"), map[string][]byte{"Show.Extras.mkv": extra})
	r.src.Seed(data, r.srcPath("cross"))

	copying, held := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(held) })
	t.Cleanup(release)
	startCopy := sync.OnceFunc(func() { close(copying) })
	r.disk.onWrite = func(string) {
		startCopy()
		<-held
	}

	firstID, err := r.svc.Start(t.Context(), r.request(first))
	require.NoError(t, err)
	select {
	case <-copying:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the first Transfer does not copy", r.get(firstID).FailureReason)
	}
	id, err := r.svc.Start(t.Context(), r.request(show{data: data, hash: hash}))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		record, err := r.store.Get(t.Context(), id)
		if err != nil {
			return false
		}
		return slices.ContainsFunc(record.Log, func(line transfer.LogLine) bool { return strings.Contains(line.Message, "Waits for Transfer") })
	}, 5*time.Second, time.Millisecond, "the second Transfer does not wait")

	folder := filepath.Join(r.dstDir, showName, "Extras")
	require.NoError(t, os.Mkdir(folder, 0o755))
	require.NoError(t, r.svc.Cancel(t.Context(), id))
	require.Eventually(t, func() bool {
		record, err := r.store.Get(t.Context(), id)
		return err == nil && !record.FinishedAt.IsZero()
	}, 5*time.Second, time.Millisecond, "the cancel does not end the second Transfer")
	require.DirExists(t, folder, "the cancel of a Transfer that only waited removes a folder that the first Transfer made")
	release()
	r.svc.Wait()

	require.Equal(t, transfer.StateCancelled, r.get(id).State)
	record := r.get(firstID)
	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
}
