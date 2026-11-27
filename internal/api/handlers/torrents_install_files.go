// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"io/fs"
	"os"
	"slices"
)

type installPaths interface {
	GetConfigPath() string
	GetDatabasePath() string
}

func (h *TorrentsHandler) SetInstallPaths(paths installPaths) {
	h.installPaths = paths
}

func (h *TorrentsHandler) isInstallFile(info fs.FileInfo) bool {
	if h.installPaths == nil {
		return false
	}
	database := h.installPaths.GetDatabasePath()
	files := []string{h.installPaths.GetConfigPath(), database, database + "-wal", database + "-shm", database + "-journal"}
	return slices.ContainsFunc(files, func(path string) bool {
		installed, err := os.Stat(path)
		return err == nil && os.SameFile(info, installed)
	})
}
