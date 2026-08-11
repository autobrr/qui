// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"fmt"
	"slices"
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
	skippedEpisodes int
}

func selectEligibleRootWork(
	scanResult *ScanResult,
	trackedFiles *trackedFilesIndex,
	parser *Parser,
	maxSearcheeAgeDays int,
	now time.Time,
	enabledIndexerIDs map[int]struct{},
	skipIndividualEpisodes bool,
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
			if workItemIsStale(item, selection.cutoff, skipIndividualEpisodes) {
				droppedItems = append(droppedItems, buildWorkItemDropDecision(item, "stale", selection.cutoff, trackedFiles))
				continue
			}
			if !workItemHasPendingFiles(item, trackedFiles, enabledIndexerIDs) {
				droppedItems = append(droppedItems, buildWorkItemDropDecision(item, "all_final", selection.cutoff, trackedFiles))
				continue
			}
			// Checked after stale and all_final so the count reads as searches
			// the option avoided this scan, not as a running total of episodes.
			if skipIndividualEpisodes && item.isEpisode {
				selection.skippedEpisodes++
				droppedItems = append(droppedItems, buildWorkItemDropDecision(item, "individual_episode", selection.cutoff, trackedFiles))
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

		// Skipped episodes are the only signal the skip option leaves behind,
		// so log them even when the root still has eligible work.
		for _, item := range droppedItems {
			if item.reason == "individual_episode" {
				logDroppedWorkItem(l, root, item, selection.cutoff)
			}
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

func workItemHasPendingFiles(item searcheeWorkItem, trackedFiles *trackedFilesIndex, enabledIndexerIDs map[int]struct{}) bool {
	if item.searchee == nil {
		return false
	}

	for _, f := range item.searchee.Files {
		if f == nil {
			continue
		}

		tracked := trackedFileForScannedFile(f, trackedFiles)
		if !isFinalTrackedFile(tracked, enabledIndexerIDs) {
			return true
		}
	}

	return false
}

// isFinalTrackedFile reports whether a tracked file needs no more search work.
// A no_match row stays final only while every enabled indexer was part of the
// search that stamped it. When a new indexer appears, the row is pending again.
// Rows without a recorded search set (legacy rows, or an unknown enabled set)
// keep the old behavior and stay final.
func isFinalTrackedFile(tracked *models.DirScanFile, enabledIndexerIDs map[int]struct{}) bool {
	if tracked == nil || !isFinalFileStatus(tracked.Status) {
		return false
	}
	if tracked.Status != models.DirScanFileStatusNoMatch {
		return true
	}
	if tracked.SearchedIndexerIDs == nil || len(enabledIndexerIDs) == 0 {
		return true
	}
	for id := range enabledIndexerIDs {
		if !slices.Contains(tracked.SearchedIndexerIDs, id) {
			return false
		}
	}
	return true
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
		logDroppedWorkItem(l, root, item, cutoff)
	}
}

func logDroppedWorkItem(l *zerolog.Logger, root *Searchee, item workItemDropDecision, cutoff time.Time) {
	if l == nil || root == nil {
		return
	}

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

func workItemIsStale(item searcheeWorkItem, cutoff time.Time, skipIndividualEpisodes bool) bool {
	if item.searchee == nil || cutoff.IsZero() {
		return false
	}

	contentFiles := filterContentFiles(item.searchee.Files)
	if len(contentFiles) == 0 {
		return false
	}

	// A mixed-age season pack is normally stale: its old episodes were already
	// searched, and a fresh episode gets its own episode search. With individual
	// episodes skipped, the pack is the only search a fresh episode has, so the
	// pack stays eligible as long as its newest episode is fresh.
	if item.tvGroup != nil && len(contentFiles) > 1 && !skipIndividualEpisodes {
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
