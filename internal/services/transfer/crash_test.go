// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
)

const (
	crashEnv = "QUI_TRANSFER_CRASH"
	crashed  = 3
)

type crash struct {
	DB          string
	Request     transfer.Request
	Export      *transfer.ExportRequest
	At          string
	Unconfirmed bool
	Damage      map[string]int64
	InTheWay    string
	Cancel      bool
	FailSave    string
	Resume      bool
}

func TestTransferResumesAfterACrash(t *testing.T) {
	const episode1, episode2 = "Show.S01E01.mkv", "Show.S01E02.mkv"
	for _, tc := range []struct {
		at            string
		unconfirmed   bool
		damage        bool
		headStart     bool
		group         bool
		killAtAdd     int
		killAtRemoval bool
		cancel        bool
		want          string
	}{
		{at: "before:prepare", want: "done"},
		{at: "after:prepare", want: "done"},
		{at: "write:3", want: "done"},
		{at: "after:place", want: "done"},
		{at: "after:add", want: "done"},
		{killAtAdd: 1, want: "ownership unknown"},
		{at: "after:added", want: "done"},
		{at: "after:after", want: "done"},
		{at: "after:remove from source", want: "done"},
		{killAtRemoval: true, want: "done"},
		{at: "after:done", want: "done"},
		{at: "after:check on the target", unconfirmed: true, want: "done"},
		{killAtAdd: 1, unconfirmed: true, want: "done"},
		{at: "after:check the files on the target", headStart: true, want: "done"},
		{killAtAdd: 1, headStart: true, want: "done"},
		{at: "after:copy the missing files", headStart: true, want: "done"},
		{at: "after:clean up", unconfirmed: true, damage: true, want: "cleaned up"},
		{at: "after:clean up", cancel: true, want: "cancelled"},
		{at: "after:added", group: true, want: "done"},
		{killAtAdd: 3, group: true, want: "ownership unknown"},
	} {
		name := tc.at
		switch {
		case tc.killAtAdd > 0:
			name = "kill at add " + strconv.Itoa(tc.killAtAdd)
		case tc.killAtRemoval:
			name = "kill at the removal from the source"
		}
		if tc.unconfirmed {
			name += ", flushes unconfirmed"
		}
		if tc.headStart {
			name += ", head start"
		}
		if tc.group {
			name += ", group"
		}
		if tc.cancel {
			name += ", cancelled"
		}
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			s := newShow(t, showName)
			r.seed(s)
			members := []string{s.hash}
			if tc.group {
				members = append(members, r.seedEpisode(s))
			}
			srcLeft := map[string]string{}
			if tc.headStart {
				write(t, filepath.Join(r.dstDir, showName), map[string][]byte{episode1: s.files[episode1]})
				srcLeft[showName+"/"+episode1] = string(s.files[episode1])
			}
			plan := crash{DB: r.dbPath, Request: r.request(s), At: tc.at, Unconfirmed: tc.unconfirmed, Cancel: tc.cancel}
			if tc.damage {
				plan.Damage = map[string]int64{showName + "/" + episode2: 20}
			}

			child := make(chan *os.Process, 1)
			var added []string
			killed := ""
			r.dst.SetOnAdd(func(hash string) {
				added = append(added, hash)
				if len(added) == tc.killAtAdd {
					killed = hash
					require.NoError(t, (<-child).Kill())
				}
			})
			r.src.SetOnWrite(func(endpoint string) {
				if tc.killAtRemoval && endpoint == "torrents/delete" {
					require.NoError(t, (<-child).Kill())
				}
			})
			runCrashChild(t, plan, child, tc.killAtAdd > 0 || tc.killAtRemoval)

			notified := &events{}
			r.deps.Notifier = notified
			r.restart()
			r.svc.Wait()

			var id int64
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT id FROM torrent_transfers`).Scan(&id))
			record := r.get(id)
			require.False(t, record.FinishedAt.IsZero(), "the Transfer ended after the restart")
			require.False(t, r.store.IsClaimed(r.source.ID, s.hash))
			adds := map[string]int{}
			for _, hash := range added {
				if slices.Contains(members, hash) {
					adds[hash]++
				} else {
					require.False(t, r.dst.Has(hash), "qui removes each helper torrent")
				}
			}
			switch tc.want {
			case "done":
				require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
				for _, hash := range members {
					require.Equal(t, 1, adds[hash], "qui adds each torrent once")
					require.True(t, r.dst.Has(hash))
				}
				require.Equal(t, contents(s, showName), tree(t, r.dstDir))
				for _, hash := range members {
					require.False(t, r.src.Has(hash))
				}
				require.Equal(t, 1, r.src.Writes("torrents/delete"))
				require.Equal(t, srcLeft, tree(t, r.srcDir))
			case "ownership unknown":
				require.Equal(t, transfer.StateFailed, record.State)
				require.Contains(t, record.FailureReason, "Ownership unknown")
				for _, hash := range members {
					require.Equal(t, hash == killed, r.dst.Has(hash), "qui keeps the torrent that it may not have added, and removes the ones it added")
				}
				require.Equal(t, contents(s, showName), tree(t, r.dstDir))
			case "cleaned up":
				require.Equal(t, transfer.StateFailed, record.State)
				require.Contains(t, record.FailureReason, "The check on homelab failed")
				require.Equal(t, 1, countSteps(record, "clean up"))
				require.Empty(t, tree(t, r.dstDir))
				require.False(t, r.dst.Has(s.hash))
			case "cancelled":
				require.Equal(t, transfer.StateCancelled, record.State)
				require.Equal(t, 1, countSteps(record, "clean up"))
				require.Empty(t, tree(t, r.dstDir))
			}
			if tc.want != "done" {
				for _, hash := range members {
					require.True(t, r.src.Has(hash))
					require.False(t, r.src.Stopped(hash))
				}
				require.Equal(t, contents(s, showName), tree(t, r.srcDir))
			}

			switch tc.want {
			case "cancelled":
				require.Empty(t, notified.events, "a cancel sends no event")
			case "done":
				require.Len(t, notified.events, 1, "the restart sends the end of the Transfer once")
				require.Equal(t, notifications.EventTransferFinished, notified.events[0].Type)
			default:
				require.Len(t, notified.events, 1, "the restart sends the end of the Transfer once")
				require.Equal(t, notifications.EventTransferFailed, notified.events[0].Type)
			}

			src, dst, sends := sentWithoutResync(r.src), sentWithoutResync(r.dst), len(notified.events)
			r.restart()
			r.svc.Wait()
			require.Equal(t, src, sentWithoutResync(r.src))
			require.Equal(t, dst, sentWithoutResync(r.dst))
			require.Len(t, notified.events, sends)
		})
	}
}

func TestTransferCleanupKeepsTheHeadStartWhenTheResumeFails(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	write(t, filepath.Join(r.dstDir, showName), map[string][]byte{episode1: s.files[episode1]})
	runCrashChild(t, crash{DB: r.dbPath, Request: r.request(s), At: "after:place"}, make(chan *os.Process, 1), false)
	r.disk.failOpenRoots = 1

	r.restart()
	r.svc.Wait()

	var id int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT id FROM torrent_transfers`).Scan(&id))
	record := r.get(id)
	require.Equal(t, transfer.StateFailed, record.State)
	require.Contains(t, record.FailureReason, "qui cannot open the save path")
	require.Equal(t, map[string]string{showName + "/" + episode1: string(s.files[episode1])}, tree(t, r.dstDir),
		"the file that was on the target stays, and the files of qui go")
	require.Equal(t, contents(s, showName), tree(t, r.srcDir))
}

func TestTransferStopsAtARenameWhileQuiIsStopped(t *testing.T) {
	const first, second, spare = "Show.S01E05.mkv", "Show.S01E06.mkv", "Show.S01E07.mkv"
	swap := []string{first, spare, second, first, spare, second}
	for _, tc := range []struct {
		name   string
		at     string
		rename []string
	}{
		{name: "a swap before the files are in place", at: "before:place", rename: swap},
		{name: "a swap after the files are in place", at: "before:add", rename: swap},
		{name: "a rename before the files are in place", at: "before:place", rename: []string{first, spare}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			files := map[string][]byte{
				episode1: bytes.Repeat([]byte("one|"), 10),
				episode2: bytes.Repeat([]byte("two|"), 10),
			}
			data, hash := qbtstub.NewContentTorrent(t, showName, 16,
				qbtstub.ContentFile{Path: episode1, Data: files[episode1]},
				qbtstub.ContentFile{Path: episode2, Data: files[episode2]})
			s := show{data: data, hash: hash, files: files}
			r.seed(s)
			r.renameOnSource(s, episode1, first, episode2, second)
			runCrashChild(t, crash{DB: r.dbPath, Request: r.request(s), At: tc.at}, make(chan *os.Process, 1), false)
			r.renameOnSource(s, tc.rename...)
			source := tree(t, r.srcDir)

			r.restart()
			r.svc.Wait()

			var id int64
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT id FROM torrent_transfers`).Scan(&id))
			record := r.get(id)
			require.Equal(t, transfer.StateFailed, record.State, record.FailureReason)
			require.Contains(t, record.FailureReason, "a file was renamed")
			require.Empty(t, tree(t, r.dstDir), "the cleanup removes the files of qui")
			require.False(t, r.dst.Has(s.hash))
			require.True(t, r.src.Has(s.hash))
			require.Equal(t, source, tree(t, r.srcDir))
		})
	}
}

func TestTransferCleanupUsesTheRecordedNames(t *testing.T) {
	const renamed = "Show.S01E05.mkv"
	for _, tc := range []struct {
		name string
		stop func(r *rig, s show) int64
		end  func(r *rig, id int64)
		want transfer.State
	}{
		{
			name: "a cancel while the Transfer waits for a retry",
			stop: func(r *rig, s show) int64 {
				r.dst.Reject = http.StatusInternalServerError
				record := r.run(r.request(s))
				require.Equal(t, transfer.FailureTemporary, record.FailureClass, record.FailureReason)
				r.dst.Reject = 0
				r.renameOnSource(s, episode1, renamed)
				return record.ID
			},
			end: func(r *rig, id int64) {
				require.NoError(t, r.svc.Cancel(t.Context(), id))
				r.svc.Wait()
			},
			want: transfer.StateCancelled,
		},
		{
			name: "a cleanup that a stop interrupted",
			stop: func(r *rig, s show) int64 {
				runCrashChild(t, crash{DB: r.dbPath, Request: r.request(s), At: "before:add"}, make(chan *os.Process, 1), false)
				r.renameOnSource(s, episode1, renamed)
				runCrashChild(t, crash{DB: r.dbPath, Resume: true, At: "after:clean up"}, make(chan *os.Process, 1), false)
				var id int64
				require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT id FROM torrent_transfers`).Scan(&id))
				return id
			},
			end: func(r *rig, _ int64) {
				r.restart()
				r.svc.Wait()
			},
			want: transfer.StateFailed,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			s := newShow(t, showName)
			r.seed(s)
			id := tc.stop(r, s)
			require.ElementsMatch(t, []string{showName + "/" + episode1, showName + "/" + episode2}, slices.Collect(maps.Keys(tree(t, r.dstDir))),
				"the files are in place on the target")
			theirs := map[string]string{showName + "/" + renamed: "theirs"}
			write(t, filepath.Join(r.dstDir, showName), map[string][]byte{renamed: []byte("theirs")})

			tc.end(r, id)

			record := r.get(id)
			require.Equal(t, tc.want, record.State, record.FailureReason)
			require.Equal(t, theirs, tree(t, r.dstDir), "the files of qui go, and the file at the new name stays")
		})
	}
}

func TestTransferKeepsTheSourceFilesAfterAChangeOnTheSource(t *testing.T) {
	const renamed = "Show.S01E05.mkv"
	theirs := bytes.Repeat([]byte("x"), 40)
	for _, change := range []struct {
		name   string
		change func(r *rig, s show)
		reason string
		moved  string
		file   string
	}{
		{name: "a rename", change: func(r *rig, s show) {
			r.renameOnSource(s, episode1, renamed)
		}, reason: "a file was renamed on seedbox", file: renamed},
		{name: "a move", change: func(r *rig, s show) {
			require.NoError(t, r.sync.SetLocation(t.Context(), r.source.ID, []string{s.hash}, filepath.Join(r.srcDir, "moved")))
		}, reason: "a torrent moved on seedbox", moved: "moved/", file: episode1},
	} {
		for _, when := range []struct {
			name string
			run  func(r *rig, s show, change func())
		}{
			{name: "a removal that resumes after a stop", run: func(r *rig, s show, change func()) {
				runCrashChild(t, crash{DB: r.dbPath, Request: r.request(s), At: "after:remove from source"}, make(chan *os.Process, 1), false)
				change()
				r.restart()
				r.svc.Wait()
			}},
			{name: "a removal in the run of the copy", run: func(r *rig, s show, change func()) {
				r.dst.OnAdd = func(string) { change() }
				r.run(r.request(s))
			}},
		} {
			t.Run(change.name+", "+when.name, func(t *testing.T) {
				r := newRig(t)
				s := newShow(t, showName)
				r.seed(s)
				require.Len(t, s.files[episode1], len(theirs))

				when.run(r, s, func() {
					change.change(r, s)
					write(t, filepath.Join(r.srcDir, showName), map[string][]byte{episode1: theirs})
				})

				var id int64
				require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT id FROM torrent_transfers`).Scan(&id))
				record := r.get(id)
				require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
				require.False(t, r.src.Has(s.hash))
				require.Equal(t, map[string]string{
					showName + "/" + episode1:                   string(theirs),
					change.moved + showName + "/" + change.file: string(s.files[episode1]),
					change.moved + showName + "/" + episode2:    string(s.files[episode2]),
				}, tree(t, r.srcDir))
				require.Equal(t, contents(s, showName), tree(t, r.dstDir))
				require.Contains(t, logOf(record), "kept its files, because "+change.reason)
			})
		}
	}
}

func TestTransferCleanupKeepsAFileInTheWayAfterACrash(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failSave string
	}{
		{name: "a crash before the end"},
		{name: "a crash after a failed record of the placed files", failSave: "remove the placed files"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			s := newShow(t, showName)
			r.seed(s)
			theirs := filepath.Join(r.dstDir, showName, episode2)
			plan := crash{DB: r.dbPath, Request: r.request(s), At: "before:failed", InTheWay: theirs, FailSave: tc.failSave}
			runCrashChild(t, plan, make(chan *os.Process, 1), false)

			r.restart()
			r.svc.Wait()

			var id int64
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT id FROM torrent_transfers`).Scan(&id))
			record := r.get(id)
			require.Equal(t, transfer.StateFailed, record.State)
			require.Contains(t, record.FailureReason, "Conflict: a file is in the way on homelab")
			require.Equal(t, map[string]string{showName + "/" + episode2: "theirs"}, tree(t, r.dstDir),
				"the file in the way stays, and the files of qui go")
			require.Equal(t, contents(s, showName), tree(t, r.srcDir))
		})
	}
}

func runCrashChild(t *testing.T, plan crash, child chan<- *os.Process, killed bool) {
	t.Helper()
	env, err := json.Marshal(plan)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTransferCrashChild$", "-test.count=1", "-test.timeout=60s")
	cmd.Env = append(os.Environ(), crashEnv+"="+string(env))
	var output strings.Builder
	cmd.Stdout, cmd.Stderr = &output, &output
	require.NoError(t, cmd.Start())
	child <- cmd.Process
	err = cmd.Wait()

	exit, ok := errors.AsType[*exec.ExitError](err)
	require.True(t, ok, "qui never reached the crash point:\n%s", output.String())
	if killed {
		require.Empty(t, child, "the parent kills qui:\n%s", output.String())
		return
	}
	require.Equal(t, crashed, exit.ExitCode(), output.String())
}

func TestTransferCrashChild(t *testing.T) {
	env := os.Getenv(crashEnv)
	if env == "" {
		t.Skip("runs only as the child of TestTransferResumesAfterACrash")
	}
	var plan crash
	require.NoError(t, json.Unmarshal([]byte(env), &plan))
	db := openDB(t, plan.DB)
	d := &disk{Backend: local.NewBackend(), unconfirmed: plan.Unconfirmed, damage: plan.Damage}
	deps, _, _ := wire(t, db, d, func() time.Time { return clock })
	at := crashAt{Querier: db, fail: plan.FailSave}
	switch kind, value, _ := strings.Cut(plan.At, ":"); kind {
	case "before":
		at.before = value
	case "after":
		at.after = value
	case "write":
		n, err := strconv.Atoi(value)
		require.NoError(t, err)
		writes := 0
		d.onWrite = func(string) {
			if writes++; writes == n {
				os.Exit(crashed)
			}
		}
	}
	if plan.InTheWay != "" {
		put := sync.OnceFunc(func() { require.NoError(t, os.WriteFile(plan.InTheWay, []byte("theirs"), 0o600)) })
		d.onWrite = func(string) { put() }
	}
	store, err := transfer.NewStore(t.Context(), at)
	require.NoError(t, err)
	deps.Store = store
	exportPolls(&deps.Config)

	svc := transfer.NewService(t.Context(), deps)
	t.Cleanup(func() { closeService(t, svc) })
	ids := make(chan int64, 1)
	if plan.Cancel {
		cancel := sync.OnceFunc(func() { require.NoError(t, svc.Cancel(t.Context(), <-ids)) })
		d.onWrite = func(string) { cancel() }
	}
	switch {
	case plan.Export != nil:
		_, _, err = svc.Export(t.Context(), *plan.Export)
	case !plan.Resume:
		var id int64
		id, err = svc.Start(t.Context(), plan.Request)
		ids <- id
	}
	require.NoError(t, err)
	svc.Wait()
}

type crashAt struct {
	dbinterface.Querier
	before, after, fail string
}

func (c crashAt) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	last := lastStep(query, args)
	if last != "" && last == c.before {
		os.Exit(crashed)
	}
	if last != "" && last == c.fail {
		return nil, errors.New("the database is locked")
	}
	res, err := c.Querier.ExecContext(ctx, query, args...)
	if last != "" && last == c.after {
		os.Exit(crashed)
	}
	return res, err
}

func lastStep(query string, args []any) string {
	if !strings.Contains(query, "steps_json") || len(args) < 2 {
		return ""
	}
	text, ok := args[1].(string)
	if !ok {
		return ""
	}
	var steps []transfer.Step
	if err := json.Unmarshal([]byte(text), &steps); err != nil || len(steps) == 0 {
		return ""
	}
	return steps[len(steps)-1].Name
}

func countSteps(record *transfer.Record, name string) int {
	n := 0
	for _, step := range record.Steps {
		if step.Name == name {
			n++
		}
	}
	return n
}
