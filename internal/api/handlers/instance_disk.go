// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
)

// InstanceDiskResponse reports the disk that holds the default save path of an
// instance, as qui reads it through the instance's filesystem access. Both
// fields are null when qui cannot read that disk.
type InstanceDiskResponse struct {
	FreeBytes  *int64 `json:"freeBytes"`
	TotalBytes *int64 `json:"totalBytes"`
}

// SetFilesystem wires the filesystem backends in, which GetDisk reads through.
func (h *InstancesHandler) SetFilesystem(pool *fsops.Pool) {
	h.filesystem = pool
}

// GetDisk returns the free and total bytes of the disk of the default save path.
func (h *InstancesHandler) GetDisk(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	disk, err := h.readDisk(ctx, instanceID)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "instances:getDisk") {
			return
		}
		log.Trace().Err(err).Int("instanceID", instanceID).Msg("Failed to read the disk of the default save path")
		RespondJSON(w, http.StatusOK, InstanceDiskResponse{})
		return
	}

	RespondJSON(w, http.StatusOK, InstanceDiskResponse{FreeBytes: &disk.BytesAvailable, TotalBytes: &disk.BytesTotal})
}

func (h *InstancesHandler) readDisk(ctx context.Context, instanceID int) (*fsops.StatfsResult, error) {
	backend, _, err := h.filesystem.Require(ctx, instanceID, models.CapabilityRead)
	if err != nil {
		return nil, err
	}
	prefs, err := h.syncManager.GetAppPreferences(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	// An empty path cleans to ".", which is a folder of qui, not of the instance.
	if prefs.SavePath == "" {
		return nil, errors.New("default save path is empty")
	}
	disk, err := backend.Statfs(ctx, backend.Paths().Clean(prefs.SavePath))
	if err != nil {
		return nil, err
	}
	if disk.BytesTotal <= 0 {
		return nil, errors.New("disk reports no size")
	}
	return disk, nil
}
