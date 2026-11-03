// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"strings"

	"github.com/autobrr/qui/internal/fsops"
)

// PathWithinDirectory reports whether target is at or under dirPath, reading both
// in d, the grammar of the directory's backend, and returns them cleaned.
func PathWithinDirectory(d fsops.PathDialect, dirPath, target string) (cleanDir, cleanTarget string, ok bool) {
	cleanDir, cleanTarget = d.Clean(dirPath), d.Clean(target)
	return cleanDir, cleanTarget, isPathWithin(d, cleanDir, cleanTarget)
}

func mergeWebhookScanRoots(d fsops.PathDialect, directoryPath, existing, next string) string {
	dir := d.Clean(directoryPath)
	current := normalizeQueuedWebhookRoot(d, dir, existing)
	incoming := normalizeQueuedWebhookRoot(d, dir, next)

	if current == incoming {
		return current
	}
	if current == dir || incoming == dir {
		return dir
	}

	// A queued root can sit outside dir after dir's path was edited, and
	// d.Dir stops moving at a root, which on Windows is not one fixed spelling.
	for candidate := current; ; {
		if isPathWithin(d, candidate, dir) {
			return dir
		}
		if isPathWithin(d, candidate, incoming) && isPathWithin(d, dir, candidate) {
			return candidate
		}
		parent := d.Dir(candidate)
		if parent == candidate {
			return dir
		}
		candidate = parent
	}
}

func normalizeQueuedWebhookRoot(d fsops.PathDialect, directoryPath, scanRoot string) string {
	if scanRoot == "" {
		return d.Clean(directoryPath)
	}
	return d.Clean(scanRoot)
}

func isPathWithin(d fsops.PathDialect, base, target string) bool {
	rel, err := d.Rel(base, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+d.Separator())
}
