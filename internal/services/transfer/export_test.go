// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/sshtest"
)

// exportPolls shortens the wait of an Export run for the target.
func exportPolls(c *transfer.Config) {
	c.ExportPolls, c.ExportPollInterval = 20, time.Millisecond
}

func (r *rig) exportRequest(s show) transfer.ExportRequest {
	return transfer.ExportRequest{
		SourceInstanceID: r.source.ID,
		TargetInstanceID: r.target.ID,
		Hash:             s.hash,
		Name:             showName,
		Placement:        transfer.Placement{SavePath: r.dstDir},
		SkipChecking:     true,
		RuleID:           7,
		RuleName:         "export",
	}
}

// export runs an Export and waits for its end.
func (r *rig) export(req transfer.ExportRequest) (*transfer.Record, transfer.ExportResult) {
	r.t.Helper()
	id, done, err := r.svc.Export(r.t.Context(), req)
	require.NoError(r.t, err)
	res, ok := <-done
	require.True(r.t, ok, "the Export run ended")
	r.svc.Wait()
	return r.get(id), res
}

// An Export adds the torrent on the target, where its files already are,
// and keeps the torrent and its files on the source. It runs on the
// qBittorrent versions that Export supported before Transfer, and it shows
// as an Export run in the list of Transfers.
func TestExportAddsTheTorrentAndKeepsTheSource(t *testing.T) {
	r := newRig(t, exportPolls)
	// qBittorrent 4.6, older than Transfer needs.
	r.src.WebAPIVersion, r.dst.WebAPIVersion = "2.9.3", "2.9.3"
	s := newShow(t, showName)
	r.seed(s)
	write(t, filepath.Join(r.dstDir, showName), s.files)

	record, res := r.export(r.exportRequest(s))

	require.Empty(t, res.Failure)
	require.False(t, res.AlreadyOnTarget)
	require.Equal(t, transfer.KindExport, record.Kind)
	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.False(t, record.FinishedAt.IsZero())
	require.Equal(t, 7, *record.RuleID)
	require.True(t, r.seeding(r.target.ID, s.hash), "the target seeds the torrent")
	add := r.dst.LastParams("torrents/add")
	require.Equal(t, r.dstDir, add.Get("savepath"))
	require.Equal(t, "false", add.Get("autoTMM"))
	require.Equal(t, "true", add.Get("seedMode"), "the Export skips the check")
	require.True(t, r.seeding(r.source.ID, s.hash), "the source seeds on")
	require.Equal(t, contents(s, showName), tree(t, r.srcDir))
	require.Zero(t, r.src.Writes("torrents/delete"))
}

// deletes records who deletes torrents.
type deletes struct {
	mu     sync.Mutex
	actors []qbittorrent.DeleteActor
}

func (d *deletes) NoteDelete(_ int, _ []string, actor qbittorrent.DeleteActor) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.actors = append(d.actors, actor)
}

func (d *deletes) list() []qbittorrent.DeleteActor {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.actors)
}

// An Export whose files are not all on the target fails. qui takes the
// torrent off the target in the name of the rule, and keeps the files there.
// The failed run never runs again by itself: the next run of the rule starts
// a new one.
func TestExportFailsAndTheNextRuleRunTriesAgain(t *testing.T) {
	r := newRig(t, exportPolls)
	noted := &deletes{}
	r.sync.SetDeleteNoter(noted)
	s := newShow(t, showName)
	r.seed(s)
	half := map[string][]byte{"Show.S01E01.mkv": s.files["Show.S01E01.mkv"]}
	write(t, filepath.Join(r.dstDir, showName), half)

	record, res := r.export(r.exportRequest(s))

	reason := "Files missing on target instance (savePath: " + r.dstDir + ")"
	require.Equal(t, reason, res.Failure)
	require.Equal(t, transfer.StateFailed, record.State)
	require.Equal(t, reason, record.FailureReason)
	require.False(t, record.FinishedAt.IsZero())
	require.False(t, r.dst.Has(s.hash), "qui takes the failed torrent off the target")
	require.Equal(t, "false", r.dst.LastParams("torrents/delete").Get("deleteFiles"))
	require.Equal(t, []qbittorrent.DeleteActor{{Kind: qbittorrent.DeleteActorAutomation, RuleName: "export"}}, noted.list())
	require.Equal(t, map[string]string{showName + "/Show.S01E01.mkv": string(half["Show.S01E01.mkv"])}, tree(t, r.dstDir))
	require.True(t, r.seeding(r.source.ID, s.hash), "the source seeds on")

	r.setClock(clock.Add(2 * time.Hour))
	r.svc.Wake()
	r.svc.Wait()
	require.Equal(t, 1, r.dst.Writes("torrents/add"), "the failed run does not run again")

	// The next run of the rule reads the target fresh.
	require.False(t, r.seeding(r.target.ID, s.hash))
	write(t, filepath.Join(r.dstDir, showName), s.files)
	again, res := r.export(r.exportRequest(s))
	require.Empty(t, res.Failure)
	require.Equal(t, transfer.StateDone, again.State, again.FailureReason)
	require.True(t, r.seeding(r.target.ID, s.hash))
}

// holdAt holds each save of a step until release closes or the run stops.
// held gets a value at each held save.
type holdAt struct {
	dbinterface.Querier
	step    string
	held    chan struct{}
	release chan struct{}
}

func newHoldAt(db dbinterface.Querier, step string) holdAt {
	return holdAt{Querier: db, step: step, held: make(chan struct{}, 16), release: make(chan struct{})}
}

func (h holdAt) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	res, err := h.Querier.ExecContext(ctx, query, args...)
	if lastStep(query, args) == h.step {
		h.held <- struct{}{}
		select {
		case <-h.release:
		case <-ctx.Done():
		}
	}
	return res, err
}

// restartOn starts qui again with its store on q.
func (r *rig) restartOn(q dbinterface.Querier) {
	r.t.Helper()
	closeService(r.t, r.svc)
	store, err := transfer.NewStore(r.t.Context(), q)
	require.NoError(r.t, err)
	r.store, r.deps.Store = store, store
	r.start()
}

func (r *rig) states(ids []int64) map[transfer.State]int {
	r.t.Helper()
	out := map[transfer.State]int{}
	for _, id := range ids {
		out[r.get(id).State]++
	}
	return out
}

// Five Export runs go at a time, and the sixth waits for a free place. They
// do not wait for the Transfer that copies from the same source, and hold
// no claim on their torrents.
func TestExportRunsFiveAtATimeOutsideTheTransferQueue(t *testing.T) {
	r := newRig(t, exportPolls)
	hold := newHoldAt(r.db, "add")
	r.restartOn(hold)
	moving := newShow(t, showName)
	r.seed(moving)
	copying, copyHeld := make(chan struct{}), make(chan struct{})
	releaseCopy := sync.OnceFunc(func() { close(copyHeld) })
	// The release comes before the stop of qui, also when the test fails.
	t.Cleanup(releaseCopy)
	holdCopy := sync.OnceFunc(func() {
		close(copying)
		<-copyHeld
	})
	r.disk.onWrite = func(string) { holdCopy() }
	transferID, err := r.svc.Start(t.Context(), r.request(moving))
	require.NoError(t, err)
	<-copying

	const exports = 6
	ids := make([]int64, 0, exports)
	results := make([]<-chan transfer.ExportResult, 0, exports)
	hashes := make([]string, 0, exports)
	for i := range exports {
		name := fmt.Sprintf("Other.Show.S%02d.720p-GRP", i+1)
		s := newShow(t, name)
		r.src.Seed(s.data, r.srcPath())
		write(t, filepath.Join(r.dstDir, name), s.files)
		req := r.exportRequest(s)
		req.Name = name
		id, done, err := r.svc.Export(t.Context(), req)
		require.NoError(t, err)
		ids, results, hashes = append(ids, id), append(results, done), append(hashes, s.hash)
	}
	for range 5 {
		<-hold.held
	}

	require.Equal(t, map[transfer.State]int{transfer.StateFinishing: 5, transfer.StateWaiting: 1}, r.states(ids))
	require.Equal(t, transfer.StateCopying, r.get(transferID).State)
	for _, hash := range hashes {
		require.False(t, r.store.IsClaimed(r.source.ID, hash), "an Export run claims nothing")
	}
	close(hold.release)
	for _, done := range results {
		res := <-done
		require.Empty(t, res.Failure)
	}
	require.Equal(t, map[transfer.State]int{transfer.StateDone: 6}, r.states(ids))
	releaseCopy()
	r.svc.Wait()
	transferred := r.get(transferID)
	require.Equal(t, transfer.StateDone, transferred.State, transferred.FailureReason)
}

// qui ends at each step of an Export run that it records, and at the add.
// After the restart, the run adds the torrent once and checks it on the
// target, or finishes its cleanup. A failed torrent whose add did not
// answer stays on the target, because qui cannot tell who added it. A
// second restart sends nothing.
func TestExportResumesAfterACrash(t *testing.T) {
	for _, tc := range []struct {
		at        string
		killAtAdd bool
		// missing leaves the files off the target.
		missing bool
		// recheckFails makes the add paused, with a check, and makes the
		// recheck after the restart fail.
		recheckFails bool
		want         transfer.State
		// kept, when set, is a part of the failure reason of a run that
		// keeps the failed torrent on the target.
		kept string
	}{
		{at: "after:add", want: transfer.StateDone},
		{killAtAdd: true, want: transfer.StateDone},
		// regression: #4231
		{killAtAdd: true, missing: true, want: transfer.StateFailed, kept: "Files missing on target instance"},
		// regression: #4231
		{killAtAdd: true, recheckFails: true, want: transfer.StateFailed, kept: "Recheck on target failed"},
		{at: "after:added", missing: true, want: transfer.StateFailed},
		{at: "after:clean up", missing: true, want: transfer.StateFailed},
		{at: "after:done", want: transfer.StateDone},
	} {
		name := tc.at
		if tc.killAtAdd {
			name = "kill at the add"
		}
		if tc.missing {
			name += ", files missing"
		}
		if tc.recheckFails {
			name += ", the recheck fails"
		}
		t.Run(name, func(t *testing.T) {
			r := newRig(t, exportPolls)
			s := newShow(t, showName)
			r.seed(s)
			if !tc.missing {
				write(t, filepath.Join(r.dstDir, showName), s.files)
			}
			req := r.exportRequest(s)
			if tc.recheckFails {
				req.Paused, req.SkipChecking = true, false
			}
			child := make(chan *os.Process, 1)
			r.dst.SetOnAdd(func(string) {
				if tc.killAtAdd {
					require.NoError(t, (<-child).Kill())
				}
			})
			runCrashChild(t, crash{DB: r.dbPath, Export: &req, At: tc.at}, child, tc.killAtAdd)
			if tc.recheckFails {
				r.dst.OnWrite = func(endpoint string) {
					r.dst.Reject = 0
					if endpoint == "torrents/recheck" {
						r.dst.Reject = http.StatusInternalServerError
					}
				}
			}

			r.restart()
			r.svc.Wait()

			var id int64
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT id FROM torrent_transfers`).Scan(&id))
			record := r.get(id)
			require.False(t, record.FinishedAt.IsZero(), "the Export run ended after the restart")
			require.Equal(t, tc.want, record.State, record.FailureReason)
			require.Equal(t, 1, r.dst.Writes("torrents/add"), "qui adds the torrent once")
			switch {
			case tc.want == transfer.StateDone:
				require.True(t, r.seeding(r.target.ID, s.hash))
			case tc.kept != "":
				require.Contains(t, record.FailureReason, "Ownership unknown")
				require.Contains(t, record.FailureReason, tc.kept)
				require.True(t, r.dst.Has(s.hash), "qui keeps the torrent that it may not have added")
				require.Zero(t, r.dst.Writes("torrents/delete"))
				require.Zero(t, countSteps(record, "clean up"))
			default:
				require.Equal(t, "Files missing on target instance (savePath: "+r.dstDir+")", record.FailureReason)
				require.False(t, r.dst.Has(s.hash), "qui takes the failed torrent off the target")
				require.Equal(t, 1, r.dst.Writes("torrents/delete"))
				require.Equal(t, 1, countSteps(record, "clean up"))
			}
			require.True(t, r.seeding(r.source.ID, s.hash), "the source seeds on")

			src, dst := sent(r.src), sent(r.dst)
			r.restart()
			r.svc.Wait()
			require.Equal(t, src, sent(r.src))
			require.Equal(t, dst, sent(r.dst))
		})
	}
}

// wakeAtCommit wakes the Service after each commit on the store, as the
// loop of Wake can between the record of an Export run and its start.
type wakeAtCommit struct {
	dbinterface.Querier
	wake func()
}

func (w wakeAtCommit) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	tx, err := w.Querier.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return wakeTx{TxQuerier: tx, wake: w.wake}, nil
}

type wakeTx struct {
	dbinterface.TxQuerier
	wake func()
}

func (t wakeTx) Commit() error {
	err := t.TxQuerier.Commit()
	if err == nil {
		t.wake()
	}
	return err
}

// A Wake that reads a new Export run before its Export call starts it
// leaves the run to that call, so the caller gets the result.
// regression: #4231
func TestExportSendsItsResultWhenAWakeComesFirst(t *testing.T) {
	r := newRig(t, exportPolls)
	r.restartOn(wakeAtCommit{Querier: r.db, wake: func() { r.svc.Wake() }})
	s := newShow(t, showName)
	r.seed(s)
	write(t, filepath.Join(r.dstDir, showName), s.files)

	record, res := r.export(r.exportRequest(s))

	require.Empty(t, res.Failure)
	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, 1, r.dst.Writes("torrents/add"))
}

// qBittorrent does not check a torrent that was added stopped. When qui
// stops between a paused add and its recheck, the run sends the recheck
// after the restart, and the torrent passes its check on the target.
// regression: #4231
func TestExportRechecksAPausedAddAfterACrash(t *testing.T) {
	r := newRig(t, exportPolls)
	s := newShow(t, showName)
	r.seed(s)
	write(t, filepath.Join(r.dstDir, showName), s.files)
	req := r.exportRequest(s)
	req.Paused, req.SkipChecking = true, false
	child := make(chan *os.Process, 1)
	r.dst.SetOnAdd(func(string) { require.NoError(t, (<-child).Kill()) })
	runCrashChild(t, crash{DB: r.dbPath, Export: &req}, child, true)
	require.Zero(t, r.dst.Writes("torrents/recheck"), "qui stopped before the recheck")

	r.restart()
	r.svc.Wait()

	var id int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT id FROM torrent_transfers`).Scan(&id))
	record := r.get(id)
	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, 1, r.dst.Writes("torrents/add"), "qui adds the torrent once")
	require.Equal(t, 1, r.dst.Writes("torrents/recheck"))
	require.True(t, r.dst.Has(s.hash))
	require.True(t, r.dst.Stopped(s.hash), "the torrent stays paused")
}

// An Export run that resumes after a restart touches nothing on a target
// that got a new address: the torrent there is not the one that qui added.
// It stops before the add, after the add, and at its cleanup, also when it
// cannot read the address.
// regression: #4231
func TestExportTouchesNothingOnATargetWithANewAddress(t *testing.T) {
	for _, tc := range []struct {
		at string
		// has seeds the torrent on the client at the new address.
		has bool
		// unread fails each read of an instance by the Export run, as on a
		// database that does not answer, while the clients still find the
		// new address.
		unread bool
	}{
		{at: "after:add"},
		{at: "after:added", has: true},
		{at: "after:clean up", has: true},
		{at: "after:add", unread: true},
		{at: "after:added", has: true, unread: true},
	} {
		name := tc.at
		if tc.unread {
			name += " unread"
		}
		t.Run(name, func(t *testing.T) {
			r := newRig(t, exportPolls)
			s := newShow(t, showName)
			r.seed(s)
			req := r.exportRequest(s)
			runCrashChild(t, crash{DB: r.dbPath, Export: &req, At: tc.at}, make(chan *os.Process, 1), false)
			moved := qbtstub.New(t, r.dstDir)
			write(t, filepath.Join(r.dstDir, showName), s.files)
			if tc.has {
				moved.Seed(s.data, r.dstDir)
			}

			_, err := r.instances.Update(t.Context(), r.target.ID, "homelab", moved.URL, "", "", nil, nil, nil)
			require.NoError(t, err)

			r.restartWithNewClients(func(deps *transfer.Deps) {
				if tc.unread {
					deps.Instances, err = models.NewInstanceStore(unanswered{r.db}, sshtest.EncryptionKey())
					require.NoError(t, err)
				}
			})
			r.svc.Wait()

			var id int64
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT id FROM torrent_transfers`).Scan(&id))
			record := r.get(id)
			require.Equal(t, transfer.StateFailed, record.State, record.FailureReason)
			require.False(t, record.FinishedAt.IsZero())
			require.Equal(t, tc.has, moved.Has(s.hash), "qui adds nothing at the new address, and removes nothing there")
			for _, endpoint := range []string{"torrents/add", "torrents/recheck", "torrents/delete"} {
				require.Zero(t, moved.Writes(endpoint), endpoint)
			}
		})
	}
}

// unanswered is a database whose single-row reads fail.
type unanswered struct{ dbinterface.Querier }

func (u unanswered) QueryRowContext(ctx context.Context, _ string, _ ...any) *sql.Row {
	return u.Querier.QueryRowContext(ctx, "SELECT no_answer")
}

// The add on the target gets the parameters that the old Export path sends
// for the fields of the rule.
func TestExportAddsWithTheFieldsOfTheRule(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(r *rig, req *transfer.ExportRequest)
		want func(r *rig) url.Values
	}{
		{
			name: "paused to a save path",
			edit: func(r *rig, req *transfer.ExportRequest) {
				req.Placement = transfer.Placement{SavePath: r.dstDir, Category: "tv", Tags: []string{"one", "two"},
					ContentLayout: "NoSubfolder"}
				req.Paused, req.SkipChecking = true, false
			},
			want: func(r *rig) url.Values {
				return url.Values{"autoTMM": {"false"}, "savepath": {r.dstDir}, "category": {"tv"}, "tags": {"one,two"},
					"paused": {"true"}, "stopped": {"true"}, "contentLayout": {"NoSubfolder"}}
			},
		},
		{
			name: "started to a category",
			edit: func(_ *rig, req *transfer.ExportRequest) {
				req.Placement = transfer.Placement{Category: "tv"}
			},
			want: func(*rig) url.Values {
				return url.Values{"autoTMM": {"true"}, "category": {"tv"}, "seedMode": {"true"}, "skip_checking": {"true"}}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, exportPolls)
			s := newShow(t, showName)
			r.seed(s)
			req := r.exportRequest(s)
			tc.edit(r, &req)

			r.export(req)

			require.Equal(t, tc.want(r), r.dst.LastParams("torrents/add"))
		})
	}
}
