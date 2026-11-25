// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package livesamples

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/autobrr/qui/internal/qbittorrent"
)

const windowLength = 3 * time.Minute

const tickInterval = time.Second

type speedSource interface {
	SyncedSpeeds() []qbittorrent.SyncedSpeed
}

type Sample struct {
	Time time.Time `json:"time"`
	Up   int64     `json:"up"`
	Down int64     `json:"down"`
}

type InstanceSamples struct {
	InstanceID int      `json:"instanceId"`
	Samples    []Sample `json:"samples"`
}

type Window struct {
	Now       time.Time         `json:"now"`
	Instances []InstanceSamples `json:"instances"`
}

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
			if speed.SyncedAt.Equal(samples[n-1].Time) {
				samples[n-1].Up, samples[n-1].Down = speed.Up, speed.Down
			}
			continue
		}
		r.instances[speed.InstanceID] = append(samples, Sample{Time: speed.SyncedAt, Up: speed.Up, Down: speed.Down})
	}
	r.drop(from)
}

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
