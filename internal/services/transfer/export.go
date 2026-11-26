// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"fmt"
	"slices"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent/torrentadd"
)

const exportsAtATime = 5

type ExportRequest struct {
	SourceInstanceID int
	TargetInstanceID int
	Hash             string
	Name             string
	Size             int64
	Placement        Placement
	Paused           bool
	SkipChecking     bool
	RuleID           int
	RuleName         string
}

type ExportResult struct {
	Failure         string
	AlreadyOnTarget bool
	TrackerDomain   string
}

func (s *Service) Export(ctx context.Context, req ExportRequest) (int64, <-chan ExportResult, error) {
	if s.ctx.Err() != nil {
		return 0, nil, errShutdown
	}
	target, err := s.deps.Instances.Get(ctx, req.TargetInstanceID)
	if err != nil {
		return 0, nil, err
	}
	record := &Record{
		Kind:             KindExport,
		State:            StateWaiting,
		StartedBy:        StartedByRule,
		RuleID:           new(req.RuleID),
		SourceInstanceID: req.SourceInstanceID,
		TargetInstanceID: req.TargetInstanceID,
		TargetEndpoint:   fsops.EndpointOf(target),
		Options:          Options{After: AfterKeepSeeding, Paused: req.Paused, SkipChecking: new(req.SkipChecking)},
		Steps:            []Step{{Name: "start", At: s.deps.Now(), Detail: req.RuleName}},
		Members: []Member{{Hash: req.Hash, Name: req.Name, TargetInstanceID: req.TargetInstanceID,
			Placement: req.Placement}},
		Size: req.Size,
	}
	if err := s.deps.Store.Create(ctx, record); err != nil {
		return 0, nil, err
	}
	done := make(chan ExportResult, 1)
	s.export(record, done) //nolint:contextcheck // the run outlives the request and runs in the context of the Service
	return record.ID, done, nil
}

func (s *Service) export(record *Record, done chan<- ExportResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[record.ID]; ok || s.closed {
		close(done)
		return
	}
	if current, err := s.deps.Store.Get(s.ctx, record.ID); err == nil {
		if !current.FinishedAt.IsZero() {
			close(done)
			return
		}
		record = current
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.runs[record.ID] = &run{cancel: cancel}
	s.wg.Go(func() {
		defer close(done)
		defer cancel()
		defer func() {
			s.mu.Lock()
			delete(s.runs, record.ID)
			s.mu.Unlock()
		}()
		e := &exportJob{job: s.newJob(record), ruleName: record.Steps[0].Detail}
		select {
		case s.exports <- struct{}{}:
		case <-ctx.Done():
			if s.cancelled(record.ID) {
				done <- e.cancel(ctx)
			}
			return
		}
		defer func() { <-s.exports }()
		res, ended := e.run(ctx)
		switch {
		case ended:
			done <- res
		case s.cancelled(record.ID):
			done <- e.cancel(ctx)
		}
	})
}

type exportJob struct {
	*job
	ruleName string
	result   ExportResult
}

func (e *exportJob) member() *Member {
	return &e.rec.Members[0]
}

func (e *exportJob) run(ctx context.Context) (res ExportResult, ended bool) {
	switch {
	case e.reached(stepCleanUp) && e.rec.State == StateCancelled:
		return e.cancel(ctx), true
	case e.reached(stepCleanUp):
		return e.end(ctx, e.rec.FailureReason, true), true
	}
	m := e.member()
	target, err := e.svc.deps.Instances.Get(ctx, m.TargetInstanceID)
	switch {
	case ctx.Err() != nil:
		return ExportResult{}, false
	case err != nil:
		return e.end(ctx, fmt.Sprintf("qui cannot read instance %d, so it adds nothing there and removes nothing from it: %v",
			m.TargetInstanceID, err), false), true
	case e.targetMoved(target):
		return e.end(ctx, target.Name+" changed its address, so qui adds nothing there and removes nothing from it.", false), true
	}
	if e.reached(stepAdd) {
		torrent, err := e.svc.torrent(ctx, m.TargetInstanceID, m.Hash)
		switch {
		case ctx.Err() != nil:
			return ExportResult{}, false
		case err == nil && torrent != nil:
			e.logf("Resumed: the target has the torrent, so qui checks it there.")
			req := e.addRequest(nil)
			unchecked := torrent.Progress == 0 &&
				slices.Contains([]qbt.TorrentState{qbt.TorrentStateStoppedDl, qbt.TorrentStatePausedDl}, torrent.State)
			if req.Settings.Recheck && unchecked {
				err := e.svc.deps.Sync.BulkAction(ctx, m.TargetInstanceID, []string{m.Hash}, "recheck")
				switch {
				case ctx.Err() != nil:
					return ExportResult{}, false
				case err != nil:
					return e.endResumed(ctx, "Recheck on target failed: "+err.Error()), true
				}
			}
			added := e.svc.adder.Confirm(ctx, req)
			if ctx.Err() != nil {
				return ExportResult{}, false
			}
			return e.endResumed(ctx, e.verify(ctx, added)), true
		}
		e.logf("Resumed: the add did not reach the target, so qui adds the torrent again.")
	}
	if _, has, err := e.svc.deps.Sync.HasTorrentByAnyHash(ctx, m.TargetInstanceID, []string{m.Hash}); err == nil && has {
		e.result.AlreadyOnTarget = true
		e.logf("The target already has the torrent, so qui adds nothing.")
		return e.end(ctx, "", false), true
	}

	data, _, tracker, err := e.svc.deps.Sync.ExportTorrent(ctx, e.rec.SourceInstanceID, m.Hash)
	e.result.TrackerDomain = tracker
	switch {
	case ctx.Err() != nil:
		return ExportResult{}, false
	case err != nil:
		return e.end(ctx, "Export failed: "+err.Error(), false), true
	case len(data) == 0:
		return e.end(ctx, "Export returned empty torrent data", false), true
	}

	if err := e.mark(ctx, StateFinishing, stepAdd); err != nil {
		if ctx.Err() != nil {
			return ExportResult{}, false
		}
		return e.end(ctx, "qui cannot record the Export run: "+err.Error(), false), true
	}
	added := e.svc.adder.Add(ctx, e.addRequest(data))
	if added.Failure != torrentadd.AddRefused {
		e.rec.Steps = append(e.rec.Steps, Step{Name: stepAdded, At: e.svc.deps.Now()})
		e.save(context.WithoutCancel(ctx))
	}
	switch {
	case ctx.Err() != nil:
		return ExportResult{}, false
	case added.Failure == torrentadd.AddRefused:
		return e.end(ctx, "Add to target failed: "+added.Err.Error(), false), true
	}
	if added.Failure == torrentadd.AfterAddFailed {
		return e.end(ctx, "Recheck on target failed: "+added.Err.Error(), true), true
	}
	return e.end(ctx, e.verify(ctx, added), true), true
}

func (e *exportJob) endResumed(ctx context.Context, reason string) ExportResult {
	if reason == "" || e.reached(stepAdded) {
		return e.end(ctx, reason, true)
	}
	return e.end(ctx, "Ownership unknown: the target has the torrent, but its add did not answer, so qui cannot tell "+
		"who added it, and keeps it there. "+reason, false)
}

func (e *exportJob) addRequest(data []byte) torrentadd.Request {
	m, cfg := e.member(), e.svc.deps.Config
	return torrentadd.Request{InstanceID: m.TargetInstanceID, Torrent: data, Hash: m.Hash,
		Settings: exportSettings(m.Placement, e.rec.Options), Polls: cfg.ExportPolls, PollInterval: cfg.ExportPollInterval}
}

func (e *exportJob) verify(ctx context.Context, added torrentadd.Result) string {
	m, cfg := e.member(), e.svc.deps.Config
	reason := exportFailure(added, cfg, *e.rec.Options.SkipChecking)
	if reason == "" {
		return ""
	}
	torrent, found, err := e.svc.deps.Sync.HasTorrentByAnyHash(ctx, m.TargetInstanceID, []string{m.Hash})
	if err == nil && found && torrent.State != qbt.TorrentStateMissingFiles && torrent.State != qbt.TorrentStateError &&
		torrent.Progress >= 1 {
		e.logf("The torrent turned healthy after the wait for it.")
		return ""
	}
	return reason
}

func (e *exportJob) mark(ctx context.Context, state State, name string) error {
	e.rec.State = state
	e.rec.Steps = append(e.rec.Steps, Step{Name: name, At: e.svc.deps.Now()})
	return e.svc.deps.Store.Save(ctx, e.rec)
}

func (e *exportJob) end(ctx context.Context, reason string, remove bool) ExportResult {
	if !e.svc.commit(e.rec.ID) {
		return e.cancel(ctx)
	}
	ctx, cancel := cleanupContext(ctx)
	defer cancel()
	m := e.member()
	event := log.Info()
	if reason == "" {
		e.logf("Done.")
		_ = e.finish(ctx, StateDone)
	} else {
		event = log.Warn().Str("reason", reason)
		e.rec.FailureReason = reason
		e.result.Failure = reason
		if remove {
			if !e.reached(stepCleanUp) {
				e.logf("Failed: %s", reason)
				e.rec.State = StateFailed
				e.rec.Steps = append(e.rec.Steps, Step{Name: stepCleanUp, At: e.svc.deps.Now()})
				e.save(ctx)
			}
			e.removeFromTarget(ctx)
		} else {
			e.logf("Failed: %s", reason)
		}
		_ = e.finish(ctx, StateFailed)
	}
	event.Int64("transfer", e.rec.ID).Int("rule", *e.rec.RuleID).Int("source", e.rec.SourceInstanceID).
		Int("target", m.TargetInstanceID).Str("hash", m.Hash).Msg("transfer: the Export run ended")
	return e.result
}

func (e *exportJob) cancel(ctx context.Context) ExportResult {
	ctx, cancel := cleanupContext(ctx)
	defer cancel()
	if !e.reached(stepCleanUp) {
		e.logf("Cancelled.")
		e.rec.State = StateCancelled
		e.rec.Steps = append(e.rec.Steps, Step{Name: stepCleanUp, At: e.svc.deps.Now()})
		e.save(ctx)
	}
	if e.reached(stepAdded) {
		e.removeFromTarget(ctx)
	}
	_ = e.finish(ctx, StateCancelled)
	log.Info().Int64("transfer", e.rec.ID).Int("rule", *e.rec.RuleID).Msg("transfer: the Export run was cancelled")
	return ExportResult{Failure: "Cancelled."}
}

func (e *exportJob) removeFromTarget(ctx context.Context) {
	m := e.member()
	target, err := e.svc.deps.Instances.Get(ctx, m.TargetInstanceID)
	switch {
	case err != nil:
		e.logf("qui could not remove the torrent from the target: %v", err)
		return
	case e.targetMoved(target):
		e.logf("The target has a new address, so qui leaves the torrent there.")
		return
	}
	actor := qbittorrent.DeleteActor{Kind: qbittorrent.DeleteActorAutomation, RuleName: e.ruleName}
	if err := e.svc.deps.Sync.DeleteTorrents(ctx, m.TargetInstanceID, []string{m.Hash}, false, actor); err != nil {
		e.logf("qui could not remove the torrent from the target: %v", err)
		return
	}
	e.logf("Removed the torrent from the target, and kept its files.")
}

func (e *exportJob) targetMoved(target *models.Instance) bool {
	return fsops.EndpointOf(target).URL != e.rec.TargetEndpoint.URL
}

func exportSettings(p Placement, o Options) torrentadd.Settings {
	skip := *o.SkipChecking
	settings := torrentadd.Settings{
		Category:      p.Category,
		Tags:          p.Tags,
		ContentLayout: qbt.ContentLayout(p.ContentLayout),
		SeedMode:      skip,
		Recheck:       o.Paused && !skip,
	}
	switch {
	case p.SavePath != "":
		settings.AutoTMM, settings.SavePath = new(false), p.SavePath
	case p.Category != "":
		settings.AutoTMM = new(true)
	}
	if o.Paused {
		settings.Stopped = new(true)
	}
	return settings
}

func exportFailure(added torrentadd.Result, cfg Config, skipChecking bool) string {
	switch added.Failure {
	case torrentadd.MissingFiles:
		return fmt.Sprintf("Files missing on target instance (savePath: %s)", added.Torrent.SavePath)
	case torrentadd.Errored:
		return "Torrent in error state on target instance"
	case torrentadd.PollFailed:
		return fmt.Sprintf("Verification failed: %v", added.Err)
	default:
	}
	if added.Outcome == torrentadd.Verified || (added.Outcome == torrentadd.Checking && !skipChecking) {
		return ""
	}
	return fmt.Sprintf("Verification timed out after %d attempts (%v)", cfg.ExportPolls,
		time.Duration(cfg.ExportPolls)*cfg.ExportPollInterval)
}
