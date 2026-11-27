// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package share

import (
	"net/netip"
	"time"

	"github.com/autobrr/qui/pkg/infohash"
)

type State string

const (
	StateActive State = "active"
	StateEnding State = "ending"
	StateEnded  State = "ended"
)

type TrackerKind string

const (
	TrackerOwn     TrackerKind = "own"
	TrackerOutside TrackerKind = "outside"
)

type Share struct {
	ID                  int64
	InstanceID          int
	Hash                infohash.Hash
	Name                string
	SavePath            string
	Files               []string
	Size                int64
	HelperHash          infohash.Hash
	Tracker             TrackerKind
	OwnerSecret         string
	RecipientSecret     string
	TorrentFile         []byte
	EndAtFirstDownload  bool
	FirstDownloaderOnly bool
	Holder              netip.Addr
	ExpiresAt           time.Time
	State               State
	EndReason           string
	Activity
	CreatedAt time.Time
	EndedAt   time.Time
}

type Activity struct {
	Peers      []Peer
	Downloaded int64
	Uploaded   int64
}

type Peer struct {
	Address  string    `json:"address"`
	Progress float64   `json:"progress"`
	SeenAt   time.Time `json:"seenAt"`
}
