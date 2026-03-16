// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
)

type managedDeleteCleanupTarget struct {
	dir     string
	baseDir string
}

const (
	managedDeleteCleanupRetryInterval = 50 * time.Millisecond
	managedDeleteCleanupRetryAttempts = 20
)

func buildManagedDeleteCleanupTargets(configuredBaseDirs string, torrents []qbt.Torrent) []managedDeleteCleanupTarget {
	baseDirs := parseManagedDeleteBaseDirs(configuredBaseDirs)
	if len(baseDirs) == 0 || len(torrents) == 0 {
		return nil
	}

	targets := make([]managedDeleteCleanupTarget, 0, len(torrents))
	seen := make(map[string]struct{}, len(torrents))

	for _, torrent := range torrents {
		dir, baseDir, ok := managedDeleteCleanupDir(baseDirs, torrent)
		if !ok {
			continue
		}

		key := baseDir + "\x00" + dir
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		targets = append(targets, managedDeleteCleanupTarget{
			dir:     dir,
			baseDir: baseDir,
		})
	}

	return targets
}

func cleanupManagedDeleteTargets(targets []managedDeleteCleanupTarget) {
	for _, target := range targets {
		pruneEmptyManagedDeleteDir(target)
	}
}

func managedDeleteCleanupDir(baseDirs []string, torrent qbt.Torrent) (string, string, bool) {
	contentPath := filepath.Clean(torrent.ContentPath)
	savePath := filepath.Clean(torrent.SavePath)

	if info, err := os.Stat(contentPath); err == nil {
		baseDir, ok := matchManagedDeleteBaseDir(baseDirs, contentPath)
		if ok {
			if info.IsDir() {
				return contentPath, baseDir, true
			}
			return filepath.Dir(contentPath), baseDir, true
		}
	}

	if baseDir, ok := matchManagedDeleteBaseDir(baseDirs, savePath); ok {
		return savePath, baseDir, true
	}

	return "", "", false
}

func parseManagedDeleteBaseDirs(configuredBaseDirs string) []string {
	parts := strings.Split(configuredBaseDirs, ",")
	baseDirs := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		baseDirs = append(baseDirs, filepath.Clean(part))
	}

	return baseDirs
}

func matchManagedDeleteBaseDir(baseDirs []string, path string) (string, bool) {
	path = filepath.Clean(path)
	if path == "" || path == "." {
		return "", false
	}

	bestMatch := ""
	for _, baseDir := range baseDirs {
		if isManagedDeletePathInsideBase(path, baseDir) && len(baseDir) > len(bestMatch) {
			bestMatch = baseDir
		}
	}

	if bestMatch == "" {
		return "", false
	}

	return bestMatch, true
}

func isManagedDeletePathInsideBase(path, baseDir string) bool {
	if path == "" || baseDir == "" {
		return false
	}

	rel, err := filepath.Rel(baseDir, path)
	if err != nil {
		return false
	}

	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func pruneEmptyManagedDeleteDir(target managedDeleteCleanupTarget) {
	for range managedDeleteCleanupRetryAttempts {
		retry := pruneEmptyManagedDeleteDirOnce(target)
		if !retry {
			return
		}

		time.Sleep(managedDeleteCleanupRetryInterval)
	}
}

func pruneEmptyManagedDeleteDirOnce(target managedDeleteCleanupTarget) bool {
	dir := filepath.Clean(target.dir)
	baseDir := filepath.Clean(target.baseDir)
	leafDir := dir

	for isManagedDeletePathInsideBase(dir, baseDir) && dir != baseDir {
		err := os.Remove(dir)
		switch {
		case err == nil, os.IsNotExist(err):
		case isDirNotEmpty(err):
			return dir == leafDir
		default:
			log.Debug().Err(err).Str("dir", dir).Str("baseDir", baseDir).
				Msg("delete cleanup: failed to prune empty managed directory")
			return false
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}

	return false
}

func isDirNotEmpty(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, syscall.ENOTEMPTY) {
		return true
	}

	return strings.Contains(strings.ToLower(err.Error()), "not empty")
}
