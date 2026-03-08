// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package linkdir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/autobrr/qui/pkg/fsutil"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/pathutil"
)

func validateInstanceDirName(name string) error {
	switch trimmed := strings.TrimSpace(name); {
	case trimmed == "":
		return errors.New("instance directory name cannot be empty")
	case strings.ContainsAny(trimmed, `/\`):
		return fmt.Errorf("instance directory name %q must not contain path separators", trimmed)
	case trimmed == "." || trimmed == "..":
		return fmt.Errorf("instance directory name %q must not be a traversal segment", trimmed)
	default:
		return nil
	}
}

func groupDestDir(baseDir, groupName, isolationFolder string) string {
	groupDir := filepath.Join(baseDir, pathutil.SanitizePathSegment(groupName))
	if isolationFolder == "" {
		return groupDir
	}
	return filepath.Join(groupDir, isolationFolder)
}

// EffectiveInstanceDirName returns the configured by-instance directory name.
// Falls back to the instance name when no override is set.
func EffectiveInstanceDirName(instanceName, override string) (string, error) {
	name := strings.TrimSpace(override)
	if name == "" {
		name = instanceName
	}
	if err := validateInstanceDirName(name); err != nil {
		return "", err
	}
	return name, nil
}

// FindMatchingBaseDir returns the first configured base dir on the same filesystem as sourcePath.
func FindMatchingBaseDir(configuredDirs, sourcePath string) (string, error) {
	if strings.TrimSpace(configuredDirs) == "" {
		return "", errors.New("base directory not configured")
	}

	dirs := strings.Split(configuredDirs, ",")
	var lastErr error

	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}

		if err := os.MkdirAll(dir, 0o755); err != nil {
			lastErr = fmt.Errorf("failed to create directory %s: %w", dir, err)
			continue
		}

		sameFS, err := fsutil.SameFilesystem(sourcePath, dir)
		if err != nil {
			lastErr = fmt.Errorf("failed to check filesystem for %s: %w", dir, err)
			continue
		}
		if sameFS {
			return dir, nil
		}

		lastErr = fmt.Errorf("directory %s is on a different filesystem", dir)
	}

	if lastErr == nil {
		lastErr = errors.New("no valid base directories configured")
	}
	return "", lastErr
}

// BuildDestDir returns the final hardlink/reflink tree root for the configured preset.
func BuildDestDir(baseDir, preset, groupName, torrentHash, torrentName string, candidateFiles []hardlinktree.TorrentFile) (string, error) {
	needsIsolation := !hardlinktree.HasCommonRootFolder(candidateFiles)
	isolationFolder := ""
	if needsIsolation || preset == "flat" || preset == "" {
		isolationFolder = pathutil.IsolationFolderName(torrentHash, torrentName)
	}

	switch preset {
	case "by-tracker":
		if strings.TrimSpace(groupName) == "" {
			groupName = "Unknown"
		}
		return groupDestDir(baseDir, groupName, isolationFolder), nil
	case "by-instance":
		if err := validateInstanceDirName(groupName); err != nil {
			return "", err
		}
		return groupDestDir(baseDir, groupName, isolationFolder), nil
	default:
		return filepath.Join(baseDir, pathutil.IsolationFolderName(torrentHash, torrentName)), nil
	}
}
