// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package transferhistory records the Transfer history of each instance and
// answers period queries on it (ADR 0052).
package transferhistory

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/trackergroup"
	"github.com/autobrr/qui/pkg/releases"
)

const (
	writeInterval = 5 * time.Minute
	// minGap is the shortest time without a read that counts as a Recording gap.
	minGap = 5 * time.Minute
	// maxQueued is the number of readings of one instance that wait for the
	// worker. A newer reading pushes out the oldest: the counters are
	// cumulative, so its bytes land with the next reading.
	maxQueued = 4
	dbTimeout = 30 * time.Second
	// maxWriteTimeout is the longest time limit of one write. The limit
	// doubles after a write that passes it: the next try is as large, so
	// with the same limit it would fail each time and store nothing.
	maxWriteTimeout = 10 * time.Minute
)

// Reading is the state of one instance at one successful read of qBittorrent.
type Reading struct {
	InstanceID int
	Time       time.Time
	// FailedSince is the start of a time without a successful read that this
	// reading ends: the first failed read, or the reading before this one
	// when the instance got a new qBittorrent client. It is zero otherwise.
	FailedSince time.Time
	// Torrents is the full torrent list of the instance.
	Torrents []Torrent
}

// Torrent is one torrent of a Reading.
type Torrent struct {
	Hash           string
	Name           string
	Category       string
	TrackerDomains []string
	// Private is nil when qBittorrent does not report the private flag.
	Private *bool
	Size    int64
	// Uploaded and Downloaded are the cumulative counters, never the session values.
	Uploaded   int64
	Downloaded int64
	Complete   bool
	AddedAt    time.Time
}

type Module struct {
	store          *models.TransferHistoryStore
	customizations *models.TrackerCustomizationStore
	parser         *releases.Parser

	mu       sync.Mutex
	queue    []Reading
	barriers []chan struct{}
	stopping bool
	wake     chan struct{}
	done     chan struct{}

	// The worker owns these until it closes done.
	instances map[int]*instance
	// reloads holds each instance that a write with an unknown outcome
	// dropped from memory.
	reloads     map[int]struct{}
	mapper      *trackergroup.Mapper
	shutdownErr error
}

// New returns a running module that stores the history in db and names
// Tracker groups from the tracker customizations.
func New(db dbinterface.Querier, customizations *models.TrackerCustomizationStore) *Module {
	m := &Module{
		store:          models.NewTransferHistoryStore(db),
		customizations: customizations,
		parser:         releases.NewParser(time.Minute),
		wake:           make(chan struct{}, 1),
		done:           make(chan struct{}),
		instances:      make(map[int]*instance),
		reloads:        make(map[int]struct{}),
	}
	go m.run()
	return m
}

// Record gives the module a reading. It never waits for the database, and
// the module takes the reading: the caller must not change it after the call.
// A reading after Shutdown does nothing.
func (m *Module) Record(r Reading) {
	m.mu.Lock()
	if m.stopping {
		m.mu.Unlock()
		return
	}
	m.queue = enqueue(m.queue, r)
	m.mu.Unlock()
	m.signal()
}

func enqueue(queue []Reading, r Reading) []Reading {
	oldest, queued := -1, 0
	for i := range queue {
		if queue[i].InstanceID == r.InstanceID {
			if oldest < 0 {
				oldest = i
			}
			queued++
		}
	}
	queue = append(queue, r)
	if queued >= maxQueued {
		dropped := queue[oldest].FailedSince
		queue = slices.Delete(queue, oldest, oldest+1)
		// The next reading of the instance ends the time without a read.
		next := oldest + slices.IndexFunc(queue[oldest:], func(q Reading) bool { return q.InstanceID == r.InstanceID })
		if !dropped.IsZero() && (queue[next].FailedSince.IsZero() || dropped.Before(queue[next].FailedSince)) {
			queue[next].FailedSince = dropped
		}
	}
	return queue
}

func (m *Module) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// Shutdown writes the changes that are still in memory and stops the module.
// It returns the error of each write that failed: those changes are lost,
// except the bytes of each torrent that is still in qBittorrent at the next start.
func (m *Module) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.stopping = true
	m.mu.Unlock()
	m.signal()
	select {
	case <-m.done:
		return m.shutdownErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// settle waits until the module processed each reading that it got before
// the call, with the writes that those readings made due.
func (m *Module) settle(ctx context.Context) error {
	barrier := make(chan struct{})
	m.mu.Lock()
	m.barriers = append(m.barriers, barrier)
	m.mu.Unlock()
	m.signal()
	select {
	case <-barrier:
		return nil
	case <-m.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Module) run() {
	defer close(m.done)
	for range m.wake {
		m.mu.Lock()
		readings, barriers, stopping := m.queue, m.barriers, m.stopping
		m.queue, m.barriers = nil, nil
		m.mu.Unlock()

		for _, r := range readings {
			m.process(r)
		}
		if stopping {
			errs := make([]error, 0, len(m.instances))
			for _, inst := range m.instances {
				errs = append(errs, m.write(inst))
			}
			m.shutdownErr = errors.Join(errs...)
		}
		for _, b := range barriers {
			close(b)
		}
		if stopping {
			return
		}
	}
}

func (m *Module) process(r Reading) {
	inst := m.instances[r.InstanceID]
	if inst == nil {
		var err error
		if inst, err = m.load(r.InstanceID); err != nil {
			log.Warn().Err(err).Int("instanceID", r.InstanceID).Msg("transfer history: failed to load the history of the instance; the next reading tries again")
			return
		}
		// qui kept reading the instance, so the time since its stored
		// reading is no Recording gap. That reading still decides which
		// torrents the instance had.
		if _, ok := m.reloads[r.InstanceID]; ok {
			inst.resumed = false
			delete(m.reloads, r.InstanceID)
		}
		m.instances[r.InstanceID] = inst
	}
	inst.apply(r, m.mapper, m.parser)
	if r.Time.Sub(inst.lastWriteAt) >= writeInterval {
		_ = m.write(inst)
		inst.lastWriteAt = r.Time
	}
}

func (m *Module) load(instanceID int) (*instance, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()
	if m.mapper == nil {
		if err := m.refreshMapper(ctx); err != nil {
			return nil, err
		}
	}
	row, records, err := m.store.LoadInstance(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return newInstance(instanceID, row, records), nil
}

type writeOutcome int

const (
	notStored writeOutcome = iota
	stored
	unknown
)

// outcome tells whether a write that returned err is stored anyway, as when
// the answer of the commit got lost. Its totals must not count twice.
func (m *Module) outcome(err error, w *models.TransferHistoryWrite) writeOutcome {
	// The database holds an earlier write with this number: its commit
	// returned an error and landed after the check below. The changes in
	// memory hold that write and the changes since.
	if errors.Is(err, models.ErrTransferHistoryWriteNumber) {
		return unknown
	}
	if !errors.Is(err, models.ErrTransferHistoryCommitUnknown) {
		return notStored
	}
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()
	writes, checkErr := m.store.Writes(ctx, w.InstanceID)
	switch {
	case checkErr != nil:
		return unknown
	case writes == w.Number:
		return stored
	default:
		return notStored
	}
}

func (m *Module) refreshMapper(ctx context.Context) error {
	customizations, err := m.customizations.List(ctx)
	if err != nil {
		return err
	}
	m.mapper = trackergroup.New(customizations)
	return nil
}

// write stores the changes of inst. After a failure, the changes stay in
// memory for the next write.
func (m *Module) write(inst *instance) error {
	if !inst.hasChanges() {
		return nil
	}
	start := time.Now()
	timeout := cmp.Or(inst.writeTimeout, dbTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	w := inst.changes()
	ids, err := m.store.Write(ctx, w)
	if err != nil {
		switch m.outcome(err, w) {
		case stored:
		case notStored:
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				inst.writeTimeout = min(2*timeout, maxWriteTimeout)
			}
			log.Error().Err(err).Int("instanceID", inst.id).Dur("timeLimit", timeout).Msg("transfer history: failed to write; the changes stay in memory for the next write")
			return fmt.Errorf("write the transfer history of instance %d: %w", inst.id, err)
		case unknown:
			// The stored counters decide the next difference, so no byte
			// counts twice. The bytes of a torrent that is gone at the next
			// reading are lost.
			log.Error().Err(err).Int("instanceID", inst.id).Msg("transfer history: cannot tell which changes are stored; the next reading loads the stored history again")
			m.reloads[inst.id] = struct{}{}
			delete(m.instances, inst.id)
			return fmt.Errorf("write the transfer history of instance %d: %w", inst.id, err)
		}
	}
	for _, rec := range w.Records {
		if rec.ID == 0 {
			rec.ID = ids[rec.Hash]
		}
	}
	inst.writes = w.Number
	inst.writeTimeout = 0
	inst.reset()
	log.Debug().Int("instanceID", inst.id).Int("torrents", len(w.Counters)).Dur("elapsed", time.Since(start)).
		Msg("transfer history: wrote the changes")
	if err := m.refreshMapper(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Warn().Err(err).Msg("transfer history: failed to read the tracker customizations")
	}
	return nil
}
