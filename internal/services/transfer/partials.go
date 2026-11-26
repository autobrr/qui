// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"
)

func (j *job) recordFlush(ctx context.Context, confirmed bool) error {
	state := FlushConfirmed
	if !confirmed {
		state = FlushUnconfirmed
	}
	if j.rec.FlushState == state || j.rec.FlushState == FlushUnconfirmed {
		return nil
	}
	j.rec.FlushState = state
	return j.svc.deps.Store.Save(ctx, j.rec)
}

func (j *job) checkPartials(ctx context.Context) error {
	for _, p := range j.parts {
		p.checked = nil
	}
	if j.rec.FlushState == FlushConfirmed || !slices.ContainsFunc(j.files, j.partial) {
		return nil
	}
	if err := j.step(ctx, StateChecking, "check the partial files on the target"); err != nil {
		return err
	}
	passed, pieces := 0, 0
	for _, p := range j.parts {
		if !slices.ContainsFunc(j.files, func(f *groupFile) bool { return f.owner == p && j.partial(f) }) {
			continue
		}
		states, err := j.checkOnTarget(ctx, p)
		if err != nil {
			return err
		}
		p.checked = make([]byte, (len(states)+7)/8)
		for piece, state := range states {
			if state == qbt.PieceStateAlreadyDownloaded {
				p.checked[piece/8] |= 0x80 >> (piece % 8)
				passed++
			}
		}
		pieces += len(states)
	}
	j.logf("The check on %s passed %d of %d pieces of the partial files.", j.target.Name, passed, pieces)
	return j.step(ctx, StateCopying, "copy the pieces that failed the check")
}

func (j *job) partial(f *groupFile) bool {
	_, made := j.made[f]
	return made && !f.linked && !f.linksCopy
}

func (p *part) isChecked(piece int) bool {
	return piece/8 < len(p.checked) && p.checked[piece/8]&(0x80>>(piece%8)) != 0
}
