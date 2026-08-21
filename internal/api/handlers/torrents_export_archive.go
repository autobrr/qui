// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/pkg/pathutil"
	"github.com/autobrr/qui/pkg/torrentname"
)

type torrentArchiveExporter interface {
	ExportTorrent(ctx context.Context, instanceID int, hash string) ([]byte, string, string, error)
}

type torrentArchiveTarget struct {
	InstanceID   int    `json:"instanceId"`
	InstanceName string `json:"instanceName"`
	Hash         string `json:"hash"`
	Category     string `json:"category"`
	Filename     string `json:"filename,omitempty"`
}

type torrentArchiveRequest struct {
	Targets []torrentArchiveTarget `json:"targets"`
}

func (h *TorrentsHandler) ExportTorrentArchive(w http.ResponseWriter, r *http.Request) {
	var request torrentArchiveRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Targets) == 0 {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	for i := range request.Targets {
		request.Targets[i].Hash = strings.TrimSpace(request.Targets[i].Hash)
		if request.Targets[i].InstanceID <= 0 || request.Targets[i].Hash == "" {
			RespondError(w, http.StatusBadRequest, "Each target requires a valid instance ID and torrent hash")
			return
		}
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="qui-torrents.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	exporter := h.archiveExporter
	if exporter == nil {
		exporter = h.syncManager
	}
	if err := writeTorrentArchive(r.Context(), w, request.Targets, exporter); err != nil {
		log.Error().Err(err).Msg("Failed to write torrent export archive")
	}
}

func writeTorrentArchive(ctx context.Context, output io.Writer, targets []torrentArchiveTarget, exporter torrentArchiveExporter) (err error) {
	archive := zip.NewWriter(output)
	defer func() {
		err = errors.Join(err, archive.Close())
	}()

	multipleInstances := hasMultipleArchiveInstances(targets)
	failures := make([]string, 0)
	usedNames := make(map[string]struct{}, len(targets))

	for _, target := range targets {
		data, suggestedName, trackerDomain, exportErr := exporter.ExportTorrent(ctx, target.InstanceID, target.Hash)
		if exportErr != nil {
			failures = append(failures, fmt.Sprintf("%s/%s (%s): %v", archiveInstanceName(target), archiveCategoryPath(target.Category), target.Hash, exportErr))
			continue
		}

		filename := torrentname.SanitizeExportFilename(suggestedName, target.Hash, trackerDomain, target.Hash)
		if target.Filename != "" {
			filename = torrentname.SanitizeExportFilename(target.Filename, target.Hash, "", "")
		}
		entryName := path.Join(archiveCategoryPath(target.Category), filename)
		if multipleInstances {
			entryName = path.Join(archiveInstanceName(target), entryName)
		}
		entryName = uniqueArchiveName(entryName, usedNames)
		entry, createErr := archive.Create(entryName)
		if createErr != nil {
			return createErr
		}
		if _, writeErr := entry.Write(data); writeErr != nil {
			return writeErr
		}
	}

	if len(failures) > 0 {
		entry, createErr := archive.Create("export-errors.txt")
		if createErr != nil {
			return createErr
		}
		if _, writeErr := io.WriteString(entry, strings.Join(failures, "\n")+"\n"); writeErr != nil {
			return writeErr
		}
	}

	return nil
}

func hasMultipleArchiveInstances(targets []torrentArchiveTarget) bool {
	if len(targets) < 2 {
		return false
	}

	first := targets[0].InstanceID
	for _, target := range targets[1:] {
		if target.InstanceID != first {
			return true
		}
	}
	return false
}

func archiveInstanceName(target torrentArchiveTarget) string {
	name := strings.TrimSpace(target.InstanceName)
	if name == "" {
		name = "Instance " + strconv.Itoa(target.InstanceID)
	}
	return pathutil.SanitizePathSegment(name)
}

func archiveCategoryPath(category string) string {
	parts := strings.FieldsFunc(category, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	if len(parts) == 0 {
		return "Uncategorized"
	}

	for i := range parts {
		parts[i] = pathutil.SanitizePathSegment(strings.TrimSpace(parts[i]))
	}
	return path.Join(parts...)
}

func uniqueArchiveName(name string, used map[string]struct{}) string {
	key := strings.ToLower(name)
	if _, exists := used[key]; !exists {
		used[key] = struct{}{}
		return name
	}

	extension := path.Ext(name)
	base := strings.TrimSuffix(name, extension)
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, suffix, extension)
		candidateKey := strings.ToLower(candidate)
		if _, exists := used[candidateKey]; !exists {
			used[candidateKey] = struct{}{}
			return candidate
		}
	}
}
