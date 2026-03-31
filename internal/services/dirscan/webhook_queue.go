// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"path/filepath"
	"strings"
)

func mergeWebhookScanRoots(directoryPath, existing, next string) string {
	dir := filepath.Clean(directoryPath)
	current := normalizeQueuedWebhookRoot(dir, existing)
	incoming := normalizeQueuedWebhookRoot(dir, next)

	if current == incoming {
		return current
	}
	if current == dir || incoming == dir {
		return dir
	}

	for candidate := current; candidate != "." && candidate != string(filepath.Separator); candidate = filepath.Dir(candidate) {
		if isPathWithin(candidate, incoming) && isPathWithin(dir, candidate) {
			return candidate
		}
		if candidate == dir {
			return dir
		}
	}

	return dir
}

func normalizeQueuedWebhookRoot(directoryPath, scanRoot string) string {
	if scanRoot == "" {
		return filepath.Clean(directoryPath)
	}
	return filepath.Clean(scanRoot)
}

func isPathWithin(base, target string) bool {
	base = filepath.Clean(base)
	target = filepath.Clean(target)
	if base == target {
		return true
	}

	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}

	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
