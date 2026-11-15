// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package livesamples keeps the Live samples of each instance in memory, for
// the Live view of the Dashboard chart.
package livesamples

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/autobrr/qui/internal/qbittorrent"
)

// windowLength is how long the recorder keeps a sample.
const windowLength = 3 * time.Minute

const tickInterval = time.Second

type speedSource interface {
	SyncedSpeeds() []qbittorrent.SyncedSpeed
}

// Sample is the upload and download speed of an instance at one time, in
// bytes per second.
type Sample struct {
	Time time.Time `json:"time"`
	Up   int64     `json:"up"`
	Down int64     `json:"down"`
}

type InstanceSamples struct {
	InstanceID int      `json:"instanceId"`
	Samples    []Sample `json:"samples"`
}

// Window holds the samples of the 3 minutes that end at Now. Instances are in
// the order of their id, and the samples of each are oldest first.
type Window struct {
	Now       time.Time         `json:"now"`
	Instances []InstanceSamples `json:"instances"`
}

// Recorder takes a sample of an instance each second in which the instance
// had a successful sync, with the speeds and the time of the last such sync.
// The next second updates the speeds of that sample if they changed.
// It never sends a request to qBittorrent: an instance that nothing syncs
// gives no sample.
type Recorder struct {
	source speedSource

	mu        sync.Mutex
	instances map[int][]Sample
}

func New(pool *qbittorrent.ClientPool) *Recorder {
	return newRecorder(pool)
}

func newRecorder(source speedSource) *Recorder {
	return &Recorder{source: source, instances: make(map[int][]Sample)}
}

// Run takes the samples each second until ctx ends.
func (r *Recorder) Run(ctx context.Context) {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.take()
		}
	}
}

func (r *Recorder) take() {
	speeds := r.source.SyncedSpeeds()
	from := time.Now().Add(-windowLength)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, speed := range speeds {
		samples := r.instances[speed.InstanceID]
		if n := len(samples); n > 0 && !speed.SyncedAt.After(samples[n-1].Time) {
			// The pool moves the sync time before the speeds of that sync reach its cache.
			if speed.SyncedAt.Equal(samples[n-1].Time) {
				samples[n-1].Up, samples[n-1].Down = speed.Up, speed.Down
			}
			continue
		}
		r.instances[speed.InstanceID] = append(samples, Sample{Time: speed.SyncedAt, Up: speed.Up, Down: speed.Down})
	}
	r.drop(from)
}

// drop removes the samples from before from, and each instance left with none.
func (r *Recorder) drop(from time.Time) {
	for id, samples := range r.instances {
		kept := slices.DeleteFunc(samples, func(sample Sample) bool { return !sample.Time.After(from) })
		if len(kept) == 0 {
			delete(r.instances, id)
			continue
		}
		r.instances[id] = kept
	}
}

// Samples returns the samples of the last 3 minutes.
func (r *Recorder) Samples() Window {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.drop(now.Add(-windowLength))
	instances := make([]InstanceSamples, 0, len(r.instances))
	for id, samples := range r.instances {
		utc := make([]Sample, len(samples))
		for i, sample := range samples {
			utc[i] = Sample{Time: sample.Time.UTC(), Up: sample.Up, Down: sample.Down}
		}
		instances = append(instances, InstanceSamples{InstanceID: id, Samples: utc})
	}
	slices.SortFunc(instances, func(a, b InstanceSamples) int { return cmp.Compare(a.InstanceID, b.InstanceID) })
	return Window{Now: now.UTC(), Instances: instances}
}
