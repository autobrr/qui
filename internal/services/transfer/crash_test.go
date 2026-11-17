// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
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
	"github.com/autobrr/qui/internal/services/transfer"
)

const (
	crashEnv = "QUI_TRANSFER_CRASH"
	// crashed is the exit code of a child that ended at its crash point.
	crashed = 3
)

// crash is what the child process runs, and where it ends.
type crash struct {
	DB      string
	Request transfer.Request
	// Export, when set, runs this Export run instead of the Transfer.
	Export *transfer.ExportRequest
	// At is "before:<step>" or "after:<step>" for the save that records
	// the step, "write:<n>" for the nth write of the copy, or empty when
	// the parent kills the child.
	At          string
	Unconfirmed bool
	Damage      map[string]int64
	// InTheWay is the path of a file that lands on the target at the first
	// write of the copy.
	InTheWay string
	// FailSave fails the save that records this step.
	FailSave string
}

// qui ends at each step that it records, and at the adds that it cannot
// record. After the restart, the Transfer resumes once or cleans up once,
// and a second restart sends nothing.
func TestTransferResumesAfterACrash(t *testing.T) {
	const episode1, episode2 = "Show.S01E01.mkv", "Show.S01E02.mkv"
	for _, tc := range []struct {
		at          string
		unconfirmed bool
		damage      bool
		headStart   bool
		// group adds a torrent of the first episode, which shares its file.
		group bool
		// killAtAdd kills qui at the nth add of a torrent to the target.
		killAtAdd int
		// killAtRemoval kills qui once the source removed the torrent, before
		// qui removes the files there.
		killAtRemoval bool
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
		{at: "after:added", group: true, want: "done"},
		// The adds are the check of the episode, the season, and the episode.
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
			plan := crash{DB: r.dbPath, Request: r.request(s), At: tc.at, Unconfirmed: tc.unconfirmed}
			if tc.damage {
				plan.Damage = map[string]int64{showName + "/" + episode2: 20}
			}

			child := make(chan *os.Process, 1)
			var added []string
			killed := ""
			r.dst.OnAdd = func(hash string) {
				added = append(added, hash)
				if len(added) == tc.killAtAdd {
					killed = hash
					require.NoError(t, (<-child).Kill())
				}
			}
			r.src.OnWrite = func(endpoint string) {
				if tc.killAtRemoval && endpoint == "torrents/delete" {
					require.NoError(t, (<-child).Kill())
				}
			}
			runCrashChild(t, plan, child, tc.killAtAdd > 0 || tc.killAtRemoval)

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
			}
			if tc.want != "done" {
				for _, hash := range members {
					require.True(t, r.src.Has(hash))
					require.False(t, r.src.Stopped(hash))
				}
				require.Equal(t, contents(s, showName), tree(t, r.srcDir))
			}

			src, dst := sent(r.src), sent(r.dst)
			r.restart()
			r.svc.Wait()
			require.Equal(t, src, sent(r.src))
			require.Equal(t, dst, sent(r.dst))
		})
	}
}

// A Transfer that qui stopped after the step place, and whose resume fails
// before it reads the target again, keeps the file that was on the target
// before the Transfer when it cleans up.
// regression: #4231
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

// A file that lands at a path of the copy during the copy stops the
// Transfer at the rename into place, and the cleanup keeps it. A crash after
// the cleanup, before qui recorded the end, keeps it too: the cleanup that
// runs again after the restart does not take it for a file that qui placed,
// also when qui could not record the files that it placed.
// regression: #4231
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

// runCrashChild runs qui in a child process on the database of the rig, and
// waits until it ends at its crash point, or until the parent kills it. The
// parent takes the process from child to kill it.
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
		// On Windows a killed process exits with code 1, as a failed test does.
		require.Empty(t, child, "the parent kills qui:\n%s", output.String())
		return
	}
	require.Equal(t, crashed, exit.ExitCode(), output.String())
}

// TestTransferCrashChild is the qui that TestTransferResumesAfterACrash
// ends. It runs the Transfer on the database of the parent, against the
// stubs of the parent.
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
	if plan.Export != nil {
		_, _, err = svc.Export(t.Context(), *plan.Export)
	} else {
		_, err = svc.Start(t.Context(), plan.Request)
	}
	require.NoError(t, err)
	svc.Wait()
}

// crashAt ends the process at the save that records a step: before the save
// or right after it. It fails the save that records the step fail.
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

// lastStep returns the last step of a save of a Transfer.
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
