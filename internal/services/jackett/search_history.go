// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"sync"
	"time"
)

const defaultHistoryCapacity = 500

type SearchHistoryEntry struct {
	ID     uint64 `json:"id"`
	JobID  uint64 `json:"jobId"`
	TaskID uint64 `json:"taskId"`

	IndexerID   int    `json:"indexerId"`
	IndexerName string `json:"indexerName"`

	Query       string            `json:"query,omitempty"`
	ReleaseName string            `json:"releaseName,omitempty"`
	Params      map[string]string `json:"params,omitempty"`
	Categories  []int             `json:"categories,omitempty"`
	ContentType string            `json:"contentType,omitempty"`
	Priority    string            `json:"priority"`
	SearchMode  string            `json:"searchMode,omitempty"`

	Status      string `json:"status"`
	ResultCount int    `json:"resultCount"`

	StartedAt   time.Time `json:"startedAt"`
	CompletedAt time.Time `json:"completedAt"`
	DurationMs  int       `json:"durationMs"`

	ErrorMessage string `json:"errorMessage,omitempty"`
}

type SearchHistoryBuffer struct {
	mu       sync.RWMutex
	entries  []SearchHistoryEntry
	head     int
	count    int
	capacity int
	nextID   uint64
}

func NewSearchHistoryBuffer(capacity int) *SearchHistoryBuffer {
	if capacity <= 0 {
		capacity = defaultHistoryCapacity
	}
	return &SearchHistoryBuffer{
		entries:  make([]SearchHistoryEntry, capacity),
		capacity: capacity,
		nextID:   1,
	}
}

func (b *SearchHistoryBuffer) Push(entry SearchHistoryEntry) uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	entry.ID = b.nextID
	b.nextID++

	b.entries[b.head] = entry
	b.head = (b.head + 1) % b.capacity

	if b.count < b.capacity {
		b.count++
	}

	return entry.ID
}

func (b *SearchHistoryBuffer) GetRecent(limit int) []SearchHistoryEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if limit <= 0 || limit > b.count {
		limit = b.count
	}

	if limit == 0 {
		return []SearchHistoryEntry{}
	}

	result := make([]SearchHistoryEntry, limit)

	idx := (b.head - 1 + b.capacity) % b.capacity
	for i := 0; i < limit; i++ {
		result[i] = b.entries[idx]
		idx = (idx - 1 + b.capacity) % b.capacity
	}

	return result
}

func (b *SearchHistoryBuffer) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.count
}

type HistoryRecorder interface {
	Record(entry SearchHistoryEntry)
}

type historyRecorderImpl struct {
	buffer *SearchHistoryBuffer
}

func NewHistoryRecorder(buffer *SearchHistoryBuffer) HistoryRecorder {
	return &historyRecorderImpl{
		buffer: buffer,
	}
}

func (r *historyRecorderImpl) Record(entry SearchHistoryEntry) {
	if r.buffer != nil {
		r.buffer.Push(entry)
	}
}

const defaultOutcomeCapacity = 1000

type IndexerOutcome struct {
	Outcome    string    `json:"outcome"`
	AddedCount int       `json:"addedCount,omitempty"`
	Message    string    `json:"message,omitempty"`
	RecordedAt time.Time `json:"recordedAt"`
}

type indexerOutcomeKey struct {
	JobID     uint64
	IndexerID int
}

type IndexerOutcomeStore struct {
	mu       sync.RWMutex
	outcomes map[indexerOutcomeKey]IndexerOutcome
	ring     []indexerOutcomeKey
	head     int
	count    int
	maxSize  int
}

func NewIndexerOutcomeStore(maxSize int) *IndexerOutcomeStore {
	if maxSize <= 0 {
		maxSize = defaultOutcomeCapacity
	}
	return &IndexerOutcomeStore{
		outcomes: make(map[indexerOutcomeKey]IndexerOutcome, maxSize),
		ring:     make([]indexerOutcomeKey, maxSize),
		maxSize:  maxSize,
	}
}

func (s *IndexerOutcomeStore) Record(jobID uint64, indexerID int, outcome string, addedCount int, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := indexerOutcomeKey{JobID: jobID, IndexerID: indexerID}

	if _, exists := s.outcomes[key]; exists {
		s.outcomes[key] = IndexerOutcome{
			Outcome:    outcome,
			AddedCount: addedCount,
			Message:    message,
			RecordedAt: time.Now(),
		}
		return
	}

	if s.count >= s.maxSize {
		oldest := s.ring[s.head]
		delete(s.outcomes, oldest)
	} else {
		s.count++
	}

	s.ring[s.head] = key
	s.head = (s.head + 1) % s.maxSize

	s.outcomes[key] = IndexerOutcome{
		Outcome:    outcome,
		AddedCount: addedCount,
		Message:    message,
		RecordedAt: time.Now(),
	}
}

func (s *IndexerOutcomeStore) Get(jobID uint64, indexerID int) (IndexerOutcome, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	oc, ok := s.outcomes[indexerOutcomeKey{JobID: jobID, IndexerID: indexerID}]
	return oc, ok
}

func (s *IndexerOutcomeStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.outcomes)
}

type SearchHistoryEntryWithOutcome struct {
	SearchHistoryEntry
	Outcome    string `json:"outcome,omitempty"`
	AddedCount int    `json:"addedCount,omitempty"`
}

type SearchHistoryResponseWithOutcome struct {
	Entries []SearchHistoryEntryWithOutcome `json:"entries"`
	Total   int                             `json:"total"`
	Source  string                          `json:"source"`
}
