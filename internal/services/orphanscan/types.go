// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"errors"
	"time"
)

// ErrScanInProgress is returned when a scan is already running for an instance.
var ErrScanInProgress = errors.New("scan already in progress for this instance")

// ErrInvalidRunStatus is returned when an operation is attempted on a run with an incompatible status.
var ErrInvalidRunStatus = errors.New("invalid run status for this operation")

// ErrRunNotFound is returned when a run cannot be found.
var ErrRunNotFound = errors.New("run not found")

// ErrCannotCancelDuringDeletion is returned when attempting to cancel a run mid-deletion.
var ErrCannotCancelDuringDeletion = errors.New("cannot cancel run while deletion is in progress")

// ErrRunAlreadyFinished is returned when attempting to modify a completed/failed/canceled run.
var ErrRunAlreadyFinished = errors.New("run already finished")

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
	ID           int64
	RunID        int64
	Path         string
	Size         int64
	ModifiedAt   time.Time
	Status       FileStatus
	ErrorMessage string
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
