// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbtstub

import (
	"bytes"
	"errors"
	"io/fs"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/stretchr/testify/assert"
)

// Swarm lets stubs reach each other as BitTorrent peers. A stub that gets
// the address of another stub as a peer of a torrent connects to it, unless
// the other stub is Unreachable. When one of the two seeds the torrent and
// the other checked its files and wants the rest, the files pass at once.
type Swarm struct {
	mu    sync.Mutex
	peers map[string]*Server
}

func NewSwarm() *Swarm {
	return &Swarm{peers: map[string]*Server{}}
}

// Join gives s the public peer address addr: the external address and the
// listen port that s reports, and the address at which the other stubs of
// the swarm reach it.
func (w *Swarm) Join(s *Server, addr netip.AddrPort) {
	w.mu.Lock()
	w.peers[addr.String()] = s
	w.mu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.swarm = w
	s.ListenPort = int(addr.Port())
	if addr.Addr().Is4() {
		s.externalV4 = addr.Addr().String()
	} else {
		s.externalV6 = addr.Addr().String()
	}
}

func (w *Swarm) at(addr string) *Server {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.peers[addr]
}

// addPeers answers torrents/addPeers: each peer must be host:port. The stub
// connects to each peer that is a stub of its swarm.
func (s *Server) addPeers(w http.ResponseWriter, param func(string) string) {
	var peers []string
	for p := range strings.SplitSeq(param("peers"), "|") {
		if _, err := netip.ParseAddrPort(p); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		peers = append(peers, p)
	}
	for _, t := range s.selected(param("hashes"), "|") {
		for _, p := range peers {
			if other := s.swarm.at(p); other != nil && other != s {
				s.connect(t, other)
			}
		}
	}
}

// connect connects the torrent t of s to the same torrent of other. It locks
// other: the swarm of a test connects one pair at a time.
func (s *Server) connect(t *Torrent, other *Server) {
	other.mu.Lock()
	defer other.mu.Unlock()
	if other.Unreachable {
		return
	}
	ot, ok := other.torrents[t.Hash]
	if !ok || t.add == nil || ot.add == nil {
		return
	}
	switch {
	case seeds(t) && wants(ot):
		other.pass(s, t, ot)
	case seeds(ot) && wants(t):
		s.pass(other, ot, t)
	}
}

func seeds(t *Torrent) bool {
	a := t.add
	return !a.stopped && a.checked && !a.checking && !a.missing && a.progress >= 1
}

func wants(t *Torrent) bool {
	a := t.add
	return !a.stopped && a.checked && !a.checking && a.progress < 1
}

// allTimeLateSyncs is how many syncs after a download report the downloaded
// count from before it. A client that polls once a second reads that count a
// few times.
const allTimeLateSyncs = 4

// pass writes each wanted file of the torrent to of s that differs from the
// file of the seeding torrent from of the stub seeder, as a download does, and
// completes the torrent. A seeder that misses a file sends nothing.
func (s *Server) pass(seeder *Server, from, to *Torrent) {
	sources, targets := seeder.diskFiles(from), s.diskFiles(to)
	data := make([][]byte, len(sources))
	for i, name := range sources {
		var err error
		if data[i], err = os.ReadFile(name); err != nil {
			return
		}
	}
	for i, name := range targets {
		if to.add.priorities[i] == 0 {
			continue
		}
		if have, err := os.ReadFile(name); err == nil && bytes.Equal(have, data[i]) {
			continue
		}
		assert.NoError(s.t, os.MkdirAll(filepath.Dir(name), 0o755))
		assert.NoError(s.t, os.WriteFile(name, data[i], 0o600))
		if err := os.Remove(name + incompleteExt); !errors.Is(err, fs.ErrNotExist) {
			assert.NoError(s.t, err)
		}
		to.add.downloaded += int64(len(data[i]))
		to.add.allTimeLate = allTimeLateSyncs
		from.Uploaded += int64(len(data[i]))
		s.FreeSpace = max(s.FreeSpace-int64(len(data[i])), 0)
	}
	to.add.progress, to.add.pieceStates = 1, nil
}

// freeSpaceAt answers app/getFreeSpaceAtPath, from qBittorrent 5.3.
func (s *Server) freeSpaceAt(w http.ResponseWriter, r *http.Request) {
	if !s.webAPIAtLeast("2.15.2") {
		http.NotFound(w, r)
		return
	}
	s.freeSpacePath = r.URL.Query().Get("path")
	_, _ = w.Write([]byte(strconv.FormatInt(s.FreeSpace, 10)))
}
