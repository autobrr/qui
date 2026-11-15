// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package livesamples

import (
	"context"
	"maps"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
)

// fakePool holds the speeds that the pool gives at its last successful sync.
// A failed sync leaves SyncedAt where it was, as the real pool does.
type fakePool struct {
	mu     sync.Mutex
	speeds map[int]qbittorrent.SyncedSpeed
}

func (p *fakePool) SyncedSpeeds() []qbittorrent.SyncedSpeed {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Collect(maps.Values(p.speeds))
}

func (p *fakePool) sync(instanceID int, up, down int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.speeds == nil {
		p.speeds = make(map[int]qbittorrent.SyncedSpeed)
	}
	p.speeds[instanceID] = qbittorrent.SyncedSpeed{InstanceID: instanceID, SyncedAt: time.Now(), Up: up, Down: down}
}

// syncTimeOnly moves the sync time before the speeds of that sync reach the
// cache, as the real pool does for a moment after each sync.
func (p *fakePool) syncTimeOnly(instanceID int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	speed := p.speeds[instanceID]
	speed.SyncedAt = time.Now()
	p.speeds[instanceID] = speed
}

func (p *fakePool) setSpeeds(instanceID int, up, down int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	speed := p.speeds[instanceID]
	speed.Up, speed.Down = up, down
	p.speeds[instanceID] = speed
}

func (p *fakePool) remove(instanceID int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.speeds, instanceID)
}

// run starts a recorder over pool and stops it at the end of the test.
func run(t *testing.T, pool *fakePool) *Recorder {
	t.Helper()
	recorder := newRecorder(pool)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		recorder.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return recorder
}

// samplesOf returns the samples of one instance as offsets from start.
func samplesOf(window Window, instanceID int, start time.Time) [][3]int64 {
	for _, instance := range window.Instances {
		if instance.InstanceID != instanceID {
			continue
		}
		samples := make([][3]int64, 0, len(instance.Samples))
		for _, sample := range instance.Samples {
			samples = append(samples, [3]int64{int64(sample.Time.Sub(start) / time.Second), sample.Up, sample.Down})
		}
		return samples
	}
	return nil
}

// wait moves the fake clock by d and lets the recorder take its ticks.
func wait(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

func TestRecorderTakesOneSampleForEachSuccessfulSync(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		pool := &fakePool{}
		recorder := run(t, pool)

		pool.sync(1, 3_743_000, 10)
		wait(2 * time.Second)
		// No new sync: the recorder takes no sample.
		wait(3 * time.Second)
		pool.sync(1, 2_000_000, 20)
		wait(2 * time.Second)

		window := recorder.Samples()
		require.Equal(t, start.Add(7*time.Second).UTC(), window.Now)
		require.Equal(t, [][3]int64{{0, 3_743_000, 10}, {5, 2_000_000, 20}}, samplesOf(window, 1, start))
	})
}

func TestRecorderTakesZeroSpeedsOfAnIdleInstanceThatSyncs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		pool := &fakePool{}
		recorder := run(t, pool)

		pool.sync(1, 0, 0)
		wait(2 * time.Second)
		pool.sync(1, 0, 0)
		wait(2 * time.Second)

		require.Equal(t, [][3]int64{{0, 0, 0}, {2, 0, 0}}, samplesOf(recorder.Samples(), 1, start))
	})
}

// regression: #4141
func TestRecorderDropsSamplesOlderThanTheWindowAndForgetsAnInstanceWithNone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		pool := &fakePool{}
		recorder := run(t, pool)

		pool.sync(1, 100, 0)
		pool.sync(2, 200, 0)
		wait(time.Minute)
		// Instance 2 fails each sync from here on, so its sync time stays.
		pool.sync(1, 101, 0)
		wait(windowLength - time.Minute + time.Second)

		window := recorder.Samples()
		require.Equal(t, [][3]int64{{60, 101, 0}}, samplesOf(window, 1, start))
		require.Len(t, window.Instances, 1, "the recorder forgets an instance with no sample in the window")
	})
}

// regression: #4141
func TestRecorderTakesNoSampleWhileASyncFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		pool := &fakePool{}
		recorder := run(t, pool)

		pool.sync(1, 100, 0)
		wait(2 * time.Second)
		// Failed syncs: the sync time does not move.
		wait(10 * time.Second)
		pool.sync(1, 300, 0)
		wait(2 * time.Second)
		// A removed client gives no new sample, and its old samples stay until they age out.
		pool.remove(1)
		wait(2 * time.Second)

		require.Equal(t, [][3]int64{{0, 100, 0}, {12, 300, 0}}, samplesOf(recorder.Samples(), 1, start))
	})
}

// regression: #4192
func TestRecorderTakesTheSpeedsThatReachTheCacheAfterTheSyncTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		pool := &fakePool{}
		recorder := run(t, pool)

		pool.sync(1, 100, 0)
		wait(2 * time.Second)
		pool.syncTimeOnly(1)
		wait(time.Second)
		pool.setSpeeds(1, 300, 0)
		wait(time.Second)

		require.Equal(t, [][3]int64{{0, 100, 0}, {2, 300, 0}}, samplesOf(recorder.Samples(), 1, start))
	})
}

func TestRecorderGivesEmptyInstancesBeforeAnySync(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		recorder := run(t, &fakePool{})
		wait(2 * time.Second)

		window := recorder.Samples()
		require.NotNil(t, window.Instances)
		require.Empty(t, window.Instances)
	})
}
