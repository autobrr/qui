// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"strings"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/fsutil"
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
			if strings.HasPrefix(sourcePath, oldPrefix) {
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

// determineLinkMode decides whether to use hardlinks, reflinks, or direct add
func (s *Service) determineLinkMode(
	targetInstance *models.Instance,
	sourcePath, targetPath string,
) string {
	// Check hardlinks first (preferred - instant, no extra space)
	if targetInstance.UseHardlinks {
		sameFS, err := fsutil.SameFilesystem(sourcePath, targetPath)
		if err == nil && sameFS {
			return "hardlink"
		}
		// Different filesystem - fall back to reflink if enabled and fallback allowed
		if targetInstance.UseReflinks && targetInstance.FallbackToRegularMode {
			return "reflink"
		}
	}

	// Check reflinks (copy-on-write)
	if targetInstance.UseReflinks {
		return "reflink"
	}

	// Direct add (no file linking - assumes shared storage or same paths)
	return "direct"
}
