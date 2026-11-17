// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
)

// Retry runs a Transfer that waits for its retry at once, as the same
// Transfer, with the copy that it kept.
func TestRetryRunsATemporaryFailureNow(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	r.dst.Reject = http.StatusInternalServerError
	failed := r.run(r.request(s))
	require.Equal(t, transfer.FailureTemporary, failed.FailureClass, failed.FailureReason)
	r.dst.Reject = 0

	id, err := r.svc.Retry(t.Context(), failed.ID)
	require.NoError(t, err)
	r.svc.Wait()

	require.Equal(t, failed.ID, id, "the same Transfer runs again")
	record := r.get(id)
	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.True(t, r.dst.Has(s.hash))
	require.False(t, r.src.Has(s.hash))
}

// holdFirstRetryWrite holds the first write of retry_at, before it reaches
// the database, until release closes.
type holdFirstRetryWrite struct {
	dbinterface.Querier
	first   *atomic.Bool
	held    chan struct{}
	release chan struct{}
}

func (h holdFirstRetryWrite) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if strings.Contains(query, "retry_at") && h.first.CompareAndSwap(true, false) {
		close(h.held)
		<-h.release
	}
	return h.Querier.ExecContext(ctx, query, args...)
}

// regression: #4242
// Two Retries of a Transfer that waits for its retry come at once. The one
// that writes last keeps the steps that the running Transfer saved.
func TestTwoRetriesKeepTheStepsOfTheRun(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	r.dst.Reject = http.StatusInternalServerError
	failed := r.run(r.request(s))
	require.Equal(t, transfer.FailureTemporary, failed.FailureClass, failed.FailureReason)
	r.dst.Reject = 0
	hold := newHoldAt(r.db, "added")
	var first atomic.Bool
	late := holdFirstRetryWrite{Querier: hold, first: &first, held: make(chan struct{}), release: make(chan struct{})}
	r.restartOn(late)

	first.Store(true)
	retried := make(chan error, 1)
	go func() {
		_, err := r.svc.Retry(t.Context(), failed.ID)
		retried <- err
	}()
	<-late.held
	_, err := r.svc.Retry(t.Context(), failed.ID)
	require.NoError(t, err)
	<-hold.held
	close(late.release)
	require.NoError(t, <-retried)

	steps := r.get(failed.ID).Steps
	require.Equal(t, "added", steps[len(steps)-1].Name, "the late Retry keeps the steps of the run")
	close(hold.release)
	r.svc.Wait()
	require.Equal(t, transfer.StateDone, r.get(failed.ID).State)
}

// Retry starts a Transfer that failed for good again, as a new Transfer with
// the same target and choices, and the old row stays.
func TestRetryStartsAPermanentFailureAgain(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	write(t, filepath.Join(r.dstDir, showName), map[string][]byte{"Show.S01E02.mkv": []byte("theirs")})
	req := r.request(s)
	req.Placement.Category = "tv"
	req.Options = transfer.Options{After: transfer.AfterKeepSeeding, LeaveStopped: true}
	failed := r.run(req)
	require.Equal(t, transfer.FailurePermanent, failed.FailureClass, failed.FailureReason)
	write(t, filepath.Join(r.dstDir, showName), map[string][]byte{"Show.S01E02.mkv": s.files["Show.S01E02.mkv"]})

	id, err := r.svc.Retry(t.Context(), failed.ID)
	require.NoError(t, err)
	r.svc.Wait()

	require.NotEqual(t, failed.ID, id)
	record := r.get(id)
	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, transfer.StartedByUser, record.StartedBy)
	require.Equal(t, transfer.AfterKeepSeeding, record.Options.After)
	require.True(t, record.Options.LeaveStopped)
	require.Equal(t, "tv", r.dst.LastParams("torrents/add").Get("category"))
	require.True(t, r.src.Has(s.hash), "the source keeps seeding")
	require.Equal(t, transfer.StateFailed, r.get(failed.ID).State, "the failed row stays")
}

// A done Transfer has no Retry.
func TestRetryRefusesADoneTransfer(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	done := r.run(r.request(s))
	require.Equal(t, transfer.StateDone, done.State, done.FailureReason)

	_, err := r.svc.Retry(t.Context(), done.ID)
	require.ErrorIs(t, err, transfer.ErrNotRetryable)
}

// Retry starts a failed Export run again as a new Export run of the same
// rule.
func TestRetryStartsAFailedExportAgain(t *testing.T) {
	r := newRig(t, exportPolls)
	s := newShow(t, showName)
	r.seed(s)
	write(t, filepath.Join(r.dstDir, showName), map[string][]byte{"Show.S01E01.mkv": s.files["Show.S01E01.mkv"]})
	failed, res := r.export(r.exportRequest(s))
	require.NotEmpty(t, res.Failure)
	write(t, filepath.Join(r.dstDir, showName), s.files)
	// The Retry reads the target fresh, as the sync of qui does by then.
	require.False(t, r.seeding(r.target.ID, s.hash))

	id, err := r.svc.Retry(t.Context(), failed.ID)
	require.NoError(t, err)
	r.svc.Wait()

	require.NotEqual(t, failed.ID, id)
	record := r.get(id)
	require.Equal(t, transfer.KindExport, record.Kind)
	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, 7, *record.RuleID)
	require.True(t, r.seeding(r.target.ID, s.hash))
}

// Cancel ends an Export run. A torrent that the run added comes off the
// target, and its files stay there. The rule gets the cancel as the result.
func TestCancelEndsAnExportRun(t *testing.T) {
	for _, tc := range []struct {
		at       string
		onTarget bool
	}{
		{at: "add"},
		{at: "added", onTarget: true},
	} {
		t.Run("at "+tc.at, func(t *testing.T) {
			r := newRig(t, exportPolls)
			hold := newHoldAt(r.db, tc.at)
			r.restartOn(hold)
			s := newShow(t, showName)
			r.seed(s)
			write(t, filepath.Join(r.dstDir, showName), s.files)

			id, done, err := r.svc.Export(t.Context(), r.exportRequest(s))
			require.NoError(t, err)
			<-hold.held
			require.Equal(t, tc.onTarget, r.dst.Has(s.hash))
			require.NoError(t, r.svc.Cancel(t.Context(), id))
			res, ok := <-done
			r.svc.Wait()

			require.True(t, ok, "the rule gets a result")
			require.Equal(t, "Cancelled.", res.Failure)
			record := r.get(id)
			require.Equal(t, transfer.StateCancelled, record.State)
			require.False(t, record.FinishedAt.IsZero())
			require.False(t, r.dst.Has(s.hash))
			require.Equal(t, contents(s, showName), tree(t, r.dstDir), "the files stay on the target")
			require.True(t, r.src.Has(s.hash))
		})
	}
}

// stopWrites refuses each write once stopped is set, as a qui that stopped
// writes nothing more.
type stopWrites struct {
	dbinterface.Querier
	stopped *atomic.Bool
}

func (s stopWrites) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if s.stopped.Load() {
		return nil, errors.New("qui stopped")
	}
	return s.Querier.ExecContext(ctx, query, args...)
}

func (s stopWrites) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	if s.stopped.Load() {
		return nil, errors.New("qui stopped")
	}
	return s.Querier.BeginTx(ctx, opts)
}

// regression: #4242
// qui stops right after a cancel took the torrent of an Export run off the
// target. After the restart, the run ends as cancelled and adds nothing.
func TestACancelledExportRunStaysCancelledAfterAStop(t *testing.T) {
	r := newRig(t, exportPolls)
	hold := newHoldAt(r.db, "added")
	var stopped atomic.Bool
	r.restartOn(stopWrites{Querier: hold, stopped: &stopped})
	s := newShow(t, showName)
	r.seed(s)
	write(t, filepath.Join(r.dstDir, showName), s.files)
	r.dst.OnWrite = func(endpoint string) {
		if endpoint == "torrents/delete" {
			stopped.Store(true)
		}
	}

	id, done, err := r.svc.Export(t.Context(), r.exportRequest(s))
	require.NoError(t, err)
	<-hold.held
	require.NoError(t, r.svc.Cancel(t.Context(), id))
	<-done
	require.True(t, stopped.Load(), "the cancel took the torrent off the target")
	r.restart()
	r.svc.Wait()

	record := r.get(id)
	require.Equal(t, transfer.StateCancelled, record.State, record.FailureReason)
	require.False(t, record.FinishedAt.IsZero())
	require.False(t, r.dst.Has(s.hash))
	require.Equal(t, 1, r.dst.Writes("torrents/add"), "the restart adds nothing")
}

// Retry places a group where the first Transfer placed it, also when the
// torrent of the request sits in a subfolder of the group.
func TestRetryPlacesTheGroupWhereItWas(t *testing.T) {
	r := newRigOn(t, onDisk, onDisk)
	s := newShow(t, showName)
	r.seed(s)
	_, crossSeedHash := r.seedHardlinkedCrossSeed(s)
	sourceFiles := tree(t, r.srcDir)
	write(t, filepath.Join(r.dstDir, showName), map[string][]byte{"Show.S01E02.mkv": []byte("theirs")})
	failed := r.run(transfer.Request{SourceInstanceID: r.source.ID, Hash: crossSeedHash, TargetInstanceID: r.target.ID})
	require.Equal(t, transfer.FailurePermanent, failed.FailureClass, failed.FailureReason)
	require.NoError(t, os.RemoveAll(filepath.Join(r.dstDir, showName)))

	id, err := r.svc.Retry(t.Context(), failed.ID)
	require.NoError(t, err)
	r.svc.Wait()

	record := r.get(id)
	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, sourceFiles, tree(t, r.dstDir))
}

// Retry keeps the category and tags of the torrent on the target, also when
// the torrent of the request went to the Cross-seed instance. A cancelled
// Transfer starts again as a new Transfer.
func TestRetryKeepsTheCategoryOfTheTarget(t *testing.T) {
	r := newRig(t)
	cs := r.withCrossSeedInstance()
	cs.refuse = "no_match"
	s := newShow(t, showName)
	r.seed(s)
	crossSeed, crossSeedHash := crossSeedOf(t, s)
	r.src.Seed(crossSeed, r.srcPath())
	r.src.Edit(crossSeedHash, func(t *qbtstub.Torrent) { t.AddedOn = 100 })
	r.src.Edit(s.hash, func(t *qbtstub.Torrent) { t.AddedOn = 200 })
	req := r.request(s)
	req.Placement.Category = "tv"
	failed := r.run(req)
	require.NoError(t, r.svc.Cancel(t.Context(), failed.ID))
	r.svc.Wait()
	require.Equal(t, transfer.StateCancelled, r.get(failed.ID).State)
	cs.refuse = ""

	id, err := r.svc.Retry(t.Context(), failed.ID)
	require.NoError(t, err)
	r.svc.Wait()

	require.Equal(t, transfer.StateDone, r.get(id).State, r.get(id).FailureReason)
	require.Equal(t, "tv", r.dst.LastParams("torrents/add").Get("category"))
}
