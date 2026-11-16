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
)

var (
	// ErrCancelTooLate refuses a cancel once the removal of the torrent from
	// the source started.
	ErrCancelTooLate = errors.New("the removal from the source started, so a cancel no longer rolls the Transfer back")
	// ErrFinished refuses a cancel of a Transfer that ended.
	ErrFinished = errors.New("the Transfer ended")

	errShutdown  = errors.New("transfers are shutting down")
	errCancelled = errors.New("the Transfer was cancelled")
)

// Config bounds the waits and the flushes of a Transfer.
type Config struct {
	// Polls and PollInterval bound each wait for qBittorrent. CheckPolls
	// bounds the wait for the check of every piece on the target.
	Polls        int
	CheckPolls   int
	PollInterval time.Duration
	// FlushBytes is how many bytes qui copies between two flushes. A piece
	// becomes a Proven piece at the confirmed flush after it.
	FlushBytes int64
	// Reconnects is how many times in a row the copy goes on after a
	// dropped connection that proved no piece, ReconnectWait apart.
	Reconnects    int
	ReconnectWait time.Duration
	// WakeInterval is how often qui looks for a Transfer that may run again.
	// Zero turns it off.
	WakeInterval time.Duration
}

func DefaultConfig() Config {
	return Config{Polls: 30, CheckPolls: 3600, PollInterval: time.Second, FlushBytes: 64 << 20,
		Reconnects: 6, ReconnectWait: 10 * time.Second, WakeInterval: time.Minute}
}

// GroupFinder finds the torrents that read the files of a torrent, with the
// check of the delete dialog.
type GroupFinder interface {
	FindDeleteMatches(ctx context.Context, selection map[int][]string) (*crossseed.DeleteMatchesResponse, error)
}

type Deps struct {
	Store     *Store
	Instances *models.InstanceStore
	Sync      *qbittorrent.SyncManager
	Files     *fsops.Pool
	Groups    GroupFinder
	// Routes and CrossSeeds send the cross-seeds of a group to the
	// Cross-seed instance of the target. Without them, a group lands whole on
	// the target.
	Routes     RouteReader
	CrossSeeds CrossSeeder
	// Now is the clock of the steps and the log. Nil is time.Now.
	Now    func() time.Time
	Config Config
}

// Service runs Transfers: one at a time for each Transfer source, in the
// order they started.
type Service struct {
	deps   Deps
	adder  *torrentadd.Adder
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	// ticked is closed once the loop of Wake ended.
	ticked chan struct{}

	mu     sync.Mutex
	closed bool
	// queues holds the Transfers of each source, the running one first.
	queues map[int][]int64
	// runs holds each Transfer that is queued, runs, or is being cancelled.
	runs map[int64]*run
	// reserved is the free space that the running Transfers need on each
	// target.
	reserved map[int]int64
}

// run is a Transfer that the Service holds.
type run struct {
	// cancel stops the Transfer while it runs.
	cancel    context.CancelFunc
	cancelled bool
	// committed is set once a cancel no longer rolls the Transfer back.
	committed bool
}

// NewService returns a Service whose Transfers stop when ctx ends or at
// Close. A stopped Transfer keeps its partial data and stays unfinished. The
// Service resumes or cleans up each unfinished Transfer at once, and looks
// for one that may run again every Config.WakeInterval while one is left.
func NewService(ctx context.Context, deps Deps) *Service {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Service{
		deps:     deps,
		adder:    torrentadd.New(deps.Sync),
		ctx:      ctx,
		cancel:   cancel,
		ticked:   make(chan struct{}),
		queues:   map[int][]int64{},
		runs:     map[int64]*run{},
		reserved: map[int]int64{},
	}
	s.Wake()                            //nolint:contextcheck // the Service runs on its own context, s.ctx
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
		}
	}
}

// Wake queues each unfinished Transfer that may run now: one that a stop
// interrupted, one whose retry time came, and one whose instances are on
// again.
func (s *Service) Wake() {
	ids, err := s.deps.Store.Unfinished(s.ctx)
	if err != nil {
		if s.ctx.Err() == nil {
			log.Error().Err(err).Msg("transfer: cannot read the unfinished Transfers")
		}
		return
	}
	now := s.deps.Now()
	for _, id := range ids {
		if s.holds(id) {
			continue
		}
		record, err := s.deps.Store.Get(s.ctx, id)
		if err != nil {
			log.Error().Err(err).Int64("transfer", id).Msg("transfer: cannot read the Transfer")
			continue
		}
		if record.Kind != KindTransfer || record.RetryAt.After(now) || s.off(s.ctx, record) {
			continue
		}
		s.enqueue(record)
	}
}

func (s *Service) holds(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.runs[id]
	return ok
}

// off reports whether an instance of the Transfer is turned off.
func (s *Service) off(ctx context.Context, r *Record) bool {
	for _, id := range []int{r.SourceInstanceID, r.TargetInstanceID} {
		if inst, err := s.deps.Instances.Get(ctx, id); err == nil && !inst.IsActive {
			return true
		}
	}
	return false
}

// Start checks the Transfer of one torrent, records it with its claim on the
// torrent, and runs it after the Transfers from the same source that started
// before it. A *RefusalError says why qui does not start it, and ErrClaimed that
// another Transfer holds the torrent.
func (s *Service) Start(ctx context.Context, req Request) (int64, error) {
	if s.ctx.Err() != nil {
		return 0, errShutdown
	}
	record, err := s.check(ctx, req)
	if err != nil {
		return 0, err
	}
	if err := s.deps.Store.Create(ctx, record); err != nil {
		return 0, err
	}
	s.enqueue(record) //nolint:contextcheck // the Transfer outlives the request and runs in the context of the Service
	return record.ID, nil
}

func (s *Service) enqueue(r *Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[r.ID]; ok || s.closed {
		return
	}
	s.runs[r.ID] = &run{}
	queue := s.queues[r.SourceInstanceID]
	s.queues[r.SourceInstanceID] = append(queue, r.ID)
	if len(queue) == 0 {
		s.wg.Go(func() { s.drain(r.SourceInstanceID) })
	}
}

// drain runs the Transfers of one source until its queue is empty.
func (s *Service) drain(sourceID int) {
	for {
		s.mu.Lock()
		queue := s.queues[sourceID]
		if len(queue) == 0 || s.ctx.Err() != nil {
			delete(s.queues, sourceID)
			s.mu.Unlock()
			return
		}
		id := queue[0]
		ctx, cancel := context.WithCancel(s.ctx)
		s.runs[id].cancel = cancel
		s.mu.Unlock()

		s.runOne(ctx, id)
		cancel()

		s.mu.Lock()
		s.queues[sourceID] = s.queues[sourceID][1:]
		delete(s.runs, id)
		s.mu.Unlock()
	}
}

func (s *Service) runOne(ctx context.Context, id int64) {
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

// Cancel stops a Transfer and takes back what it did: it removes the files
// that qui wrote and the torrents that qui added, and starts the source
// again. The cleanup runs in the background. Once the removal of the
// torrent from the source started, Cancel returns ErrCancelTooLate.
func (s *Service) Cancel(ctx context.Context, id int64) error {
	record, err := s.deps.Store.Get(ctx, id)
	switch {
	case err != nil:
		return err
	case !record.FinishedAt.IsZero():
		return ErrFinished
	case s.newJob(record).reached(stepRemoveFromSource):
		return ErrCancelTooLate
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errShutdown
	}
	r, held := s.runs[id]
	switch {
	case !held:
		s.runs[id] = &run{cancelled: true}
	case r.committed:
		return ErrCancelTooLate
	case r.cancelled:
		return nil
	case r.cancel != nil:
		r.cancelled = true
		r.cancel()
		return nil
	default:
		r.cancelled = true
		s.queues[record.SourceInstanceID] = slices.DeleteFunc(s.queues[record.SourceInstanceID], func(queued int64) bool { return queued == id })
	}
	s.wg.Go(func() { s.rollBack(context.WithoutCancel(ctx), id) })
	return nil
}

// rollBack takes back what a cancelled Transfer did that does not run.
func (s *Service) rollBack(ctx context.Context, id int64) {
	defer func() {
		s.mu.Lock()
		delete(s.runs, id)
		s.mu.Unlock()
	}()
	record, err := s.deps.Store.Get(ctx, id)
	if err != nil {
		log.Error().Err(err).Int64("transfer", id).Msg("transfer: cannot read the Transfer")
		return
	}
	if record.FinishedAt.IsZero() {
		s.newJob(record).rollBack(ctx)
	}
}

func (s *Service) cancelled(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	return ok && r.cancelled
}

// commit ends the time in which a cancel rolls the Transfer back. It
// reports false when the user cancelled the Transfer first.
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
	return &job{svc: s, rec: record, away: map[int]bool{}}
}

// Wait waits until every Transfer that started has ended or stopped.
func (s *Service) Wait() {
	s.wg.Wait()
}

// Close stops the running Transfers and waits for them. They keep their
// partial data and stay unfinished.
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	<-s.ticked
	s.wg.Wait()
}

// reserve claims bytes of the free space on a target, and fails when free
// does not hold them next to what the running Transfers to it claimed.
func (s *Service) reserve(targetID int, bytes, free int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if others := s.reserved[targetID]; free-others < bytes {
		return fmt.Errorf("not enough free space on the target: the Transfer needs %s, and %s is free", formatBytes(bytes), formatBytes(max(free-others, 0)))
	}
	s.reserved[targetID] += bytes
	return nil
}

func (s *Service) unreserve(targetID int, bytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reserved[targetID] -= bytes
	if s.reserved[targetID] <= 0 {
		delete(s.reserved, targetID)
	}
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
