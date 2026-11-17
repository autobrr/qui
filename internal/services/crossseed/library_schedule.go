// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/activity"
	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/internal/services/systemnotice"
)

const (
	// libraryScheduleRetryDelay is the wait after a scheduled run could not start.
	libraryScheduleRetryDelay = time.Hour
	// libraryScheduleBusyDelay is the longest wait for an active Library Scan
	// run to end. The end of a run also wakes the loop.
	libraryScheduleBusyDelay = time.Minute
	// libraryScheduleIdleDelay is the longest wait while the schedule is off.
	// A save of the Library settings also wakes the loop.
	libraryScheduleIdleDelay = time.Hour
	// The schedule interval and age window stop at 100 years, because a
	// larger value overflows a time.Duration.
	maxScheduleIntervalHours = 100 * 365 * 24
	maxScheduleMaxAgeDays    = 100 * 365
)

// errScheduleNoInstance is the answer when the saved Library settings name no
// instance, or an instance that was deleted.
var errScheduleNoInstance = fmt.Errorf("%w: the saved Library settings name no instance", ErrInvalidRequest)

// Reasons of the "Library Scan schedule cannot run" System notice.
const (
	scheduleBlockedTorznabOffNoGazelle = "torznab_off_no_gazelle"
	scheduleBlockedNoIndexer           = "no_indexer"
	scheduleBlockedInstanceMissing     = "instance_missing"
)

// libraryScheduleLoop starts a scheduled run each time the Library Scan
// schedule is due, until ctx ends.
func (s *Service) libraryScheduleLoop(ctx context.Context) {
	log.Debug().Msg("[CROSSSEED-SCHEDULE] Library Scan schedule loop started")
	defer log.Debug().Msg("[CROSSSEED-SCHEDULE] Library Scan schedule loop stopped")

	for {
		timer := time.NewTimer(s.libraryScheduleTick(ctx))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-s.scheduleWake:
			timer.Stop()
		}
	}
}

// libraryScheduleTick starts a scheduled run when one is due, and returns the
// wait until the next check.
func (s *Service) libraryScheduleTick(ctx context.Context) time.Duration {
	settings, err := s.GetSearchSettings(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("[CROSSSEED-SCHEDULE] Failed to load the Library settings")
		return libraryScheduleBusyDelay
	}
	if !settings.ScheduleEnabled {
		s.endScheduleOutage()
		return libraryScheduleIdleDelay
	}
	next, err := s.nextScheduledSearchRun(ctx, settings)
	if err != nil {
		log.Warn().Err(err).Msg("[CROSSSEED-SCHEDULE] Failed to load the last scheduled run")
		return libraryScheduleBusyDelay
	}
	if wait := time.Until(next); wait > 0 {
		return wait
	}

	if _, err := s.startScheduledSearchRun(ctx, settings); err != nil {
		if errors.Is(err, ErrSearchRunActive) {
			return libraryScheduleBusyDelay
		}
		if ctx.Err() == nil {
			s.scheduledStartFailed(ctx, err)
		}
		return libraryScheduleRetryDelay
	}
	return scheduleInterval(settings)
}

// RunScheduledSearchNow starts a scheduled run with the saved Library settings,
// and the schedule counts its next run from this start. It returns
// ErrSearchRunActive while a Library Scan run is active.
func (s *Service) RunScheduledSearchNow(ctx context.Context) (*models.CrossSeedSearchRun, error) {
	if s.automationStore == nil {
		return nil, errors.New("cross-seed automation not configured")
	}
	settings, err := s.GetSearchSettings(ctx)
	if err != nil {
		return nil, err
	}
	// The end of the run wakes the loop, which then counts from this start.
	return s.startScheduledSearchRun(ctx, settings)
}

// freshTorrentReader reads the torrents after a sync with qBittorrent. The
// sync manager of qui has it. Most test fakes do not.
type freshTorrentReader interface {
	GetTorrentRefsFresh(ctx context.Context, instanceID int, filter qbittorrent.TorrentRefFilter) ([]*qbt.Torrent, error)
}

var _ freshTorrentReader = (*qbittorrent.SyncManager)(nil)

// startScheduledSearchRun starts a scheduled run with the saved Library
// settings and saves its start as the last scheduled start.
func (s *Service) startScheduledSearchRun(ctx context.Context, settings *models.CrossSeedSearchSettings) (*models.CrossSeedSearchRun, error) {
	if settings.InstanceID == nil {
		return nil, errScheduleNoInstance
	}
	instanceID := *settings.InstanceID
	if s.searchRunActive() {
		return nil, ErrSearchRunActive
	}
	// A scheduled run that cannot read the torrents does not start, so the
	// schedule tries again in an hour and not after a full interval. The read
	// syncs with qBittorrent, because the cached list stays after qBittorrent
	// goes offline.
	if s.syncManager != nil {
		read := s.syncManager.GetTorrentRefs
		if fresh, ok := s.syncManager.(freshTorrentReader); ok {
			read = fresh.GetTorrentRefsFresh
		}
		if _, err := read(ctx, instanceID, qbittorrent.TorrentRefFilter{}); err != nil {
			return nil, fmt.Errorf("read the torrents of instance %d: %w", instanceID, err)
		}
	}

	run, err := s.StartSearchRun(ctx, SearchRunOptions{
		InstanceID:             instanceID,
		Categories:             settings.Categories,
		Tags:                   settings.Tags,
		ExcludeCategories:      settings.ExcludeCategories,
		ExcludeTags:            settings.ExcludeTags,
		IntervalSeconds:        settings.IntervalSeconds,
		IndexerIDs:             settings.IndexerIDs,
		DisableTorznab:         !settings.TorznabEnabled,
		CooldownMinutes:        settings.CooldownMinutes,
		RequestedBy:            "schedule",
		SkipIndividualEpisodes: settings.SkipIndividualEpisodes,
		MaxAddedAgeDays:        settings.MaxAddedAgeDays,
		Kind:                   models.CrossSeedSearchRunKindScheduled,
		ScheduleMaxSearches:    settings.ScheduleMaxSearches,
		ScheduleMaxAgeDays:     settings.ScheduleMaxAgeDays,
	})
	if err != nil {
		return nil, err
	}

	s.scheduleMu.Lock()
	s.scheduleLastStart = run.StartedAt
	s.scheduleMu.Unlock()
	if err := s.automationStore.SetLastScheduledSearchRun(context.WithoutCancel(ctx), run.StartedAt); err != nil {
		// The start in memory keeps the schedule until qui restarts.
		log.Warn().Err(err).Msg("[CROSSSEED-SCHEDULE] Failed to save the start of the scheduled run")
	}
	s.endScheduleOutage()
	log.Info().Int64("runID", run.ID).Int("instanceID", instanceID).Msg("[CROSSSEED-SCHEDULE] Started a scheduled Library Scan run")
	return run, nil
}

// nextScheduledSearchRun returns when the next scheduled run starts: one
// interval after the last scheduled start, or at once when no scheduled run
// started yet. After a failed start, it is no earlier than the hourly retry.
func (s *Service) nextScheduledSearchRun(ctx context.Context, settings *models.CrossSeedSearchSettings) (time.Time, error) {
	last, found, err := s.automationStore.LastScheduledSearchRun(ctx)
	if err != nil {
		return time.Time{}, err
	}
	s.scheduleMu.Lock()
	if s.scheduleLastStart.After(last) {
		last, found = s.scheduleLastStart, true
	}
	retryAt := s.scheduleRetryAt
	s.scheduleMu.Unlock()
	next := time.Now().UTC()
	if found {
		next = last.Add(scheduleInterval(settings))
	}
	if retryAt.After(next) {
		next = retryAt
	}
	return next, nil
}

// nextScheduledSearchRunAt is the next scheduled start for the search status,
// or nil while the schedule is off.
func (s *Service) nextScheduledSearchRunAt(ctx context.Context) *time.Time {
	if s.automationStore == nil {
		return nil
	}
	settings, err := s.GetSearchSettings(ctx)
	if err != nil || !settings.ScheduleEnabled {
		return nil
	}
	next, err := s.nextScheduledSearchRun(ctx, settings)
	if err != nil {
		return nil
	}
	return &next
}

func scheduleInterval(settings *models.CrossSeedSearchSettings) time.Duration {
	return time.Duration(settings.ScheduleIntervalHours) * time.Hour
}

func (s *Service) searchRunActive() bool {
	s.searchMu.RLock()
	defer s.searchMu.RUnlock()
	return s.searchCancel != nil
}

// scheduledStartFailed sets the hourly retry, and tells the user once for each
// outage: by the System notice when the settings block the schedule, else by
// a failure notification.
func (s *Service) scheduledStartFailed(ctx context.Context, err error) {
	reason := scheduleBlockedReason(err)
	log.Warn().Err(err).Str("reason", reason).Msg("[CROSSSEED-SCHEDULE] A scheduled Library Scan run could not start; trying again in an hour")

	s.scheduleMu.Lock()
	s.scheduleRetryAt = time.Now().UTC().Add(libraryScheduleRetryDelay)
	notified := s.scheduleFailureNotified
	s.scheduleFailureNotified = true
	s.scheduleMu.Unlock()
	if s.activityPublisher != nil {
		s.activityPublisher.Publish(activity.Event{Kind: activity.KindCrossSeedSearch})
	}

	if reason != "" && s.systemNotices != nil {
		// The notice sends its own notification when it opens.
		s.systemNotices.OpenWithReason(systemnotice.LibraryScanScheduleBlocked, reason, err.Error()) //nolint:contextcheck // the notification has its own timeout, as each System notice does
		return
	}
	if notified || s.notifier == nil {
		return
	}
	msg := fmt.Sprintf("The Library Scan schedule could not start a run: %v. qui tries again every hour.", err)
	s.notifier.Notify(context.WithoutCancel(ctx), notifications.Event{
		Type:         notifications.EventCrossSeedSearchFailed,
		InstanceName: "Library Scan schedule",
		Message:      msg,
		ErrorMessage: err.Error(),
		ErrorMessages: []string{
			err.Error(),
		},
	})
}

// endScheduleOutage closes the System notice and forgets the failed starts.
func (s *Service) endScheduleOutage() {
	s.scheduleMu.Lock()
	s.scheduleRetryAt = time.Time{}
	s.scheduleFailureNotified = false
	s.scheduleMu.Unlock()
	if s.systemNotices != nil {
		s.systemNotices.Close(systemnotice.LibraryScanScheduleBlocked)
	}
}

// scheduleBlockedReason returns the System notice reason for a start that
// failed on the settings, or "" for a fault that clears without the user.
func scheduleBlockedReason(err error) string {
	switch {
	case errors.Is(err, errTorznabOffNoGazelle):
		return scheduleBlockedTorznabOffNoGazelle
	case errors.Is(err, ErrNoIndexersConfigured):
		return scheduleBlockedNoIndexer
	case errors.Is(err, errScheduleNoInstance):
		return scheduleBlockedInstanceMissing
	default:
		return ""
	}
}

// forgetLastScheduledStart makes the schedule due at once.
func (s *Service) forgetLastScheduledStart(ctx context.Context) error {
	if err := s.automationStore.ClearLastScheduledSearchRun(ctx); err != nil {
		return err
	}
	s.scheduleMu.Lock()
	s.scheduleLastStart = time.Time{}
	s.scheduleMu.Unlock()
	return nil
}

// librarySettingsSaved lets a fix in the saved settings take effect at once.
func (s *Service) librarySettingsSaved() {
	s.scheduleMu.Lock()
	s.scheduleRetryAt = time.Time{}
	s.scheduleMu.Unlock()
	s.wakeLibrarySchedule()
}

func (s *Service) wakeLibrarySchedule() {
	select {
	case s.scheduleWake <- struct{}{}:
	default:
	}
}
