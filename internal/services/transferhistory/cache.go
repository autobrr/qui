// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory

import (
	"maps"
	"slices"
	"sync"
)

// maxCachedAnswers is the number of answers that the cache keeps. Without a
// write, the keys of new periods would grow.
const maxCachedAnswers = 64

// answerCache keeps the answers of the transfer queries by a release
// attribute until the stored history of their instance changes: a write, a
// fold, or a delete of history. A year of torrent rows takes seconds to read.
type answerCache struct {
	mu sync.Mutex
	// version counts the changes of the stored history. An answer that a
	// query read before a change is not kept.
	version uint64
	// folds is the number of folds that move torrent days now. A query can
	// read a part of a fold, so no answer is kept while one runs.
	folds   int
	answers map[answerKey][]TransferRow
}

// answerKey holds what the answer of a query by a release attribute depends
// on. The torrent rows are on UTC days, so the key holds days, not instants.
type answerKey struct {
	instanceID     int
	fromDay, toDay int64
	noStart        bool
	bucket         Bucket
	groupBy        Grouping
	group          string
	hasGroup       bool
	limit          int
}

func keyOf(q TransferQuery) answerKey {
	key := answerKey{
		instanceID: q.InstanceID,
		noStart:    q.From.IsZero(),
		bucket:     q.Bucket,
		groupBy:    q.GroupBy,
		limit:      q.Limit,
	}
	key.fromDay, key.toDay = daysOf(q.From, q.To)
	if q.Group != nil {
		key.group, key.hasGroup = *q.Group, true
	}
	return key
}

// get returns a kept answer, and the version of the history that a new
// answer must be read at to be kept.
func (c *answerCache) get(key answerKey) (rows []TransferRow, version uint64, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rows, ok = c.answers[key]
	return slices.Clone(rows), c.version, ok
}

// put keeps rows that a query read at version, unless the history changed
// since then or a fold runs.
func (c *answerCache) put(key answerKey, version uint64, rows []TransferRow) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if version != c.version || c.folds > 0 {
		return
	}
	if c.answers == nil || len(c.answers) >= maxCachedAnswers {
		c.answers = make(map[answerKey][]TransferRow)
	}
	c.answers[key] = slices.Clone(rows)
}

// changed drops the answers of an instance. Call it after each change of the
// stored history of the instance, also one that failed, because a part of it
// can be stored.
func (c *answerCache) changed(instanceID int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.version++
	maps.DeleteFunc(c.answers, func(key answerKey, _ []TransferRow) bool { return key.instanceID == instanceID })
}

// changedAll drops each answer, after a change of the history of each instance.
func (c *answerCache) changedAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.version++
	clear(c.answers)
}

// foldStarted drops each answer, and keeps no new one until foldEnded.
func (c *answerCache) foldStarted() {
	c.mu.Lock()
	c.folds++
	c.mu.Unlock()
	c.changedAll()
}

func (c *answerCache) foldEnded() {
	c.mu.Lock()
	c.folds--
	c.mu.Unlock()
	c.changedAll()
}
