// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proto

import "github.com/autobrr/qui/pkg/hardlinktree"

// All paths in requests are absolute. The helper rejects any path not under an allowed root.
// Command.RequestID is the canonical idempotency token — it is not duplicated inside payloads.

// --- fs.stat ---

type StatRequest struct {
	Paths []string `json:"paths"`
}

type StatEntry struct {
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Size    int64  `json:"size,omitempty"`
	ModTime string `json:"modTime,omitempty"` // RFC 3339 Nano
	IsDir   bool   `json:"isDir,omitempty"`
	Mode    uint32 `json:"mode,omitempty"`
	Err     string `json:"err,omitempty"` // "not_found", "permission", etc.
}

type StatResponse struct {
	Entries []StatEntry `json:"entries"`
}

// --- fs.lstat ---

type LstatRequest struct {
	Paths      []string `json:"paths"`
	WantFileID bool     `json:"wantFileID,omitempty"`
	WantNlinks bool     `json:"wantNlinks,omitempty"`
}

type LstatEntry struct {
	StatEntry
	IsSymlink bool   `json:"isSymlink,omitempty"`
	FileID    []byte `json:"fileID,omitempty"` // hardlink.FileID.Bytes()
	Nlinks    uint64 `json:"nlinks,omitempty"`
}

type LstatResponse struct {
	Entries []LstatEntry `json:"entries"`
}

// --- fs.walk (streaming) ---

type WalkRequest struct {
	Root           string   `json:"root"`
	SkipHidden     bool     `json:"skipHidden,omitempty"`
	IgnoreDirNames []string `json:"ignoreDirNames,omitempty"`
	IgnorePaths    []string `json:"ignorePaths,omitempty"`
	WantFileID     bool     `json:"wantFileID,omitempty"`
	WantNlinks     bool     `json:"wantNlinks,omitempty"`
	MaxEntries     int      `json:"maxEntries,omitempty"` // 0 = unlimited
}

// WalkEntry is sent as one Result.Payload frame per entry.
// The final frame carries Done:true on the Result envelope.
type WalkEntry struct {
	Path      string `json:"path,omitempty"`
	RelPath   string `json:"relPath,omitempty"`
	IsDir     bool   `json:"isDir,omitempty"`
	IsSymlink bool   `json:"isSymlink,omitempty"`
	Size      int64  `json:"size,omitempty"`
	ModTime   string `json:"modTime,omitempty"`
	Mode      uint32 `json:"mode,omitempty"`
	FileID    []byte `json:"fileID,omitempty"`
	Nlinks    uint64 `json:"nlinks,omitempty"`
	Err       string `json:"err,omitempty"`
	Truncated bool   `json:"truncated,omitempty"` // set on last frame if MaxEntries was hit
}

// --- fs.statfs ---

type StatfsRequest struct {
	Path string `json:"path"`
}

type StatfsResponse struct {
	BytesAvailable int64  `json:"bytesAvailable"`
	BytesTotal     int64  `json:"bytesTotal"`
	Filesystem     string `json:"filesystem,omitempty"` // best-effort
}

// --- fs.readdir ---

type ReadDirRequest struct {
	Path       string `json:"path"`
	MaxEntries int    `json:"maxEntries,omitempty"` // 0 = no cap (subject to per-op cap)
}

type DirEntry struct {
	Name      string `json:"name"`
	IsDir     bool   `json:"isDir,omitempty"`
	IsSymlink bool   `json:"isSymlink,omitempty"`
	Size      int64  `json:"size,omitempty"`
	ModTime   string `json:"modTime,omitempty"`
	Mode      uint32 `json:"mode,omitempty"`
}

type ReadDirResponse struct {
	Entries   []DirEntry `json:"entries"`
	Truncated bool       `json:"truncated,omitempty"`
}

// --- fs.samefs ---

type SameFSRequest struct {
	Path1 string `json:"path1"`
	Path2 string `json:"path2"`
}

type SameFSResponse struct {
	Same bool `json:"same"`
}

// --- fs.mkdir ---

type MkdirRequest struct {
	Path string `json:"path"`
	Perm uint32 `json:"perm"`
}

// --- fs.remove / fs.removeall ---

type RemoveRequest struct {
	Path        string   `json:"path"`
	Recursive   bool     `json:"recursive,omitempty"`
	IgnorePaths []string `json:"ignorePaths,omitempty"` // server-side ignore list (orphanscan)
}

type RemoveResponse struct {
	Removed      bool   `json:"removed"`
	Disposition  string `json:"disposition,omitempty"` // "deleted" | "skipped_missing" | "skipped_ignored"
	RemovedBytes int64  `json:"removedBytes,omitempty"`
}

// --- tree.hardlink / tree.reflink ---

// TreeCreateRequest carries the entire TreePlan. The helper executes it atomically
// and rolls back on partial failure.
type TreeCreateRequest struct {
	Plan     hardlinktree.TreePlan `json:"plan"`
	Mode     string                `json:"mode"`               // "hardlink" or "reflink"
	SourceFS string                `json:"sourceFS,omitempty"` // hint for pre-flight checks
}

type TreeCreateResponse struct {
	Created       int      `json:"created"`
	SkippedExists int      `json:"skippedExists"`
	RolledBack    bool     `json:"rolledBack"`
	Err           string   `json:"err,omitempty"`
	DiagFiles     []string `json:"diagFiles,omitempty"` // truncated debug, opt-in
}

// --- tree.remove ---

type TreeRemoveRequest struct {
	Plan hardlinktree.TreePlan `json:"plan"`
}

// --- control.cancel ---

type CancelRequest struct {
	RequestIDs []string `json:"requestIDs"` // ops to abort
}

// --- diag.echo ---

type DiagEchoRequest struct {
	Message string `json:"message"`
}

type DiagEchoResponse struct {
	Message string `json:"message"`
}
