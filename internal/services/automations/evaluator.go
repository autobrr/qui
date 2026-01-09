// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
)

const maxConditionDepth = 20

// minContainsNameLength is the minimum name length for CONTAINS_IN matching
// to avoid surprising matches on short names.
const minContainsNameLength = 10

// categoryEntry stores torrent info for category-based lookups.
type categoryEntry struct {
	Hash           string // torrent hash for self-exclusion
	Name           string // lowercased name (for EXISTS_IN exact match)
	NormalizedName string // normalized name for CONTAINS_IN (separators → space)
}

// EvalContext provides additional context for condition evaluation.
type EvalContext struct {
	// UnregisteredSet contains hashes of unregistered torrents (from SyncManager health counts)
	UnregisteredSet map[string]struct{}
	// TrackerDownSet contains hashes of torrents whose trackers are down (from SyncManager health counts)
	TrackerDownSet map[string]struct{}
	// HardlinkScopeByHash maps torrent hash to its hardlink scope (none, torrents_only, outside_qbittorrent)
	HardlinkScopeByHash map[string]string
	// InstanceHasLocalAccess indicates whether the instance has local filesystem access
	InstanceHasLocalAccess bool
	// FreeSpace is the free space on the instance's filesystem
	FreeSpace int64
	// SpaceToClear is the amount of disk space that will be cleared by the "free space" condition
	SpaceToClear int64
	// FilesToClear is a map of cross-seed keys to the amount of disk space that will be cleared by the "free space" condition, ensuring we don't double count cross-seeds
	FilesToClear map[crossSeedKey]struct{}
	// HardlinkSignatureByHash maps torrent hash to its hardlink signature (sorted file IDs joined with ";").
	// Only populated when includeHardlinks is enabled for FREE_SPACE rules.
	HardlinkSignatureByHash map[string]string
	// HardlinkSignaturesToClear tracks hardlink signatures already counted in space projection.
	// Torrents with the same signature share physical files and should only be counted once.
	HardlinkSignaturesToClear map[string]struct{}

	// CategoryIndex maps lowercased category → lowercased name → set of hashes.
	// Enables O(1) EXISTS_IN lookups while supporting self-exclusion.
	CategoryIndex map[string]map[string]map[string]struct{}

	// CategoryNames maps lowercased category → slice of categoryEntry.
	// Used for CONTAINS_IN iteration (stores pre-normalized names).
	CategoryNames map[string][]categoryEntry

	// NowUnix is the current Unix timestamp, used for age field evaluation.
	// If zero, time.Now().Unix() is used. Set this for deterministic tests.
	NowUnix int64

	// TrackerDisplayNameByDomain maps lowercase tracker domains to their display names.
	// Used for UseTrackerAsTag with UseDisplayName option.
	TrackerDisplayNameByDomain map[string]string

	// ContentGroupByHash maps torrent hash to its content group (all torrents sharing same ContentPath).
	// Used for SAME_CONTENT_COUNT, UNREGISTERED_SAME_CONTENT_COUNT, REGISTERED_SAME_CONTENT_COUNT conditions.
	ContentGroupByHash map[string][]string

	// BasenameGroupByHash maps torrent hash to its basename group (all torrents sharing same content folder/file name).
	// Used when IncludeCrossSeeds is enabled for *_SAME_CONTENT_COUNT fields.
	// Groups torrents like "D:\Movies\SomeMovie" and "E:\Downloads\SomeMovie" together.
	BasenameGroupByHash map[string][]string
}

// separatorReplacer replaces common torrent name separators with spaces.
var separatorReplacer = strings.NewReplacer(".", " ", "_", " ", "-", " ")

// whitespaceCollapser collapses multiple spaces into one.
var whitespaceCollapser = regexp.MustCompile(`\s+`)

// normalizeName normalizes a torrent name for CONTAINS_IN comparison:
// lowercase + replace . _ - with space + collapse whitespace.
func normalizeName(s string) string {
	s = strings.ToLower(s)
	s = separatorReplacer.Replace(s)
	s = whitespaceCollapser.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// BuildCategoryIndex builds the category lookup structures from a list of torrents.
// Returns both the CategoryIndex (for O(1) EXISTS_IN) and CategoryNames (for CONTAINS_IN iteration).
func BuildCategoryIndex(torrents []qbt.Torrent) (map[string]map[string]map[string]struct{}, map[string][]categoryEntry) {
	categoryIndex := make(map[string]map[string]map[string]struct{})
	categoryNames := make(map[string][]categoryEntry)

	for _, t := range torrents {
		// Use lowercased + trimmed category as key (empty string is valid for uncategorized)
		catKey := strings.ToLower(strings.TrimSpace(t.Category))
		nameLower := strings.ToLower(t.Name)

		// Build CategoryIndex for O(1) EXISTS_IN lookup
		if categoryIndex[catKey] == nil {
			categoryIndex[catKey] = make(map[string]map[string]struct{})
		}
		if categoryIndex[catKey][nameLower] == nil {
			categoryIndex[catKey][nameLower] = make(map[string]struct{})
		}
		categoryIndex[catKey][nameLower][t.Hash] = struct{}{}

		// Build CategoryNames for CONTAINS_IN iteration
		categoryNames[catKey] = append(categoryNames[catKey], categoryEntry{
			Hash:           t.Hash,
			Name:           nameLower,
			NormalizedName: normalizeName(t.Name),
		})
	}

	return categoryIndex, categoryNames
}

// BuildContentGroupIndex builds a map of torrent hash → list of all hashes sharing the same ContentPath.
// This enables SAME_CONTENT_COUNT, UNREGISTERED_SAME_CONTENT_COUNT, and REGISTERED_SAME_CONTENT_COUNT conditions.
// Uses normalized ContentPath for case-insensitive, platform-agnostic comparison.
func BuildContentGroupIndex(torrents []qbt.Torrent) map[string][]string {
	if len(torrents) == 0 {
		return nil
	}

	// First pass: group torrents by normalized ContentPath
	byContentPath := make(map[string][]string) // normalized path → list of hashes
	for _, t := range torrents {
		normalizedPath := normalizeContentPath(t.ContentPath)
		if normalizedPath == "" {
			continue // Skip torrents without ContentPath
		}
		byContentPath[normalizedPath] = append(byContentPath[normalizedPath], t.Hash)
	}

	// Second pass: build hash → group mapping (only for groups with 2+ torrents)
	result := make(map[string][]string)
	for _, hashes := range byContentPath {
		if len(hashes) < 2 {
			continue // No cross-seeds, skip single-torrent groups
		}
		// All torrents in this group share the same content
		for _, h := range hashes {
			result[h] = hashes
		}
	}

	return result
}

// normalizeContentPath standardizes a content path for comparison.
// Lowercases, normalizes path separators, and removes trailing slashes.
func normalizeContentPath(p string) string {
	if p == "" {
		return ""
	}
	p = strings.ToLower(p)
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimSuffix(p, "/")
	return p
}

// getContentBasename extracts just the folder/file name from a content path.
// For "D:\Movies\Some.Movie.2024" returns "some.movie.2024" (lowercased).
// This allows matching cross-seeds that have the same content but different parent directories.
func getContentBasename(contentPath string) string {
	normalized := normalizeContentPath(contentPath)
	if normalized == "" {
		return ""
	}
	// Find the last path component
	if idx := strings.LastIndex(normalized, "/"); idx >= 0 {
		return normalized[idx+1:]
	}
	return normalized
}

// BuildBasenameGroupIndex builds a map of torrent hash → list of all hashes sharing the same content basename.
// This groups cross-seeds that have the same folder/file name but different full paths.
// For example, "D:\Movies\SomeMovie" and "E:\Downloads\SomeMovie" would be grouped together.
func BuildBasenameGroupIndex(torrents []qbt.Torrent) map[string][]string {
	if len(torrents) == 0 {
		return nil
	}

	// First pass: group torrents by content basename
	byBasename := make(map[string][]string) // basename → list of hashes
	for _, t := range torrents {
		basename := getContentBasename(t.ContentPath)
		if basename == "" {
			continue // Skip torrents without ContentPath
		}
		byBasename[basename] = append(byBasename[basename], t.Hash)
	}

	// Debug: log groups with many members
	for basename, hashes := range byBasename {
		if len(hashes) >= 5 {
			log.Trace().
				Str("basename", basename).
				Int("count", len(hashes)).
				Msg("automations: basename group with 5+ members")
		}
	}

	// Second pass: build hash → group mapping (only for groups with 2+ torrents)
	result := make(map[string][]string)
	for _, hashes := range byBasename {
		if len(hashes) < 2 {
			continue // No cross-seeds, skip single-torrent groups
		}
		// All torrents in this group share the same content basename
		for _, h := range hashes {
			result[h] = hashes
		}
	}

	return result
}

// existsInCategory checks if a different torrent with the exact same name exists in the target category.
func existsInCategory(torrentHash, torrentName, targetCategory string, ctx *EvalContext) bool {
	if ctx == nil || ctx.CategoryIndex == nil {
		return false
	}

	// Normalize inputs
	catKey := strings.ToLower(strings.TrimSpace(targetCategory))
	// Treat all-whitespace as "no match" (but empty string is valid for uncategorized)
	if targetCategory != "" && catKey == "" {
		return false
	}
	nameLower := strings.ToLower(torrentName)

	// Lookup category
	nameMap, ok := ctx.CategoryIndex[catKey]
	if !ok {
		return false
	}

	// Lookup name
	hashSet, ok := nameMap[nameLower]
	if !ok {
		return false
	}

	// Check if any hash in the set is different from the current torrent
	for hash := range hashSet {
		if hash != torrentHash {
			return true
		}
	}
	return false
}

// containsInCategory checks if a different torrent with a similar name exists in the target category.
// Uses bidirectional contains matching with normalization.
func containsInCategory(torrentHash, torrentName, targetCategory string, ctx *EvalContext) bool {
	if ctx == nil || ctx.CategoryNames == nil {
		return false
	}

	// Normalize inputs
	catKey := strings.ToLower(strings.TrimSpace(targetCategory))
	// Treat all-whitespace as "no match" (but empty string is valid for uncategorized)
	if targetCategory != "" && catKey == "" {
		return false
	}

	// Skip if current torrent name is too short
	normalizedCurrent := normalizeName(torrentName)
	if len(normalizedCurrent) < minContainsNameLength {
		return false
	}

	// Lookup category entries
	entries, ok := ctx.CategoryNames[catKey]
	if !ok {
		return false
	}

	// Check each entry for bidirectional contains match
	for _, entry := range entries {
		// Skip self
		if entry.Hash == torrentHash {
			continue
		}
		// Skip entries with short normalized names
		if len(entry.NormalizedName) < minContainsNameLength {
			continue
		}
		// Bidirectional contains: either contains the other
		if strings.Contains(normalizedCurrent, entry.NormalizedName) ||
			strings.Contains(entry.NormalizedName, normalizedCurrent) {
			return true
		}
	}
	return false
}

// getContentGroup returns the appropriate content group for a torrent.
// When includeCrossSeeds is true and BasenameGroupByHash is available, uses basename matching
// (matches torrents with the same folder/file name regardless of parent directory).
// Otherwise uses ContentPath matching (exact path match).
func getContentGroup(hash string, includeCrossSeeds bool, ctx *EvalContext) []string {
	if ctx == nil {
		return nil
	}
	// When includeCrossSeeds is enabled, prefer basename matching (broader match)
	if includeCrossSeeds && ctx.BasenameGroupByHash != nil {
		if group := ctx.BasenameGroupByHash[hash]; len(group) > 0 {
			return group
		}
	}
	// Fall back to ContentPath matching (exact match)
	if ctx.ContentGroupByHash != nil {
		return ctx.ContentGroupByHash[hash]
	}
	return nil
}

// getSameContentCount returns the total count of torrents sharing the same ContentPath (including self).
// When includeCrossSeeds is true, also matches by content basename (folder/file name).
// Returns 1 if no content group data is available (torrent only counts itself).
func getSameContentCount(hash string, includeCrossSeeds bool, ctx *EvalContext) int {
	group := getContentGroup(hash, includeCrossSeeds, ctx)
	if len(group) == 0 {
		return 1 // Not in any group, count only self
	}
	return len(group)
}

// getUnregisteredSameContentCount returns the count of OTHER unregistered torrents sharing the same ContentPath.
// When includeCrossSeeds is true, also matches by content basename (folder/file name).
// Excludes the current torrent from the count.
func getUnregisteredSameContentCount(hash string, includeCrossSeeds bool, ctx *EvalContext) int {
	group := getContentGroup(hash, includeCrossSeeds, ctx)
	if len(group) == 0 {
		return 0 // Not in any group
	}
	count := 0
	for _, h := range group {
		if h == hash {
			continue // Skip self
		}
		if ctx != nil && ctx.UnregisteredSet != nil {
			if _, isUnreg := ctx.UnregisteredSet[h]; isUnreg {
				count++
			}
		}
	}
	return count
}

// getRegisteredSameContentCount returns the count of OTHER registered torrents sharing the same ContentPath.
// When includeCrossSeeds is true, also matches by content basename (folder/file name).
// Excludes the current torrent from the count.
func getRegisteredSameContentCount(hash string, includeCrossSeeds bool, ctx *EvalContext) int {
	group := getContentGroup(hash, includeCrossSeeds, ctx)
	if len(group) == 0 {
		return 0 // Not in any group
	}
	count := 0
	for _, h := range group {
		if h == hash {
			continue // Skip self
		}
		// Registered = not in UnregisteredSet
		if ctx == nil || ctx.UnregisteredSet == nil {
			count++ // If no unregistered data, assume all are registered
		} else if _, isUnreg := ctx.UnregisteredSet[h]; !isUnreg {
			count++
		}
	}
	return count
}

// ConditionUsesField checks if a condition tree references a specific field.
func ConditionUsesField(cond *RuleCondition, field ConditionField) bool {
	if cond == nil {
		return false
	}
	if cond.Field == field {
		return true
	}
	for _, child := range cond.Conditions {
		if ConditionUsesField(child, field) {
			return true
		}
	}
	return false
}

// ConditionUsesIncludeCrossSeeds checks if any condition in the tree uses IncludeCrossSeeds.
func ConditionUsesIncludeCrossSeeds(cond *RuleCondition) bool {
	if cond == nil {
		return false
	}
	if cond.IncludeCrossSeeds {
		return true
	}
	for _, child := range cond.Conditions {
		if ConditionUsesIncludeCrossSeeds(child) {
			return true
		}
	}
	return false
}

// EvaluateCondition recursively evaluates a condition against a torrent.
// Returns true if the torrent matches the condition.
// For conditions that require additional context (like isUnregistered), use EvaluateConditionWithContext.
func EvaluateCondition(cond *RuleCondition, torrent qbt.Torrent, depth int) bool {
	return EvaluateConditionWithContext(cond, torrent, nil, depth)
}

// EvaluateConditionWithContext recursively evaluates a condition against a torrent with optional context.
// Returns true if the torrent matches the condition.
func EvaluateConditionWithContext(cond *RuleCondition, torrent qbt.Torrent, ctx *EvalContext, depth int) bool {
	if cond == nil || depth > maxConditionDepth {
		return false
	}

	// Compile regex if needed, but skip for EXISTS_IN/CONTAINS_IN operators
	// (cond.Value is a category name, not a pattern)
	if cond.Operator != OperatorExistsIn && cond.Operator != OperatorContainsIn {
		if cond.Regex || cond.Operator == OperatorMatches {
			if err := cond.CompileRegex(); err != nil {
				log.Debug().
					Err(err).
					Str("field", string(cond.Field)).
					Str("pattern", cond.Value).
					Msg("automations: regex compilation failed")
				return false
			}
		}
	}

	var result bool

	// Handle logical operators (AND/OR) with child conditions
	if cond.IsGroup() {
		switch cond.Operator {
		case OperatorOr:
			// OR: at least one child must match
			result = false
			for _, child := range cond.Conditions {
				if EvaluateConditionWithContext(child, torrent, ctx, depth+1) {
					result = true
					break
				}
			}
		case OperatorAnd:
			// AND: all children must match
			result = true
			for _, child := range cond.Conditions {
				if !EvaluateConditionWithContext(child, torrent, ctx, depth+1) {
					result = false
					break
				}
			}
		}
	} else {
		// Leaf condition: evaluate against the torrent
		result = evaluateLeaf(cond, torrent, ctx)
	}

	// Apply negation if specified
	if cond.Negate {
		result = !result
	}

	return result
}

// evaluateLeaf evaluates a leaf condition (not a group) against a torrent.
func evaluateLeaf(cond *RuleCondition, torrent qbt.Torrent, ctx *EvalContext) bool {
	switch cond.Field {
	// String fields
	case FieldName:
		// EXISTS_IN/CONTAINS_IN are special operators for cross-category lookups
		if cond.Operator == OperatorExistsIn {
			return existsInCategory(torrent.Hash, torrent.Name, cond.Value, ctx)
		}
		if cond.Operator == OperatorContainsIn {
			return containsInCategory(torrent.Hash, torrent.Name, cond.Value, ctx)
		}
		return compareString(torrent.Name, cond)
	case FieldHash:
		return compareString(torrent.Hash, cond)
	case FieldCategory:
		return compareString(torrent.Category, cond)
	case FieldTags:
		return compareTags(torrent.Tags, cond)
	case FieldSavePath:
		return compareString(torrent.SavePath, cond)
	case FieldContentPath:
		return compareString(torrent.ContentPath, cond)
	case FieldState:
		return compareState(torrent, cond, ctx)
	case FieldTracker:
		return compareString(torrent.Tracker, cond)
	case FieldComment:
		return compareString(torrent.Comment, cond)

	// Bytes fields (int64)
	case FieldSize:
		return compareInt64(torrent.Size, cond)
	case FieldTotalSize:
		return compareInt64(torrent.TotalSize, cond)
	case FieldDownloaded:
		return compareInt64(torrent.Downloaded, cond)
	case FieldUploaded:
		return compareInt64(torrent.Uploaded, cond)
	case FieldAmountLeft:
		return compareInt64(torrent.AmountLeft, cond)
	case FieldFreeSpace:
		if ctx == nil {
			return false
		}
		return compareInt64(ctx.FreeSpace+ctx.SpaceToClear, cond)

	// Timestamp/duration fields (int64)
	case FieldAddedOn:
		return compareInt64(torrent.AddedOn, cond)
	case FieldCompletionOn:
		return compareInt64(torrent.CompletionOn, cond)
	case FieldLastActivity:
		return compareInt64(torrent.LastActivity, cond)
	case FieldSeedingTime:
		return compareInt64(torrent.SeedingTime, cond)
	case FieldTimeActive:
		return compareInt64(torrent.TimeActive, cond)

	// Age fields (time since timestamp)
	case FieldAddedOnAge:
		return compareAge(torrent.AddedOn, cond, ctx)
	case FieldCompletionOnAge:
		// If completion_on is 0 (never completed), don't match
		if torrent.CompletionOn == 0 {
			return false
		}
		return compareAge(torrent.CompletionOn, cond, ctx)
	case FieldLastActivityAge:
		// If last_activity is 0 (never had activity), don't match
		if torrent.LastActivity == 0 {
			return false
		}
		return compareAge(torrent.LastActivity, cond, ctx)

	// Float64 fields
	case FieldRatio:
		return compareFloat64(torrent.Ratio, cond)
	case FieldProgress:
		return compareFloat64(torrent.Progress, cond)
	case FieldAvailability:
		return compareFloat64(torrent.Availability, cond)

	// Speed fields (int64)
	case FieldDlSpeed:
		return compareInt64(torrent.DlSpeed, cond)
	case FieldUpSpeed:
		return compareInt64(torrent.UpSpeed, cond)

	// Count fields (int64)
	case FieldNumSeeds:
		return compareInt64(torrent.NumSeeds, cond)
	case FieldNumLeechs:
		return compareInt64(torrent.NumLeechs, cond)
	case FieldNumComplete:
		return compareInt64(torrent.NumComplete, cond)
	case FieldNumIncomplete:
		return compareInt64(torrent.NumIncomplete, cond)
	case FieldTrackersCount:
		return compareInt64(torrent.TrackersCount, cond)

	// Cross-seed count fields
	case FieldSameContentCount:
		count := getSameContentCount(torrent.Hash, cond.IncludeCrossSeeds, ctx)
		return compareInt64(int64(count), cond)
	case FieldUnregisteredSameContentCount:
		count := getUnregisteredSameContentCount(torrent.Hash, cond.IncludeCrossSeeds, ctx)
		// Handle percentage operators
		if isPercentOperator(cond.Operator) {
			total := getSameContentCount(torrent.Hash, cond.IncludeCrossSeeds, ctx)
			return comparePercent(count, total, cond)
		}
		return compareInt64(int64(count), cond)
	case FieldRegisteredSameContentCount:
		count := getRegisteredSameContentCount(torrent.Hash, cond.IncludeCrossSeeds, ctx)
		// Handle percentage operators
		if isPercentOperator(cond.Operator) {
			total := getSameContentCount(torrent.Hash, cond.IncludeCrossSeeds, ctx)
			return comparePercent(count, total, cond)
		}
		return compareInt64(int64(count), cond)

	// Boolean fields
	case FieldPrivate:
		return compareBool(torrent.Private, cond)
	case FieldIsUnregistered:
		isUnregistered := false
		if ctx != nil && ctx.UnregisteredSet != nil {
			_, isUnregistered = ctx.UnregisteredSet[torrent.Hash]
		}
		return compareBool(isUnregistered, cond)

	case FieldHardlinkScope:
		// Instances without local filesystem access cannot detect hardlink scope.
		// Return false so the condition doesn't match and rules won't trigger unintended actions.
		// Note: Automations using HARDLINK_SCOPE are validated at creation time to require local access.
		if ctx == nil || !ctx.InstanceHasLocalAccess {
			return false
		}
		// If scope couldn't be computed for this torrent (files inaccessible, stat failures, etc.),
		// treat as "unknown" and don't match any condition to prevent unintended rule triggers.
		if ctx.HardlinkScopeByHash == nil {
			return false
		}
		scope, ok := ctx.HardlinkScopeByHash[torrent.Hash]
		if !ok {
			return false // Unknown scope - don't match
		}
		return compareHardlinkScope(scope, cond)

	default:
		return false
	}
}

func compareState(torrent qbt.Torrent, cond *RuleCondition, ctx *EvalContext) bool {
	if cond == nil {
		return false
	}

	matches := matchesStateValue(torrent, cond.Value, ctx)
	switch cond.Operator {
	case OperatorEqual:
		return matches
	case OperatorNotEqual:
		return !matches
	default:
		// Preserve legacy behavior for non-state operators (even though the UI only offers EQUAL/NOT_EQUAL).
		return compareString(string(torrent.State), cond)
	}
}

// matchesStateValue matches against the torrent "status" buckets used by the sidebar filters
// (e.g. "errored", "stalled_uploading") with a fallback to exact torrent.State string matching.
func matchesStateValue(torrent qbt.Torrent, value string, ctx *EvalContext) bool {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return false
	}

	switch strings.ToLower(normalized) {
	// Sidebar status buckets
	case "completed":
		return torrent.Progress >= 1.0
	case "downloading":
		return slices.Contains([]qbt.TorrentState{
			qbt.TorrentStateDownloading,
			qbt.TorrentStateStalledDl,
			qbt.TorrentStateMetaDl,
			qbt.TorrentStateQueuedDl,
			qbt.TorrentStateAllocating,
			qbt.TorrentStateCheckingDl,
			qbt.TorrentStateForcedDl,
		}, torrent.State)
	case "uploading", "seeding":
		return slices.Contains([]qbt.TorrentState{
			qbt.TorrentStateUploading,
			qbt.TorrentStateStalledUp,
			qbt.TorrentStateQueuedUp,
			qbt.TorrentStateCheckingUp,
			qbt.TorrentStateForcedUp,
		}, torrent.State)
	case "paused", "stopped":
		return slices.Contains([]qbt.TorrentState{
			qbt.TorrentStatePausedDl,
			qbt.TorrentStatePausedUp,
			qbt.TorrentStateStoppedDl,
			qbt.TorrentStateStoppedUp,
		}, torrent.State)
	case "running", "resumed":
		return !slices.Contains([]qbt.TorrentState{
			qbt.TorrentStatePausedDl,
			qbt.TorrentStatePausedUp,
			qbt.TorrentStateStoppedDl,
			qbt.TorrentStateStoppedUp,
		}, torrent.State)
	case "active":
		return slices.Contains([]qbt.TorrentState{
			qbt.TorrentStateDownloading,
			qbt.TorrentStateUploading,
			qbt.TorrentStateForcedDl,
			qbt.TorrentStateForcedUp,
		}, torrent.State)
	case "inactive":
		return !slices.Contains([]qbt.TorrentState{
			qbt.TorrentStateDownloading,
			qbt.TorrentStateUploading,
			qbt.TorrentStateForcedDl,
			qbt.TorrentStateForcedUp,
		}, torrent.State)
	case "stalled":
		return slices.Contains([]qbt.TorrentState{
			qbt.TorrentStateStalledDl,
			qbt.TorrentStateStalledUp,
		}, torrent.State)
	case "stalled_uploading", "stalled_seeding":
		return torrent.State == qbt.TorrentStateStalledUp
	case "stalled_downloading":
		return torrent.State == qbt.TorrentStateStalledDl
	case "checking":
		return slices.Contains([]qbt.TorrentState{
			qbt.TorrentStateCheckingDl,
			qbt.TorrentStateCheckingUp,
			qbt.TorrentStateCheckingResumeData,
		}, torrent.State)
	case "moving":
		return torrent.State == qbt.TorrentStateMoving
	case "errored", "error":
		return torrent.State == qbt.TorrentStateError || torrent.State == qbt.TorrentStateMissingFiles
	case "missingfiles":
		return torrent.State == qbt.TorrentStateMissingFiles
	case "unregistered":
		if ctx == nil || ctx.UnregisteredSet == nil {
			return false
		}
		_, ok := ctx.UnregisteredSet[torrent.Hash]
		return ok
	case "tracker_down":
		if ctx == nil || ctx.TrackerDownSet == nil {
			return false
		}
		_, ok := ctx.TrackerDownSet[torrent.Hash]
		return ok
	}

	// Fallback to raw torrent state (qBittorrent Web API value, e.g. "stalledUP").
	return strings.EqualFold(string(torrent.State), normalized)
}

// compareString compares a string value against the condition.
func compareString(value string, cond *RuleCondition) bool {
	// Regex matching
	if cond.Regex || cond.Operator == OperatorMatches {
		if cond.Compiled == nil {
			return false
		}
		return cond.Compiled.MatchString(value)
	}

	switch cond.Operator {
	case OperatorEqual:
		return strings.EqualFold(value, cond.Value)
	case OperatorNotEqual:
		return !strings.EqualFold(value, cond.Value)
	case OperatorContains:
		return strings.Contains(strings.ToLower(value), strings.ToLower(cond.Value))
	case OperatorNotContains:
		return !strings.Contains(strings.ToLower(value), strings.ToLower(cond.Value))
	case OperatorStartsWith:
		return strings.HasPrefix(strings.ToLower(value), strings.ToLower(cond.Value))
	case OperatorEndsWith:
		return strings.HasSuffix(strings.ToLower(value), strings.ToLower(cond.Value))
	default:
		return false
	}
}

// compareTags compares tags against the condition, treating tags as a set.
// For string operators, checks individual tags rather than the full comma-separated string.
// Regex matching still operates on the full string for flexibility.
func compareTags(tagsRaw string, cond *RuleCondition) bool {
	// Regex matching operates on full string for flexibility
	if cond.Regex || cond.Operator == OperatorMatches {
		if cond.Compiled == nil {
			return false
		}
		return cond.Compiled.MatchString(tagsRaw)
	}

	tags := splitTags(tagsRaw)
	condValue := strings.ToLower(strings.TrimSpace(cond.Value))

	switch cond.Operator {
	case OperatorEqual:
		return anyTagMatches(tags, condValue, strings.EqualFold)
	case OperatorNotEqual:
		return !anyTagMatches(tags, condValue, strings.EqualFold)
	case OperatorContains:
		return anyTagMatches(tags, condValue, tagContains)
	case OperatorNotContains:
		return !anyTagMatches(tags, condValue, tagContains)
	case OperatorStartsWith:
		return anyTagMatches(tags, condValue, tagStartsWith)
	case OperatorEndsWith:
		return anyTagMatches(tags, condValue, tagEndsWith)
	default:
		return false
	}
}

// anyTagMatches returns true if any tag in the slice satisfies the match function.
func anyTagMatches(tags []string, condValue string, match func(string, string) bool) bool {
	for _, tag := range tags {
		if match(tag, condValue) {
			return true
		}
	}
	return false
}

// tagContains checks if tag contains condValue (case-insensitive).
func tagContains(tag, condValue string) bool {
	return strings.Contains(strings.ToLower(tag), condValue)
}

// tagStartsWith checks if tag starts with condValue (case-insensitive).
func tagStartsWith(tag, condValue string) bool {
	return strings.HasPrefix(strings.ToLower(tag), condValue)
}

// tagEndsWith checks if tag ends with condValue (case-insensitive).
func tagEndsWith(tag, condValue string) bool {
	return strings.HasSuffix(strings.ToLower(tag), condValue)
}

// compareInt64 compares an int64 value against the condition.
func compareInt64(value int64, cond *RuleCondition) bool {
	// Parse the condition value as int64
	condValue, err := strconv.ParseInt(cond.Value, 10, 64)
	if err != nil && cond.Value != "" {
		return false
	}

	switch cond.Operator {
	case OperatorEqual:
		return value == condValue
	case OperatorNotEqual:
		return value != condValue
	case OperatorGreaterThan:
		return value > condValue
	case OperatorGreaterThanOrEqual:
		return value >= condValue
	case OperatorLessThan:
		return value < condValue
	case OperatorLessThanOrEqual:
		return value <= condValue
	case OperatorBetween:
		if cond.MinValue == nil || cond.MaxValue == nil {
			return false
		}
		return float64(value) >= *cond.MinValue && float64(value) <= *cond.MaxValue
	default:
		return false
	}
}

// isPercentOperator returns true if the operator is a percentage-based operator.
func isPercentOperator(op ConditionOperator) bool {
	switch op {
	case OperatorGreaterThanPercent, OperatorGreaterThanOrEqualPercent,
		OperatorLessThanPercent, OperatorLessThanOrEqualPercent:
		return true
	default:
		return false
	}
}

// comparePercent compares a count as a percentage of total against the condition value.
// The condition value should be a percentage (0-100).
// For example, if count=3 and total=4, the percentage is 75%.
func comparePercent(count, total int, cond *RuleCondition) bool {
	if total == 0 {
		return false // Avoid division by zero
	}

	// Parse the condition value as percentage (0-100)
	condPercent, err := strconv.ParseFloat(cond.Value, 64)
	if err != nil {
		return false
	}

	// Calculate actual percentage
	actualPercent := (float64(count) / float64(total)) * 100

	switch cond.Operator {
	case OperatorGreaterThanPercent:
		return actualPercent > condPercent
	case OperatorGreaterThanOrEqualPercent:
		return actualPercent >= condPercent
	case OperatorLessThanPercent:
		return actualPercent < condPercent
	case OperatorLessThanOrEqualPercent:
		return actualPercent <= condPercent
	default:
		return false
	}
}

// compareFloat64 compares a float64 value against the condition.
func compareFloat64(value float64, cond *RuleCondition) bool {
	// Parse the condition value as float64
	condValue, err := strconv.ParseFloat(cond.Value, 64)
	if err != nil && cond.Value != "" {
		return false
	}

	switch cond.Operator {
	case OperatorEqual:
		return value == condValue
	case OperatorNotEqual:
		return value != condValue
	case OperatorGreaterThan:
		return value > condValue
	case OperatorGreaterThanOrEqual:
		return value >= condValue
	case OperatorLessThan:
		return value < condValue
	case OperatorLessThanOrEqual:
		return value <= condValue
	case OperatorBetween:
		if cond.MinValue == nil || cond.MaxValue == nil {
			return false
		}
		return value >= *cond.MinValue && value <= *cond.MaxValue
	default:
		return false
	}
}

// compareBool compares a boolean value against the condition.
func compareBool(value bool, cond *RuleCondition) bool {
	condValue := strings.ToLower(cond.Value) == "true" || cond.Value == "1"

	switch cond.Operator {
	case OperatorEqual:
		return value == condValue
	case OperatorNotEqual:
		return value != condValue
	default:
		return false
	}
}

// compareHardlinkScope compares a hardlink scope value against the condition.
func compareHardlinkScope(value string, cond *RuleCondition) bool {
	switch cond.Operator {
	case OperatorEqual:
		return strings.EqualFold(value, cond.Value)
	case OperatorNotEqual:
		return !strings.EqualFold(value, cond.Value)
	default:
		return false
	}
}

// compareAge computes the age (time since timestamp) and compares it against the condition.
// Age is calculated as: nowUnix - timestamp, clamped to 0 to avoid clock-skew weirdness.
func compareAge(timestamp int64, cond *RuleCondition, ctx *EvalContext) bool {
	// Get current time from context (for testability) or use time.Now()
	nowUnix := time.Now().Unix()
	if ctx != nil && ctx.NowUnix > 0 {
		nowUnix = ctx.NowUnix
	}

	// Compute age in seconds, clamped to 0 to avoid negative ages from clock skew
	ageSeconds := max(nowUnix-timestamp, 0)

	return compareInt64(ageSeconds, cond)
}

// splitTags splits a comma-separated tag string into individual tags.
// Returns nil for empty input.
func splitTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
