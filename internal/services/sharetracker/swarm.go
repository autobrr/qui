// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sharetracker

import (
	"errors"
	"net/netip"
	"time"
)

var errFull = errors.New("this Share has as many peers as it takes")

const peerLife = 2 * Interval

type swarm struct {
	ownerSeen  time.Time
	recipients map[netip.AddrPort]time.Time
	holder     netip.Addr
}

// answer is the peer list of one announce. owner is true when the client also
// gets the public peer address of the user's own client. locked is true when
// the client took the lock, and blocked when the lock keeps it out.
type answer struct {
	owner   bool
	locked  bool
	blocked bool
	peers   []netip.AddrPort
}

// join records the announce of client in the swarm of its Share and returns
// the peers that the client gets. A new recipient client of a Share that has
// its peers per Share gets errFull. With FirstDownloaderOnly, the first
// recipient client that announces with something left to download takes the
// lock, and from then on the user's client and that address see only each
// other. A new swarm starts with the holder of the table.
func (t *Tracker) join(table *Table, client announcer, peer netip.AddrPort, req announceRequest) (answer, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	// The table of this announce can be older than a Share that another
	// announce already joined, so the swarms follow the table as it is now.
	active := t.shares.ActiveShares()
	for id := range t.swarms {
		if _, ok := active.share(id); !ok {
			delete(t.swarms, id)
		}
	}
	s := t.swarms[client.share.ID]
	if s == nil {
		stored, _ := active.share(client.share.ID)
		s = &swarm{recipients: make(map[netip.AddrPort]time.Time), holder: stored.Holder}
		t.swarms[client.share.ID] = s
	}
	now := t.now()
	for p, seen := range s.recipients {
		if now.Sub(seen) > peerLife {
			delete(s.recipients, p)
		}
	}
	if now.Sub(s.ownerSeen) > peerLife {
		s.ownerSeen = time.Time{}
	}
	lock := client.share.FirstDownloaderOnly
	if client.owner {
		s.ownerSeen = now
		if req.event == EventStopped {
			s.ownerSeen = time.Time{}
		}
		var peers []netip.AddrPort
		for p := range s.recipients {
			if !lock || p.Addr() == s.holder {
				peers = append(peers, p)
			}
		}
		return answer{peers: peers}, nil
	}
	if req.event == EventStopped {
		delete(s.recipients, peer)
		return answer{blocked: lock && peer.Addr() != s.holder}, nil
	}
	if _, known := s.recipients[peer]; !known && len(s.recipients) >= table.peersPerShare {
		return answer{}, errFull
	}
	s.recipients[peer] = now
	if lock {
		locked := !s.holder.IsValid() && req.left > 0
		if locked {
			s.holder = peer.Addr()
		}
		held := peer.Addr() == s.holder
		return answer{owner: held && !s.ownerSeen.IsZero(), locked: locked, blocked: !held}, nil
	}
	a := answer{owner: !s.ownerSeen.IsZero()}
	for p := range s.recipients {
		if p != peer {
			a.peers = append(a.peers, p)
		}
	}
	return a, nil
}

// Unlock lets the next downloader of the Share take the lock, also when the
// table still names a holder.
func (t *Tracker) Unlock(shareID int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.swarms[shareID]
	if s == nil {
		s = &swarm{recipients: make(map[netip.AddrPort]time.Time)}
		t.swarms[shareID] = s
	}
	s.holder = netip.Addr{}
}

func (t *Tracker) Holder(shareID int64) netip.Addr {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s := t.swarms[shareID]; s != nil {
		return s.holder
	}
	return netip.Addr{}
}
