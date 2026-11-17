// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
)

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
// Retry runs a Transfer that waits for its retry at once, as the same
// Transfer. When two Retries come at once, the one that writes last keeps the
// steps that the running Transfer saved.
func TestRetryRunsATemporaryFailureNow(t *testing.T) {
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
	id, err := r.svc.Retry(t.Context(), failed.ID)
	require.NoError(t, err)
	require.Equal(t, failed.ID, id, "the same Transfer runs again")
	<-hold.held
	close(late.release)
	require.NoError(t, <-retried)

	steps := r.get(failed.ID).Steps
	require.Equal(t, "added", steps[len(steps)-1].Name, "the late Retry keeps the steps of the run")
	close(hold.release)
	r.svc.Wait()
	record := r.get(id)
	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.True(t, r.dst.Has(s.hash))
	require.False(t, r.src.Has(s.hash))
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

// Cancel ends an Export run before its add. The rule gets the cancel as the
// result.
func TestCancelEndsAnExportRun(t *testing.T) {
	r := newRig(t, exportPolls)
	hold := newHoldAt(r.db, "add")
	r.restartOn(hold)
	s := newShow(t, showName)
	r.seed(s)
	write(t, filepath.Join(r.dstDir, showName), s.files)

	id, done, err := r.svc.Export(t.Context(), r.exportRequest(s))
	require.NoError(t, err)
	<-hold.held
	require.False(t, r.dst.Has(s.hash))
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
// Cancel ends an Export run after its add: the torrent comes off the target,
// and its files stay there. The rule gets the cancel as the result. When qui
// stops right after the removal, the run ends as cancelled after the restart
// and adds nothing.
func TestCancelTakesTheTorrentOfAnExportRunOffTheTarget(t *testing.T) {
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
	require.True(t, r.dst.Has(s.hash))
	require.NoError(t, r.svc.Cancel(t.Context(), id))
	res, ok := <-done
	require.True(t, ok, "the rule gets a result")
	require.Equal(t, "Cancelled.", res.Failure)
	require.True(t, stopped.Load(), "the cancel took the torrent off the target")
	r.restart()
	r.svc.Wait()

	record := r.get(id)
	require.Equal(t, transfer.StateCancelled, record.State, record.FailureReason)
	require.False(t, record.FinishedAt.IsZero())
	require.False(t, r.dst.Has(s.hash))
	require.Equal(t, 1, r.dst.Writes("torrents/add"), "the restart adds nothing")
	require.Equal(t, contents(s, showName), tree(t, r.dstDir), "the files stay on the target")
	require.True(t, r.src.Has(s.hash))
}

// Retry keeps the category and tags of the torrent on the target, also when
// the torrent of the request went to the Cross-seed instance. A cancelled
// Transfer starts again as a new Transfer by the user, with the same choices,
// and the old row stays.
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
	req.Options = transfer.Options{After: transfer.AfterKeepSeeding, LeaveStopped: true}
	failed := r.run(req)
	require.NoError(t, r.svc.Cancel(t.Context(), failed.ID))
	r.svc.Wait()
	require.Equal(t, transfer.StateCancelled, r.get(failed.ID).State)
	cs.refuse = ""

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
	require.Equal(t, transfer.StateCancelled, r.get(failed.ID).State, "the cancelled row stays")
}
