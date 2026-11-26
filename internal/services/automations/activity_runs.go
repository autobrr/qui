// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"errors"
	"slices"
	"sync"
	"time"
)

var ErrActivityRunNotFound = errors.New("activity run not found")

type ActivityRunTorrent struct {
	Hash             string   `json:"hash"`
	Name             string   `json:"name"`
	TrackerDomain    string   `json:"trackerDomain,omitempty"`
	TagsAdded        []string `json:"tagsAdded,omitempty"`
	TagsRemoved      []string `json:"tagsRemoved,omitempty"`
	Category         string   `json:"category,omitempty"`
	MovePath         string   `json:"movePath,omitempty"`
	Size             *int64   `json:"size,omitempty"`
	Ratio            *float64 `json:"ratio,omitempty"`
	AddedOn          *int64   `json:"addedOn,omitempty"`
	UploadLimitKiB   *int64   `json:"uploadLimitKiB,omitempty"`
	DownloadLimitKiB *int64   `json:"downloadLimitKiB,omitempty"`
	RatioLimit       *float64 `json:"ratioLimit,omitempty"`
	SeedingMinutes   *int64   `json:"seedingMinutes,omitempty"`
}

type ActivityRunPage struct {
	Total int                  `json:"total"`
	Items []ActivityRunTorrent `json:"items"`
}

type activityRunEntry struct {
	instanceID int
	createdAt  time.Time
	items      []ActivityRunTorrent
}

type activityRunStore struct {
	mu        sync.Mutex
	retention time.Duration
	maxRuns   int
	runs      map[int]activityRunEntry
	order     []int
}

func newActivityRunStore(retention time.Duration, maxRuns int) *activityRunStore {
	if retention <= 0 {
		retention = 24 * time.Hour
	}
	if maxRuns <= 0 {
		maxRuns = 500
	}
	return &activityRunStore{
		retention: retention,
		maxRuns:   maxRuns,
		runs:      make(map[int]activityRunEntry),
	}
}

func (s *activityRunStore) Put(activityID int, instanceID int, items []ActivityRunTorrent) {
	if s == nil || activityID <= 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneLocked(time.Now())

	if _, exists := s.runs[activityID]; exists {
		for i, id := range s.order {
			if id == activityID {
				s.order = append(s.order[:i], s.order[i+1:]...)
				break
			}
		}
	}

	itemsCopy := slices.Clone(items)
	s.runs[activityID] = activityRunEntry{
		instanceID: instanceID,
		createdAt:  time.Now(),
		items:      itemsCopy,
	}
	s.order = append(s.order, activityID)
	s.pruneLocked(time.Now())
}

func (s *activityRunStore) Get(instanceID int, activityID int, offset int, limit int) (ActivityRunPage, bool) {
	if s == nil || activityID <= 0 {
		return ActivityRunPage{}, false
	}

	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 200
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneLocked(time.Now())

	entry, ok := s.runs[activityID]
	if !ok || entry.instanceID != instanceID {
		return ActivityRunPage{}, false
	}

	total := len(entry.items)
	if offset >= total {
		return ActivityRunPage{Total: total, Items: []ActivityRunTorrent{}}, true
	}

	end := min(offset+limit, total)

	page := ActivityRunPage{
		Total: total,
		Items: slices.Clone(entry.items[offset:end]),
	}

	return page, true
}

func (s *activityRunStore) Prune() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
}

func (s *activityRunStore) pruneLocked(now time.Time) {
	if len(s.runs) == 0 {
		s.order = nil
		return
	}

	cutoff := now.Add(-s.retention)
	filtered := s.order[:0]
	for _, id := range s.order {
		entry, ok := s.runs[id]
		if !ok {
			continue
		}
		if s.retention > 0 && entry.createdAt.Before(cutoff) {
			delete(s.runs, id)
			continue
		}
		filtered = append(filtered, id)
	}
	s.order = filtered

	if s.maxRuns > 0 && len(s.order) > s.maxRuns {
		excess := len(s.order) - s.maxRuns
		for i := range excess {
			id := s.order[i]
			delete(s.runs, id)
		}
		s.order = s.order[excess:]
	}
}

func (s *Service) GetActivityRun(instanceID int, activityID int, limit int, offset int) (*ActivityRunPage, error) {
	if s == nil || s.activityRuns == nil {
		return nil, ErrActivityRunNotFound
	}

	page, ok := s.activityRuns.Get(instanceID, activityID, offset, limit)
	if !ok {
		return nil, ErrActivityRunNotFound
	}

	return &page, nil
}
