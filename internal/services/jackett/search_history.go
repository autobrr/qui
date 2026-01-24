// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"sync"
	"time"
)

const defaultHistoryCapacity = 500

// SearchHistoryEntry captures details of a completed search task.
type SearchHistoryEntry struct {
	ID     uint64 `json:"id"`
	JobID  uint64 `json:"jobId"`
	TaskID uint64 `json:"taskId"`

	// Indexer details
	IndexerID   int    `json:"indexerId"`
	IndexerName string `json:"indexerName"`

	// Search parameters
	Query       string            `json:"query,omitempty"`
	ReleaseName string            `json:"releaseName,omitempty"` // Original full release name
	Params      map[string]string `json:"params,omitempty"`      // Raw params sent to indexer
	Categories  []int             `json:"categories,omitempty"`
	ContentType string            `json:"contentType,omitempty"`
	Priority    string            `json:"priority"`
	SearchMode  string            `json:"searchMode,omitempty"`

	// Results
	Status      string `json:"status"` // success, error, skipped, rate_limited
	ResultCount int    `json:"resultCount"`

	// Timing
	StartedAt   time.Time `json:"startedAt"`
	CompletedAt time.Time `json:"completedAt"`
	DurationMs  int       `json:"durationMs"`

	// Error details (if applicable)
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// SearchHistoryResponse is the API response for search history queries.
type SearchHistoryResponse struct {
	Entries []SearchHistoryEntry `json:"entries"`
	Total   int                  `json:"total"`
	Source  string               `json:"source"` // "memory" or "database"
}

// SearchHistoryBuffer is a thread-safe ring buffer for live search history.
type SearchHistoryBuffer struct {
	mu       sync.RWMutex
	entries  []SearchHistoryEntry
	head     int // Next write position
	count    int // Current count (0 to capacity)
	capacity int
	nextID   uint64
}

// NewSearchHistoryBuffer creates a new ring buffer with the given capacity.
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

// Push adds an entry to the buffer and returns its assigned ID.
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

// GetRecent returns the most recent entries, up to the specified limit.
// Entries are returned in reverse chronological order (newest first).
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

	// Start from the most recent entry (one before head)
	idx := (b.head - 1 + b.capacity) % b.capacity
	for i := 0; i < limit; i++ {
		result[i] = b.entries[idx]
		idx = (idx - 1 + b.capacity) % b.capacity
	}

	return result
}

// GetByIndexer returns recent entries for a specific indexer.
func (b *SearchHistoryBuffer) GetByIndexer(indexerID int, limit int) []SearchHistoryEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if limit <= 0 {
		limit = b.count
	}

	result := make([]SearchHistoryEntry, 0, limit)

	// Start from the most recent entry
	idx := (b.head - 1 + b.capacity) % b.capacity
	for i := 0; i < b.count && len(result) < limit; i++ {
		if b.entries[idx].IndexerID == indexerID {
			result = append(result, b.entries[idx])
		}
		idx = (idx - 1 + b.capacity) % b.capacity
	}

	return result
}

// Count returns the current number of entries in the buffer.
func (b *SearchHistoryBuffer) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.count
}

// Stats returns basic statistics about the buffer.
type SearchHistoryStats struct {
	Count       int            `json:"count"`
	Capacity    int            `json:"capacity"`
	ByStatus    map[string]int `json:"byStatus"`
	ByPriority  map[string]int `json:"byPriority"`
	AvgDuration float64        `json:"avgDurationMs"`
}

func (b *SearchHistoryBuffer) Stats() SearchHistoryStats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	stats := SearchHistoryStats{
		Count:      b.count,
		Capacity:   b.capacity,
		ByStatus:   make(map[string]int),
		ByPriority: make(map[string]int),
	}

	if b.count == 0 {
		return stats
	}

	var totalDuration int64
	idx := (b.head - 1 + b.capacity) % b.capacity
	for i := 0; i < b.count; i++ {
		entry := b.entries[idx]
		stats.ByStatus[entry.Status]++
		stats.ByPriority[entry.Priority]++
		totalDuration += int64(entry.DurationMs)
		idx = (idx - 1 + b.capacity) % b.capacity
	}

	stats.AvgDuration = float64(totalDuration) / float64(b.count)

	return stats
}

// HistoryRecorder is the interface for recording search history.
type HistoryRecorder interface {
	Record(entry SearchHistoryEntry)
}

// historyRecorderImpl combines in-memory buffer with optional persistent store.
type historyRecorderImpl struct {
	buffer *SearchHistoryBuffer
	// store will be added when SQLite persistence is implemented
}

// NewHistoryRecorder creates a new history recorder with the given buffer.
func NewHistoryRecorder(buffer *SearchHistoryBuffer) HistoryRecorder {
	return &historyRecorderImpl{
		buffer: buffer,
	}
}

// Record adds an entry to both the in-memory buffer and persistent store.
func (r *historyRecorderImpl) Record(entry SearchHistoryEntry) {
	if r.buffer != nil {
		r.buffer.Push(entry)
	}
	// Persistent store recording will be added later
}

// --- Indexer Outcome Tracking ---

const defaultOutcomeCapacity = 1000

// IndexerOutcome represents the cross-seed outcome for a specific indexer's search results.
type IndexerOutcome struct {
	Outcome    string    `json:"outcome"`              // "added", "failed", "no_match", ""
	AddedCount int       `json:"addedCount,omitempty"` // Number of torrents added from this indexer
	Message    string    `json:"message,omitempty"`
	RecordedAt time.Time `json:"recordedAt"`
}

// indexerOutcomeKey uniquely identifies an indexer's results within a job.
type indexerOutcomeKey struct {
	JobID     uint64
	IndexerID int
}

// IndexerOutcomeStore tracks cross-seed outcomes per (JobID, IndexerID).
// Uses a bounded map with a ring buffer for FIFO eviction.
type IndexerOutcomeStore struct {
	mu       sync.RWMutex
	outcomes map[indexerOutcomeKey]IndexerOutcome
	ring     []indexerOutcomeKey // Ring buffer for FIFO eviction
	head     int                 // Next write position (also oldest entry when full)
	count    int                 // Current entries (0 to maxSize)
	maxSize  int
}

// NewIndexerOutcomeStore creates a new outcome store with the given capacity.
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

// Record stores an outcome for a specific (jobID, indexerID) pair.
func (s *IndexerOutcomeStore) Record(jobID uint64, indexerID int, outcome string, addedCount int, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := indexerOutcomeKey{JobID: jobID, IndexerID: indexerID}

	// Update existing entry without changing eviction order
	if _, exists := s.outcomes[key]; exists {
		s.outcomes[key] = IndexerOutcome{
			Outcome:    outcome,
			AddedCount: addedCount,
			Message:    message,
			RecordedAt: time.Now(),
		}
		return
	}

	// New entry - evict oldest if at capacity
	if s.count >= s.maxSize {
		oldest := s.ring[s.head]
		delete(s.outcomes, oldest)
	} else {
		s.count++
	}

	// Write new key at head and advance
	s.ring[s.head] = key
	s.head = (s.head + 1) % s.maxSize

	s.outcomes[key] = IndexerOutcome{
		Outcome:    outcome,
		AddedCount: addedCount,
		Message:    message,
		RecordedAt: time.Now(),
	}
}

// Get retrieves the outcome for a specific (jobID, indexerID) pair.
func (s *IndexerOutcomeStore) Get(jobID uint64, indexerID int) (IndexerOutcome, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	oc, ok := s.outcomes[indexerOutcomeKey{JobID: jobID, IndexerID: indexerID}]
	return oc, ok
}

// Count returns the current number of outcomes stored.
func (s *IndexerOutcomeStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.outcomes)
}

// SearchHistoryEntryWithOutcome extends SearchHistoryEntry with outcome data for API responses.
type SearchHistoryEntryWithOutcome struct {
	SearchHistoryEntry
	Outcome    string `json:"outcome,omitempty"`    // "added", "failed", "no_match", ""
	AddedCount int    `json:"addedCount,omitempty"` // Torrents added from this indexer
}

// SearchHistoryResponseWithOutcome is the API response that includes outcome data.
type SearchHistoryResponseWithOutcome struct {
	Entries []SearchHistoryEntryWithOutcome `json:"entries"`
	Total   int                             `json:"total"`
	Source  string                          `json:"source"` // "memory" or "database"
}
