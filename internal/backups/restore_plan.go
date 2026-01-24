// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
)

// RestoreMode controls how a snapshot should be applied to a live qBittorrent instance.
type RestoreMode string

const (
	RestoreModeIncremental RestoreMode = "incremental"
	RestoreModeOverwrite   RestoreMode = "overwrite"
	RestoreModeComplete    RestoreMode = "complete"
)

// CategorySpec captures the desired state for a category during restore.
type CategorySpec struct {
	Name     string `json:"name"`
	SavePath string `json:"savePath,omitempty"`
}

// CategoryUpdate captures the changes required to align an existing category with the snapshot.
type CategoryUpdate struct {
	Name        string `json:"name"`
	CurrentPath string `json:"currentPath"`
	DesiredPath string `json:"desiredPath"`
}

// TagSpec encapsulates a tag that should be present after restore.
type TagSpec struct {
	Name string `json:"name"`
}

// TagUpdate captures tag mutations required for overwrite/complete restores.
type TagUpdate struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
}

// TorrentSpec describes a torrent that should exist after the restore completes.
type TorrentSpec struct {
	Manifest ManifestItem `json:"manifest"`
}

// TorrentUpdate captures adjustments required for an existing torrent.
type TorrentUpdate struct {
	Hash    string          `json:"hash"`
	Current LiveTorrent     `json:"current"`
	Desired SnapshotTorrent `json:"desired"`
	Changes []DiffChange    `json:"changes"`
}

// DiffChange captures a single field difference between snapshot and live state.
type DiffChange struct {
	Field     string `json:"field"`
	Supported bool   `json:"supported"`
	Current   any    `json:"current,omitempty"`
	Desired   any    `json:"desired,omitempty"`
	Message   string `json:"message,omitempty"`
}

// CategoryPlan bundles create/update/delete intents for categories.
type CategoryPlan struct {
	Create []CategorySpec   `json:"create,omitempty"`
	Update []CategoryUpdate `json:"update,omitempty"`
	Delete []string         `json:"delete,omitempty"`
}

// TagPlan bundles create/delete intents for tags.
type TagPlan struct {
	Create []TagSpec `json:"create,omitempty"`
	Delete []string  `json:"delete,omitempty"`
}

// TorrentPlan bundles add/update/delete intents for torrents.
type TorrentPlan struct {
	Add    []TorrentSpec   `json:"add,omitempty"`
	Update []TorrentUpdate `json:"update,omitempty"`
	Delete []string        `json:"delete,omitempty"`
}

// RestorePlan is the full set of actions required to align the live instance with the snapshot.
type RestorePlan struct {
	Mode       RestoreMode  `json:"mode"`
	RunID      int64        `json:"runId"`
	InstanceID int          `json:"instanceId"`
	Categories CategoryPlan `json:"categories"`
	Tags       TagPlan      `json:"tags"`
	Torrents   TorrentPlan  `json:"torrents"`
}

// RestorePlanOptions controls post-processing applied to a generated plan.
type RestorePlanOptions struct {
	ExcludeHashes []string
}

// SnapshotTorrent provides convenient access to torrent metadata captured in the snapshot.
type SnapshotTorrent struct {
	Hash        string   `json:"hash"`
	Name        string   `json:"name"`
	Category    *string  `json:"category,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	ArchivePath string   `json:"archivePath,omitempty"`
	BlobPath    string   `json:"blobPath,omitempty"`
	SizeBytes   int64    `json:"sizeBytes,omitempty"`
	InfoHashV1  *string  `json:"infoHashV1,omitempty"`
	InfoHashV2  *string  `json:"infoHashV2,omitempty"`
}

// SnapshotState represents the desired state recorded in a backup snapshot.
type SnapshotState struct {
	RunID      int64                              `json:"runId"`
	InstanceID int                                `json:"instanceId"`
	Categories map[string]models.CategorySnapshot `json:"categories"`
	Tags       map[string]struct{}                `json:"tags"`
	Torrents   map[string]SnapshotTorrent         `json:"torrents"`
}

// LiveCategory captures the current state for a category in qBittorrent.
type LiveCategory struct {
	Name     string `json:"name"`
	SavePath string `json:"savePath"`
}

// LiveTorrent captures the subset of torrent fields we need for planning.
type LiveTorrent struct {
	Hash        string   `json:"hash"`
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	TrackerURLs []string `json:"trackerUrls,omitempty"`
	InfoHashV1  string   `json:"infoHashV1,omitempty"`
	InfoHashV2  string   `json:"infoHashV2,omitempty"`
	SizeBytes   int64    `json:"sizeBytes,omitempty"`
}

// LiveState represents the state of the live qBittorrent instance relevant for planning a restore.
type LiveState struct {
	InstanceID int                     `json:"instanceId"`
	Categories map[string]LiveCategory `json:"categories"`
	Tags       map[string]struct{}     `json:"tags"`
	Torrents   map[string]LiveTorrent  `json:"torrents"`
}

// PlanRestoreDiff loads snapshot and live state, returning the diff plan for the requested mode.
func (s *Service) PlanRestoreDiff(ctx context.Context, runID int64, mode RestoreMode, opts *RestorePlanOptions) (*RestorePlan, error) {
	if s == nil {
		return nil, errors.New("nil backup service")
	}

	if mode == "" {
		mode = RestoreModeIncremental
	}

	if !isValidRestoreMode(mode) {
		return nil, fmt.Errorf("unsupported restore mode: %s", mode)
	}

	snapshot, err := s.loadSnapshotState(ctx, runID)
	if err != nil {
		return nil, err
	}

	live, err := s.loadLiveState(ctx, snapshot.InstanceID)
	if err != nil {
		return nil, err
	}

	plan, err := buildRestorePlan(snapshot, live, mode)
	if err != nil {
		return nil, err
	}

	applyRestorePlanOptions(plan, opts)

	return plan, nil
}

// ParseRestoreMode normalizes the provided mode string and ensures it is supported.
func ParseRestoreMode(value string) (RestoreMode, error) {
	mode := RestoreMode(strings.ToLower(strings.TrimSpace(value)))
	if mode == "" {
		return RestoreModeIncremental, nil
	}
	if !isValidRestoreMode(mode) {
		return "", fmt.Errorf("unsupported restore mode: %s", value)
	}
	return mode, nil
}

func isValidRestoreMode(mode RestoreMode) bool {
	switch mode {
	case RestoreModeIncremental, RestoreModeOverwrite, RestoreModeComplete:
		return true
	default:
		return false
	}
}

func applyRestorePlanOptions(plan *RestorePlan, opts *RestorePlanOptions) {
	if plan == nil || opts == nil {
		return
	}

	if len(opts.ExcludeHashes) == 0 {
		return
	}

	exclude := make(map[string]struct{}, len(opts.ExcludeHashes))
	for _, hash := range opts.ExcludeHashes {
		normalized := strings.TrimSpace(strings.ToLower(hash))
		if normalized == "" {
			continue
		}
		exclude[normalized] = struct{}{}
	}

	if len(exclude) == 0 {
		return
	}

	filterTorrentSpecs := func(items []TorrentSpec) []TorrentSpec {
		if len(items) == 0 {
			return items
		}
		filtered := items[:0]
		for _, item := range items {
			hash := strings.TrimSpace(strings.ToLower(item.Manifest.Hash))
			if _, skip := exclude[hash]; skip {
				continue
			}
			filtered = append(filtered, item)
		}
		return filtered
	}

	filterTorrentUpdates := func(items []TorrentUpdate) []TorrentUpdate {
		if len(items) == 0 {
			return items
		}
		filtered := items[:0]
		for _, item := range items {
			hash := strings.TrimSpace(strings.ToLower(item.Hash))
			if _, skip := exclude[hash]; skip {
				continue
			}
			filtered = append(filtered, item)
		}
		return filtered
	}

	filterHashes := func(items []string) []string {
		if len(items) == 0 {
			return items
		}
		filtered := items[:0]
		for _, hash := range items {
			normalized := strings.TrimSpace(strings.ToLower(hash))
			if _, skip := exclude[normalized]; skip {
				continue
			}
			filtered = append(filtered, hash)
		}
		return filtered
	}

	plan.Torrents.Add = filterTorrentSpecs(plan.Torrents.Add)
	plan.Torrents.Update = filterTorrentUpdates(plan.Torrents.Update)
	plan.Torrents.Delete = filterHashes(plan.Torrents.Delete)

}

// buildRestorePlan compares snapshot and live state to determine the actions needed to reach parity.
func buildRestorePlan(snapshot *SnapshotState, live *LiveState, mode RestoreMode) (*RestorePlan, error) {
	if snapshot == nil {
		return nil, errors.New("nil snapshot state")
	}
	if live == nil {
		return nil, errors.New("nil live state")
	}

	plan := &RestorePlan{
		Mode:       mode,
		RunID:      snapshot.RunID,
		InstanceID: snapshot.InstanceID,
		Categories: CategoryPlan{},
		Tags:       TagPlan{},
		Torrents:   TorrentPlan{},
	}

	plan.Categories = buildCategoryPlan(snapshot.Categories, live.Categories, mode)
	plan.Tags = buildTagPlan(snapshot.Tags, live.Tags, mode)
	torrentPlan, err := buildTorrentPlan(snapshot.Torrents, live.Torrents, mode)
	if err != nil {
		return nil, err
	}
	plan.Torrents = torrentPlan

	return plan, nil
}

func (s *Service) loadSnapshotState(ctx context.Context, runID int64) (*SnapshotState, error) {
	manifest, err := s.LoadManifest(ctx, runID)
	if err != nil {
		return nil, err
	}

	torrents := make(map[string]SnapshotTorrent, len(manifest.Items))
	for _, item := range manifest.Items {
		hash := strings.ToLower(strings.TrimSpace(item.Hash))
		if hash == "" {
			continue
		}

		copyTags := make([]string, len(item.Tags))
		copy(copyTags, item.Tags)

		torrents[hash] = SnapshotTorrent{
			Hash:        hash,
			Name:        item.Name,
			Category:    item.Category,
			Tags:        copyTags,
			ArchivePath: item.ArchivePath,
			BlobPath:    item.TorrentBlob,
			SizeBytes:   item.SizeBytes,
			InfoHashV1:  item.InfoHashV1,
			InfoHashV2:  item.InfoHashV2,
		}
	}

	categorySnapshots := make(map[string]models.CategorySnapshot, len(manifest.Categories))
	for name, snapshot := range manifest.Categories {
		trimmedName := strings.TrimSpace(name)
		if trimmedName == "" {
			continue
		}
		categorySnapshots[trimmedName] = models.CategorySnapshot{SavePath: strings.TrimSpace(snapshot.SavePath)}
	}

	tagSet := make(map[string]struct{}, len(manifest.Tags))
	for _, tag := range manifest.Tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed == "" {
			continue
		}
		tagSet[trimmed] = struct{}{}
	}

	return &SnapshotState{
		RunID:      runID,
		InstanceID: manifest.InstanceID,
		Categories: categorySnapshots,
		Tags:       tagSet,
		Torrents:   torrents,
	}, nil
}

func (s *Service) loadLiveState(ctx context.Context, instanceID int) (*LiveState, error) {
	categories, err := s.syncManager.GetCategories(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("load categories: %w", err)
	}

	tags, err := s.syncManager.GetTags(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("load tags: %w", err)
	}

	torrents, err := s.syncManager.GetAllTorrents(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("load torrents: %w", err)
	}

	liveCategories := make(map[string]LiveCategory, len(categories))
	for name, category := range categories {
		trimmedName := strings.TrimSpace(name)
		if trimmedName == "" {
			continue
		}
		liveCategories[trimmedName] = LiveCategory{
			Name:     trimmedName,
			SavePath: strings.TrimSpace(category.SavePath),
		}
	}

	tagSet := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed == "" {
			continue
		}
		tagSet[trimmed] = struct{}{}
	}

	liveTorrents := make(map[string]LiveTorrent, len(torrents))
	for _, torrent := range torrents {
		hash := strings.ToLower(strings.TrimSpace(torrent.Hash))
		if hash == "" {
			continue
		}

		liveTorrents[hash] = LiveTorrent{
			Hash:        hash,
			Name:        torrent.Name,
			Category:    strings.TrimSpace(torrent.Category),
			Tags:        splitTags(torrent.Tags),
			TrackerURLs: uniqueTrackerURLs(torrent.Trackers),
			InfoHashV1:  strings.TrimSpace(torrent.InfohashV1),
			InfoHashV2:  strings.TrimSpace(torrent.InfohashV2),
			SizeBytes:   torrent.TotalSize,
		}
	}

	return &LiveState{
		InstanceID: instanceID,
		Categories: liveCategories,
		Tags:       tagSet,
		Torrents:   liveTorrents,
	}, nil
}

func uniqueTrackerURLs(trackers []qbt.TorrentTracker) []string {
	if len(trackers) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(trackers))
	result := make([]string, 0, len(trackers))
	for _, tracker := range trackers {
		url := strings.TrimSpace(tracker.Url)
		if url == "" {
			continue
		}
		if _, exists := seen[url]; exists {
			continue
		}
		seen[url] = struct{}{}
		result = append(result, url)
	}

	if len(result) > 1 {
		sort.Strings(result)
	}

	return result
}

func buildCategoryPlan(snapshot map[string]models.CategorySnapshot, live map[string]LiveCategory, mode RestoreMode) CategoryPlan {
	allowUpdates := mode == RestoreModeOverwrite || mode == RestoreModeComplete
	includeDeletes := mode == RestoreModeComplete

	plan := CategoryPlan{}

	for name, snap := range snapshot {
		trimmedName := strings.TrimSpace(name)
		if trimmedName == "" {
			continue
		}

		liveCat, exists := live[trimmedName]
		if !exists {
			plan.Create = append(plan.Create, CategorySpec{Name: trimmedName, SavePath: strings.TrimSpace(snap.SavePath)})
			continue
		}

		desiredPath := strings.TrimSpace(snap.SavePath)
		if allowUpdates && desiredPath != liveCat.SavePath {
			plan.Update = append(plan.Update, CategoryUpdate{
				Name:        trimmedName,
				CurrentPath: liveCat.SavePath,
				DesiredPath: desiredPath,
			})
		}
	}

	if includeDeletes {
		for name := range live {
			trimmedName := strings.TrimSpace(name)
			if trimmedName == "" {
				continue
			}
			if _, exists := snapshot[trimmedName]; !exists {
				plan.Delete = append(plan.Delete, trimmedName)
			}
		}
	}

	sort.Slice(plan.Create, func(i, j int) bool {
		return plan.Create[i].Name < plan.Create[j].Name
	})
	sort.Slice(plan.Update, func(i, j int) bool {
		return plan.Update[i].Name < plan.Update[j].Name
	})
	sort.Strings(plan.Delete)

	return plan
}

func buildTagPlan(snapshot map[string]struct{}, live map[string]struct{}, mode RestoreMode) TagPlan {
	includeDeletes := mode == RestoreModeComplete

	plan := TagPlan{}

	for tag := range snapshot {
		trimmed := strings.TrimSpace(tag)
		if trimmed == "" {
			continue
		}
		if _, exists := live[trimmed]; !exists {
			plan.Create = append(plan.Create, TagSpec{Name: trimmed})
		}
	}

	if includeDeletes {
		for tag := range live {
			trimmed := strings.TrimSpace(tag)
			if trimmed == "" {
				continue
			}
			if _, exists := snapshot[trimmed]; !exists {
				plan.Delete = append(plan.Delete, trimmed)
			}
		}
	}

	sort.Slice(plan.Create, func(i, j int) bool {
		return plan.Create[i].Name < plan.Create[j].Name
	})
	sort.Strings(plan.Delete)

	return plan
}

func buildTorrentPlan(snapshot map[string]SnapshotTorrent, live map[string]LiveTorrent, mode RestoreMode) (TorrentPlan, error) {
	allowUpdates := mode == RestoreModeOverwrite || mode == RestoreModeComplete
	includeDeletes := mode == RestoreModeComplete

	plan := TorrentPlan{}

	for hash, snap := range snapshot {
		normalizedHash := strings.ToLower(strings.TrimSpace(hash))
		if normalizedHash == "" {
			continue
		}

		liveTorrent, exists := live[normalizedHash]
		if !exists {
			plan.Add = append(plan.Add, TorrentSpec{Manifest: snapshotTorrentToManifestItem(snap)})
			continue
		}

		if !allowUpdates {
			continue
		}

		changes := computeTorrentChanges(snap, liveTorrent)
		if len(changes) == 0 {
			continue
		}

		plan.Update = append(plan.Update, TorrentUpdate{
			Hash:    normalizedHash,
			Current: cloneLiveTorrent(liveTorrent),
			Desired: snap,
			Changes: changes,
		})
	}

	if includeDeletes {
		for hash := range live {
			normalizedHash := strings.ToLower(strings.TrimSpace(hash))
			if normalizedHash == "" {
				continue
			}
			if _, exists := snapshot[normalizedHash]; !exists {
				plan.Delete = append(plan.Delete, normalizedHash)
			}
		}
	}

	sort.Slice(plan.Add, func(i, j int) bool {
		return strings.ToLower(plan.Add[i].Manifest.Hash) < strings.ToLower(plan.Add[j].Manifest.Hash)
	})
	sort.Slice(plan.Update, func(i, j int) bool {
		return strings.ToLower(plan.Update[i].Hash) < strings.ToLower(plan.Update[j].Hash)
	})
	sort.Strings(plan.Delete)

	return plan, nil
}

func snapshotTorrentToManifestItem(t SnapshotTorrent) ManifestItem {
	manifest := ManifestItem{
		Hash:        strings.ToLower(strings.TrimSpace(t.Hash)),
		Name:        t.Name,
		ArchivePath: t.ArchivePath,
		SizeBytes:   t.SizeBytes,
		TorrentBlob: t.BlobPath,
	}
	if t.Category != nil {
		categoryCopy := strings.TrimSpace(*t.Category)
		manifest.Category = &categoryCopy
	}
	if t.InfoHashV1 != nil {
		infoCopy := strings.TrimSpace(*t.InfoHashV1)
		if infoCopy != "" {
			manifest.InfoHashV1 = &infoCopy
		}
	}
	if t.InfoHashV2 != nil {
		infoCopy := strings.TrimSpace(*t.InfoHashV2)
		if infoCopy != "" {
			manifest.InfoHashV2 = &infoCopy
		}
	}
	if len(t.Tags) > 0 {
		manifest.Tags = cloneStringSlice(t.Tags)
	}

	return manifest
}

func computeTorrentChanges(snapshot SnapshotTorrent, live LiveTorrent) []DiffChange {
	var changes []DiffChange

	snapshotCategory := normalizeCategory(snapshot.Category)
	liveCategory := normalizeCategoryPtr(live.Category)
	if snapshotCategory != liveCategory {
		changes = append(changes, DiffChange{
			Field:     "category",
			Supported: true,
			Current:   liveCategory,
			Desired:   snapshotCategory,
		})
	}

	if !stringSetsEqual(snapshot.Tags, live.Tags) {
		changes = append(changes, DiffChange{
			Field:     "tags",
			Supported: true,
			Current:   cloneStringSlice(live.Tags),
			Desired:   cloneStringSlice(snapshot.Tags),
		})
	}

	if snapshot.InfoHashV1 != nil {
		desiredV1 := strings.TrimSpace(*snapshot.InfoHashV1)
		if desiredV1 != "" && desiredV1 != strings.TrimSpace(live.InfoHashV1) {
			changes = append(changes, DiffChange{
				Field:     "infohash_v1",
				Supported: false,
				Current:   strings.TrimSpace(live.InfoHashV1),
				Desired:   desiredV1,
				Message:   "infohash values are read-only; verify torrent integrity manually",
			})
		}
	}
	if snapshot.InfoHashV2 != nil {
		desiredV2 := strings.TrimSpace(*snapshot.InfoHashV2)
		if desiredV2 != "" && desiredV2 != strings.TrimSpace(live.InfoHashV2) {
			changes = append(changes, DiffChange{
				Field:     "infohash_v2",
				Supported: false,
				Current:   strings.TrimSpace(live.InfoHashV2),
				Desired:   desiredV2,
				Message:   "infohash values are read-only; verify torrent integrity manually",
			})
		}
	}

	if snapshot.SizeBytes > 0 && snapshot.SizeBytes != live.SizeBytes {
		changes = append(changes, DiffChange{
			Field:     "sizeBytes",
			Supported: false,
			Current:   live.SizeBytes,
			Desired:   snapshot.SizeBytes,
			Message:   "local data size differs; re-verify files or re-download",
		})
	}

	return changes
}

func cloneLiveTorrent(t LiveTorrent) LiveTorrent {
	return LiveTorrent{
		Hash:        t.Hash,
		Name:        t.Name,
		Category:    t.Category,
		Tags:        cloneStringSlice(t.Tags),
		TrackerURLs: cloneStringSlice(t.TrackerURLs),
		InfoHashV1:  t.InfoHashV1,
		InfoHashV2:  t.InfoHashV2,
		SizeBytes:   t.SizeBytes,
	}
}

func cloneStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	copySlice := make([]string, len(in))
	copy(copySlice, in)
	return copySlice
}

func normalizeCategory(category *string) string {
	if category == nil {
		return ""
	}
	return strings.TrimSpace(*category)
}

func normalizeCategoryPtr(value string) string {
	return strings.TrimSpace(value)
}

func stringSetsEqual(a, b []string) bool {
	setA := toStringSet(a)
	setB := toStringSet(b)
	if len(setA) != len(setB) {
		return false
	}
	for key := range setA {
		if _, exists := setB[key]; !exists {
			return false
		}
	}
	return true
}

func toStringSet(items []string) map[string]struct{} {
	if len(items) == 0 {
		return map[string]struct{}{}
	}
	result := make(map[string]struct{}, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		result[trimmed] = struct{}{}
	}
	return result
}
