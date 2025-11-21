package crossseed

import (
	"testing"
	"time"
)

func TestCrossSeedObservationsSnapshot(t *testing.T) {
	obs := newCrossSeedObservations()

	obs.RecordStashHit()
	obs.RecordStashMiss()
	obs.RecordDedup(3, 7)
	obs.RecordSearchTimeout()
	obs.RecordLimiterWait(125 * time.Millisecond)
	obs.WorkerJobStarted()
	obs.WorkerJobFinished()

	snapshot := obs.Snapshot(4)

	if snapshot.StashHits != 1 {
		t.Fatalf("expected 1 stash hit, got %d", snapshot.StashHits)
	}
	if snapshot.StashMisses != 1 {
		t.Fatalf("expected 1 stash miss, got %d", snapshot.StashMisses)
	}
	if snapshot.DeduplicatedGroups != 3 {
		t.Fatalf("expected 3 dedup groups, got %d", snapshot.DeduplicatedGroups)
	}
	if snapshot.DeduplicatedDuplicates != 7 {
		t.Fatalf("expected 7 duplicates, got %d", snapshot.DeduplicatedDuplicates)
	}
	if snapshot.SearchTimeouts != 1 {
		t.Fatalf("expected 1 search timeout, got %d", snapshot.SearchTimeouts)
	}
	if snapshot.RateLimiterWaits != 1 {
		t.Fatalf("expected 1 limiter wait, got %d", snapshot.RateLimiterWaits)
	}
	if snapshot.RateLimiterWaitTimeMs != (125 * time.Millisecond).Milliseconds() {
		t.Fatalf("expected wait time 125ms, got %d", snapshot.RateLimiterWaitTimeMs)
	}
	if snapshot.ActiveWorkers != 0 {
		t.Fatalf("expected no active workers, got %d", snapshot.ActiveWorkers)
	}
	if snapshot.WorkerCapacity != 4 {
		t.Fatalf("expected worker capacity 4, got %d", snapshot.WorkerCapacity)
	}
}
