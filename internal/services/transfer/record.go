// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

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

type State string

const (
	StateWaiting   State = "waiting"
	StateCopying   State = "copying"
	StateChecking  State = "checking"
	StateFinishing State = "finishing"
	StateDone      State = "done"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

type Way string

const (
	WaySFTP       Way = "sftp"
	WayLocal      Way = "local"
	WayNone       Way = "none"
	WayBitTorrent Way = "bittorrent"
)

var ForcedWays = map[string]Way{"": "", "auto": "", "copy": WaySFTP, "bittorrent": WayBitTorrent}

type StartedBy string

const (
	StartedByRule StartedBy = "rule"
	StartedByUser StartedBy = "user"
)

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

type After string

const (
	AfterRemoveWithFiles After = "removeWithFiles"
	AfterKeepFiles       After = "keepFiles"
	AfterKeepSeeding     After = "keepSeeding"
)

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
	Size             int64
	RetryAt          time.Time
	FailureClass     FailureClass
	FailureReason    string
	CreatedAt        time.Time
	FinishedAt       time.Time
}

type Options struct {
	After        After `json:"after"`
	LeaveStopped bool  `json:"leaveStopped"`
	ForcedWay    Way   `json:"forcedWay"`
	Paused       bool  `json:"paused"`
	SkipChecking *bool `json:"skipChecking"`
}

type Step struct {
	Name   string    `json:"name"`
	At     time.Time `json:"at"`
	Detail string    `json:"detail"`
}

type LogLine struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
	Hint    *Hint     `json:"hint,omitzero"`
}

type HintKind string

const HintTCPAndUTP HintKind = "tcpAndUtp"

type Hint struct {
	Kind       HintKind `json:"kind"`
	InstanceID int      `json:"instanceId"`
}

type Member struct {
	Hash             string
	Name             string
	SourceSavePath   string
	TargetInstanceID int
	Placement        Placement
	ProvenPieces     []byte
}

type Placement struct {
	SavePath         string
	Category         string
	Tags             []string
	ContentLayout    string
	SavePathOfTarget bool
	AutoTMM          bool
	Endpoint         fsops.Endpoint
}

type OwnerKind string

const (
	OwnerTransfer OwnerKind = "transfer"
	OwnerShare    OwnerKind = "share"
)

const (
	TransferHelperTag = "qui-transfer"
	ShareHelperTag    = "qui-share"
)

type Helper struct {
	InstanceID int
	Hash       string
	Random     []byte
	Owner      OwnerKind
	OwnerID    int64
	CreatedAt  time.Time
}
