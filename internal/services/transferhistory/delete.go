// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/rs/zerolog/log"
)

// HistoryDelete selects the history that DeleteHistory deletes: the history
// of InstanceID, or of each instance when AllInstances is set. A zero Before
// deletes everything. Otherwise the module deletes the UTC days before the UTC
// day of Before, so that each row from Before on stays.
type HistoryDelete struct {
	InstanceID   int
	AllInstances bool
	Before       time.Time
}

type deleteRequest struct {
	HistoryDelete
	done chan error
}

// ErrStopped is the error of a call after Shutdown.
var ErrStopped = errors.New("transfer history stopped")

// ErrInvalidDelete is the error of a delete that names no instance.
var ErrInvalidDelete = errors.New("invalid history delete")

// deleteTimeout is the time limit of one delete of history.
const deleteTimeout = maxWriteTimeout

// DeleteHistory deletes history, after each reading that the module got
// before the call. After a delete of everything, the next reading of an
// instance starts a new Baseline, and a removed instance stays without
// history. A delete before a date also deletes the Baseline when the date is
// after the first recording, and the first recording moves to that date.
//
// A large delete is a series of short writes, so the other writers of qui
// get the database between them. After an error, a part of the history can
// be gone. Readings and the queries of the module wait until the delete
// ends. The delete goes on when ctx ends first.
func (m *Module) DeleteHistory(ctx context.Context, d HistoryDelete) error {
	if !d.AllInstances && d.InstanceID <= 0 {
		return ErrInvalidDelete
	}
	req := deleteRequest{HistoryDelete: d, done: make(chan error, 1)}
	m.mu.Lock()
	if m.stopping {
		m.mu.Unlock()
		return ErrStopped
	}
	m.deletes = append(m.deletes, req)
	m.mu.Unlock()
	m.signal()
	select {
	case err := <-req.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// deleteHistory runs on the worker. No fold runs beside it, and a transfer
// query waits until the delete ends, so that it does not read a part of it.
// It logs a failure, because the caller can stop waiting before the end.
func (m *Module) deleteHistory(d HistoryDelete) (err error) {
	defer func() {
		if err != nil {
			log.Error().Err(err).Int("instanceID", d.InstanceID).Bool("allInstances", d.AllInstances).Time("before", d.Before).
				Msg("transfer history: failed to delete history; a part of it can be gone")
		}
	}()
	m.foldRun.Lock()
	defer m.foldRun.Unlock()
	m.foldMu.Lock()
	defer m.foldMu.Unlock()
	if !d.Before.IsZero() {
		if err := m.unload(d); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), deleteTimeout)
	defer cancel()
	defer m.dropSize()
	defer func() {
		if d.AllInstances {
			m.answers.changedAll()
		} else {
			m.answers.changed(d.InstanceID)
		}
	}()

	ids := []int{d.InstanceID}
	if d.AllInstances {
		stored, err := m.store.Instances(ctx)
		if err != nil {
			return err
		}
		ids = slices.Collect(maps.Keys(m.instances))
		for _, inst := range stored {
			ids = append(ids, inst.InstanceID)
		}
		slices.Sort(ids)
		ids = slices.Compact(ids)
	}
	if d.Before.IsZero() {
		return m.deleteEverything(ctx, ids, d.AllInstances)
	}
	return m.deleteBefore(ctx, ids, dayOf(d.Before))
}

func (m *Module) deleteEverything(ctx context.Context, ids []int, all bool) error {
	// The changes in memory go too, so the next reading starts a new Baseline.
	for _, id := range ids {
		delete(m.instances, id)
		delete(m.reloads, id)
	}
	var err error
	if all {
		err = m.store.DeleteAllHistory(ctx)
	} else {
		err = m.store.DeleteInstanceHistory(ctx, ids[0])
	}
	if err != nil {
		return err
	}
	// A sync that ran while the user removed the instance can still give a reading.
	removed, err := m.store.RemovedInstances(ctx, ids)
	if err != nil {
		return err
	}
	for _, id := range removed {
		m.removed[id] = struct{}{}
	}
	return nil
}

// unload writes the changes in memory of the instances of d, and drops them
// from memory. The changes can hold a day that the delete covers. After the
// delete, the next reading loads the stored history again. That reading ends
// a Recording gap only when the instance was resumed, as while recording is off.
func (m *Module) unload(d HistoryDelete) error {
	for id, inst := range m.instances {
		if !d.AllInstances && id != d.InstanceID {
			continue
		}
		if err := m.write(inst); err != nil {
			return err
		}
		delete(m.instances, id)
		if !inst.resumed {
			m.reloads[id] = struct{}{}
		}
	}
	return nil
}

func (m *Module) deleteBefore(ctx context.Context, ids []int, beforeDay int64) error {
	for _, id := range ids {
		if err := m.store.DeleteHistoryBefore(ctx, id, beforeDay); err != nil {
			return err
		}
	}
	return nil
}
