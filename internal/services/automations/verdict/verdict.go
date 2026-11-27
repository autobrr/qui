// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package verdict

import (
	"time"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

type reading uint8

const (
	readRaw reading = iota
	readMarked
	readDue
)

type Reader struct {
	snapshot *qbittorrent.TrackerHealthSnapshot
	reading  reading
	wait     time.Duration
	now      time.Time
}

func Raw(snapshot *qbittorrent.TrackerHealthSnapshot) Reader {
	return Reader{snapshot: snapshot}
}

func (r Reader) Known() bool {
	return r.snapshot != nil
}

func (r Reader) Is(hash string, health qbittorrent.TrackerHealth) bool {
	if health != qbittorrent.TrackerHealthUnregistered {
		return r.snapshot.Health(hash) == health
	}
	switch r.reading {
	case readMarked:
		return marks(r.snapshot, hash)
	case readDue:
		return r.due(hash)
	}
	return r.snapshot.Health(hash) == qbittorrent.TrackerHealthUnregistered
}

func (r Reader) due(hash string) bool {
	since, marked := r.snapshot.UnregisteredSince(hash)
	return marked && r.snapshot.Health(hash) == qbittorrent.TrackerHealthUnregistered && r.now.Sub(since) >= r.wait
}

type Waited struct {
	snapshot *qbittorrent.TrackerHealthSnapshot
	now      time.Time
}

func ForDelete(snapshot *qbittorrent.TrackerHealthSnapshot, now time.Time) Waited {
	return Waited{snapshot: snapshot, now: now}
}

func (w Waited) Loaded() bool {
	return w.snapshot.UnregisteredSinceLoaded()
}

func (w Waited) Raw() Reader {
	return Raw(w.snapshot)
}

func (w Waited) Marked() Reader {
	return Reader{snapshot: w.snapshot, reading: readMarked, now: w.now}
}

func (w Waited) Due(wait time.Duration) Reader {
	if wait <= 0 {
		return w.Raw()
	}
	return Reader{snapshot: w.snapshot, reading: readDue, wait: wait, now: w.now}
}

func (w Waited) Marks(hash string) bool {
	return marks(w.snapshot, hash)
}

func marks(snapshot *qbittorrent.TrackerHealthSnapshot, hash string) bool {
	_, timed := snapshot.UnregisteredSince(hash)
	return timed || snapshot.Health(hash) == qbittorrent.TrackerHealthUnregistered
}

func (w Waited) UnregisteredNow(hash string) bool {
	return w.snapshot.Health(hash) == qbittorrent.TrackerHealthUnregistered
}

func (w Waited) DueAt(hash string, wait time.Duration) time.Time {
	since, timed := w.snapshot.UnregisteredSince(hash)
	if !timed {
		since = w.now
	}
	return since.Add(wait)
}

func DeleteWait(action *models.DeleteAction) time.Duration {
	if action.UnregisteredWaitSeconds == nil {
		return models.DefaultUnregisteredWait
	}
	return time.Duration(*action.UnregisteredWaitSeconds) * time.Second
}
