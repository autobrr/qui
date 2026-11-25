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

func shouldEnableAutoTMM(crossCategory string, matchedAutoManaged bool, useCategoryFromIndexer bool, useCustomCategory bool, actualCategorySavePath string, matchedSavePath string) autoTMMDecision {
	pathsMatch := actualCategorySavePath != "" && matchedSavePath != "" &&
		normalizePath(actualCategorySavePath) == normalizePath(matchedSavePath)

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

func isWindowsDriveAbs(p string) bool {
	return pathcmp.IsWindowsDriveAbs(p)
}

func normalizePath(p string) string {
	return pathcmp.NormalizePath(p)
}

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

	if !path.IsAbs(contentPath) && !isWindowsDriveAbs(contentPath) {
		return ""
	}

	if len(candidateFiles) == 1 {
		return normalizePath(contentPath[:strings.LastIndex(contentPath, "/")+1])
	}

	return contentPath
}

func applyCategoryAffix(category, affixMode, affix string) string {
	if category == "" || affix == "" || affixMode == "" {
		return category
	}
	switch affixMode {
	case models.CategoryAffixModePrefix:
		if strings.HasPrefix(category, affix) {
			return category
		}
		return affix + category
	case models.CategoryAffixModeSuffix:
		if strings.HasSuffix(category, affix) {
			return category
		}
		return category + affix
	default:
		return category
	}
}

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

	if cached, ok := s.createdCategories.Load(key); ok && cacheMatches(cached) {
		cachedSavePath, _ := cached.(string)
		return cachedSavePath, nil
	}

	actualSavePathValue, err, _ := s.categoryCreationGroup.Do(key, func() (any, error) {
		if cached, ok := s.createdCategories.Load(key); ok && cacheMatches(cached) {
			cachedSavePath, _ := cached.(string)
			return cachedSavePath, nil
		}

		categories, err := s.syncManager.GetCategories(ctx, instanceID)
		if err != nil {
			return nil, fmt.Errorf("get categories: %w", err)
		}

		if existing, exists := categories[crossCategory]; exists {
			actualSavePath := normalizePathForComparison(existing.SavePath)

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

func (s *Service) determineCrossSeedCategory(ctx context.Context, req *CrossSeedRequest, matchedTorrent *qbt.Torrent, settings *models.CrossSeedAutomationSettings) (baseCategory, crossCategory string) {
	var matchedCategory string
	if matchedTorrent != nil {
		matchedCategory = matchedTorrent.Category
	}

	if settings == nil && s != nil {
		settings, _ = s.GetAutomationSettings(ctx)
	}

	if settings != nil && settings.UseCustomCategory && settings.CustomCategory != "" {
		return settings.CustomCategory, settings.CustomCategory
	}

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

	for _, tag := range sourceTags {
		addTag(tag)
	}

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

func linkCategorySavePath(d fsops.PathDialect, tree *linktree.Tree, route *candidateRoute) string {
	presetDir := tree.PresetDir
	if route != nil {
		presetDir, _ = crossseedroute.ToCrossSeed(d, route.rows, tree.BaseDir, presetDir)
	}
	if tree.PresetDir == tree.BaseDir {
		return presetDir
	}
	return normalizePath(presetDir)
}

func (s *Service) resolveTrackerDisplayName(ctx context.Context, incomingTrackerDomain string, req *CrossSeedRequest) string {
	var customizations []*models.TrackerCustomization
	if s.trackerCustomizationStore != nil {
		if customs, err := s.trackerCustomizationStore.List(ctx); err == nil {
			customizations = customs
		}
	}

	indexerName := ""
	if req.IndexerName != "" {
		indexerName = req.IndexerName
	}

	return models.ResolveTrackerDisplayName(incomingTrackerDomain, indexerName, customizations)
}

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
