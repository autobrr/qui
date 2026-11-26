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

type HistoryDelete struct {
	InstanceID   int
	AllInstances bool
	Before       time.Time
}

type deleteRequest struct {
	HistoryDelete
	done chan error
}

var ErrStopped = errors.New("transfer history stopped")

var ErrInvalidDelete = errors.New("invalid history delete")

const deleteTimeout = maxWriteTimeout

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
	removed, err := m.store.RemovedInstances(ctx, ids)
	if err != nil {
		return err
	}
	for _, id := range removed {
		m.removed[id] = struct{}{}
	}
	return nil
}

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
