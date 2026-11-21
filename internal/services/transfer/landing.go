// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"strings"

	"github.com/autobrr/qui/internal/fsops"
)

type landing struct {
	targetID int
	paths    fsops.PathDialect
	tops     map[string]struct{}
}

func (j *job) landing() landing {
	d := j.rootPaths
	l := landing{targetID: j.target.ID, paths: d, tops: map[string]struct{}{}}
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
	for other, l := range s.landings {
		if other != id && own.meets(l) {
			return other, s.left
		}
	}
	s.landings[id] = own
	return 0, nil
}

func (s *Service) leave(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.landings[id]; !ok {
		return
	}
	delete(s.landings, id)
	close(s.left)
	s.left = make(chan struct{})
}

func (j *job) land(ctx context.Context) error {
	own := j.landing()
	waited := false
	for {
		other, left := j.svc.land(j.rec.ID, own)
		if other == 0 {
			break
		}
		if !waited {
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
