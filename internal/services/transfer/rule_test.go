// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
)

// regression: #4242
// A Preview runs the checks of Start and tells the way and the size of the
// Transfer: each file of the group once. It records, claims, and writes
// nothing, and refuses a torrent that a Transfer holds, as Start does.
func TestPreviewTellsTheWayAndTheSize(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	crossSeed, _ := crossSeedOf(t, s)
	r.src.Seed(crossSeed, r.srcPath())
	r.seedEpisode(s)

	preview, err := r.svc.Preview(t.Context(), r.request(s))

	require.NoError(t, err)
	require.Equal(t, transfer.WaySFTP, preview.Way)
	require.Equal(t, int64(70), preview.Bytes, "the cross-seed and the episode read the same 70 bytes")
	require.Len(t, preview.Members, 3)
	require.False(t, r.store.IsClaimed(r.source.ID, s.hash))
	unfinished, err := r.store.Unfinished(t.Context())
	require.NoError(t, err)
	require.Empty(t, unfinished)
	require.Zero(t, r.dst.Writes("torrents/add"))
	require.Empty(t, tree(t, r.dstDir))

	bittorrent := r.request(s)
	bittorrent.Options.ForcedWay = transfer.WayBitTorrent
	preview, err = r.svc.Preview(t.Context(), bittorrent)
	require.NoError(t, err)
	require.Equal(t, transfer.WayBitTorrent, preview.Way)

	_, err = r.svc.Preview(t.Context(), transfer.Request{SourceInstanceID: r.source.ID, Hash: strings.Repeat("ab", 20), TargetInstanceID: r.target.ID})
	_, refused := errors.AsType[*transfer.RefusalError](err)
	require.True(t, refused, "want a refusal, got %v", err)

	var whileHeld error
	r.startOnWrite(s, func(int64) { _, whileHeld = r.svc.Preview(t.Context(), r.request(s)) })
	require.ErrorIs(t, whileHeld, transfer.ErrClaimed)
}

// regression: #4242
// A rule matches each cross-seed of a set that reads one copy of the files
// through hardlinks, and a Transfer torrent passes these files for each
// Transfer that starts. So a rule starts no Transfer of a torrent while a
// Transfer holds a cross-seed of the set, and while the target has one. An
// .nfo of its own keeps a cross-seed in the set. The user can still start it.
func TestARulePassesTheFilesOfHardlinkedCrossSeedsOnce(t *testing.T) {
	r := newRigOn(t, onDisk, noAccess)
	r.connect()
	r.src.Unreachable, r.dst.Unreachable = true, true
	s := newShow(t, showName)
	r.seed(s)
	links, nfo := filepath.Join(r.srcDir, "links", showName), []byte("another tracker")
	require.NoError(t, os.MkdirAll(links, 0o755))
	for name := range s.files {
		require.NoError(t, os.Link(filepath.Join(r.srcDir, showName, name), filepath.Join(links, name)))
	}
	require.NoError(t, os.WriteFile(filepath.Join(links, "Show.nfo"), nfo, 0o600))
	withNFO, crossSeedHash := qbtstub.NewContentTorrent(t, showName, 32,
		qbtstub.ContentFile{Path: "Show.S01E01.mkv", Data: s.files["Show.S01E01.mkv"]},
		qbtstub.ContentFile{Path: "Show.S01E02.mkv", Data: s.files["Show.S01E02.mkv"]},
		qbtstub.ContentFile{Path: "Show.nfo", Data: nfo})
	r.src.Seed(withNFO, r.srcPath("links"))
	byRule := r.request(s)
	byRule.StartedBy, byRule.RuleID = transfer.StartedByRule, new(7)
	byRule.Options.After = transfer.AfterKeepSeeding
	crossSeed := byRule
	crossSeed.Hash = crossSeedHash

	first := r.run(byRule)
	require.Equal(t, transfer.FailureTemporary, first.FailureClass)
	require.False(t, r.dst.Has(s.hash))
	_, err := r.svc.Start(t.Context(), crossSeed)
	require.ErrorIs(t, err, transfer.ErrStays, "a Transfer holds the first torrent of the set")

	r.src.Unreachable, r.dst.Unreachable = false, false
	r.setClock(clock.Add(time.Hour))
	r.svc.Wake()
	r.svc.Wait()
	require.Equal(t, transfer.StateDone, r.get(first.ID).State)
	require.False(t, r.store.IsClaimed(r.source.ID, s.hash))
	_, err = r.svc.Start(t.Context(), crossSeed)
	require.ErrorIs(t, err, transfer.ErrStays, "the target has the first torrent of the set")

	crossSeed.StartedBy, crossSeed.RuleID = transfer.StartedByUser, nil
	preview, err := r.svc.Preview(t.Context(), crossSeed)
	require.NoError(t, err)
	require.Equal(t, 1, preview.Stay)
}

// regression: #4242
// A longer pack reads the files of a pack through hardlinks and has one more
// episode. The two do not have the same files, so a rule starts the Transfer
// of the longer pack while the target has the other one.
func TestARuleMovesACrossSeedThatHasOtherFiles(t *testing.T) {
	r := newRigOn(t, onDisk, noAccess)
	r.connect()
	pack := newShow(t, showName)
	r.seed(pack)
	links, third := filepath.Join(r.srcDir, "links", showName), []byte("the third episode of the show")
	require.NoError(t, os.MkdirAll(links, 0o755))
	for name := range pack.files {
		require.NoError(t, os.Link(filepath.Join(r.srcDir, showName, name), filepath.Join(links, name)))
	}
	require.NoError(t, os.WriteFile(filepath.Join(links, "Show.S01E03.mkv"), third, 0o600))
	longer, longerHash := qbtstub.NewContentTorrent(t, showName, 32,
		qbtstub.ContentFile{Path: "Show.S01E01.mkv", Data: pack.files["Show.S01E01.mkv"]},
		qbtstub.ContentFile{Path: "Show.S01E02.mkv", Data: pack.files["Show.S01E02.mkv"]},
		qbtstub.ContentFile{Path: "Show.S01E03.mkv", Data: third})
	r.src.Seed(longer, r.srcPath("links"))
	byRule := r.request(pack)
	byRule.StartedBy, byRule.RuleID = transfer.StartedByRule, new(7)
	byRule.Options.After = transfer.AfterKeepSeeding

	record := r.run(byRule)
	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)

	byRule.Hash = longerHash
	preview, err := r.svc.Preview(t.Context(), byRule)
	require.NoError(t, err)
	require.Equal(t, 1, preview.Stay, "the pack on the target is a cross-seed on hardlinks")
	require.Empty(t, preview.Versions)
}

// regression: #4242
// After a Transfer fails for good, a rule starts no new Transfer of the
// torrent: the failure waits for the user. The user can still start one.
func TestARuleWaitsForTheUserAfterAPermanentFailure(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	r.disk.free = 69
	byRule := r.request(s)
	byRule.StartedBy, byRule.RuleID = transfer.StartedByRule, new(7)
	require.Equal(t, transfer.FailurePermanent, r.run(byRule).FailureClass)

	_, err := r.svc.Start(t.Context(), byRule)
	require.ErrorIs(t, err, transfer.ErrWaitsForUser)
	_, err = r.svc.Preview(t.Context(), byRule)
	require.ErrorIs(t, err, transfer.ErrWaitsForUser)
	crossSeed, crossSeedHash := crossSeedOf(t, s)
	r.src.Seed(crossSeed, r.srcPath())
	byRule.Hash = crossSeedHash
	_, err = r.svc.Start(t.Context(), byRule)
	require.ErrorIs(t, err, transfer.ErrWaitsForUser, "a new cross-seed would take the torrent along")

	r.disk.free = 0
	require.Equal(t, transfer.StateDone, r.run(r.request(s)).State)
}

// A Transfer sends its end as an event: finished, or failed with the reason.
func TestTransferSendsItsEndAsAnEvent(t *testing.T) {
	r := newRig(t)
	sent := &events{}
	r.deps.Notifier = sent
	r.restart()
	s := newShow(t, showName)
	r.seed(s)

	r.disk.free = 69
	r.run(r.request(s))
	r.disk.free = 0
	r.run(r.request(s))

	require.Len(t, sent.events, 2)
	failed, finished := sent.events[0], sent.events[1]
	require.Equal(t, notifications.EventTransferFailed, failed.Type)
	require.Equal(t, notifications.EventTransferFinished, finished.Type)
	for _, event := range sent.events {
		require.Equal(t, r.source.ID, event.InstanceID)
		require.Equal(t, showName, event.TorrentName)
		require.Equal(t, s.hash, event.TorrentHash)
	}
	require.Equal(t, "Torrent: "+showName+"\nTarget: homelab", finished.Message)
	require.True(t, strings.HasPrefix(failed.Message, "Torrent: "+showName+"\nTarget: homelab\nError: Not enough free space on homelab"), failed.Message)
}

// regression: #4242
// A Transfer sends the event of its end after qui records the end. When that
// record fails, the next start of qui ends the Transfer again and sends the
// one event.
func TestATransferSendsOneEventAfterTheRecordOfItsEnd(t *testing.T) {
	for _, tc := range []struct {
		name  string
		free  int64
		event notifications.EventType
	}{
		{name: "finished", event: notifications.EventTransferFinished},
		{name: "failed for good", free: 69, event: notifications.EventTransferFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			sent := &events{}
			r.deps.Notifier = sent
			var failing atomic.Bool
			r.restartOn(failTx{Querier: r.db, failing: &failing})
			s := newShow(t, showName)
			r.seed(s)
			r.disk.free = tc.free
			r.disk.onStatfs = func() { failing.Store(true) }

			record := r.run(r.request(s))
			require.True(t, record.FinishedAt.IsZero(), "the record of the end failed")
			require.Empty(t, sent.events)

			r.disk.onStatfs = nil
			failing.Store(false)
			r.restart()
			r.svc.Wait()
			require.False(t, r.get(record.ID).FinishedAt.IsZero())
			require.Len(t, sent.events, 1)
			require.Equal(t, tc.event, sent.events[0].Type)
		})
	}
}

// regression: #4242
// A Transfer that resumes after one of its instances was deleted fails for
// good, and its event shows a deleted target by its ID.
func TestATransferSendsItsEndAfterAnInstanceIsDeleted(t *testing.T) {
	for _, side := range []string{"source", "target"} {
		r := newRig(t)
		sent := &events{}
		r.deps.Notifier = sent
		r.restart()
		s := newShow(t, showName)
		r.seed(s)
		id := r.startOnWrite(s, func(int64) { r.cancel() })
		gone, target := r.source, "homelab"
		if side == "target" {
			gone, target = r.target, fmt.Sprintf("Instance %d", r.target.ID)
		}
		require.NoError(t, r.instances.Delete(t.Context(), gone.ID))

		r.restart()
		r.svc.Wait()

		require.Equal(t, transfer.FailurePermanent, r.get(id).FailureClass, side)
		require.Len(t, sent.events, 1, side)
		require.Equal(t, r.source.ID, sent.events[0].InstanceID, side)
		prefix := "Torrent: " + showName + "\nTarget: " + target + "\nError: "
		require.True(t, strings.HasPrefix(sent.events[0].Message, prefix), "%s: %s", side, sent.events[0].Message)
	}
}

type events struct {
	mu     sync.Mutex
	events []notifications.Event
}

func (e *events) Notify(_ context.Context, event notifications.Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
}
