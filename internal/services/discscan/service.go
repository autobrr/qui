// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package discscan

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/autobrr/go-bdinfo/pkg/bdinfo"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/activity"
)

var ErrRunNotFound = errors.New("disc scan not found")

var ErrRunFinished = errors.New("disc scan already finished")

const (
	interruptedMessage = "interrupted by qui restart"
	progressInterval   = 2 * time.Second
	dbRetryInterval    = 5 * time.Second
)

type Scanner func(ctx context.Context, path string, onProgress func(processed, total int64)) (bdinfo.Result, error)

type Service struct {
	store             *models.DiscScanStore
	activityPublisher activity.Publisher
	scan              Scanner
	wake              chan struct{}

	mu            sync.Mutex
	runningID     int64
	cancelRunning context.CancelFunc
}

func NewService(store *models.DiscScanStore) *Service {
	return &Service{
		store:             store,
		activityPublisher: activity.NopPublisher{},
		scan:              runBDInfo,
		wake:              make(chan struct{}, 1),
	}
}

func (s *Service) SetActivityPublisher(publisher activity.Publisher) {
	if publisher == nil {
		return
	}
	s.activityPublisher = publisher
}

func (s *Service) SetScanner(scan Scanner) {
	s.scan = scan
}

func (s *Service) Start(ctx context.Context) {
	if err := s.store.MarkInterrupted(ctx, interruptedMessage); err != nil {
		log.Error().Err(err).Msg("discscan: failed to mark interrupted runs")
	}
	go s.loop(ctx)
}

func (s *Service) Enqueue(ctx context.Context, instanceID int, torrentHash, discPath, resolvedPath string, force bool) (*models.DiscScanRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	latest, err := s.store.Latest(ctx, instanceID, resolvedPath)
	if err != nil {
		return nil, err
	}
	if latest != nil && (latest.Active() || (!force && latest.Status == models.DiscScanStatusCompleted)) {
		return latest, nil
	}

	id, err := s.store.Create(ctx, instanceID, torrentHash, discPath, resolvedPath)
	if err != nil {
		return nil, err
	}
	s.emit(instanceID, id)
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return s.store.GetByInstance(ctx, instanceID, id)
}

func (s *Service) Cancel(ctx context.Context, instanceID int, runID int64) (*models.DiscScanRun, error) {
	run, err := s.store.GetByInstance(ctx, instanceID, runID)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, ErrRunNotFound
	}

	changed, err := s.store.MarkCanceled(ctx, runID)
	if err != nil {
		return nil, err
	}
	if !changed {
		return nil, ErrRunFinished
	}

	s.mu.Lock()
	if s.runningID == runID && s.cancelRunning != nil {
		s.cancelRunning()
	}
	s.mu.Unlock()

	s.emit(instanceID, runID)
	return s.store.GetByInstance(ctx, instanceID, runID)
}

func (s *Service) loop(ctx context.Context) {
	for {
		run, err := s.store.NextPending(ctx)
		if err == nil && run != nil {
			err = s.process(ctx, run)
		}
		if err != nil {
			log.Error().Err(err).Msg("discscan: worker error")
			select {
			case <-ctx.Done():
				return
			case <-time.After(dbRetryInterval):
			}
			continue
		}
		if run == nil {
			select {
			case <-ctx.Done():
				return
			case <-s.wake:
			}
		}
	}
}

func (s *Service) process(ctx context.Context, run *models.DiscScanRun) error {
	scanCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.runningID, s.cancelRunning = run.ID, cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.runningID, s.cancelRunning = 0, nil
		s.mu.Unlock()
		cancel()
	}()

	started, err := s.store.MarkScanning(ctx, run.ID)
	if err != nil {
		return fmt.Errorf("start run %d: %w", run.ID, err)
	}
	if !started {
		return nil
	}
	s.emit(run.InstanceID, run.ID)

	var lastWrite time.Time
	onProgress := func(processed, total int64) {
		if time.Since(lastWrite) < progressInterval {
			return
		}
		lastWrite = time.Now()
		if err := s.store.UpdateProgress(ctx, run.ID, processed, total); err != nil {
			log.Warn().Err(err).Int64("runID", run.ID).Msg("discscan: failed to write progress")
			return
		}
		s.emit(run.InstanceID, run.ID)
	}

	log.Info().Int64("runID", run.ID).Str("path", run.ResolvedPath).Msg("discscan: scanning disc")
	result, err := s.scan(scanCtx, run.ResolvedPath, onProgress)
	switch {
	case scanCtx.Err() != nil:
	case err != nil:
		log.Error().Err(err).Int64("runID", run.ID).Str("path", run.ResolvedPath).Msg("discscan: scan failed")
		if _, err := s.store.MarkFailed(ctx, run.ID, err.Error()); err != nil {
			log.Error().Err(err).Int64("runID", run.ID).Msg("discscan: failed to mark run failed")
		}
	default:
		if _, err := s.store.MarkCompleted(ctx, run.ID, result.Report, result.QuickSummary, result.ForumsBlock); err != nil {
			log.Error().Err(err).Int64("runID", run.ID).Msg("discscan: failed to store report")
		}
	}
	s.emit(run.InstanceID, run.ID)
	return nil
}

func (s *Service) emit(instanceID int, runID int64) {
	s.activityPublisher.Publish(activity.Event{
		Kind:       activity.KindDiscScanRun,
		InstanceID: instanceID,
		ResourceID: strconv.FormatInt(runID, 10),
	})
}

func runBDInfo(ctx context.Context, path string, onProgress func(processed, total int64)) (bdinfo.Result, error) {
	settings := bdinfo.DefaultSettings("")
	settings.MainPlaylistOnly = true
	settings.GenerateStreamDiagnostics = true
	settings.ExtendedStreamDiagnostics = false
	settings.IncludeVersionAndNotes = false

	return bdinfo.Run(ctx, bdinfo.Options{
		Path:     path,
		Settings: settings,
		OnProgress: func(ev bdinfo.ProgressEvent) {
			if ev.TotalBytes > 0 {
				onProgress(int64(ev.ProcessedBytes), int64(ev.TotalBytes)) // #nosec G115 -- disc sizes fit int64
			}
		},
	})
}
