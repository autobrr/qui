// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
// Cancel ends an Export run while it waits for the target to list the torrent
// that it added: the torrent comes off the target, and its files stay there.
// The rule gets the cancel as the result. When qui stops right after the
// removal, the run ends as cancelled after the restart and adds nothing.
func TestCancelTakesTheTorrentOfAnExportRunOffTheTarget(t *testing.T) {
	r := newRig(t, func(c *transfer.Config) { c.ExportPolls, c.ExportPollInterval = 2, time.Hour })
	var stopped atomic.Bool
	r.restartOn(stopWrites{Querier: r.db, stopped: &stopped})
	s := newShow(t, showName)
	r.seed(s)
	write(t, filepath.Join(r.dstDir, showName), s.files)
	r.dst.ListAfterSyncs = 1
	// Both hooks run under the lock of the stub, so polled closes at the
	// first sync after the add.
	added, polled := false, make(chan struct{})
	r.dst.OnAdd = func(string) { added = true }
	r.dst.OnSync = func() {
		if added {
			added = false
			close(polled)
		}
	}
	r.dst.OnWrite = func(endpoint string) {
		if endpoint == "torrents/delete" {
			stopped.Store(true)
		}
	}

	id, done, err := r.svc.Export(t.Context(), r.exportRequest(s))
	require.NoError(t, err)
	<-polled
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

// pauseRead holds the next read of the torrents of a Transfer, once armed,
// until release closes.
type pauseRead struct {
	dbinterface.Querier
	armed   *atomic.Bool
	held    chan struct{}
	release chan struct{}
}

func (p pauseRead) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "FROM torrent_transfer_members") && p.armed.CompareAndSwap(true, false) {
		close(p.held)
		<-p.release
	}
	return p.Querier.QueryContext(ctx, query, args...)
}

// regression: #4242
// A cancel that read an Export run just before the run ended leaves the done
// run as it is, with its torrent on the target.
func TestCancelLeavesAnExportRunThatEndedMeanwhile(t *testing.T) {
	r := newRig(t, exportPolls)
	hold := newHoldAt(r.db, "added")
	var armed atomic.Bool
	pause := pauseRead{Querier: hold, armed: &armed, held: make(chan struct{}), release: make(chan struct{})}
	r.restartOn(pause)
	s := newShow(t, showName)
	r.seed(s)
	write(t, filepath.Join(r.dstDir, showName), s.files)

	id, done, err := r.svc.Export(t.Context(), r.exportRequest(s))
	require.NoError(t, err)
	<-hold.held
	armed.Store(true)
	cancelled := make(chan error, 1)
	go func() { cancelled <- r.svc.Cancel(t.Context(), id) }()
	<-pause.held
	close(hold.release)
	require.Empty(t, (<-done).Failure)
	r.svc.Wait()
	close(pause.release)

	require.ErrorIs(t, <-cancelled, transfer.ErrFinished)
	r.svc.Wait()
	require.Equal(t, transfer.StateDone, r.get(id).State)
	require.True(t, r.dst.Has(s.hash), "the torrent of the done run stays on the target")
}

// regression: #4242
// A cancel that read a Transfer just before the Transfer ended leaves the
// done Transfer as it is, with its torrent on the target.
func TestCancelLeavesATransferThatEndedMeanwhile(t *testing.T) {
	r := newRig(t)
	hold := newHoldAt(r.db, "added")
	var armed atomic.Bool
	pause := pauseRead{Querier: hold, armed: &armed, held: make(chan struct{}), release: make(chan struct{})}
	r.restartOn(pause)
	s := newShow(t, showName)
	r.seed(s)

	id, err := r.svc.Start(t.Context(), r.request(s))
	require.NoError(t, err)
	<-hold.held
	armed.Store(true)
	cancelled := make(chan error, 1)
	go func() { cancelled <- r.svc.Cancel(t.Context(), id) }()
	<-pause.held
	close(hold.release)
	r.svc.Wait()
	close(pause.release)

	require.ErrorIs(t, <-cancelled, transfer.ErrFinished)
	r.svc.Wait()
	require.Equal(t, transfer.StateDone, r.get(id).State)
	require.True(t, r.dst.Has(s.hash), "the torrent of the done Transfer stays on the target")
}

// holdCreate holds the first write of a Transfer after its commit, once
// armed, until release closes.
type holdCreate struct {
	dbinterface.Querier
	armed   *atomic.Bool
	held    chan struct{}
	release chan struct{}
}

func (h holdCreate) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	tx, err := h.Querier.BeginTx(ctx, opts)
	if err != nil || !h.armed.CompareAndSwap(true, false) {
		return tx, err
	}
	return heldCommit{TxQuerier: tx, held: h.held, release: h.release}, nil
}

type heldCommit struct {
	dbinterface.TxQuerier
	held    chan struct{}
	release chan struct{}
}

func (c heldCommit) Commit() error {
	err := c.TxQuerier.Commit()
	close(c.held)
	<-c.release
	return err
}

// failTx refuses each transaction while failing is set: the record of the
// end of a Transfer fails, and a save of its steps goes through.
type failTx struct {
	dbinterface.Querier
	failing *atomic.Bool
}

func (f failTx) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	if f.failing.Load() {
		return nil, errors.New("database is locked")
	}
	return f.Querier.BeginTx(ctx, opts)
}

// regression: #4242
// A cancel that ends an Export run between its record and its start keeps
// it cancelled: the run adds nothing to the target. When the cancel saved
// its cleanup but could not record the end, the run finishes the cancel.
func TestCancelBeforeAnExportRunStartsKeepsItCancelled(t *testing.T) {
	for _, tc := range []struct {
		name        string
		finishFails bool
	}{
		{name: "the cancel records the end"},
		{name: "the record of the end fails", finishFails: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, exportPolls)
			var armed, failing atomic.Bool
			hold := holdCreate{Querier: r.db, armed: &armed, held: make(chan struct{}), release: make(chan struct{})}
			r.restartOn(failTx{Querier: hold, failing: &failing})
			s := newShow(t, showName)
			r.seed(s)
			write(t, filepath.Join(r.dstDir, showName), s.files)

			armed.Store(true)
			type started struct {
				done <-chan transfer.ExportResult
				err  error
			}
			export := make(chan started, 1)
			go func() {
				_, done, err := r.svc.Export(t.Context(), r.exportRequest(s))
				export <- started{done, err}
			}()
			<-hold.held
			ids, err := r.store.Unfinished(t.Context())
			require.NoError(t, err)
			require.Len(t, ids, 1)
			failing.Store(tc.finishFails)
			require.NoError(t, r.svc.Cancel(t.Context(), ids[0]))
			r.svc.Wait()
			failing.Store(false)
			require.Equal(t, tc.finishFails, r.get(ids[0]).FinishedAt.IsZero())
			close(hold.release)
			run := <-export
			require.NoError(t, run.err)
			<-run.done
			r.svc.Wait()

			record := r.get(ids[0])
			require.Equal(t, transfer.StateCancelled, record.State)
			require.False(t, record.FinishedAt.IsZero())
			require.False(t, r.dst.Has(s.hash), "the cancelled run adds nothing")
		})
	}
}

// regression: #4242
// A cancel that comes while an Export run records its end leaves the run as
// it ends, with its torrent on the target, and answers that the run ended.
func TestCancelLeavesAnExportRunThatRecordsItsEnd(t *testing.T) {
	r := newRig(t, exportPolls)
	hold := newHoldAt(r.db, "done")
	r.restartOn(hold)
	s := newShow(t, showName)
	r.seed(s)
	write(t, filepath.Join(r.dstDir, showName), s.files)

	id, done, err := r.svc.Export(t.Context(), r.exportRequest(s))
	require.NoError(t, err)
	<-hold.held
	err = r.svc.Cancel(t.Context(), id)
	close(hold.release)
	require.ErrorIs(t, err, transfer.ErrFinished)
	require.Empty(t, (<-done).Failure)
	r.svc.Wait()

	require.Equal(t, transfer.StateDone, r.get(id).State)
	require.True(t, r.dst.Has(s.hash), "the torrent of the done run stays on the target")
}

// Retry keeps the category, the tags, and the management mode of the torrent
// on the target, also when the torrent of the request went to the Cross-seed
// instance. A cancelled Transfer starts again as a new Transfer by the user,
// with the same choices, and the old row stays.
func TestRetryKeepsTheCategoryOfTheTarget(t *testing.T) {
	r := newRig(t)
	r.dst.AutoManagement = true
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
	r.dst.Edit(crossSeedHash, func(landed *qbtstub.Torrent) {
		require.True(t, landed.AutoTMM, "the target manages the torrent as before the retry")
	})
	require.True(t, r.src.Has(s.hash), "the source keeps seeding")
	require.Equal(t, transfer.StateCancelled, r.get(failed.ID).State, "the cancelled row stays")
}

// A Retry of a Transfer whose request named no save path names none either.
// The new Transfer goes to the folder that the target has for the category by
// then, also for a group that stays on manual management.
// regression: #4242
func TestRetryUsesTheFolderOfTheCategoryAgain(t *testing.T) {
	for _, tc := range []struct {
		name      string
		automatic bool
	}{
		{name: "the target adds on manual management"},
		{name: "the target adds on automatic management", automatic: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.dst.AutoManagement = tc.automatic
			r.dst.AddCategory("archive", filepath.Join(r.dstDir, "full"))
			pack := newPack(t)
			r.seed(pack)
			episode, episodeHash := singleFileTorrent(t, episode2, pack.files[episode2])
			r.src.Seed(episode, r.srcPath(showName))
			r.dst.Reject = http.StatusInternalServerError
			failed := r.run(transfer.Request{
				SourceInstanceID: r.source.ID, Hash: episodeHash, TargetInstanceID: r.target.ID,
				Placement: transfer.Placement{Category: "archive"},
			})
			require.NoError(t, r.svc.Cancel(t.Context(), failed.ID))
			r.svc.Wait()
			require.Equal(t, transfer.StateCancelled, r.get(failed.ID).State)
			r.dst.Reject = 0
			r.dst.AddCategory("archive", filepath.Join(r.dstDir, "roomy"))

			id, err := r.svc.Retry(t.Context(), failed.ID)
			require.NoError(t, err)
			r.svc.Wait()

			record := r.get(id)
			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Equal(t, contents(pack, path.Join("roomy", showName)), tree(t, r.dstDir))
		})
	}
}
