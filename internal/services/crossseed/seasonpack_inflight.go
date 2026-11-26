// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"sync"
)

type seasonPackApplies struct {
	mu    sync.Mutex
	packs map[string]*seasonPackTurn
}

type seasonPackTurn struct {
	claims  int
	turn    chan struct{}
	addedTo map[int]bool
}

func (a *seasonPackApplies) claim(infoHash string) (pack *seasonPackTurn, release func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	pack = a.packs[infoHash]
	if pack == nil {
		pack = &seasonPackTurn{turn: make(chan struct{}, 1), addedTo: map[int]bool{}}
		if a.packs == nil {
			a.packs = map[string]*seasonPackTurn{}
		}
		a.packs[infoHash] = pack
	}
	pack.claims++
	return pack, func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		pack.claims--
		if pack.claims == 0 {
			delete(a.packs, infoHash)
		}
	}
}

func (p *seasonPackTurn) takeTurn(ctx context.Context) (giveUp func(), err error) {
	select {
	case p.turn <- struct{}{}:
		return func() { <-p.turn }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
