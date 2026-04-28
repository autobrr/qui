// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package proto defines the NDJSON wire types shared between qui and qui-helper.
// Every line on the helper's stdin is a Command; every line on stdout is a Result
// (or a HelloBanner on the very first line). Types here are pure data — no
// business logic, no internal/ imports.
package proto

import "encoding/json"

// Command is written by qui to the helper's stdin (one NDJSON line per command).
type Command struct {
	RequestID string          `json:"requestID"`          // UUID, qui-generated
	Op        string          `json:"op"`                 // "fs.stat", "tree.hardlink", "control.cancel", …
	Args      json.RawMessage `json:"args"`               // op-specific payload
	Deadline  string          `json:"deadline,omitempty"` // RFC 3339; helper aborts if exceeded
}

// Result is written by the helper to stdout (one NDJSON line per result frame).
type Result struct {
	RequestID string          `json:"requestID"`
	OK        bool            `json:"ok"`
	Code      string          `json:"code,omitempty"`    // stable error code
	Error     string          `json:"error,omitempty"`   // human-readable error detail
	Payload   json.RawMessage `json:"payload,omitempty"` // op-specific response
	Done      bool            `json:"done,omitempty"`    // true on last frame (always true for non-streaming ops)
	Frame     int             `json:"frame,omitempty"`   // monotonic per-requestID counter (streaming ops only)
}

// HelloBanner is the first line the helper writes to stdout on session startup.
// qui parses it before sending any commands.
type HelloBanner struct {
	HelperVersion string   `json:"helperVersion"`
	ProtoVersion  string   `json:"protoVersion"` // "1"
	Capabilities  []string `json:"capabilities"`
	AllowedRoots  []string `json:"allowedRoots"`
	ReflinkRoots  []string `json:"reflinkRoots"` // subset of AllowedRoots whose FS supports CoW reflinks
	Platform      string   `json:"platform"`      // "linux", "darwin", "windows"
	Hostname      string   `json:"hostname"`
	PID           int      `json:"pid"`
	StartedAt     string   `json:"startedAt"` // RFC 3339
}

// Op constants. Whether an op streams is determined solely by Op via IsStreamingOp.
const (
	OpStat     = "fs.stat"
	OpLstat    = "fs.lstat"
	OpReadDir  = "fs.readdir"
	OpWalk     = "fs.walk"
	OpStatfs   = "fs.statfs"
	OpSameFS   = "fs.samefs"
	OpMkdir    = "fs.mkdir"
	OpRemove   = "fs.remove"
	OpRemoveAll = "fs.removeall"

	OpTreeHardlink = "tree.hardlink"
	OpTreeReflink  = "tree.reflink"
	OpTreeRemove   = "tree.remove"

	OpControlCancel   = "control.cancel"
	OpControlShutdown = "control.shutdown"

	OpDiagEcho = "diag.echo"
)

// IsStreamingOp returns true if the op produces multiple Result frames before Done.
func IsStreamingOp(op string) bool {
	return op == OpWalk
}

// Stable error codes returned in Result.Code.
const (
	CodePathNotAllowed    = "path_not_allowed"
	CodePathNotFound      = "path_not_found"
	CodePermissionDenied  = "permission_denied"
	CodeCrossDevice       = "cross_device"
	CodeTreePartialRollback = "tree_partial_rollback"
	CodeVersionSkew       = "version_skew"
	CodeRequestTooLarge   = "request_too_large"
	CodeInternal          = "internal"
	CodeConnectionLost    = "connection_lost"
	CodeCancelled         = "cancelled"
	CodeDeadlineExceeded  = "deadline_exceeded"
)
