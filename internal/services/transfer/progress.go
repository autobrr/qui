// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"sync"
	"time"
)

// Progress is how far a running Transfer passed the bytes of its group.
type Progress struct {
	// Done counts the bytes that reached the target, of Total.
	Done, Total int64
	// Speed is the bytes a second over the last speedWindow, 0 until qui
	// saw bytes pass.
	Speed int64
}

// speedWindow is how far back the speed of a Transfer looks.
const speedWindow = 10 * time.Second

// meter counts the bytes of one run. The streams of a copy add to it at
// once. A nil meter counts nothing.
type meter struct {
	now func() time.Time

	mu          sync.Mutex
	done, total int64
	// samples are the counts of done at most once a second, over the last
	// speedWindow.
	samples []sample
}

type sample struct {
	at   time.Time
	done int64
}

// set starts the count again at done of total, as a copy that resumes does.
func (m *meter) set(done, total int64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.done, m.total = done, total
	m.samples = nil
	m.sample()
}

// reach moves the count to done, as a poll of a download reads it.
func (m *meter) reach(done int64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.done = done
	m.sample()
}

// add counts n more bytes.
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
	if last := len(m.samples) - 1; last < 0 || now.Sub(m.samples[last].at) >= time.Second {
		m.samples = append(m.samples, sample{at: now, done: m.done})
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
	first := m.samples[0]
	if elapsed := m.now().Sub(first.at); elapsed > 0 {
		p.Speed = int64(float64(m.done-first.done) / elapsed.Seconds())
	}
	return p
}

// Progress returns the progress of a Transfer that passes its bytes now, and
// false for any other Transfer.
func (s *Service) Progress(id int64) (Progress, bool) {
	s.mu.Lock()
	r, ok := s.runs[id]
	s.mu.Unlock()
	if !ok || r.meter == nil {
		return Progress{}, false
	}
	r.meter.mu.Lock()
	counting := r.meter.total > 0
	r.meter.mu.Unlock()
	if !counting {
		return Progress{}, false
	}
	return r.meter.progress(), true
}
