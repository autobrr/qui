// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"strings"

	"github.com/autobrr/qui/internal/fsops"
)

type landing struct {
	order              int64
	sourceID, targetID int
	paths              fsops.PathDialect
	tops               map[string]struct{}
	held               bool
}

func (j *job) landing() landing {
	d := j.rootPaths
	l := landing{sourceID: j.source.ID, targetID: j.target.ID, paths: d, tops: map[string]struct{}{}}
	for _, p := range j.parts {
		for _, f := range p.plan {
			if f.pad {
				continue
			}
			first, _, _ := strings.Cut(f.name, "/")
			l.tops[fsops.NormalizePath(d, d.Join(p.Placement.SavePath, first))] = struct{}{}
		}
	}
	return l
}

func (l landing) meets(other landing) bool {
	return l.targetID == other.targetID && (l.holdsOrIsBelow(other) || other.holdsOrIsBelow(l))
}

func (l landing) holdsOrIsBelow(other landing) bool {
	for top := range l.tops {
		for dir := top; ; {
			if _, ok := other.tops[dir]; ok {
				return true
			}
			parent := l.paths.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return false
}

func (s *Service) land(id int64, own landing) (inTheWay int64, left <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	was := s.landings[id]
	own.order = was.order
	earlier := false
	for other, l := range s.landings {
		switch {
		case other == id:
		case l.tops == nil:
			earlier = earlier || l.order < own.order && l.sourceID == own.sourceID && l.targetID == own.targetID
		case (l.held || l.order < own.order) && own.meets(l) && (inTheWay == 0 || other < inTheWay):
			inTheWay = other
		}
	}
	own.held = inTheWay == 0 && !earlier
	if was.tops == nil {
		s.landed()
	}
	s.landings[id] = own
	if own.held {
		return 0, nil
	}
	return inTheWay, s.left
}

func (s *Service) leave(id int64) {
	delete(s.landings, id)
	s.landed()
}

func (s *Service) landed() {
	close(s.left)
	s.left = make(chan struct{})
}

func (j *job) land(ctx context.Context) error {
	own := j.landing()
	waited := false
	for {
		other, left := j.svc.land(j.rec.ID, own)
		if left == nil {
			break
		}
		if other != 0 && !waited {
			waited = true
			j.rec.State = StateWaiting
			j.logf("Waits for Transfer %d, which writes at the same path on %s.", other, j.target.Name)
			j.save(ctx)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-left:
		}
	}
	if waited {
		j.rec.State = StateCopying
		j.save(ctx)
	}
	return nil
}
