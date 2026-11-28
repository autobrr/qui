// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package verdict

import (
	"time"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/infohash"
)

type reading uint8

const (
	readRaw reading = iota
	readMarked
	readDue
)

type Reader struct {
	snapshot *qbittorrent.TrackerHealthSnapshot
	cleared  *infohash.Set
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
	if isCleared(r.cleared, hash) {
		return false
	}
	switch r.reading {
	case readMarked:
		return marks(r.snapshot, hash)
	case readDue:
		return r.due(hash)
	default:
		return r.snapshot.Health(hash) == qbittorrent.TrackerHealthUnregistered
	}
}

func (r Reader) due(hash string) bool {
	since, marked := r.snapshot.UnregisteredSince(hash)
	return marked && r.snapshot.Health(hash) == qbittorrent.TrackerHealthUnregistered && r.now.Sub(since) >= r.wait
}

type Waited struct {
	snapshot *qbittorrent.TrackerHealthSnapshot
	cleared  *infohash.Set
	now      time.Time
}

func ForDelete(snapshot *qbittorrent.TrackerHealthSnapshot, now time.Time) Waited {
	return Waited{snapshot: snapshot, now: now}
}

func (w Waited) Without(cleared *infohash.Set) Waited {
	w.cleared = cleared
	return w
}

func (w Waited) Reload(snapshot *qbittorrent.TrackerHealthSnapshot) Waited {
	w.snapshot = snapshot
	return w
}

func (w Waited) Moved(hash string, current Waited) bool {
	before, had := w.snapshot.UnregisteredSince(hash)
	after, has := current.snapshot.UnregisteredSince(hash)
	return had != has || !before.Equal(after) || w.snapshot.Health(hash) != current.snapshot.Health(hash)
}

func isCleared(cleared *infohash.Set, hash string) bool {
	if cleared == nil || cleared.Len() == 0 {
		return false
	}
	parsed, err := infohash.Parse(hash)
	return err == nil && cleared.Has(parsed)
}

func (w Waited) Loaded() bool {
	return w.snapshot.UnregisteredSinceLoaded()
}

func (w Waited) Raw() Reader {
	return Reader{snapshot: w.snapshot, cleared: w.cleared}
}

func (w Waited) Marked() Reader {
	return Reader{snapshot: w.snapshot, cleared: w.cleared, reading: readMarked, now: w.now}
}

func (w Waited) Due(wait time.Duration) Reader {
	if wait <= 0 {
		return w.Raw()
	}
	return Reader{snapshot: w.snapshot, cleared: w.cleared, reading: readDue, wait: wait, now: w.now}
}

func (w Waited) Marks(hash string) bool {
	return !isCleared(w.cleared, hash) && marks(w.snapshot, hash)
}

func marks(snapshot *qbittorrent.TrackerHealthSnapshot, hash string) bool {
	_, timed := snapshot.UnregisteredSince(hash)
	return timed || snapshot.Health(hash) == qbittorrent.TrackerHealthUnregistered
}

func (w Waited) UnregisteredNow(hash string) bool {
	return !isCleared(w.cleared, hash) && w.snapshot.Health(hash) == qbittorrent.TrackerHealthUnregistered
}

func (w Waited) Since(hash string) (time.Time, bool) {
	return w.snapshot.UnregisteredSince(hash)
}

func (w Waited) UnregisteredBy(hash string) (qbittorrent.UnregisteredTracker, bool) {
	if isCleared(w.cleared, hash) {
		return qbittorrent.UnregisteredTracker{}, false
	}
	return w.snapshot.UnregisteredBy(hash)
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
