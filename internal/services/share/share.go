// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package share

import (
	"net/netip"
	"time"
)

type State string

const (
	StateActive State = "active"
	// StateEnding: qui removes the Share torrent from the client, and retries
	// while the client is offline.
	StateEnding State = "ending"
	StateEnded  State = "ended"
)

type TrackerKind string

const (
	TrackerOwn TrackerKind = "own"
	// TrackerOutside is the tracker URL of the settings. qui receives no
	// announces from it.
	TrackerOutside TrackerKind = "outside"
)

// Share is one Share. InstanceID, Hash, Name, SavePath, Files, and Size are
// the real torrent at the start. HelperHash is the infohash of the Share
// torrent. TorrentFile is the .torrent of the recipient; List leaves it out.
// Holder is the address of the recipient client that holds the lock of a
// Share with FirstDownloaderOnly, and zero while no client holds it.
type Share struct {
	ID                  int64
	InstanceID          int
	Hash                string
	Name                string
	SavePath            string
	Files               []string
	Size                int64
	HelperHash          string
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

// Activity is what qui learned of the recipient: the addresses that
// announced (the connected peers with an outside tracker), the bytes that
// the recipient has, and the bytes that the user's client sent.
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
