// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent/torrentadd"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/internal/services/peeraddress"
)

var (
	ErrCancelTooLate = errors.New("the removal from the source started, so a cancel no longer rolls the Transfer back")
	ErrFinished      = errors.New("the Transfer ended")

	errShutdown  = errors.New("transfers are shutting down")
	errCancelled = errors.New("the Transfer was cancelled")
)

type Config struct {
	Polls              int
	CheckPolls         int
	PollInterval       time.Duration
	FlushBytes         int64
	ReadBytes          int64
	Reconnects         int
	ReconnectWait      time.Duration
	WakeInterval       time.Duration
	ExportPolls        int
	ExportPollInterval time.Duration
	PeerPolls          int
	SnapshotAge        time.Duration
	PerSource          int
}

func DefaultConfig() Config {
	return Config{PerSource: 3, Polls: 30, CheckPolls: 3600, PollInterval: time.Second, FlushBytes: 64 << 20, ReadBytes: 16 << 20,
		Reconnects: 6, ReconnectWait: 10 * time.Second, WakeInterval: time.Minute,
		ExportPolls: 10, ExportPollInterval: 3 * time.Second, PeerPolls: 120, SnapshotAge: 2500 * time.Millisecond}
}

type PeerAddresses interface {
	PublicPeerAddress(ctx context.Context, instanceID int) (string, error)
}

type GroupFinder interface {
	FindDeleteMatches(ctx context.Context, selection map[int][]string) (*crossseed.DeleteMatchesResponse, error)
}

type Deps struct {
	Store      *Store
	Instances  *models.InstanceStore
	Sync       *qbittorrent.SyncManager
	Files      *fsops.Pool
	Groups     GroupFinder
	Routes     RouteReader
	CrossSeeds CrossSeeder
	Peers      PeerAddresses
	Notifier   notifications.Notifier
	Now        func() time.Time
	Config     Config
}

type Service struct {
	deps   Deps
	adder  *torrentadd.Adder
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	ticked chan struct{}

	mu              sync.Mutex
	closed          bool
	queues          map[int]*queue
	runs            map[int64]*run
	reserved, freed map[int]int64
	landings        map[int64]landing
	arrivals        int64
	left            chan struct{}
	exports         chan struct{}
	purged          time.Time
	rollingBack     sync.Mutex
}

type queue struct {
	waiting []int64
	workers int
}

type run struct {
	cancel    context.CancelFunc
	cancelled bool
	committed bool
	meter     *meter
	targetID  int
}

func NewService(ctx context.Context, deps Deps) *Service {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Peers == nil {
		deps.Peers = peeraddress.New(deps.Instances, deps.Sync)
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Service{
		deps:     deps,
		adder:    torrentadd.New(deps.Sync),
		ctx:      ctx,
		cancel:   cancel,
		ticked:   make(chan struct{}),
		queues:   map[int]*queue{},
		runs:     map[int64]*run{},
		reserved: map[int]int64{},
		freed:    map[int]int64{},
		landings: map[int64]landing{},
		left:     make(chan struct{}),
		exports:  make(chan struct{}, exportsAtATime),
	}
	s.purge()                           //nolint:contextcheck // the Service runs on its own context, s.ctx
	s.wake(true)                        //nolint:contextcheck // the Service runs on its own context, s.ctx
	go s.tick(deps.Config.WakeInterval) //nolint:contextcheck // the Service runs on its own context, s.ctx
	return s
}

func (s *Service) tick(interval time.Duration) {
	defer close(s.ticked)
	if interval <= 0 {
		return
	}
	ticks := time.Tick(interval)
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticks:
			if s.deps.Store.HasClaims() {
				s.Wake()
			}
			if s.deps.Now().Sub(s.purged) >= purgeInterval {
				s.purge()
			}
		}
	}
}

func (s *Service) Wake() {
	s.wake(false)
}

func (s *Service) wake(exports bool) {
	ids, err := s.deps.Store.Unfinished(s.ctx)
	if err != nil {
		if s.ctx.Err() == nil {
			log.Error().Err(err).Msg("transfer: cannot read the unfinished Transfers")
		}
		return
	}
	now := s.deps.Now()
	unread := 0
	var readErr error
	for _, id := range ids {
		if s.holds(id) {
			continue
		}
		record, err := s.deps.Store.Get(s.ctx, id)
		if err != nil {
			log.Debug().Err(err).Int64("transfer", id).Msg("transfer: cannot read the Transfer")
			unread++
			readErr = err
			continue
		}
		if record.Kind == KindExport {
			if exports {
				s.export(record, make(chan ExportResult, 1))
			}
			continue
		}
		if record.Kind != KindTransfer || record.RetryAt.After(now) || s.off(s.ctx, record) {
			continue
		}
		s.enqueue(record)
	}
	if unread > 0 {
		log.Error().Err(readErr).Int("transfers", unread).Msg("transfer: cannot read Transfers to wake")
	}
}

func (s *Service) holds(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.runs[id]
	return ok
}

func (s *Service) off(ctx context.Context, r *Record) bool {
	for _, id := range []int{r.SourceInstanceID, r.TargetInstanceID} {
		if inst, err := s.deps.Instances.Get(ctx, id); err == nil && !inst.IsActive {
			return true
		}
	}
	return false
}

func (s *Service) Start(ctx context.Context, req Request) (int64, error) {
	if s.ctx.Err() != nil {
		return 0, errShutdown
	}
	plan, err := s.check(ctx, req)
	if err != nil {
		return 0, err
	}
	record := plan.record
	if err := s.deps.Store.Create(ctx, record); err != nil {
		return 0, err
	}
	s.enqueue(record) //nolint:contextcheck // the Transfer outlives the request and runs in the context of the Service
	return record.ID, nil
}

type Preview struct {
	Way      Way
	Bytes    int64
	Members  []Member
	Stay     int
	Versions []string
}

func (s *Service) Preview(ctx context.Context, req Request) (*Preview, error) {
	plan, err := s.check(ctx, req)
	if err != nil {
		return nil, err
	}
	record := plan.record
	for _, m := range record.Members {
		if s.deps.Store.IsClaimed(record.SourceInstanceID, m.Hash) {
			return nil, fmt.Errorf("%w: %s on instance %d", ErrClaimed, m.Hash, record.SourceInstanceID)
		}
	}
	files, err := s.deps.Files.GetBackend(ctx, record.SourceInstanceID)
	if err != nil {
		return nil, fmt.Errorf("read the files of instance %d: %w", record.SourceInstanceID, err)
	}
	return &Preview{Way: record.Way, Bytes: groupBytes(files.Paths(), plan.group), Members: record.Members,
		Stay: len(plan.stay), Versions: plan.versions}, nil
}

func (s *Service) enqueue(r *Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[r.ID]; ok || s.closed {
		return
	}
	s.runs[r.ID] = &run{targetID: r.TargetInstanceID}
	q := s.queues[r.SourceInstanceID]
	if q == nil {
		q = &queue{}
		s.queues[r.SourceInstanceID] = q
	}
	q.waiting = append(q.waiting, r.ID)
	if q.workers < max(s.deps.Config.PerSource, 1) {
		q.workers++
		s.wg.Go(func() { s.drain(r.SourceInstanceID) })
	}
}

func (s *Service) drain(sourceID int) {
	for {
		s.mu.Lock()
		q := s.queues[sourceID]
		if len(q.waiting) == 0 || s.ctx.Err() != nil {
			for _, id := range q.waiting {
				delete(s.runs, id)
			}
			q.waiting = nil
			if q.workers--; q.workers == 0 {
				delete(s.queues, sourceID)
			}
			s.mu.Unlock()
			return
		}
		id := q.waiting[0]
		q.waiting = q.waiting[1:]
		ctx, cancel := context.WithCancel(s.ctx)
		m := &meter{now: s.deps.Now}
		s.runs[id].cancel, s.runs[id].meter = cancel, m
		s.arrivals++
		s.landings[id] = landing{order: s.arrivals, sourceID: sourceID, targetID: s.runs[id].targetID}
		s.mu.Unlock()

		s.runOne(ctx, id, m)
		cancel()

		s.mu.Lock()
		cancelled := s.runs[id].cancelled
		if !cancelled {
			delete(s.runs, id)
			s.leave(id)
		}
		s.mu.Unlock()
		if cancelled {
			s.rollBack(context.WithoutCancel(s.ctx), id)
		}
	}
}

func (s *Service) runOne(ctx context.Context, id int64, m *meter) {
	record, err := s.deps.Store.Get(ctx, id)
	if err != nil {
		log.Error().Err(err).Int64("transfer", id).Msg("transfer: cannot read the Transfer")
		return
	}
	if !record.FinishedAt.IsZero() {
		return
	}
	j := s.newJob(record)
	j.meter = m
	if j.reached(stepCleanUp) {
		j.cleanUpAgain(ctx)
		return
	}
	if record.State == StateCancelled {
		if err := s.deps.Store.Finish(ctx, id, StateCancelled); err != nil {
			log.Error().Err(err).Int64("transfer", id).Msg("transfer: cannot finish the Transfer")
		}
		return
	}
	err = j.run(ctx)
	switch {
	case s.cancelled(id):
		j.rollBack(ctx)
	case s.ctx.Err() != nil:
		j.release()
		log.Info().Int64("transfer", id).Msg("transfer: stopped, the partial data stays")
	default:
		j.end(ctx, err)
	}
}

func (s *Service) Cancel(ctx context.Context, id int64) error {
	record, err := s.current(ctx, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errShutdown
	}
	r, held := s.runs[id]
	if !held {
		if record, err = s.current(ctx, id); err != nil {
			return err
		}
	}
	switch {
	case !held && record.Kind == KindExport:
		s.runs[id] = &run{cancelled: true}
		s.wg.Go(func() {
			(&exportJob{job: s.newJob(record), ruleName: record.Steps[0].Detail}).cancel(context.WithoutCancel(ctx))
			s.mu.Lock()
			delete(s.runs, id)
			s.mu.Unlock()
		})
		return nil
	case !held:
	case r.committed && record.Kind == KindExport:
		return ErrFinished
	case r.committed:
		return ErrCancelTooLate
	case r.cancelled:
		return nil
	case r.cancel != nil:
		r.cancelled = true
		r.cancel()
		return nil
	}
	ended := false
	if record.Kind == KindTransfer {
		if ended, err = s.keepCancel(ctx, record); err != nil {
			return err
		}
	}
	if held {
		r.cancelled = true
		if q := s.queues[record.SourceInstanceID]; q != nil {
			q.waiting = slices.DeleteFunc(q.waiting, func(queued int64) bool { return queued == id })
		}
	}
	if ended {
		delete(s.runs, id)
		return nil
	}
	if !held {
		s.runs[id] = &run{cancelled: true}
	}
	s.wg.Go(func() {
		s.rollingBack.Lock()
		defer s.rollingBack.Unlock()
		s.rollBack(context.WithoutCancel(ctx), id)
	})
	return nil
}

func (s *Service) current(ctx context.Context, id int64) (*Record, error) {
	record, err := s.deps.Store.Get(ctx, id)
	switch {
	case err != nil:
		return nil, err
	case !record.FinishedAt.IsZero():
		return nil, ErrFinished
	case s.newJob(record).reached(stepRemoveFromSource):
		return nil, ErrCancelTooLate
	}
	return record, nil
}

func (s *Service) keepCancel(ctx context.Context, record *Record) (ended bool, err error) {
	j := s.newJob(record)
	j.logf("Cancelled.")
	if !j.reached(stepPrepare) {
		return true, j.finish(ctx, StateCancelled)
	}
	record.State, record.RetryAt = StateCancelled, time.Time{}
	record.Steps = append(record.Steps, Step{Name: stepCleanUp, At: s.deps.Now()})
	return false, s.deps.Store.Save(ctx, record)
}

func (s *Service) rollBack(ctx context.Context, id int64) {
	defer func() {
		s.mu.Lock()
		delete(s.runs, id)
		s.leave(id)
		s.mu.Unlock()
	}()
	record, err := s.deps.Store.Get(ctx, id)
	if err != nil {
		log.Error().Err(err).Int64("transfer", id).Msg("transfer: cannot read the Transfer")
		return
	}
	if !record.FinishedAt.IsZero() {
		return
	}
	j := s.newJob(record)
	if j.reached(stepCleanUp) {
		j.cleanUpAgain(ctx)
		return
	}
	j.rollBack(ctx)
}

func (s *Service) cancelled(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	return ok && r.cancelled
}

func (s *Service) commit(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return true
	}
	if r.cancelled {
		return false
	}
	r.committed = true
	return true
}

func (s *Service) newJob(record *Record) *job {
	return &job{svc: s, rec: record, unanswered: map[string]bool{}, away: map[int]bool{}}
}

func (s *Service) Wait() {
	s.wg.Wait()
}

func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	stopped := make(chan struct{})
	go func() {
		<-s.ticked
		s.wg.Wait()
		close(stopped)
	}()
	select {
	case <-stopped:
		return nil
	case <-ctx.Done():
		s.mu.Lock()
		running := len(s.runs)
		s.mu.Unlock()
		log.Warn().Int("runs", running).Msg("transfer: runs did not stop in time, the next start resumes them")
		return ctx.Err()
	}
}

func (s *Service) reserve(targetID int, bytes int64, read space, own int64) (others int64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	others = s.reserved[targetID] - own + s.freed[targetID] - read.freed
	if read.free-own-others < bytes {
		return others, false
	}
	s.reserved[targetID] += bytes
	return others, true
}

func (s *Service) unreserve(targetID int, bytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.freed[targetID] += bytes
	s.reserved[targetID] -= bytes
	if s.reserved[targetID] <= 0 {
		delete(s.reserved, targetID)
	}
}

func (s *Service) freedOn(targetID int) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.freed[targetID]
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
