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
	ErrMissingSourceID       = errors.New("source instance ID is required")
	ErrMissingTargetID       = errors.New("target instance ID is required")
	ErrSourceTargetSame      = errors.New("source and target instance must be different")
	ErrMissingTorrentHash    = errors.New("torrent hash is required")
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
		return ErrMissingSourceID
	}
	if r.TargetInstanceID == 0 {
		return ErrMissingTargetID
	}
	if r.SourceInstanceID == r.TargetInstanceID {
		return ErrSourceTargetSame
	}
	if r.TorrentHash == "" {
		return ErrMissingTorrentHash
	}
	return nil
}

// MoveRequest is a convenience wrapper for common move operations
type MoveRequest struct {
	SourceInstanceID int               `json:"sourceInstanceId"`
	TargetInstanceID int               `json:"targetInstanceId"`
	Hash             string            `json:"hash"`
	PathMappings     map[string]string `json:"pathMappings,omitempty"`
	DeleteFromSource bool              `json:"deleteFromSource"`
	PreserveCategory bool              `json:"preserveCategory"`
	PreserveTags     bool              `json:"preserveTags"`
}

func (r *MoveRequest) Validate() error {
	if r.SourceInstanceID == 0 {
		return ErrMissingSourceID
	}
	if r.TargetInstanceID == 0 {
		return ErrMissingTargetID
	}
	if r.SourceInstanceID == r.TargetInstanceID {
		return ErrSourceTargetSame
	}
	if r.Hash == "" {
		return ErrMissingTorrentHash
	}
	return nil
}

// ListOptions for filtering transfer list
type ListOptions struct {
	InstanceID *int                   // Filter by source or target instance
	States     []models.TransferState // Filter by states
	Limit      int
	Offset     int
}
