// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"errors"
	"time"
)

// ErrScanInProgress is returned when a scan is already running for an instance.
var ErrScanInProgress = errors.New("scan already in progress for this instance")

// ErrDeletionInProgress is returned by ChangeSSH while an orphan scan deletes on the instance.
var ErrDeletionInProgress = errors.New("orphan scan deletion in progress for this instance")

// ErrInvalidRunStatus is returned when an operation is attempted on a run with an incompatible status.
var ErrInvalidRunStatus = errors.New("invalid run status for this operation")

// ErrRunNotFound is returned when a run cannot be found.
var ErrRunNotFound = errors.New("run not found")

// ErrCannotCancelDuringDeletion is returned when attempting to cancel a run mid-deletion.
var ErrCannotCancelDuringDeletion = errors.New("cannot cancel run while deletion is in progress")

// ErrRunAlreadyFinished is returned when attempting to modify a completed/failed/canceled run.
var ErrRunAlreadyFinished = errors.New("run already finished")

// ErrFilesystemModeChanged is returned when a run is confirmed after the instance's filesystem mode changed.
var ErrFilesystemModeChanged = errors.New("filesystem access changed since the scan")

// FilesystemModeChangedMessage is the user-facing text for ErrFilesystemModeChanged,
// stored on the failed run and returned in the 409 body.
const FilesystemModeChangedMessage = "Filesystem access for this instance changed since the scan. Run a new scan before deleting."

// RemoteNoIdentityMessage is the 409 body for a remote run that cannot be
// deleted: its instance is on the sftp tier, or its walk missed an identity.
const RemoteNoIdentityMessage = "This remote instance can delete orphan files only when it reports file identity and the scan read it for every file. The preview is kept."

// AutoCleanupNoIdentityMessage is added to a scheduled remote run that
// automatic cleanup skipped for the same reason.
const AutoCleanupNoIdentityMessage = "Automatic cleanup did not run, because this remote instance does not report file identity or the scan could not read it for every file. Review the preview instead."

// AutoCleanupSkippedMessage is added instead when the run already holds
// IdentityIncompleteMessage, which gives the reason and what to do.
const AutoCleanupSkippedMessage = "Automatic cleanup did not run."

// IdentityIncompleteMessage is the warning on a remote run whose walk did not
// read every file's identity.
const IdentityIncompleteMessage = "This preview cannot be deleted, because the scan did not read a file identity for every file. The remote server's find failed or was refused, a folder held more entries than find lists at once, files changed while it ran, or the walk stopped early. Without an identity for every file, a second path to a torrent's file can look like an orphan. Export the list, or run a new scan."

// ScanModeChangedMessage is stored on a scan that failed because the
// instance's filesystem mode changed while it ran.
const ScanModeChangedMessage = "Filesystem access for this instance changed during the scan. Run a new scan."

// RunStatus represents the status of an orphan scan run.
type RunStatus string

const (
	RunStatusPending      RunStatus = "pending"
	RunStatusScanning     RunStatus = "scanning"
	RunStatusPreviewReady RunStatus = "preview_ready"
	RunStatusDeleting     RunStatus = "deleting"
	RunStatusCompleted    RunStatus = "completed"
	RunStatusFailed       RunStatus = "failed"
	RunStatusCanceled     RunStatus = "canceled"
)

// TriggerType represents how a scan was triggered.
type TriggerType string

const (
	TriggerManual    TriggerType = "manual"
	TriggerScheduled TriggerType = "scheduled"
)

// FileStatus represents the status of an orphan file in a scan.
type FileStatus string

const (
	FileStatusPending FileStatus = "pending"
	FileStatusDeleted FileStatus = "deleted"
	FileStatusSkipped FileStatus = "skipped"
	FileStatusFailed  FileStatus = "failed"
)

// OrphanFile represents a file found during an orphan scan.
type OrphanFile struct {
	ID             int64
	RunID          int64
	Path           string
	Size           int64
	ModifiedAt     time.Time
	Status         FileStatus
	ErrorMessage   string
	IsAbandonedDir bool

	files int // files counted into Size, for dropAliasOrphans
}

// AbandonedDir is a visited directory. The candidate pass decides whether the
// run can empty it without removing protected content.
type AbandonedDir struct {
	Path    string
	ModTime time.Time
	// directFiles counts observed files; the walker subtracts orphan children.
	directFiles int
}

// Settings represents orphan scan settings for an instance.
type Settings struct {
	ID                  int64
	InstanceID          int
	Enabled             bool
	GracePeriodMinutes  int
	IgnorePaths         []string
	ScanIntervalHours   int
	PreviewSort         string
	MaxFilesPerRun      int
	AutoCleanupEnabled  bool
	AutoCleanupMaxFiles int
	ScanDefaultSavePath bool
	ScanCategoryPaths   bool
	DeleteAbandonedDirs bool
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// Run represents an orphan scan run.
type Run struct {
	ID             int64
	InstanceID     int
	Status         RunStatus
	TriggeredBy    TriggerType
	ScanPaths      []string
	FilesFound     int
	FilesDeleted   int
	FoldersDeleted int
	BytesReclaimed int64
	Truncated      bool
	ErrorMessage   string
	StartedAt      time.Time
	CompletedAt    *time.Time
}

// ScanResult holds the results of a directory scan.
type ScanResult struct {
	Orphans   []OrphanFile
	Truncated bool
	ScanPaths []string
}
