// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"errors"

	"github.com/autobrr/qui/internal/models"
)

// Errors
var (
	ErrTransferAlreadyExists = errors.New("transfer already exists for this torrent")
	ErrCannotCancel          = errors.New("cannot cancel transfer in current state")
	ErrSourceNotAccessible   = errors.New("source instance not accessible or lacks local filesystem access")
	ErrTargetNotAccessible   = errors.New("target instance not accessible or lacks local filesystem access")
	ErrTorrentNotFound       = errors.New("torrent not found on source instance")
	ErrNoLinkModeAvailable   = errors.New("no link mode available (hardlink/reflink not enabled and different filesystems)")
)

// TransferRequest is the request to queue a new transfer
type TransferRequest struct {
	SourceInstanceID int               `json:"sourceInstanceId"`
	TargetInstanceID int               `json:"targetInstanceId"`
	TorrentHash      string            `json:"torrentHash"`
	PathMappings     map[string]string `json:"pathMappings,omitempty"`
	DeleteFromSource bool              `json:"deleteFromSource"`
	PreserveCategory bool              `json:"preserveCategory"`
	PreserveTags     bool              `json:"preserveTags"`
}

// Validate validates the transfer request
func (r *TransferRequest) Validate() error {
	if r.SourceInstanceID == 0 {
		return errors.New("source instance ID is required")
	}
	if r.TargetInstanceID == 0 {
		return errors.New("target instance ID is required")
	}
	if r.SourceInstanceID == r.TargetInstanceID {
		return errors.New("source and target instance must be different")
	}
	if r.TorrentHash == "" {
		return errors.New("torrent hash is required")
	}
	return nil
}

// MoveRequest is a convenience wrapper for common move operations
type MoveRequest struct {
	SourceInstanceID int               `json:"sourceInstanceId"`
	TargetInstanceID int               `json:"targetInstanceId"`
	Hash             string            `json:"hash"`
	PathMappings     map[string]string `json:"pathMappings,omitempty"`
	DeleteFromSource bool              `json:"deleteFromSource"` // Default: true
	PreserveCategory bool              `json:"preserveCategory"` // Default: true
	PreserveTags     bool              `json:"preserveTags"`     // Default: true
}

// ListOptions for filtering transfer list
type ListOptions struct {
	InstanceID *int                    // Filter by source or target instance
	States     []models.TransferState  // Filter by states
	Limit      int
	Offset     int
}
