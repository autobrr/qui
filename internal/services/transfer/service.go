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
	// bounds the wait for the check of every piece on the target or the source.
	Polls        int
	CheckPolls   int
	PollInterval time.Duration
	// FlushBytes is how many bytes qui copies between two flushes. A piece
	// becomes a Proven piece at the confirmed flush after it.
	FlushBytes int64
	// ReadBytes is how many bytes all the connections of a copy read at
	// once. Zero reads one piece at a time.
	ReadBytes int64
	// Reconnects is how many times in a row the copy goes on after a
	// dropped connection that proved no piece, ReconnectWait apart.
	Reconnects    int
	ReconnectWait time.Duration
	// WakeInterval is how often qui looks for a Transfer that may run again.
	// Zero turns it off.
	WakeInterval time.Duration
	// ExportPolls and ExportPollInterval bound the wait of an Export run for
	// the torrent on the target.
	ExportPolls        int
	ExportPollInterval time.Duration
	// PeerPolls bounds the wait for the first bytes of a Transfer torrent in
	// each direction, and a download that gets no byte, PollInterval apart.
	PeerPolls int
	// SnapshotAge is how old the Sync snapshot can be when a long wait for a
	// Transfer torrent reads it without a sync of its own. A browser tab on
	// the instance syncs every 2 seconds, and the Transfers page reads the
	// progress every 3 seconds: the age lies between the two.
	SnapshotAge time.Duration
	PerSource   int
}

func DefaultConfig() Config {
	return Config{PerSource: 3, Polls: 30, CheckPolls: 3600, PollInterval: time.Second, FlushBytes: 64 << 20, ReadBytes: 16 << 20,
		Reconnects: 6, ReconnectWait: 10 * time.Second, WakeInterval: time.Minute,
		ExportPolls: 10, ExportPollInterval: 3 * time.Second, PeerPolls: 120, SnapshotAge: 2500 * time.Millisecond}
}

// PeerAddresses gives the public peer address of an instance as host:port,
// and peeraddress.ErrNoAddress when it has none.
type PeerAddresses interface {
	PublicPeerAddress(ctx context.Context, instanceID int) (string, error)
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
	// Peers gives the addresses at which the two clients of a Transfer
	// torrent reach each other. Nil reads them with peeraddress.
	Peers PeerAddresses
	// Notifier gets the event of each end of a Transfer. Nil sends none.
	Notifier notifications.Notifier
	// Now is the clock of the steps and the log. Nil is time.Now.
	Now    func() time.Time
	Config Config
}

// Service runs Transfers: Config.PerSource at a time for each Transfer
// source, in the order they started.
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
	// queues holds the queue of each source while one of its workers runs.
	queues map[int]*queue
	// runs holds each Transfer that is queued, runs, or is being cancelled.
	runs map[int64]*run
	// reserved is the free space that the running Transfers need on each
	// target, and freed what the runs gave back there since the start.
	reserved, freed map[int]int64
	landings        map[int64]landing
	arrivals        int64
	left            chan struct{}
	// exports holds a place for each Export run that runs.
	exports chan struct{}
	// purged is when qui last removed the rows that finished more than
	// keepFinished ago.
	purged time.Time
	// rollingBack lets one rollback that Cancel starts run at a time, so
	// that a cancel of many Transfers does not send all their cleanups to
	// the instances at once. The rollback of a run that a cancel stopped
	// does not wait for it: the next Transfers of its source wait for that
	// rollback.
	rollingBack sync.Mutex
}

type queue struct {
	waiting []int64
	workers int
}

// run is a Transfer that the Service holds.
type run struct {
	// cancel stops the Transfer while it runs.
	cancel    context.CancelFunc
	cancelled bool
	// committed is set once a cancel no longer rolls the Transfer back.
	committed bool
	// meter counts the bytes of a Transfer that runs.
	meter    *meter
	targetID int
}

// NewService returns a Service whose Transfers stop when ctx ends or at
// Close. A stopped Transfer keeps its partial data and stays unfinished. The
// Service resumes or cleans up each unfinished Transfer at once, and looks
// for one that may run again every Config.WakeInterval while one is left.
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

// Wake queues each unfinished Transfer that may run now: one that a stop
// interrupted, one whose retry time came, and one whose instances are on
// again.
func (s *Service) Wake() {
	s.wake(false)
}

// wake with exports also runs each Export run that a stop of qui
// interrupted. Only the start does: later, an Export run that the Service
// does not hold is one whose Export call has not started it yet.
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

// Preview is the Transfer that Start would start.
type Preview struct {
	Way Way
	// Bytes is the size of the files of the Shared-file group, a file that
	// two members read at one path once.
	Bytes   int64
	Members []Member
	// Stay counts the cross-seeds that stay on the source: they read the
	// files through hardlinks, and a Transfer torrent makes no hardlinks.
	Stay int
	// Versions holds the hashes of those that have the same files as the
	// torrent of the request, for a request of a rule. The rule starts no
	// Transfer of them while this one runs.
	Versions []string
}

// Preview runs the checks of Start, with its refusals, and records and claims
// nothing.
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

// drain runs the Transfers of one source, one after the other, until none
// waits.
func (s *Service) drain(sourceID int) {
	for {
		s.mu.Lock()
		q := s.queues[sourceID]
		if len(q.waiting) == 0 || s.ctx.Err() != nil {
			// A stop leaves the queued Transfers to the next start.
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
		// A cancel that came while the run ended still rolls it back.
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
	// A cancel with nothing to take back saves its state, and then ends the
	// Transfer. A stop of qui between the two writes leaves only the end.
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

// Cancel stops a Transfer and takes back what it did: it removes the files
// that qui wrote and the torrents that qui added, and starts the source
// again. The cleanup runs in the background. The cancel of a Transfer that
// does not run is saved when Cancel returns, so a restart of qui keeps it.
// Once the removal of the torrent from the source started, Cancel returns
// ErrCancelTooLate, and once an Export run records its end, ErrFinished.
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
		// The run may have ended since the read above: it leaves s.runs only
		// after it saved its end.
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
		// An Export run is held but waits in no queue.
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

// current reads the record of a Transfer that a cancel can still stop.
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

// keepCancel saves the cancel of a Transfer that does not run, so that a
// stop of qui after Cancel answers cannot lose it. A Transfer that changed
// nothing yet ends, and keepCancel reports ended. Another one records its
// cleanup, which the rollback runs, or the next start of qui.
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

// rollBack takes back what a cancelled Transfer did that does not run.
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
	return &job{svc: s, rec: record, unanswered: map[string]bool{}, away: map[int]bool{}}
}

// Wait waits until every Transfer that started has ended or stopped.
func (s *Service) Wait() {
	s.wg.Wait()
}

// Close stops the running Transfers and waits for them until ctx ends. They
// keep their partial data and stay unfinished. A run that waits on a call
// which ignores the stop, such as an SFTP write that the server never
// answers, can outlast ctx: Close then returns ctx's error and leaves the
// run to the exit of qui, after which the next start resumes it.
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

// reserve claims bytes of read, the free space on a target, for a Transfer
// that claimed own of it already. It fails when the free space does not hold
// the bytes next to own and others: what the other running Transfers to the
// target claimed, and what runs gave back there since the read, because
// their files can fill that space now.
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

// freedOn is what the runs gave back on a target since the start.
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
