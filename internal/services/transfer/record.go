// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package transfer keeps the records of Transfers and of the helper torrents
// that qui adds for Transfers and Shares, and answers which torrents qui must
// leave alone.
package transfer

import (
	"time"

	"github.com/autobrr/qui/internal/fsops"
)

type Kind string

const (
	KindTransfer Kind = "transfer"
	KindExport   Kind = "export"
)

// State is the state of a Transfer or of an Export run.
type State string

const (
	// StateWaiting: the Transfer waits for its turn.
	StateWaiting   State = "waiting"
	StateCopying   State = "copying"
	StateChecking  State = "checking"
	StateFinishing State = "finishing"
	StateDone      State = "done"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

// Way is how a Transfer moves the files, such as SFTP or a Transfer torrent.
type Way string

const (
	// WaySFTP: qui copies the bytes over SFTP, through qui, from or to a side
	// on qui's own disk or between two SFTP sides.
	WaySFTP Way = "sftp"
	// WayLocal: both sides are on qui's own disk. qui copies the files, or
	// links them when both are on one filesystem.
	WayLocal Way = "local"
	// WayNone: the target sees the files of the source, and no byte moves.
	WayNone Way = "none"
	// WayBitTorrent: the two qBittorrents pass the files by a Transfer
	// torrent, and no byte goes through qui.
	WayBitTorrent Way = "bittorrent"
)

// ForcedWays maps the way that a user picks to Options.ForcedWay. With
// "auto" or no value, qui picks the way.
var ForcedWays = map[string]Way{"": "", "auto": "", "copy": WaySFTP, "bittorrent": WayBitTorrent}

type StartedBy string

const (
	StartedByRule StartedBy = "rule"
	StartedByUser StartedBy = "user"
)

// FlushState says whether the target confirms that written bytes are on disk.
type FlushState string

const (
	FlushUnknown     FlushState = ""
	FlushConfirmed   FlushState = "confirmed"
	FlushUnconfirmed FlushState = "unconfirmed"
)

type FailureClass string

const (
	FailureTemporary FailureClass = "temporary"
	FailurePermanent FailureClass = "permanent"
)

// After is what happens to the torrents on the Transfer source after the
// Transfer.
type After string

const (
	AfterRemoveWithFiles After = "removeWithFiles"
	AfterKeepFiles       After = "keepFiles"
	AfterKeepSeeding     After = "keepSeeding"
)

// Record is one Transfer, or one Export to Instance run.
type Record struct {
	ID               int64
	Kind             Kind
	Way              Way
	State            State
	StartedBy        StartedBy
	RuleID           *int
	SourceInstanceID int
	TargetInstanceID int
	SourceEndpoint   fsops.Endpoint
	TargetEndpoint   fsops.Endpoint
	Options          Options
	Steps            []Step
	Log              []LogLine
	Members          []Member
	FlushState       FlushState
	ReservedBytes    int64
	// Size is the size of the torrent of the request at the start.
	Size int64
	// RetryAt is zero when no retry waits.
	RetryAt       time.Time
	FailureClass  FailureClass
	FailureReason string
	CreatedAt     time.Time
	// FinishedAt is zero while the Transfer holds its torrents and folders.
	FinishedAt time.Time
}

// Options are the choices of the user for the whole Transfer.
type Options struct {
	After        After `json:"after"`
	LeaveStopped bool  `json:"leaveStopped"`
	// ForcedWay is empty when qui picks the way. WaySFTP forces a copy
	// through qui, on qui's own disk or over SFTP, and WayBitTorrent a
	// Transfer torrent between the clients.
	ForcedWay Way `json:"forcedWay"`
	// Paused and SkipChecking are the fields of Export to Instance.
	Paused       bool  `json:"paused"`
	SkipChecking *bool `json:"skipChecking"`
}

// Step is one step of a Transfer, recorded before qui takes it.
type Step struct {
	Name   string    `json:"name"`
	At     time.Time `json:"at"`
	Detail string    `json:"detail"`
}

// LogLine is one line of the log that the Details panel shows.
type LogLine struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
	// Hint is set on a line that gives advice that the page can show next to
	// the Transfer.
	Hint *Hint `json:"hint,omitzero"`
}

// HintKind names the advice of a Hint.
type HintKind string

// HintTCPAndUTP: the client uses qBittorrent's default "TCP and μTP". In
// qui's tests, "TCP only" passed a Transfer torrent about three times
// faster. qui never changes the setting.
const HintTCPAndUTP HintKind = "tcpAndUtp"

// Hint is advice about one instance of a Transfer.
type Hint struct {
	Kind       HintKind `json:"kind"`
	InstanceID int      `json:"instanceId"`
}

// Member is one torrent of a Transfer, by its infohash on the Transfer source.
type Member struct {
	Hash           string
	Name           string
	SourceSavePath string
	// TargetInstanceID is the Transfer target, or the Cross-seed instance of
	// the target.
	TargetInstanceID int
	Placement        Placement
	// ProvenPieces has one bit for each Proven piece, piece 0 in the high bit
	// of the first byte. It is the only resume record.
	ProvenPieces []byte
}

// Placement is where a member lands on its target instance.
type Placement struct {
	SavePath      string
	Category      string
	Tags          []string
	ContentLayout string
	// SavePathOfTarget is true when the request named no save path, so that
	// SavePath is the folder that the target has for the category. Start
	// sets it, and a Retry then names no save path either.
	SavePathOfTarget bool
	// AutoTMM is true when the target manages the save path of the member:
	// the request named no save path, and Automatic Torrent Management is
	// the default mode of the target. Start sets it.
	AutoTMM bool
	// Endpoint is where qui reached the Cross-seed instance at the start, for
	// a member that lands there, and zero for a member on the target.
	Endpoint fsops.Endpoint
}

// OwnerKind names the table of the owner of a helper torrent.
type OwnerKind string

const (
	OwnerTransfer OwnerKind = "transfer"
	OwnerShare    OwnerKind = "share"
)

// The tags that qui gives a helper torrent when it adds it. qBittorrent's
// own "run on add" and "run on finished" programs see helper torrents, and
// a script skips one by its tag. qui itself trusts only the registry.
const (
	TransferHelperTag = "qui-transfer"
	ShareHelperTag    = "qui-share"
)

// Helper is a helper torrent: a Transfer torrent or a Share torrent.
type Helper struct {
	InstanceID int
	Hash       string
	// Random is the stored random value that the helper builder put in the
	// info dictionary.
	Random    []byte
	Owner     OwnerKind
	OwnerID   int64
	CreatedAt time.Time
}
