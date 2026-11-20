// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package torrentadd adds a torrent on an instance and reports what
// qBittorrent made of it.
package torrentadd

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
)

// Adder adds torrents through the sync manager.
type Adder struct {
	sync *qbittorrent.SyncManager
}

// New returns an Adder that adds through sync.
func New(sync *qbittorrent.SyncManager) *Adder {
	return &Adder{sync: sync}
}

// Request is one add.
type Request struct {
	InstanceID int
	// Torrent is the .torrent file, and Hash its infohash.
	Torrent  []byte
	Hash     string
	Settings Settings
	// Polls is how many fresh syncs confirm the add, PollInterval apart. The
	// polls end early once the torrent is verified or failed.
	Polls        int
	PollInterval time.Duration
}

// Settings are the add settings. A nil or empty value leaves the setting to
// qBittorrent.
type Settings struct {
	SavePath      string
	Category      string
	Tags          []string
	ContentLayout qbt.ContentLayout
	Stopped       *bool
	// Forced starts the torrent forced. qBittorrent before 5.1 ignores it.
	Forced          bool
	AutoTMM         *bool
	UseDownloadPath *bool
	// SeedMode is "Skip hash check": qBittorrent takes the files on trust.
	SeedMode bool
	// StopWhenFilesChecked stops the torrent once the check of its files
	// ends, and the polls end there.
	StopWhenFilesChecked bool
	ShareLimits          *ShareLimits
	// FilePriorities holds a priority for each file, in the order of the
	// .torrent. qBittorrent 5.3 refuses them in the add of a .torrent, so the
	// add is stopped, the priorities follow, and the torrent then starts
	// unless Stopped is true.
	FilePriorities []int
	// Recheck checks the files after the add. A stopped torrent stays
	// stopped after the check.
	Recheck bool
}

// ShareLimits are the share limits of the torrent: -2 uses the global limit,
// -1 sets none. The seeding times are in minutes.
type ShareLimits struct {
	RatioLimit               float64
	SeedingTimeLimit         int64
	InactiveSeedingTimeLimit int64
}

// Outcome is what qBittorrent shows for the torrent at the last poll.
type Outcome uint8

const (
	// Failed: the add or a call after it failed, or the torrent is not
	// listed, misses files, or is in the error state. Result.Failure says
	// which.
	Failed Outcome = iota
	// Accepted: qBittorrent lists the torrent, incomplete and not checking.
	Accepted
	// Checking: qBittorrent still checks the files.
	Checking
	// Verified: qBittorrent shows the torrent complete. In seed mode it took
	// the files on trust and found none missing.
	Verified
)

// Failure is why an add failed.
type Failure uint8

const (
	_ Failure = iota
	// AddRefused: qBittorrent refused the add.
	AddRefused
	// AfterAddFailed: a call after the add failed: the file priorities, the
	// start, or the recheck. The torrent stays on the instance.
	AfterAddFailed
	// NotListed: no poll listed the torrent.
	NotListed
	// PollFailed: the last poll could not read the state.
	PollFailed
	// MissingFiles: qBittorrent found files missing.
	MissingFiles
	// Errored: the torrent is in the error state.
	Errored
	// Cancelled: ctx ended during the polls.
	Cancelled
	// CheckNotSeen: the polls after a recheck ran out while the state could
	// still be the one from before it.
	CheckNotSeen
)

// Result is the outcome of an add. Torrent is what the last poll read, nil
// when it read nothing.
type Result struct {
	Outcome Outcome
	Failure Failure
	Err     error
	Torrent *qbt.Torrent
}

// final reports whether more polls cannot change the result. With the stop
// condition "files checked", a stopped torrent has ended its check.
func (r Result) final(s Settings) bool {
	if s.StopWhenFilesChecked && r.checkedAndStopped() {
		return true
	}
	return r.Outcome == Verified || r.Failure == MissingFiles || r.Failure == Errored
}

// checkedAndStopped reports whether the torrent is stopped and incomplete,
// as it is after a check that found pieces missing, when qBittorrent stops
// the torrent at the end of the check.
func (r Result) checkedAndStopped() bool {
	return r.Torrent != nil && r.Outcome == Accepted && stopped(r.Torrent.State)
}

func stopped(state qbt.TorrentState) bool {
	return slices.Contains([]qbt.TorrentState{qbt.TorrentStateStoppedDl, qbt.TorrentStatePausedDl, qbt.TorrentStateStoppedUp, qbt.TorrentStatePausedUp}, state)
}

// Add adds the torrent, makes the calls after the add, and polls until
// qBittorrent shows the torrent verified or failed, or the polls run out.
// The calls after the add wait for qBittorrent to list the torrent, so when
// there are any, the polls get the time that ctx had left at the start, also
// after ctx ends.
func (a *Adder) Add(ctx context.Context, req Request) Result {
	deadline, hasDeadline := ctx.Deadline()
	budget := time.Until(deadline)
	if _, err := a.sync.AddTorrent(ctx, req.InstanceID, req.Torrent, req.Settings.options()); err != nil {
		return Result{Failure: AddRefused, Err: err}
	}
	if req.Settings.hasAfterAdd() {
		if err := a.afterAdd(qbittorrent.WithPostAddBulkActionRetry(ctx), req); err != nil {
			return Result{Failure: AfterAddFailed, Err: err}
		}
		if hasDeadline {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), budget)
			defer cancel()
		}
	}
	return a.Confirm(ctx, req)
}

func (s Settings) hasAfterAdd() bool {
	return len(s.FilePriorities) > 0 || s.Recheck
}

// afterAdd sets the file priorities, starts a torrent that was stopped only
// for them, and rechecks it. ctx carries the retry for calls after an add,
// which waits until qBittorrent lists the new torrent.
func (a *Adder) afterAdd(ctx context.Context, req Request) error {
	hashes := []string{req.Hash}
	if len(req.Settings.FilePriorities) > 0 {
		byPriority := map[int][]int{}
		for index, priority := range req.Settings.FilePriorities {
			byPriority[priority] = append(byPriority[priority], index)
		}
		for _, priority := range slices.Sorted(maps.Keys(byPriority)) {
			if err := a.sync.SetTorrentFilePriority(ctx, req.InstanceID, req.Hash, byPriority[priority], priority); err != nil {
				return err
			}
		}
		// torrents/start would reset a forced torrent to automatic, so a
		// forced one starts with setForceStart.
		switch {
		case req.Settings.Stopped != nil && *req.Settings.Stopped:
		case req.Settings.Forced:
			if err := a.sync.SetForceStart(ctx, req.InstanceID, hashes, true); err != nil {
				return err
			}
		default:
			if err := a.sync.BulkAction(ctx, req.InstanceID, hashes, "resume"); err != nil {
				return err
			}
		}
	}
	if req.Settings.Recheck {
		return a.sync.BulkAction(ctx, req.InstanceID, hashes, "recheck")
	}
	return nil
}

// options are the parameters of the add call. qBittorrent before 5.0 reads
// paused, later versions read stopped.
func (s Settings) options() map[string]string {
	options := map[string]string{}
	set := func(key, value string) {
		if value != "" {
			options[key] = value
		}
	}
	setBool := func(key string, value *bool) {
		if value != nil {
			options[key] = strconv.FormatBool(*value)
		}
	}
	set("savepath", s.SavePath)
	set("category", s.Category)
	set("tags", strings.Join(s.Tags, ","))
	set("contentLayout", string(s.ContentLayout))
	stopped := s.Stopped
	if len(s.FilePriorities) > 0 {
		stopped = new(true)
	}
	setBool("stopped", stopped)
	setBool("paused", stopped)
	if s.Forced {
		options["forced"] = "true"
	}
	setBool("autoTMM", s.AutoTMM)
	setBool("useDownloadPath", s.UseDownloadPath)
	if s.SeedMode {
		options["seedMode"] = "true"
	}
	if s.StopWhenFilesChecked {
		options["stopCondition"] = "FilesChecked"
	}
	if l := s.ShareLimits; l != nil {
		options["ratioLimit"] = strconv.FormatFloat(l.RatioLimit, 'f', -1, 64)
		options["seedingTimeLimit"] = strconv.FormatInt(l.SeedingTimeLimit, 10)
		options["inactiveSeedingTimeLimit"] = strconv.FormatInt(l.InactiveSeedingTimeLimit, 10)
	}
	return options
}

// Confirm polls fresh syncs until the torrent is verified or failed, or the
// polls run out, as Add does after its add. req.Torrent is not used.
func (a *Adder) Confirm(ctx context.Context, req Request) Result {
	return a.confirm(ctx, req, func(_ int, res Result) bool { return res.final(req.Settings) })
}

// staleRefreshes is how many status refreshes of qBittorrent can still report
// the state from before a recheck. qBittorrent recomputes the state that it
// reports only at a refresh, and a refresh that began before the recheck can
// end after it.
const staleRefreshes = 2

// defaultRefreshInterval is the status refresh interval of qBittorrent in
// milliseconds, for a qBittorrent that does not report its own.
const defaultRefreshInterval = 1500

// stalePolls is how many polls of req after a recheck can still read the
// state from before it. The count follows the status refresh interval of the
// instance, which the user can set far above the default. It reads the
// interval from qBittorrent, not from the cache of the preferences, which can
// hold a lower one.
func (a *Adder) stalePolls(ctx context.Context, req Request) (int, error) {
	client, err := a.sync.GetClient(ctx, req.InstanceID)
	if err != nil {
		return 0, fmt.Errorf("read the status refresh interval: %w", err)
	}
	prefs, err := client.GetAppPreferencesCtx(ctx)
	if err != nil {
		return 0, fmt.Errorf("read the status refresh interval: %w", err)
	}
	refresh := time.Duration(cmp.Or(prefs.RefreshInterval, defaultRefreshInterval)) * time.Millisecond
	return int(staleRefreshes*refresh/req.PollInterval) + 1, nil
}

// Recheck checks the files of a stopped torrent, and waits for the check as
// ConfirmRecheck does. The torrent stays stopped.
func (a *Adder) Recheck(ctx context.Context, req Request) Result {
	if err := a.sync.BulkAction(qbittorrent.WithPostAddBulkActionRetry(ctx), req.InstanceID, []string{req.Hash}, "recheck"); err != nil {
		return Result{Failure: AfterAddFailed, Err: err}
	}
	return a.ConfirmRecheck(ctx, req)
}

// ConfirmRecheck polls fresh syncs until the recheck that a stopped torrent
// just got ends, or the polls run out. Verified says that the check found
// every piece. Of req, it uses InstanceID, Hash, Polls, and PollInterval.
func (a *Adder) ConfirmRecheck(ctx context.Context, req Request) Result {
	stale, err := a.stalePolls(ctx, req)
	if err != nil {
		return Result{Failure: PollFailed, Err: err}
	}
	checking, accepted := false, false
	res := a.confirm(ctx, req, func(poll int, res Result) bool {
		checking = checking || res.Outcome == Checking
		// A state that is not a check can be the one from before the
		// recheck, also a complete one.
		accepted = (checking || poll >= stale) && res.final(Settings{StopWhenFilesChecked: true})
		return accepted
	})
	if res.Outcome == Verified && !accepted {
		return Result{Failure: CheckNotSeen, Torrent: res.Torrent,
			Err: fmt.Errorf("qBittorrent showed no result of the check within %s", time.Duration(req.Polls)*req.PollInterval)}
	}
	return res
}

// confirm polls until final says that the result of a poll, counted from 0,
// cannot change.
func (a *Adder) confirm(ctx context.Context, req Request, final func(poll int, res Result) bool) Result {
	owner, err := a.sync.GetSyncOwner(ctx, req.InstanceID)
	if err != nil {
		return Result{Failure: PollFailed, Err: fmt.Errorf("unable to get sync manager: %w", err)}
	}
	var res Result
	for poll := range req.Polls {
		if poll > 0 {
			select {
			case <-ctx.Done():
				return Result{Failure: Cancelled, Err: ctx.Err()}
			case <-time.After(req.PollInterval):
			}
		}
		res = a.poll(ctx, owner, req)
		if final(poll, res) {
			return res
		}
	}
	return res
}

// poll reads the state of the torrent after a fresh sync.
func (a *Adder) poll(ctx context.Context, owner *syncowner.Owner, req Request) Result {
	if err := owner.Sync(ctx); err != nil {
		return Result{Failure: PollFailed, Err: err}
	}
	torrent, found, err := a.sync.HasTorrentByAnyHash(ctx, req.InstanceID, []string{req.Hash})
	switch {
	case err != nil:
		return Result{Failure: PollFailed, Err: err}
	case !found:
		return Result{Failure: NotListed}
	}
	res := Result{Torrent: torrent}
	switch torrent.State {
	case qbt.TorrentStateMissingFiles:
		res.Failure = MissingFiles
	case qbt.TorrentStateError:
		res.Failure = Errored
	case qbt.TorrentStateCheckingUp, qbt.TorrentStateCheckingDl, qbt.TorrentStateCheckingResumeData:
		res.Outcome = Checking
	default:
		res.Outcome = Accepted
		if torrent.Progress >= 1 {
			res.Outcome = Verified
		}
	}
	return res
}
