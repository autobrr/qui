// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package share runs Shares: it gives the files of one torrent to someone
// whose client qui does not manage, through a Share torrent that the user's
// client seeds, and owns qui's own Share tracker.
package share

import "time"

// State is the state of a Share.
type State string

const (
	StateActive State = "active"
	// StateEnding: qui removes the Share torrent from the client, and retries
	// while the client is offline.
	StateEnding State = "ending"
	// StateEnded: the Share torrent is gone from the client.
	StateEnded State = "ended"
)

// TrackerKind is the Share tracker of a Share.
type TrackerKind string

const (
	// TrackerOwn is qui's own Share tracker.
	TrackerOwn TrackerKind = "own"
	// TrackerOutside is the tracker URL of the settings. qui receives no
	// announces from it.
	TrackerOutside TrackerKind = "outside"
)

// Share is one Share. InstanceID, Hash, Name, SavePath, Files, and Size are
// the real torrent at the start. HelperHash is the infohash of the Share
// torrent. TorrentFile is the .torrent of the recipient; List leaves it out.
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
	ExpiresAt           time.Time
	State               State
	EndReason           string
	Activity
	CreatedAt time.Time
	// EndedAt is zero until the Share has ended.
	EndedAt time.Time
}

// Activity is what qui learned of the recipient: the addresses that
// announced (the connected peers with an outside tracker), the bytes that
// the recipient has, and the bytes that the user's client sent.
type Activity struct {
	Peers      []Peer
	Downloaded int64
	Uploaded   int64
}

// Peer is one address that announced, or one connected peer of the user's
// client with an outside tracker.
type Peer struct {
	Address  string    `json:"address"`
	Progress float64   `json:"progress"`
	SeenAt   time.Time `json:"seenAt"`
}
