// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package arr

import "sync"

type instanceChanges struct {
	mu      sync.Mutex
	changed chan struct{}
}

func (c *instanceChanges) InstancesChanged() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.changed == nil {
		c.changed = make(chan struct{})
	}
	return c.changed
}

func (c *instanceChanges) NoteInstancesChanged() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.changed != nil {
		close(c.changed)
		c.changed = nil
	}
}
