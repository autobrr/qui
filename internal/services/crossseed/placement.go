// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/internal/services/linktree"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/pathcmp"
)

// autoTMMDecision holds the inputs and result of autoTMM evaluation for logging.
type autoTMMDecision struct {
	Enabled            bool
	CrossCategory      string
	MatchedAutoManaged bool
	UseIndexerCategory bool
	UseCustomCategory  bool
	CategorySavePath   string
	MatchedSavePath    string
	PathsMatch         bool
}

// shouldEnableAutoTMM determines whether Auto Torrent Management should be enabled.
// autoTMM can only be enabled when:
// - A cross-seed category was successfully created (crossCategory != "")
// - The matched torrent uses autoTMM
// - Not using indexer categories or custom categories (which may have different paths)
//
// We trust qBittorrent's implicit path calculation when categories don't have explicit
// save paths configured. If the matched torrent is using autoTMM successfully, the
// cross-seed should inherit that setting.
//
// Returns the decision struct for logging and whether autoTMM should be enabled.
func shouldEnableAutoTMM(crossCategory string, matchedAutoManaged bool, useCategoryFromIndexer bool, useCustomCategory bool, actualCategorySavePath string, matchedSavePath string) autoTMMDecision {
	// Check if explicit category save path matches the matched torrent's path (informational).
	pathsMatch := actualCategorySavePath != "" && matchedSavePath != "" &&
		normalizePath(actualCategorySavePath) == normalizePath(matchedSavePath)

	// Enable autoTMM if the matched torrent uses autoTMM and we're not using indexer or custom categories.
	// When a category has no explicit save path, qBittorrent uses an implicit path:
	// <default_save_path>/<category_name>. Since the matched torrent is already using
	// autoTMM successfully at its current path, the cross-seed should too.
	// Custom categories disable autoTMM like indexer categories for safety.
	enabled := crossCategory != "" && matchedAutoManaged && !useCategoryFromIndexer && !useCustomCategory

	return autoTMMDecision{
		Enabled:            enabled,
		CrossCategory:      crossCategory,
		MatchedAutoManaged: matchedAutoManaged,
		UseIndexerCategory: useCategoryFromIndexer,
		UseCustomCategory:  useCustomCategory,
		CategorySavePath:   actualCategorySavePath,
		MatchedSavePath:    matchedSavePath,
		PathsMatch:         pathsMatch,
	}
}

// isWindowsDriveAbs is preserved for local readability.
// Shared implementation lives in pkg/pathcmp.
func isWindowsDriveAbs(p string) bool {
	return pathcmp.IsWindowsDriveAbs(p)
}

// normalizePath is preserved for local readability.
// Shared implementation lives in pkg/pathcmp.
func normalizePath(p string) string {
	return pathcmp.NormalizePath(p)
}

// rootlessDestDir returns the content dir that a rootless target sends an add
// to, or "" when the add lands at the target's save path instead. The apply and
// the Manual match save-path preview must agree on this, so both call it.
func rootlessDestDir(matchedTorrent *qbt.Torrent, candidateFiles qbt.TorrentFiles, isEpisodeInPack bool) string {
	if isEpisodeInPack || detectCommonRoot(candidateFiles) != "" {
		return ""
	}
	return resolveRootlessContentDir(matchedTorrent, candidateFiles)
}

func resolveRootlessContentDir(matchedTorrent *qbt.Torrent, candidateFiles qbt.TorrentFiles) string {
	if matchedTorrent == nil || matchedTorrent.ContentPath == "" || len(candidateFiles) == 0 {
		return ""
	}

	contentPath := normalizePath(matchedTorrent.ContentPath)
	if contentPath == "" || contentPath == "." {
		return ""
	}

	// Rootless content dir logic expects an absolute storage path.
	// Accept POSIX absolute (/downloads/...) and Windows drive paths (C:/...).
	if !path.IsAbs(contentPath) && !isWindowsDriveAbs(contentPath) {
		return ""
	}

	// qBittorrent returns the full file path for single-file torrents.
	// Keep the trailing slash so normalizePath keeps C:/ and //server/share roots.
	if len(candidateFiles) == 1 {
		return normalizePath(contentPath[:strings.LastIndex(contentPath, "/")+1])
	}

	// Multi-file torrents use the storage directory as content path.
	return contentPath
}

// applyCategoryAffix applies a prefix or suffix to a category based on the affix mode.
// Returns the category unchanged if affixMode is empty or affix is empty.
func applyCategoryAffix(category, affixMode, affix string) string {
	if category == "" || affix == "" || affixMode == "" {
		return category
	}
	switch affixMode {
	case models.CategoryAffixModePrefix:
		// Avoid double-prefixing
		if strings.HasPrefix(category, affix) {
			return category
		}
		return affix + category
	case models.CategoryAffixModeSuffix:
		// Avoid double-suffixing
		if strings.HasSuffix(category, affix) {
			return category
		}
		return category + affix
	default:
		return category
	}
}

// ensureCrossCategory ensures a .cross suffixed category exists with the correct save_path.
// If compareExistingSavePath is true and the category already exists, it verifies the save_path
// matches (logs warning if different). Link modes pass false because they add torrents with an
// explicit savepath and autoTMM disabled, so category save_path drift is not actionable there.
// If it doesn't exist, it creates it with the provided save_path.
//
// It returns the category's actual save_path (normalized for comparison): the existing path when
// the category already exists, otherwise the path it was created with. Regular mode uses this to
// detect divergence from the matched torrent's location and pin an explicit savepath instead of
// trusting autoTMM. The returned path is empty only when crossCategory is empty.
//
// This function uses singleflight to deduplicate concurrent creation attempts for the same
// category, and maintains local state to avoid relying on potentially stale GetCategories responses.
func (s *Service) ensureCrossCategory(
	ctx context.Context,
	instanceID int,
	crossCategory,
	savePath string,
	compareExistingSavePath bool,
) (string, error) {
	if crossCategory == "" {
		return "", nil
	}

	key := fmt.Sprintf("%d:%s", instanceID, crossCategory)
	requestedSavePath := normalizePathForComparison(savePath)

	cacheMatches := func(cached any) bool {
		if !compareExistingSavePath {
			return true
		}
		cachedSavePath, ok := cached.(string)
		if !ok {
			return false
		}
		if requestedSavePath == "" {
			return true
		}
		return normalizePathForComparison(cachedSavePath) == requestedSavePath
	}

	// Fast path: we already created or verified this category in this session
	// for the same requested save path.
	if cached, ok := s.createdCategories.Load(key); ok && cacheMatches(cached) {
		cachedSavePath, _ := cached.(string)
		return cachedSavePath, nil
	}

	// Use singleflight to deduplicate concurrent calls for the same category.
	// Multiple goroutines trying to create the same category will share a single execution.
	actualSavePathValue, err, _ := s.categoryCreationGroup.Do(key, func() (any, error) {
		// Double-check inside singleflight (another goroutine might have completed just before us)
		// for the same requested save path.
		if cached, ok := s.createdCategories.Load(key); ok && cacheMatches(cached) {
			cachedSavePath, _ := cached.(string)
			return cachedSavePath, nil
		}

		// Get existing categories from qBittorrent
		categories, err := s.syncManager.GetCategories(ctx, instanceID)
		if err != nil {
			return nil, fmt.Errorf("get categories: %w", err)
		}

		// Check if category already exists
		if existing, exists := categories[crossCategory]; exists {
			actualSavePath := normalizePathForComparison(existing.SavePath)

			// Category exists - check if save_path differs (informational warning only)
			if shouldWarnOnCategorySavePathMismatch(compareExistingSavePath, requestedSavePath, existing.SavePath) {
				log.Warn().
					Int("instanceID", instanceID).
					Str("category", crossCategory).
					Str("expectedSavePath", savePath).
					Str("actualSavePath", existing.SavePath).
					Msg("[CROSSSEED] Cross-seed category exists with different save path")
			}
			s.createdCategories.Store(key, actualSavePath)
			return actualSavePath, nil
		}

		// Create the category with the save_path
		if err := s.syncManager.CreateCategory(ctx, instanceID, crossCategory, savePath); err != nil {
			return nil, fmt.Errorf("create category: %w", err)
		}

		s.createdCategories.Store(key, requestedSavePath)
		log.Debug().
			Int("instanceID", instanceID).
			Str("category", crossCategory).
			Str("savePath", savePath).
			Msg("[CROSSSEED] Created category for cross-seed")

		return requestedSavePath, nil
	})

	if err != nil {
		return "", err
	}

	actualSavePath, _ := actualSavePathValue.(string)
	if !compareExistingSavePath || requestedSavePath == "" || actualSavePath == requestedSavePath {
		return actualSavePath, nil
	}

	categories, err := s.syncManager.GetCategories(ctx, instanceID)
	if err != nil {
		return "", fmt.Errorf("get categories after singleflight: %w", err)
	}

	if existing, exists := categories[crossCategory]; exists {
		actualSavePath = normalizePathForComparison(existing.SavePath)
		if shouldWarnOnCategorySavePathMismatch(compareExistingSavePath, requestedSavePath, existing.SavePath) {
			log.Warn().
				Int("instanceID", instanceID).
				Str("category", crossCategory).
				Str("expectedSavePath", savePath).
				Str("actualSavePath", existing.SavePath).
				Msg("[CROSSSEED] Cross-seed category exists with different save path")
		}
		s.createdCategories.Store(key, actualSavePath)
		return actualSavePath, nil
	}

	if err := s.syncManager.CreateCategory(ctx, instanceID, crossCategory, savePath); err != nil {
		return "", fmt.Errorf("create category after singleflight: %w", err)
	}

	s.createdCategories.Store(key, requestedSavePath)
	log.Debug().
		Int("instanceID", instanceID).
		Str("category", crossCategory).
		Str("savePath", savePath).
		Msg("[CROSSSEED] Created category for cross-seed after singleflight revalidation")

	return requestedSavePath, nil
}

func shouldWarnOnCategorySavePathMismatch(compareExistingSavePath bool, requestedSavePath, existingSavePath string) bool {
	if !compareExistingSavePath || requestedSavePath == "" || existingSavePath == "" {
		return false
	}

	return normalizePathForComparison(existingSavePath) != requestedSavePath
}

// determineCrossSeedCategory selects the category to apply to a cross-seeded torrent.
// Returns (baseCategory, crossCategory) where baseCategory is used to look up save_path
// and crossCategory is the final category name (with .cross suffix if enabled).
// If settings is nil, it will be loaded from the database.
func (s *Service) determineCrossSeedCategory(ctx context.Context, req *CrossSeedRequest, matchedTorrent *qbt.Torrent, settings *models.CrossSeedAutomationSettings) (baseCategory, crossCategory string) {
	var matchedCategory string
	if matchedTorrent != nil {
		matchedCategory = matchedTorrent.Category
	}

	// Load settings if not provided by caller
	if settings == nil && s != nil {
		settings, _ = s.GetAutomationSettings(ctx)
	}

	// Custom category takes priority - use exact category without any affix
	if settings != nil && settings.UseCustomCategory && settings.CustomCategory != "" {
		return settings.CustomCategory, settings.CustomCategory
	}

	// Helper to apply category affix (prefix or suffix) based on settings
	applyAffix := func(cat string) string {
		if settings != nil && settings.UseCrossCategoryAffix && settings.CategoryAffixMode != "" && settings.CategoryAffix != "" {
			return applyCategoryAffix(cat, settings.CategoryAffixMode, settings.CategoryAffix)
		}
		return cat
	}

	if req == nil {
		return matchedCategory, applyAffix(matchedCategory)
	}

	if req.Category != "" {
		return req.Category, applyAffix(req.Category)
	}

	if req.IndexerName != "" && settings != nil && settings.UseCategoryFromIndexer {
		return req.IndexerName, applyAffix(req.IndexerName)
	}

	return matchedCategory, applyAffix(matchedCategory)
}

// buildCrossSeedTags merges source-specific tags with optional matched torrent tags.
// Parameters:
//   - sourceTags: tags configured for this source type (e.g., RSSAutomationTags, SeededSearchTags)
//   - matchedTags: comma-separated tags from the matched torrent in qBittorrent
//   - inheritSourceTags: whether to also include tags from the matched torrent
func buildCrossSeedTags(sourceTags []string, matchedTags string, inheritSourceTags bool) []string {
	tagSet := make(map[string]struct{})
	finalTags := make([]string, 0)

	addTag := func(tag string) {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return
		}
		if _, exists := tagSet[tag]; exists {
			return
		}
		tagSet[tag] = struct{}{}
		finalTags = append(finalTags, tag)
	}

	// Add source-specific tags first
	for _, tag := range sourceTags {
		addTag(tag)
	}

	// Optionally inherit tags from the matched torrent
	if inheritSourceTags && matchedTags != "" {
		for tag := range strings.SplitSeq(matchedTags, ",") {
			addTag(tag)
		}
	}

	return finalTags
}

func selectExistingSourceFiles(sourceFiles, candidateFiles qbt.TorrentFiles) []hardlinktree.TorrentFile {
	matches, _ := matchMaterializedSourceFilesToCandidates(sourceFiles, candidateFiles)
	existingSourceFiles := make([]hardlinktree.TorrentFile, 0, len(matches))
	for _, match := range matches {
		f := sourceFiles[match.sourceIndex]
		existingSourceFiles = append(existingSourceFiles, hardlinktree.TorrentFile{
			Path: f.Name,
			Size: f.Size,
		})
	}

	return existingSourceFiles
}

func sourceFilesTotalSize(files qbt.TorrentFiles) int64 {
	var total int64
	for _, f := range files {
		total += f.Size
	}
	return total
}

func treeFilesTotalSize(files []hardlinktree.TorrentFile) int64 {
	var total int64
	for _, f := range files {
		total += f.Size
	}
	return total
}

// linkedTreePaths collects the torrent paths of the linked tree files for the
// linked-file check.
func linkedTreePaths(linked []hardlinktree.TorrentFile) map[string]struct{} {
	paths := make(map[string]struct{}, len(linked))
	for _, file := range linked {
		paths[file.Path] = struct{}{}
	}
	return paths
}

func materializedCoverage(sourceFiles qbt.TorrentFiles, materializedFiles []hardlinktree.TorrentFile) (float64, int64, int64) {
	totalBytes := sourceFilesTotalSize(sourceFiles)
	if totalBytes <= 0 {
		return 1, 0, 0
	}

	materializedBytes := treeFilesTotalSize(materializedFiles)
	return float64(materializedBytes) / float64(totalBytes), materializedBytes, totalBytes
}

func hasUnmaterializedSourceFiles(sourceFiles qbt.TorrentFiles, materializedFiles []hardlinktree.TorrentFile) bool {
	return len(sourceFiles) != len(materializedFiles)
}

func coverageThresholdFromTolerance(tolerancePercent float64) float64 {
	if tolerancePercent < 0 {
		tolerancePercent = 0
	} else if tolerancePercent > 100 {
		tolerancePercent = 100
	}

	return 1.0 - (tolerancePercent / 100.0)
}

func belowThresholdMessage(mode string, coverage, threshold float64, materializedBytes, totalBytes int64, materializedFiles, totalFiles int) string {
	return fmt.Sprintf(
		"Skipped %s mode: matched files cover %.1f%% of torrent bytes (%d/%d files, %d/%d bytes), below required %.1f%% threshold",
		mode,
		coverage*100,
		materializedFiles,
		totalFiles,
		materializedBytes,
		totalBytes,
		threshold*100,
	)
}

// linkCategorySavePath is the save path of the cross-seed category of a link
// tree: its Preset directory, without the folder of the torrent, as the
// instance that gets the add sees it.
func linkCategorySavePath(d fsops.PathDialect, tree *linktree.Tree, route *candidateRoute) string {
	presetDir := tree.PresetDir
	if route != nil {
		// Map before normalizePath, which needs no dialect but loses the one
		// that the map needs. A link tree maps under the same row first.
		presetDir, _ = crossseedroute.ToCrossSeed(d, route.rows, tree.BaseDir, presetDir)
	}
	if tree.PresetDir == tree.BaseDir {
		return presetDir
	}
	return normalizePath(presetDir)
}

// resolveTrackerDisplayName resolves the display name for the tracker.
// Uses the incoming torrent's tracker domain (extracted from .torrent bytes) for the
// "by-tracker" hardlink directory preset. Falls back to indexer name, then domain.
func (s *Service) resolveTrackerDisplayName(ctx context.Context, incomingTrackerDomain string, req *CrossSeedRequest) string {
	// Get tracker customizations
	var customizations []*models.TrackerCustomization
	if s.trackerCustomizationStore != nil {
		if customs, err := s.trackerCustomizationStore.List(ctx); err == nil {
			customizations = customs
		}
	}

	// Use indexer name as fallback
	indexerName := ""
	if req.IndexerName != "" {
		indexerName = req.IndexerName
	}

	return models.ResolveTrackerDisplayName(incomingTrackerDomain, indexerName, customizations)
}

// rankBaseDirs orders dirs by the number of leading path components they share
// with source, so the dir on the source's own disk comes first. Ties keep the
// configured order.
func rankBaseDirs(d fsops.PathDialect, dirs []string, source string) []string {
	ranked := slices.Clone(dirs)
	slices.SortStableFunc(ranked, func(a, b string) int {
		return cmp.Compare(sharedPathComponents(d, b, source), sharedPathComponents(d, a, source))
	})
	return ranked
}

func sharedPathComponents(d fsops.PathDialect, a, b string) int {
	aParts := strings.Split(d.Clean(a), d.Separator())
	bParts := strings.Split(d.Clean(b), d.Separator())
	n := 0
	for n < len(aParts) && n < len(bParts) && aParts[n] == bParts[n] {
		n++
	}
	return n
}

func safeTorrentRelativeFilePath(name string) (string, bool) {
	if name == "" || strings.Contains(name, `\`) || path.IsAbs(name) || isWindowsPathForm(name) || hasDotDotSegment(name) {
		return "", false
	}

	cleaned := path.Clean(name)
	if cleaned == "." ||
		path.IsAbs(cleaned) ||
		strings.HasPrefix(cleaned, "/") ||
		strings.HasPrefix(cleaned, `\`) ||
		isWindowsPathForm(cleaned) ||
		hasDotDotSegment(cleaned) {
		return "", false
	}

	return cleaned, true
}

func isWindowsPathForm(p string) bool {
	if strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//") {
		return true
	}
	if len(p) < 2 {
		return false
	}
	c := p[0]
	return ((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) && p[1] == ':'
}

func hasDotDotSegment(p string) bool {
	for part := range strings.SplitSeq(p, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}
