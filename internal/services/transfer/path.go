// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"strings"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/fsutil"
	"github.com/autobrr/qui/pkg/reflinktree"
)

// computeTargetPath determines where files should be placed on target
func (s *Service) computeTargetPath(
	sourcePath string,
	targetInstance *models.Instance,
	mappings map[string]string,
) string {
	// 1. Check explicit path mappings first (longest prefix match)
	if len(mappings) > 0 {
		var bestMatch string
		var bestReplacement string
		for oldPrefix, newPrefix := range mappings {
			// Require exact match or path separator after prefix to avoid partial name matches
			// e.g., "/data" should not match "/data2", but should match "/data" and "/data/foo"
			if sourcePath == oldPrefix || strings.HasPrefix(sourcePath, oldPrefix) {
				// Ensure prefix ends on a path boundary when prefix has no trailing separator
				if len(sourcePath) > len(oldPrefix) &&
					!strings.HasSuffix(oldPrefix, "/") && !strings.HasSuffix(oldPrefix, "\\") {
					next := sourcePath[len(oldPrefix)]
					if next != '/' && next != '\\' {
						continue
					}
				}
				if len(oldPrefix) > len(bestMatch) {
					bestMatch = oldPrefix
					bestReplacement = newPrefix
				}
			}
		}
		if bestMatch != "" {
			return strings.Replace(sourcePath, bestMatch, bestReplacement, 1)
		}
	}

	// 2. Use target instance's HardlinkBaseDir if configured
	if targetInstance.HardlinkBaseDir != "" {
		return targetInstance.HardlinkBaseDir
	}

	// 3. Same path (same server, shared storage)
	return sourcePath
}

// determineLinkMode decides whether to use hardlinks, reflinks, or direct (usage/symlink/copy)
func (s *Service) determineLinkMode(
	targetInstance *models.Instance,
	sourcePath, targetPath string,
) (string, error) {
	// Check hardlinks first (preferred - instant, no extra space)
	if targetInstance.UseHardlinks {
		sameFS, err := fsutil.SameFilesystem(sourcePath, targetPath)
		if err == nil && sameFS {
			return "hardlink", nil
		} else if !targetInstance.FallbackToRegularMode {
			return "", ErrNoLinkModeAvailable
		}
	} else if targetInstance.UseReflinks {
		// Reflinks require same filesystem
		if sameFS, err := fsutil.SameFilesystem(sourcePath, targetPath); err == nil && sameFS {
			if supported, _ := reflinktree.SupportsReflink(targetPath); supported {
				return "reflink", nil
			}
		}
		if !targetInstance.FallbackToRegularMode {
			return "", ErrNoLinkModeAvailable
		}
	}

	// Plain old direct usage / copy
	return "direct", nil
}
