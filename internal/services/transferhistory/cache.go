// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory

import (
	"maps"
	"slices"
	"sync"
)

const maxCachedAnswers = 64

type answerCache struct {
	mu      sync.Mutex
	version uint64
	folds   int
	answers map[answerKey][]TransferRow
}

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

func (c *answerCache) get(key answerKey) (rows []TransferRow, version uint64, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rows, ok = c.answers[key]
	return slices.Clone(rows), c.version, ok
}

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

func (c *answerCache) changed(instanceID int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.version++
	maps.DeleteFunc(c.answers, func(key answerKey, _ []TransferRow) bool { return key.instanceID == instanceID })
}

func (c *answerCache) changedAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.version++
	clear(c.answers)
}

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
