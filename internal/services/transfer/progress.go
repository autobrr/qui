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
	// moved is the count of done when it last changed, and when. A write
	// within a second of the last sample leaves its count out of samples.
	moved sample
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
	m.samples, m.moved = nil, sample{}
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
	if m.moved.at.IsZero() || m.done != m.moved.done {
		m.moved = sample{at: now, done: m.done}
	}
	if last := len(m.samples) - 1; last < 0 || now.Sub(m.samples[last].at) >= time.Second {
		m.samples = append(m.samples, sample{at: now, done: m.done})
	}
	m.age(now)
}

// age drops the samples that the speed at now no longer needs: it keeps the
// last sample from before the speedWindow and each one after. When nothing
// moved over the whole speedWindow, the last count is the only sample, so
// the speed is 0.
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

// Progress returns the progress of a Transfer that passes its bytes now, and
// false for any other Transfer.
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
