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

func (a *seasonPackApplies) enter(ctx context.Context, infoHash string) (*seasonPackTurn, func(), error) {
	a.mu.Lock()
	pack := a.packs[infoHash]
	if pack == nil {
		pack = &seasonPackTurn{turn: make(chan struct{}, 1), addedTo: map[int]bool{}}
		if a.packs == nil {
			a.packs = map[string]*seasonPackTurn{}
		}
		a.packs[infoHash] = pack
	}
	pack.claims++
	a.mu.Unlock()

	select {
	case pack.turn <- struct{}{}:
		return pack, func() {
			<-pack.turn
			a.release(infoHash, pack)
		}, nil
	case <-ctx.Done():
		a.release(infoHash, pack)
		return nil, nil, ctx.Err()
	}
}

func (a *seasonPackApplies) release(infoHash string, pack *seasonPackTurn) {
	a.mu.Lock()
	defer a.mu.Unlock()
	pack.claims--
	if pack.claims == 0 {
		delete(a.packs, infoHash)
	}
}
