// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog"

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
	l *zerolog.Logger,
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
		droppedItems := make([]workItemDropDecision, 0, len(items))
		for _, item := range items {
			if item.searchee == nil {
				continue
			}
			if workItemIsStale(item, selection.cutoff) {
				droppedItems = append(droppedItems, buildWorkItemDropDecision(item, "stale", selection.cutoff, trackedFiles))
				continue
			}
			if !workItemHasPendingFiles(item, trackedFiles) {
				droppedItems = append(droppedItems, buildWorkItemDropDecision(item, "all_final", selection.cutoff, trackedFiles))
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
			logRootSelectionDrops(l, root, droppedItems, selection.cutoff)
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

type workItemDropDecision struct {
	name             string
	path             string
	reason           string
	contentFiles     int
	newestContentMod time.Time
	statuses         string
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

func buildWorkItemDropDecision(
	item searcheeWorkItem,
	reason string,
	cutoff time.Time,
	trackedFiles *trackedFilesIndex,
) workItemDropDecision {
	decision := workItemDropDecision{reason: reason}
	if item.searchee == nil {
		return decision
	}

	contentFiles := filterContentFiles(item.searchee.Files)
	decision.name = item.searchee.Name
	decision.path = item.searchee.Path
	decision.contentFiles = len(contentFiles)
	decision.newestContentMod = newestContentModTime(contentFiles)
	decision.statuses = summarizeTrackedStatuses(item, trackedFiles)

	if decision.reason == "stale" && !cutoff.IsZero() && decision.newestContentMod.IsZero() {
		decision.statuses = "no_content_files"
	}

	return decision
}

func newestContentModTime(files []*ScannedFile) time.Time {
	var newest time.Time
	for _, f := range files {
		if f == nil {
			continue
		}
		if f.ModTime.After(newest) {
			newest = f.ModTime
		}
	}
	return newest
}

func summarizeTrackedStatuses(item searcheeWorkItem, trackedFiles *trackedFilesIndex) string {
	if item.searchee == nil {
		return ""
	}

	counts := make(map[string]int)
	for _, f := range item.searchee.Files {
		if f == nil {
			continue
		}

		status := "untracked"
		if tracked := trackedFileForScannedFile(f, trackedFiles); tracked != nil {
			status = string(tracked.Status)
		}
		counts[status]++
	}

	if len(counts) == 0 {
		return ""
	}

	keys := make([]string, 0, len(counts))
	for status := range counts {
		keys = append(keys, status)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, status := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", status, counts[status]))
	}

	return strings.Join(parts, ", ")
}

func logRootSelectionDrops(l *zerolog.Logger, root *Searchee, droppedItems []workItemDropDecision, cutoff time.Time) {
	if l == nil || root == nil || len(droppedItems) == 0 {
		return
	}

	event := l.Debug().
		Str("rootName", root.Name).
		Str("rootPath", root.Path).
		Int("rootFiles", len(root.Files)).
		Int("droppedItems", len(droppedItems))
	if !cutoff.IsZero() {
		event = event.Time("cutoff", cutoff)
	}
	event.Msg("dirscan: no eligible work items for root")

	for _, item := range droppedItems {
		event := l.Debug().
			Str("rootPath", root.Path).
			Str("itemName", item.name).
			Str("itemPath", item.path).
			Str("reason", item.reason).
			Int("contentFiles", item.contentFiles).
			Str("statuses", item.statuses)
		if !item.newestContentMod.IsZero() {
			event = event.Time("newestContentMod", item.newestContentMod)
		}
		if !cutoff.IsZero() {
			event = event.Time("cutoff", cutoff)
		}
		event.Msg("dirscan: dropped work item")
	}
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
