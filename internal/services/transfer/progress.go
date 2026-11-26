// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"sync"
	"time"
)

type Progress struct {
	Done, Total int64
	Speed       int64
}

const speedWindow = 10 * time.Second

type meter struct {
	now func() time.Time

	mu          sync.Mutex
	done, total int64
	samples     []sample
	moved       sample
}

type sample struct {
	at   time.Time
	done int64
}

func (m *meter) set(done, total int64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.done, m.total = done, total
	m.samples, m.moved = nil, sample{}
	m.sample()
}

func (m *meter) reach(done int64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.done = done
	m.sample()
}

func (m *meter) add(n int64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.done += n
	m.sample()
}

func (m *meter) sample() {
	now := m.now()
	if m.moved.at.IsZero() || m.done != m.moved.done {
		m.moved = sample{at: now, done: m.done}
	}
	if last := len(m.samples) - 1; last < 0 || now.Sub(m.samples[last].at) >= time.Second {
		m.samples = append(m.samples, sample{at: now, done: m.done})
	}
	m.age(now)
}

func (m *meter) age(now time.Time) {
	if now.Sub(m.moved.at) >= speedWindow {
		m.samples = append(m.samples[:0], m.moved)
		return
	}
	for len(m.samples) > 1 && now.Sub(m.samples[1].at) >= speedWindow {
		m.samples = m.samples[1:]
	}
}

func (m *meter) progress() Progress {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := Progress{Done: m.done, Total: m.total}
	if len(m.samples) == 0 {
		return p
	}
	now := m.now()
	m.age(now)
	first := m.samples[0]
	if elapsed := now.Sub(first.at); elapsed > 0 {
		p.Speed = int64(float64(m.done-first.done) / elapsed.Seconds())
	}
	return p
}

func (s *Service) Progress(id int64) (Progress, bool) {
	s.mu.Lock()
	var m *meter
	if r, ok := s.runs[id]; ok {
		m = r.meter
	}
	s.mu.Unlock()
	if m == nil {
		return Progress{}, false
	}
	m.mu.Lock()
	counting := m.total > 0
	m.mu.Unlock()
	if !counting {
		return Progress{}, false
	}
	return m.progress(), true
}
