// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"time"

	"github.com/autobrr/qui/internal/models"
)

type rootWorkSelection struct {
	root  *Searchee
	items []searcheeWorkItem
}

type scanWorkSelection struct {
	roots           []rootWorkSelection
	cutoff          time.Time
	discoveredFiles int
	eligibleFiles   int
	skippedFiles    int
}

func selectEligibleRootWork(
	scanResult *ScanResult,
	trackedFiles *trackedFilesIndex,
	parser *Parser,
	maxSearcheeAgeDays int,
	now time.Time,
) scanWorkSelection {
	selection := scanWorkSelection{}
	if scanResult == nil {
		return selection
	}

	if maxSearcheeAgeDays > 0 {
		selection.cutoff = now.AddDate(0, 0, -maxSearcheeAgeDays)
	}

	discoveredPaths := make(map[string]struct{})
	eligiblePaths := make(map[string]struct{})

	for _, root := range scanResult.Searchees {
		if root == nil {
			continue
		}

		for _, f := range root.Files {
			if f == nil {
				continue
			}
			discoveredPaths[f.Path] = struct{}{}
		}

		items := buildSearcheeWorkItems(root, parser)
		pendingItems := make([]searcheeWorkItem, 0, len(items))
		for _, item := range items {
			if item.searchee == nil || workItemIsStale(item, selection.cutoff) {
				continue
			}
			if !workItemHasPendingFiles(item, trackedFiles) {
				continue
			}
			pendingItems = append(pendingItems, item)
			for _, f := range item.searchee.Files {
				if f == nil {
					continue
				}
				eligiblePaths[f.Path] = struct{}{}
			}
		}

		if len(pendingItems) == 0 {
			continue
		}

		selection.roots = append(selection.roots, rootWorkSelection{
			root:  root,
			items: pendingItems,
		})
	}

	selection.discoveredFiles = len(discoveredPaths)
	selection.eligibleFiles = len(eligiblePaths)
	selection.skippedFiles = max(selection.discoveredFiles-selection.eligibleFiles, 0)

	return selection
}

func workItemHasPendingFiles(item searcheeWorkItem, trackedFiles *trackedFilesIndex) bool {
	if item.searchee == nil {
		return false
	}

	for _, f := range item.searchee.Files {
		if f == nil {
			continue
		}

		tracked := trackedFileForScannedFile(f, trackedFiles)
		if tracked == nil || !isFinalFileStatus(tracked.Status) {
			return true
		}
	}

	return false
}

func trackedFileForScannedFile(f *ScannedFile, trackedFiles *trackedFilesIndex) *models.DirScanFile {
	if f == nil || trackedFiles == nil {
		return nil
	}

	if tracked := trackedFiles.byPath[f.Path]; tracked != nil {
		return tracked
	}
	if !f.FileID.IsZero() {
		if tracked := trackedFiles.byFileID[string(f.FileID.Bytes())]; tracked != nil {
			return tracked
		}
	}
	return nil
}

func workItemIsStale(item searcheeWorkItem, cutoff time.Time) bool {
	if item.searchee == nil || cutoff.IsZero() {
		return false
	}

	contentFiles := filterContentFiles(item.searchee.Files)
	if len(contentFiles) == 0 {
		return false
	}

	if item.tvGroup != nil && len(contentFiles) > 1 {
		for _, f := range contentFiles {
			if f == nil {
				continue
			}
			if f.ModTime.Before(cutoff) {
				return true
			}
		}
		return false
	}

	var newest time.Time
	for _, f := range contentFiles {
		if f == nil {
			continue
		}
		if f.ModTime.After(newest) {
			newest = f.ModTime
		}
	}
	if newest.IsZero() {
		return false
	}
	return newest.Before(cutoff)
}
