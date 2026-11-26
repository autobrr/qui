// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/dirscan"
)

type webhookMatch struct {
	dir           *models.DirScanDirectory
	cleanPath     string
	tiedInstances []int
}

func webhookInstanceID(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("instanceId")
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		return 0, errors.New("instanceId is not a positive integer")
	}
	return id, nil
}

func instanceHasEnabledDirectory(dirs []*models.DirScanDirectory, instanceID int) bool {
	return slices.ContainsFunc(dirs, func(dir *models.DirScanDirectory) bool {
		return dir.Enabled && dir.TargetInstanceID == instanceID
	})
}

func (h *DirScanHandler) matchWebhookDirectory(r *http.Request, dirs []*models.DirScanDirectory, instanceID int, resolvedPath string) webhookMatch {
	var match webhookMatch
	bestLen := 0
	for _, dir := range dirs {
		if !dir.Enabled || (instanceID != 0 && dir.TargetInstanceID != instanceID) {
			continue
		}
		dirPath, candidatePath, ok := dirscan.PathWithinDirectory(h.service.DirectoryPaths(r.Context(), dir), dir.Path, resolvedPath)
		if !ok {
			continue
		}
		switch {
		case len(dirPath) > bestLen:
			match = webhookMatch{dir: dir, cleanPath: candidatePath, tiedInstances: []int{dir.TargetInstanceID}}
			bestLen = len(dirPath)
		case len(dirPath) == bestLen:
			match.tiedInstances = append(match.tiedInstances, dir.TargetInstanceID)
		}
	}
	return match
}

func webhookConflictMessage(tiedInstances []int) string {
	slices.Sort(tiedInstances)
	ids := slices.Compact(tiedInstances)
	if len(ids) < 2 {
		return "Multiple directories match the given path"
	}
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = strconv.Itoa(id)
	}
	return fmt.Sprintf("Multiple directories match the given path (instances %s). Add instanceId to the webhook URL to pick one", strings.Join(names, ", "))
}
