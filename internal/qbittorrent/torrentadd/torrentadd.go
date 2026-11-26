// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

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

type Adder struct {
	sync *qbittorrent.SyncManager
}

func New(sync *qbittorrent.SyncManager) *Adder {
	return &Adder{sync: sync}
}

type Request struct {
	InstanceID   int
	Torrent      []byte
	Hash         string
	Settings     Settings
	Polls        int
	PollInterval time.Duration
}

type Settings struct {
	SavePath             string
	Category             string
	Tags                 []string
	ContentLayout        qbt.ContentLayout
	Stopped              *bool
	Forced               bool
	AutoTMM              *bool
	UseDownloadPath      *bool
	SeedMode             bool
	StopWhenFilesChecked bool
	ShareLimits          *ShareLimits
	FilePriorities       []int
	Recheck              bool
}

type ShareLimits struct {
	RatioLimit               float64
	SeedingTimeLimit         int64
	InactiveSeedingTimeLimit int64
}

type Outcome uint8

const (
	Failed Outcome = iota
	Accepted
	Checking
	Verified
)

type Failure uint8

const (
	_ Failure = iota
	AddRefused
	AfterAddFailed
	NotListed
	PollFailed
	MissingFiles
	Errored
	Cancelled
	CheckNotSeen
)

type Result struct {
	Outcome Outcome
	Failure Failure
	Err     error
	Torrent *qbt.Torrent
}

func (r Result) final(s Settings) bool {
	if s.StopWhenFilesChecked && r.checkedAndStopped() {
		return true
	}
	return r.Outcome == Verified || r.Failure == MissingFiles || r.Failure == Errored
}

func (r Result) checkedAndStopped() bool {
	return r.Torrent != nil && r.Outcome == Accepted && stopped(r.Torrent.State)
}

func stopped(state qbt.TorrentState) bool {
	return slices.Contains([]qbt.TorrentState{qbt.TorrentStateStoppedDl, qbt.TorrentStatePausedDl, qbt.TorrentStateStoppedUp, qbt.TorrentStatePausedUp}, state)
}

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

func (a *Adder) Confirm(ctx context.Context, req Request) Result {
	return a.confirm(ctx, req, func(_ int, res Result) bool { return res.final(req.Settings) })
}

const staleRefreshes = 2

const defaultRefreshInterval = 1500

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

func (a *Adder) Recheck(ctx context.Context, req Request) Result {
	if err := a.sync.BulkAction(qbittorrent.WithPostAddBulkActionRetry(ctx), req.InstanceID, []string{req.Hash}, "recheck"); err != nil {
		return Result{Failure: AfterAddFailed, Err: err}
	}
	return a.ConfirmRecheck(ctx, req)
}

func (a *Adder) ConfirmRecheck(ctx context.Context, req Request) Result {
	stale, err := a.stalePolls(ctx, req)
	if err != nil {
		return Result{Failure: PollFailed, Err: err}
	}
	checking, accepted := false, false
	res := a.confirm(ctx, req, func(poll int, res Result) bool {
		checking = checking || res.Outcome == Checking
		accepted = (checking || poll >= stale) && res.final(Settings{StopWhenFilesChecked: true})
		return accepted
	})
	if res.Outcome == Verified && !accepted {
		return Result{Failure: CheckNotSeen, Torrent: res.Torrent,
			Err: fmt.Errorf("qBittorrent showed no result of the check within %s", time.Duration(req.Polls)*req.PollInterval)}
	}
	return res
}

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
