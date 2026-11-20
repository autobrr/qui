// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sharetracker

import (
	"net/netip"
	"sync"
	"time"
)

const announcesPerMinute = 30

type rateLimit struct {
	mu      sync.Mutex
	windows map[netip.Addr]window
	swept   time.Time
}

type window struct {
	start time.Time
	count int
}

func (l *rateLimit) allow(from netip.Addr, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.swept) >= time.Minute {
		for addr, w := range l.windows {
			if now.Sub(w.start) >= time.Minute {
				delete(l.windows, addr)
			}
		}
		l.swept = now
	}
	w, ok := l.windows[from]
	if !ok || now.Sub(w.start) >= time.Minute {
		w = window{start: now}
	}
	w.count++
	l.windows[from] = w
	return w.count <= announcesPerMinute
}
