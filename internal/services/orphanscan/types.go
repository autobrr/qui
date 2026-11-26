// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"errors"
	"time"
)

var ErrScanInProgress = errors.New("scan already in progress for this instance")

var ErrDeletionInProgress = errors.New("orphan scan deletion in progress for this instance")

var ErrInvalidRunStatus = errors.New("invalid run status for this operation")

var ErrRunNotFound = errors.New("run not found")

var ErrCannotCancelDuringDeletion = errors.New("cannot cancel run while deletion is in progress")

var ErrRunAlreadyFinished = errors.New("run already finished")

var ErrFilesystemModeChanged = errors.New("filesystem access changed since the scan")

const FilesystemModeChangedMessage = "Filesystem access for this instance changed since the scan. Run a new scan before deleting."

const RemoteNoIdentityMessage = "This remote instance can delete orphan files only when it reports file identity and the scan read it for every file. The preview is kept."

const AutoCleanupNoIdentityMessage = "Automatic cleanup did not run, because this remote instance does not report file identity or the scan could not read it for every file. Review the preview instead."

const AutoCleanupSkippedMessage = "Automatic cleanup did not run."

const IdentityIncompleteMessage = "This preview cannot be deleted, because the scan did not read a file identity for every file. The remote server's find failed or was refused, a folder held more entries than find lists at once, files changed while it ran, or the walk stopped early. Without an identity for every file, a second path to a torrent's file can look like an orphan. Export the list, or run a new scan."

const ScanModeChangedMessage = "Filesystem access for this instance changed during the scan. Run a new scan."

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

type TriggerType string

const (
	TriggerManual    TriggerType = "manual"
	TriggerScheduled TriggerType = "scheduled"
)

type FileStatus string

const (
	FileStatusPending FileStatus = "pending"
	FileStatusDeleted FileStatus = "deleted"
	FileStatusSkipped FileStatus = "skipped"
	FileStatusFailed  FileStatus = "failed"
)

type OrphanFile struct {
	ID             int64
	RunID          int64
	Path           string
	Size           int64
	ModifiedAt     time.Time
	Status         FileStatus
	ErrorMessage   string
	IsAbandonedDir bool

	files int
}

type AbandonedDir struct {
	Path        string
	ModTime     time.Time
	directFiles int
}

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

type ScanResult struct {
	Orphans   []OrphanFile
	Truncated bool
	ScanPaths []string
}
