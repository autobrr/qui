// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
)

// EconomyScore represents a torrent's economy score and related metrics
type EconomyScore struct {
	Hash                string   `json:"hash"`
	Name                string   `json:"name"`
	Size                int64    `json:"size"`
	Seeds               int      `json:"seeds"` // Total seeds from tracker
	Peers               int      `json:"peers"` // Total peers from tracker
	Ratio               float64  `json:"ratio"`
	Age                 int64    `json:"age"`          // Age in days
	EconomyScore        float64  `json:"economyScore"` // Retention-based score (higher = keep longer)
	StorageValue        float64  `json:"storageValue"`
	RarityBonus         float64  `json:"rarityBonus"`
	DeduplicationFactor float64  `json:"deduplicationFactor"`
	ReviewPriority      float64  `json:"reviewPriority"`       // Priority for review (lower = needs more attention)
	Duplicates          []string `json:"duplicates,omitempty"` // Hash of duplicate torrents
	Tracker             string   `json:"tracker"`
	State               string   `json:"state"`
	Category            string   `json:"category"`
	LastActivity        int64    `json:"lastActivity"`
}

// EconomyStats represents aggregated economy statistics
type EconomyStats struct {
	TotalTorrents        int     `json:"totalTorrents"`
	TotalStorage         int64   `json:"totalStorage"`
	DeduplicatedStorage  int64   `json:"deduplicatedStorage"`
	StorageSavings       int64   `json:"storageSavings"`
	AverageEconomyScore  float64 `json:"averageEconomyScore"`
	HighValueTorrents    int     `json:"highValueTorrents"`
	RareContentCount     int     `json:"rareContentCount"`
	WellSeededOldContent int     `json:"wellSeededOldContent"`
}

// OptimizationOpportunity represents a specific optimization opportunity
type OptimizationOpportunity struct {
	Type        string   `json:"type"` // "cross_seeding_opportunity", "old_content_cleanup", "ratio_optimization", etc.
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Priority    string   `json:"priority"` // "high", "medium", "low"
	Savings     int64    `json:"savings"`  // Storage savings in bytes
	Impact      float64  `json:"impact"`   // Impact score (0-100)
	Torrents    []string `json:"torrents"` // Affected torrent hashes
}

// StorageOptimization represents storage-related optimization data
type StorageOptimization struct {
	TotalPotentialSavings    int64 `json:"totalPotentialSavings"`
	DeduplicationSavings     int64 `json:"deduplicationSavings"`
	OldContentCleanupSavings int64 `json:"oldContentCleanupSavings"`
	RatioOptimizationSavings int64 `json:"ratioOptimizationSavings"`
	UnusedContentSavings     int64 `json:"unusedContentSavings"`
}

// TorrentGroup represents a group of related torrents (duplicates)
type TorrentGroup struct {
	ID                string         `json:"id"`                // Unique group identifier
	Torrents          []EconomyScore `json:"torrents"`          // All torrents in this group
	PrimaryTorrent    EconomyScore   `json:"primaryTorrent"`    // The "best" torrent in the group
	GroupType         string         `json:"groupType"`         // "duplicate", "unique", "last_seed"
	TotalSize         int64          `json:"totalSize"`         // Combined size of all torrents in group
	DeduplicatedSize  int64          `json:"deduplicatedSize"`  // Size if keeping only the best copy
	PotentialSavings  int64          `json:"potentialSavings"`  // Size that could be saved
	RecommendedAction string         `json:"recommendedAction"` // "keep_all", "keep_best", "preserve"
	Priority          int            `json:"priority"`          // Group priority for review (1=highest)
}

// PaginationInfo contains pagination metadata
type PaginationInfo struct {
	Page        int  `json:"page"`
	PageSize    int  `json:"pageSize"`
	TotalItems  int  `json:"totalItems"`
	TotalPages  int  `json:"totalPages"`
	HasNextPage bool `json:"hasNextPage"`
	HasPrevPage bool `json:"hasPrevPage"`
}

// PaginatedReviewTorrents contains paginated review torrent data
type PaginatedReviewTorrents struct {
	Torrents        []EconomyScore   `json:"torrents"`      // Individual torrents for flat view
	Groups          [][]EconomyScore `json:"groups"`        // Legacy grouped view
	TorrentGroups   []TorrentGroup   `json:"torrentGroups"` // Enhanced grouped view with metadata
	Pagination      PaginationInfo   `json:"pagination"`
	GroupingEnabled bool             `json:"groupingEnabled"` // Whether grouping should be used in UI
}

// EconomyAnalysis represents the complete economy analysis
type EconomyAnalysis struct {
	Scores              []EconomyScore            `json:"scores"`
	Stats               EconomyStats              `json:"stats"`
	TopValuable         []EconomyScore            `json:"topValuable"`
	Duplicates          map[string][]string       `json:"duplicates"` // Map of content hash to torrent hashes
	Optimizations       []OptimizationOpportunity `json:"optimizations"`
	StorageOptimization StorageOptimization       `json:"storageOptimization"`
	ReviewTorrents      PaginatedReviewTorrents   `json:"reviewTorrents"`  // Full review torrents and groups
	ReviewThreshold     float64                   `json:"reviewThreshold"` // Threshold used for review filtering
}

// EconomyService handles torrent economy calculations
type EconomyService struct {
	syncManager *SyncManager
}

// NewEconomyService creates a new economy service
func NewEconomyService(syncManager *SyncManager) *EconomyService {
	return &EconomyService{
		syncManager: syncManager,
	}
}

// Helper functions for common operations

// createDuplicateHashSet creates a set of all duplicate hashes for quick lookup
func createDuplicateHashSet(duplicates map[string][]string) map[string]bool {
	duplicateHashes := make(map[string]bool, len(duplicates)*2) // Pre-allocate
	for primaryHash, dupHashes := range duplicates {
		duplicateHashes[primaryHash] = true
		for _, hash := range dupHashes {
			duplicateHashes[hash] = true
		}
	}
	return duplicateHashes
}

// createScoreMap creates a map from hash to score pointer for quick lookup
func createScoreMap(scores []EconomyScore) map[string]*EconomyScore {
	scoreMap := make(map[string]*EconomyScore, len(scores))
	for i := range scores {
		scoreMap[scores[i].Hash] = &scores[i]
	}
	return scoreMap
}

// normalizeStoragePath prepares storage paths for comparison
func normalizeStoragePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}

	cleaned := filepath.Clean(path)
	if cleaned == "." {
		return ""
	}

	return filepath.ToSlash(cleaned)
}

// getTorrentStorageKey returns a normalized storage key for a torrent hash
func getTorrentStorageKey(torrentDetails map[string]qbt.Torrent, hash string) string {
	if torrent, ok := torrentDetails[hash]; ok {
		if key := normalizeStoragePath(torrent.ContentPath); key != "" {
			return key
		}
		if key := normalizeStoragePath(filepath.Join(torrent.SavePath, torrent.Name)); key != "" {
			return key
		}
	}
	return ""
}

// storagePathsEqual checks if two normalized storage paths refer to the same location
func storagePathsEqual(pathA, pathB string) bool {
	if pathA == "" || pathB == "" {
		return false
	}
	return strings.EqualFold(pathA, pathB)
}

// findBestCopyInGroup finds the best copy in a duplicate group by economy score
func findBestCopyInGroup(hashes []string, scoreMap map[string]*EconomyScore) string {
	if len(hashes) == 0 {
		return ""
	}

	bestHash := hashes[0]
	bestScore := float64(-1)

	for _, hash := range hashes {
		if score := scoreMap[hash]; score != nil && score.EconomyScore > bestScore {
			bestHash = hash
			bestScore = score.EconomyScore
		}
	}

	return bestHash
}

// calculateRarityBonus calculates rarity bonus based on total seed count from tracker
func calculateRarityBonus(totalSeeds int) float64 {
	switch {
	case totalSeeds == 0:
		return 10.0 // Extremely rare
	case totalSeeds < 5:
		return 5.0 // Very rare
	case totalSeeds < 10:
		return 2.0 // Rare
	case totalSeeds < 50:
		return 1.0 // Moderately rare
	default:
		return 0.1 // Common
	}
}

// calculateSeedFactor calculates seed factor based on total seed count from tracker and other factors
func calculateSeedFactor(totalSeeds int, age int64, isDuplicate bool) float64 {
	if isDuplicate {
		if totalSeeds == 0 {
			return 1.5 // Extra bonus for being the last seed of duplicate content
		}
		return 1.0 // All live duplicates are equally valuable
	}

	// For unique torrents
	if totalSeeds == 0 {
		return 3.0 // Major bonus for being the sole remaining seed
	}

	if totalSeeds > 10 {
		// Well-seeded unique torrents get penalized (especially old ones)
		if age > 30 {
			return 0.3 // Heavy penalty for old well-seeded unique content
		} else if age > 7 {
			return 0.6 // Medium penalty for moderately old well-seeded unique content
		}
		return 0.8 // Light penalty for new well-seeded unique content
	}

	if totalSeeds > 5 {
		// Moderately seeded unique torrents get some penalty
		if age > 30 {
			return 0.5
		}
		return 0.7
	}

	// Poorly seeded unique torrents (1-5 seeds) are more valuable
	if age > 30 {
		return 0.7 // Still penalized for age, but less than well-seeded
	}
	return 1.0 // Keep at base level
}

// AnalyzeEconomy performs a complete economy analysis for an instance
func (es *EconomyService) AnalyzeEconomy(ctx context.Context, instanceID int) (*EconomyAnalysis, error) {
	return es.AnalyzeEconomyWithPagination(ctx, instanceID, 1, 10)
}

// AnalyzeEconomyWithPagination performs a complete economy analysis for an instance with pagination
func (es *EconomyService) AnalyzeEconomyWithPagination(ctx context.Context, instanceID int, page, pageSize int) (*EconomyAnalysis, error) {
	return es.AnalyzeEconomyWithPaginationAndSorting(ctx, instanceID, page, pageSize, "", false, FilterOptions{})
}

// AnalyzeEconomyWithPaginationAndSorting performs a complete economy analysis for an instance with pagination, sorting, and filtering
func (es *EconomyService) AnalyzeEconomyWithPaginationAndSorting(ctx context.Context, instanceID int, page, pageSize int, sortField string, sortDesc bool, filters FilterOptions) (*EconomyAnalysis, error) {
	// Get all torrents
	torrents, err := es.getAllTorrents(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrents: %w", err)
	}

	if len(torrents) == 0 {
		return &EconomyAnalysis{
			Scores:              []EconomyScore{},
			Stats:               EconomyStats{},
			TopValuable:         []EconomyScore{},
			Duplicates:          make(map[string][]string),
			Optimizations:       []OptimizationOpportunity{},
			StorageOptimization: StorageOptimization{},
		}, nil
	}

	// Calculate economy scores
	scores := es.calculateEconomyScores(torrents)

	// Index torrents by hash for storage path comparisons
	torrentDetails := make(map[string]qbt.Torrent, len(torrents))
	for _, torrent := range torrents {
		torrentDetails[torrent.Hash] = torrent
	}

	// Find duplicates
	duplicates := es.findDuplicates(ctx, instanceID, torrents)

	// Pre-calculate duplicate hash set for performance optimization
	duplicateHashSet := createDuplicateHashSet(duplicates)

	// Apply deduplication adjustments so duplicate metadata and scoring stay in sync
	scores = es.applyDeduplicationFactors(scores, duplicates, duplicateHashSet, torrentDetails)

	// Apply filters to scores
	scores = es.applyFiltersToScores(scores, filters)

	// Sort scores based on sortField and sortDesc
	sortedScores := es.sortScores(scores, sortField, sortDesc)

	// Calculate statistics
	stats := es.calculateStats(scores, duplicates, duplicateHashSet, torrentDetails)

	// Calculate optimization opportunities
	optimizations := es.calculateOptimizationOpportunities(scores, duplicates, duplicateHashSet, torrentDetails)

	// Calculate storage optimization data
	storageOptimization := es.calculateStorageOptimization(scores, duplicates, duplicateHashSet, torrentDetails)

	// Get top valuable torrents (configurable limit)
	topValuableLimit := 50 // Configurable limit instead of hard coded 20
	topValuable := sortedScores
	if len(topValuable) > topValuableLimit {
		topValuable = topValuable[:topValuableLimit]
	}

	// Calculate review threshold and filter review torrents
	reviewThreshold := es.calculateReviewThreshold(scores)
	reviewTorrents := es.buildReviewTorrents(scores, reviewThreshold)

	// Apply sorting to review torrents as well
	reviewTorrents = es.sortScores(reviewTorrents, sortField, sortDesc)

	// Create torrent groups
	torrentGroups := es.createTorrentGroups(reviewTorrents)
	enhancedGroups := es.createEnhancedTorrentGroups(reviewTorrents, duplicates, duplicateHashSet)

	// Create paginated review torrents
	paginatedReviewTorrents := es.CreatePaginatedReviewTorrents(reviewTorrents, torrentGroups, enhancedGroups, page, pageSize)

	return &EconomyAnalysis{
		Scores:              scores,
		Stats:               stats,
		TopValuable:         topValuable,
		Duplicates:          duplicates,
		Optimizations:       optimizations,
		StorageOptimization: storageOptimization,
		ReviewTorrents:      paginatedReviewTorrents,
		ReviewThreshold:     reviewThreshold,
	}, nil
}

// getAllTorrents gets all torrents for analysis
func (es *EconomyService) getAllTorrents(ctx context.Context, instanceID int) ([]qbt.Torrent, error) {
	// Get fresh data from sync manager
	_, syncManager, err := es.syncManager.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	// Get all torrents
	torrentFilterOptions := qbt.TorrentFilterOptions{
		Filter: qbt.TorrentFilterAll,
	}

	torrents := syncManager.GetTorrents(torrentFilterOptions)
	log.Debug().
		Int("instanceID", instanceID).
		Int("torrentCount", len(torrents)).
		Msg("Retrieved torrents for economy analysis")

	return torrents, nil
}

// calculateEconomyScores calculates economy scores for all torrents
func (es *EconomyService) calculateEconomyScores(torrents []qbt.Torrent) []EconomyScore {
	scores := make([]EconomyScore, len(torrents))

	for i, torrent := range torrents {
		scores[i] = es.calculateSingleEconomyScore(torrent)
	}

	return scores
}

// calculateSingleEconomyScore calculates the economy score for a single torrent
func (es *EconomyService) calculateSingleEconomyScore(torrent qbt.Torrent) EconomyScore {
	now := time.Now()
	addedTime := time.Unix(torrent.AddedOn, 0)
	ageInDays := int64(now.Sub(addedTime).Hours() / 24)
	lastActivityTime := time.Unix(torrent.LastActivity, 0)
	daysSinceActivity := int64(now.Sub(lastActivityTime).Hours() / 24)

	// Base storage value (size in GB)
	storageValue := float64(torrent.Size) / (1024 * 1024 * 1024)

	// Calculate retention score based on age and other factors
	retentionScore := es.calculateRetentionScore(torrent, ageInDays, daysSinceActivity)

	// Calculate rarity bonus based on total seeds from tracker
	rarityBonus := calculateRarityBonus(int(torrent.NumComplete))

	return EconomyScore{
		Hash:                torrent.Hash,
		Name:                torrent.Name,
		Size:                torrent.Size,
		Seeds:               int(torrent.NumComplete),   // Total seeds from tracker
		Peers:               int(torrent.NumIncomplete), // Total peers from tracker
		Ratio:               torrent.Ratio,
		Age:                 ageInDays,
		EconomyScore:        retentionScore,
		StorageValue:        storageValue,
		RarityBonus:         rarityBonus,
		DeduplicationFactor: 1.0,            // Will be updated later
		ReviewPriority:      retentionScore, // Use economy score for review priority
		Tracker:             torrent.Tracker,
		State:               string(torrent.State),
		Category:            torrent.Category,
		LastActivity:        torrent.LastActivity,
	}
}

// calculateRetentionScore calculates how long content should be retained
func (es *EconomyService) calculateRetentionScore(torrent qbt.Torrent, ageInDays, daysSinceActivity int64) float64 {
	// Base retention score starts high for new content
	baseRetention := 100.0

	// Age factor: content loses retention value over time
	ageFactor := 1.0
	if ageInDays > 7 {
		// Gradual decline after 1 week
		ageFactor = math.Max(0.1, math.Pow(0.98, float64(ageInDays-7)))
	}

	// Activity factor: recent activity increases retention value
	activityBonus := 1.0
	if daysSinceActivity < 1 {
		activityBonus = 2.0 // Very recent activity
	} else if daysSinceActivity < 7 {
		activityBonus = 1.5 // Recent activity
	} else if daysSinceActivity < 30 {
		activityBonus = 1.2 // Somewhat recent
	} else if daysSinceActivity > 90 {
		activityBonus = 0.5 // Very old activity
	}

	// Ratio factor: better ratio = higher retention
	ratioFactor := 1.0
	if torrent.Ratio > 2.0 {
		ratioFactor = 1.3 // Excellent ratio
	} else if torrent.Ratio > 1.0 {
		ratioFactor = 1.1 // Good ratio
	} else if torrent.Ratio < 0.3 {
		ratioFactor = 0.7 // Poor ratio
	}

	// Calculate base retention score
	return baseRetention * ageFactor * activityBonus * ratioFactor
}

// findDuplicates finds duplicate content based on name similarity only (bypassing file overlap for performance)
func (es *EconomyService) findDuplicates(ctx context.Context, instanceID int, torrents []qbt.Torrent) map[string][]string {
	duplicates := make(map[string][]string)

	// Group by normalized name first (for performance)
	contentGroups := make(map[string][]qbt.Torrent)

	for _, torrent := range torrents {
		// Normalize name for comparison
		normalizedName := es.normalizeContentName(torrent.Name)
		contentGroups[normalizedName] = append(contentGroups[normalizedName], torrent)
	}

	// For groups with multiple torrents, verify file overlap before marking as duplicates
	for _, group := range contentGroups {
		if len(group) > 1 {
			// Sort group by size (prefer larger/older torrents as primary)
			sort.Slice(group, func(i, j int) bool {
				if group[i].Size != group[j].Size {
					return group[i].Size > group[j].Size
				}
				addedTimeI := time.Unix(group[i].AddedOn, 0)
				addedTimeJ := time.Unix(group[j].AddedOn, 0)
				return addedTimeI.Before(addedTimeJ)
			})

			// Get file information for all torrents in this group
			fileInfos := es.getBatchTorrentFiles(ctx, instanceID, group)
			if len(fileInfos) == 0 {
				continue // Skip if we can't get file info
			}

			// Use first torrent as potential primary
			primary := group[0]
			primaryFiles, hasPrimaryFiles := fileInfos[primary.Hash]
			if !hasPrimaryFiles {
				continue // Skip if primary has no file info
			}

			var trueDuplicates []qbt.Torrent

			// Check each other torrent against the primary for file overlap
			for i := 1; i < len(group); i++ {
				if dupFiles, hasDupFiles := fileInfos[group[i].Hash]; hasDupFiles {
					if es.hasSignificantFileOverlap(primaryFiles, dupFiles) {
						trueDuplicates = append(trueDuplicates, group[i])
					}
				}
			}

			// Only mark as duplicates if we found actual file overlap
			if len(trueDuplicates) > 0 {
				var dupHashes []string
				for _, dup := range trueDuplicates {
					dupHashes = append(dupHashes, dup.Hash)
				}
				duplicates[primary.Hash] = dupHashes
			}
		}
	}

	log.Debug().
		Int("duplicateGroups", len(duplicates)).
		Msg("Found duplicate content groups with file verification")

	return duplicates
}

// getBatchTorrentFiles gets file information for multiple torrents efficiently
func (es *EconomyService) getBatchTorrentFiles(ctx context.Context, instanceID int, torrents []qbt.Torrent) map[string]qbt.TorrentFiles {
	fileInfos := make(map[string]qbt.TorrentFiles)

	// Get client once
	client, _, err := es.syncManager.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get client for batch file retrieval")
		return fileInfos
	}

	// Get files for each torrent
	for _, torrent := range torrents {
		files, err := client.GetFilesInformationCtx(ctx, torrent.Hash)
		if err != nil {
			log.Warn().Err(err).Str("hash", torrent.Hash).Msg("Failed to get files for torrent, skipping")
			continue
		}
		fileInfos[torrent.Hash] = *files
	}

	return fileInfos
}

// findFileOverlaps compares file lists between torrents to find actual duplicates
func (es *EconomyService) findFileOverlaps(fileInfos map[string]qbt.TorrentFiles, torrents []qbt.Torrent) map[string][]string {
	duplicates := make(map[string][]string)

	if len(torrents) < 2 {
		return duplicates
	}

	// Compare each pair of torrents
	for i := 0; i < len(torrents)-1; i++ {
		for j := i + 1; j < len(torrents); j++ {
			torrentA := torrents[i]
			torrentB := torrents[j]

			filesA, existsA := fileInfos[torrentA.Hash]
			filesB, existsB := fileInfos[torrentB.Hash]

			if !existsA || !existsB {
				continue
			}

			// Check if these torrents have significant file overlap
			if es.hasSignificantFileOverlap(filesA, filesB) {
				// Add to duplicates map
				if _, exists := duplicates[torrentA.Hash]; !exists {
					duplicates[torrentA.Hash] = []string{}
				}
				duplicates[torrentA.Hash] = append(duplicates[torrentA.Hash], torrentB.Hash)
			}
		}
	}

	return duplicates
}

// hasSignificantFileOverlap checks if two torrent file lists have significant overlap
func (es *EconomyService) hasSignificantFileOverlap(filesA, filesB qbt.TorrentFiles) bool {
	if len(filesA) == 0 || len(filesB) == 0 {
		return false
	}

	// Create maps for quick lookup
	fileMapA := make(map[string]int64)
	fileMapB := make(map[string]int64)

	for _, file := range filesA {
		normalizedPath := es.normalizeFilePath(file.Name)
		fileMapA[normalizedPath] = file.Size
	}

	for _, file := range filesB {
		normalizedPath := es.normalizeFilePath(file.Name)
		fileMapB[normalizedPath] = file.Size
	}

	// Count matching files (same path and size)
	matchingFiles := 0
	totalFilesA := len(fileMapA)

	for path, sizeA := range fileMapA {
		if sizeB, exists := fileMapB[path]; exists && sizeA == sizeB {
			matchingFiles++
		}
	}

	// Consider them duplicates if they have significant overlap
	overlapRatio := float64(matchingFiles) / float64(totalFilesA)
	minOverlap := 0.8
	if len(fileMapA) > 1 {
		minOverlap = 0.6
	}

	return overlapRatio >= minOverlap
}

// normalizeFilePath normalizes a file path for comparison
func (es *EconomyService) normalizeFilePath(path string) string {
	// Remove leading slashes and normalize separators
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimPrefix(path, "\\")
	path = strings.ReplaceAll(path, "\\", "/")
	return strings.ToLower(path)
}

// mergeUniqueHashes merges two slices of hashes, removing duplicates
func (es *EconomyService) mergeUniqueHashes(a, b []string) []string {
	hashSet := make(map[string]bool)
	result := make([]string, 0, len(a)+len(b))

	// Add all from a
	for _, hash := range a {
		if !hashSet[hash] {
			hashSet[hash] = true
			result = append(result, hash)
		}
	}

	// Add all from b
	for _, hash := range b {
		if !hashSet[hash] {
			hashSet[hash] = true
			result = append(result, hash)
		}
	}

	return result
}

// normalizeContentName normalizes a torrent name for duplicate detection
func (es *EconomyService) normalizeContentName(name string) string {
	// Remove common patterns
	name = strings.ToLower(name)

	// Remove quality indicators
	patterns := []string{
		"\\[.*?\\]", "\\(.*?\\)", "1080p", "720p", "480p", "2160p", "4k",
		"bluray", "webrip", "hdtv", "x264", "x265", "hevc", "aac", "ac3",
		"mp4", "mkv", "avi", "s01e", "s02e", "s03e", "season", "episode",
		"complete", "collection", "pack", "batch",
	}

	for _, pattern := range patterns {
		name = strings.ReplaceAll(name, pattern, "")
	}

	// Remove extra spaces and punctuation
	fields := strings.FieldsFunc(name, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ' ')
	})
	name = strings.Join(fields, " ")

	return strings.ToLower(name)
}

// applyDeduplicationFactors updates economy scores based on duplicates
func (es *EconomyService) applyDeduplicationFactors(scores []EconomyScore, duplicates map[string][]string, duplicateHashSet map[string]bool, torrentDetails map[string]qbt.Torrent) []EconomyScore {
	if len(scores) == 0 {
		return scores
	}

	// Pre-calculate shared data structures
	scoreMap := createScoreMap(scores)
	// duplicateHashSet is now passed as parameter instead of computed here

	// Apply seed factors and duplicate bonuses
	es.applySeedFactors(scores, duplicateHashSet)

	// Handle duplicate groupings for storage optimization
	es.processDuplicateGroups(scores, duplicates, scoreMap, torrentDetails)

	// Set review priority for unique torrents
	es.setUniqueTorrentReviewPriorities(scores, duplicateHashSet)

	return scores
}

// applySeedFactors applies seed factors based on duplicate status
func (es *EconomyService) applySeedFactors(scores []EconomyScore, duplicateHashSet map[string]bool) {
	for i := range scores {
		score := &scores[i]
		isDuplicate := duplicateHashSet[score.Hash]

		// Apply seed factor
		seedFactor := calculateSeedFactor(score.Seeds, score.Age, isDuplicate)

		if isDuplicate {
			// Duplicates get a significant bonus for being "free" storage
			score.EconomyScore = score.EconomyScore * seedFactor * 2.5
		} else {
			score.EconomyScore = score.EconomyScore * seedFactor
		}
	}
}

// processDuplicateGroups handles duplicate groupings for storage optimization
func (es *EconomyService) processDuplicateGroups(scores []EconomyScore, duplicates map[string][]string, scoreMap map[string]*EconomyScore, torrentDetails map[string]qbt.Torrent) {
	for primaryHash, duplicateHashes := range duplicates {
		if _, exists := scoreMap[primaryHash]; !exists {
			continue
		}

		// Find the best copy in this duplicate group
		allHashes := append([]string{primaryHash}, duplicateHashes...)
		bestHash := findBestCopyInGroup(allHashes, scoreMap)
		bestStorageKey := getTorrentStorageKey(torrentDetails, bestHash)

		// Mark the best copy as the "keeper" and others as potential removes
		for _, hash := range allHashes {
			if score := scoreMap[hash]; score != nil {
				if hash == bestHash {
					// Best copy is the keeper - no deduplication savings from this torrent
					score.DeduplicationFactor = 0.0
					score.Duplicates = make([]string, 0, len(allHashes)-1)
					for _, h := range allHashes {
						if h != bestHash {
							score.Duplicates = append(score.Duplicates, h)
						}
					}
					score.ReviewPriority = score.EconomyScore
				} else {
					storageKey := getTorrentStorageKey(torrentDetails, hash)
					sharesStorage := storagePathsEqual(bestStorageKey, storageKey)

					if sharesStorage {
						// Shares underlying files with the keeper - removing it won't free space
						score.DeduplicationFactor = 0.0
						score.ReviewPriority = score.EconomyScore
					} else {
						// Other copies are marked for potential storage optimization
						// 100% of their storage can be saved by removing them
						score.DeduplicationFactor = 1.0
						score.ReviewPriority = score.EconomyScore * 0.95
					}

					// Populate duplicates array
					score.Duplicates = make([]string, 0, len(allHashes)-1)
					for _, h := range allHashes {
						if h != hash {
							score.Duplicates = append(score.Duplicates, h)
						}
					}
				}
			}
		}
	}
}

// setUniqueTorrentReviewPriorities sets review priority for unique torrents
func (es *EconomyService) setUniqueTorrentReviewPriorities(scores []EconomyScore, duplicateHashSet map[string]bool) {
	for i := range scores {
		score := &scores[i]
		if !duplicateHashSet[score.Hash] {
			// Unique torrents have no deduplication potential
			score.DeduplicationFactor = 0.0
			score.ReviewPriority = score.EconomyScore
		}
	}
}

// calculateStats calculates aggregated economy statistics
func (es *EconomyService) calculateStats(scores []EconomyScore, duplicates map[string][]string, duplicateHashSet map[string]bool, torrentDetails map[string]qbt.Torrent) EconomyStats {
	if len(scores) == 0 {
		return EconomyStats{}
	}

	// Pre-calculate shared data structures
	scoreMap := createScoreMap(scores)

	var totalStorage int64
	var totalEconomyScore float64
	var highValueCount int
	var rareContentCount int
	var wellSeededOldCount int

	// Calculate deduplicated storage using the same logic as storage optimization
	deduplicatedStorage := es.calculateDeduplicatedStorage(scores, duplicates, scoreMap, duplicateHashSet, torrentDetails)

	// Calculate stats in a single pass
	for _, score := range scores {
		totalStorage += score.Size
		totalEconomyScore += score.EconomyScore

		if score.EconomyScore > 50.0 {
			highValueCount++
		}

		if score.Seeds < 5 {
			rareContentCount++
		}

		if score.Seeds > 10 && score.Age > 30 {
			wellSeededOldCount++
		}
	}

	storageSavings := totalStorage - deduplicatedStorage

	return EconomyStats{
		TotalTorrents:        len(scores),
		TotalStorage:         totalStorage,
		DeduplicatedStorage:  deduplicatedStorage,
		StorageSavings:       storageSavings,
		AverageEconomyScore:  totalEconomyScore / float64(len(scores)),
		HighValueTorrents:    highValueCount,
		RareContentCount:     rareContentCount,
		WellSeededOldContent: wellSeededOldCount,
	}
}

// calculateDeduplicatedStorage calculates the storage used if we keep only the best copy of each duplicate group
func (es *EconomyService) calculateDeduplicatedStorage(scores []EconomyScore, duplicates map[string][]string, scoreMap map[string]*EconomyScore, duplicateHashSet map[string]bool, torrentDetails map[string]qbt.Torrent) int64 {
	countedHashes := make(map[string]bool)

	// Add all non-duplicates
	for _, score := range scores {
		if !duplicateHashSet[score.Hash] {
			countedHashes[score.Hash] = true
		}
	}

	// For each duplicate group, add only the best copy
	for primaryHash, dupHashes := range duplicates {
		allHashes := append([]string{primaryHash}, dupHashes...)
		bestHash := findBestCopyInGroup(allHashes, scoreMap)
		bestStorageKey := getTorrentStorageKey(torrentDetails, bestHash)
		countedHashes[bestHash] = true

		// Include any copies that share the same storage as the keeper
		for _, hash := range allHashes {
			if hash == bestHash {
				continue
			}
			storageKey := getTorrentStorageKey(torrentDetails, hash)
			if storagePathsEqual(bestStorageKey, storageKey) {
				countedHashes[hash] = true
			}
		}
	}

	// Calculate total deduplicated storage
	var deduplicatedStorage int64
	for _, score := range scores {
		if countedHashes[score.Hash] {
			deduplicatedStorage += score.Size
		}
	}

	return deduplicatedStorage
}

// calculateOptimizationOpportunities identifies specific optimization opportunities
func (es *EconomyService) calculateOptimizationOpportunities(scores []EconomyScore, duplicates map[string][]string, duplicateHashSet map[string]bool, torrentDetails map[string]qbt.Torrent) []OptimizationOpportunity {
	var opportunities []OptimizationOpportunity

	// Pre-calculate shared data structures
	scoreMap := createScoreMap(scores)
	// duplicateHashSet is now passed as parameter instead of computed here

	// 1. Duplicate removal opportunities
	if dupOp := es.createDuplicateRemovalOpportunity(scores, duplicates, scoreMap, torrentDetails); dupOp != nil {
		opportunities = append(opportunities, *dupOp)
	}

	// 2. Old well-seeded unique content cleanup
	if oldOp := es.createOldContentCleanupOpportunity(scores, duplicateHashSet); oldOp != nil {
		opportunities = append(opportunities, *oldOp)
	}

	// 3. Ratio optimization opportunities
	if ratioOp := es.createRatioOptimizationOpportunity(scores); ratioOp != nil {
		opportunities = append(opportunities, *ratioOp)
	}

	// 4. Unused content opportunities
	if unusedOp := es.createUnusedContentOpportunity(scores); unusedOp != nil {
		opportunities = append(opportunities, *unusedOp)
	}

	// 5. Critical preservation - torrents where we're the last seed
	if lastSeedOp := es.createLastSeedOpportunity(scores); lastSeedOp != nil {
		opportunities = append(opportunities, *lastSeedOp)
	}

	// 6. High-value content preservation
	if highValueOp := es.createHighValueOpportunity(scores, duplicateHashSet); highValueOp != nil {
		opportunities = append(opportunities, *highValueOp)
	}

	// Sort by impact (highest first)
	sort.Slice(opportunities, func(i, j int) bool {
		return opportunities[i].Impact > opportunities[j].Impact
	})

	return opportunities
}

// createDuplicateRemovalOpportunity creates duplicate removal optimization opportunity
func (es *EconomyService) createDuplicateRemovalOpportunity(scores []EconomyScore, duplicates map[string][]string, scoreMap map[string]*EconomyScore, torrentDetails map[string]qbt.Torrent) *OptimizationOpportunity {
	if len(duplicates) == 0 {
		return nil
	}

	var duplicateHashesToRemove []string
	var totalSavings int64

	for primaryHash, dupHashes := range duplicates {
		allHashes := append([]string{primaryHash}, dupHashes...)
		bestHash := findBestCopyInGroup(allHashes, scoreMap)
		bestStorageKey := getTorrentStorageKey(torrentDetails, bestHash)

		// Remove all copies except the best one
		for _, hash := range allHashes {
			if hash != bestHash {
				storageKey := getTorrentStorageKey(torrentDetails, hash)
				if storagePathsEqual(bestStorageKey, storageKey) {
					// Shares the same files as the keeper - no storage reclaimed by removing it
					continue
				}
				if score := scoreMap[hash]; score != nil {
					duplicateHashesToRemove = append(duplicateHashesToRemove, hash)
					totalSavings += score.Size
				}
			}
		}
	}

	if len(duplicateHashesToRemove) == 0 {
		return nil
	}

	return &OptimizationOpportunity{
		Type:        "cross_seeding_opportunity",
		Title:       "Remove Duplicate Content",
		Description: fmt.Sprintf("Remove %d duplicate torrents while keeping the most valuable copy of each content group", len(duplicateHashesToRemove)),
		Priority:    "high",
		Savings:     totalSavings,
		Impact:      85.0,
		Torrents:    duplicateHashesToRemove,
	}
}

// createOldContentCleanupOpportunity creates old content cleanup optimization opportunity
func (es *EconomyService) createOldContentCleanupOpportunity(scores []EconomyScore, duplicateHashSet map[string]bool) *OptimizationOpportunity {
	var oldWellSeededHashes []string
	var oldWellSeededSize int64

	for _, score := range scores {
		if !duplicateHashSet[score.Hash] && score.Seeds > 10 && score.Age > 60 && score.EconomyScore < 30.0 {
			oldWellSeededHashes = append(oldWellSeededHashes, score.Hash)
			oldWellSeededSize += score.Size
		}
	}

	if len(oldWellSeededHashes) == 0 {
		return nil
	}

	savings := int64(float64(oldWellSeededSize) * 0.8)
	return &OptimizationOpportunity{
		Type:        "old_content_cleanup",
		Title:       "Clean Up Old Well-Seeded Unique Content",
		Description: fmt.Sprintf("Remove %d old, well-seeded unique torrents that are easily replaceable and have low retention value", len(oldWellSeededHashes)),
		Priority:    "high",
		Savings:     savings,
		Impact:      75.0,
		Torrents:    oldWellSeededHashes,
	}
}

// createRatioOptimizationOpportunity creates ratio optimization opportunity
func (es *EconomyService) createRatioOptimizationOpportunity(scores []EconomyScore) *OptimizationOpportunity {
	var lowRatioHashes []string
	var lowRatioSize int64

	for _, score := range scores {
		if score.Ratio < 0.5 && score.State == "seeding" && score.Age > 7 {
			lowRatioHashes = append(lowRatioHashes, score.Hash)
			lowRatioSize += score.Size
		}
	}

	if len(lowRatioHashes) == 0 {
		return nil
	}

	savings := int64(float64(lowRatioSize) * 0.6)
	return &OptimizationOpportunity{
		Type:        "ratio_optimization",
		Title:       "Optimize Low-Ratio Torrents",
		Description: fmt.Sprintf("Consider removing or reseeding %d torrents with poor upload/download ratios", len(lowRatioHashes)),
		Priority:    "medium",
		Savings:     savings,
		Impact:      55.0,
		Torrents:    lowRatioHashes,
	}
}

// createUnusedContentOpportunity creates unused content opportunity
func (es *EconomyService) createUnusedContentOpportunity(scores []EconomyScore) *OptimizationOpportunity {
	var unusedHashes []string
	var unusedSize int64

	for _, score := range scores {
		if score.State == "paused" && score.LastActivity == 0 && score.Age > 30 {
			unusedHashes = append(unusedHashes, score.Hash)
			unusedSize += score.Size
		}
	}

	if len(unusedHashes) == 0 {
		return nil
	}

	savings := int64(float64(unusedSize) * 0.9)
	return &OptimizationOpportunity{
		Type:        "unused_content_cleanup",
		Title:       "Remove Unused Content",
		Description: fmt.Sprintf("Remove %d paused torrents that have never been active", len(unusedHashes)),
		Priority:    "low",
		Savings:     savings,
		Impact:      75.0,
		Torrents:    unusedHashes,
	}
}

// createLastSeedOpportunity creates last seed preservation opportunity
func (es *EconomyService) createLastSeedOpportunity(scores []EconomyScore) *OptimizationOpportunity {
	var lastSeedHashes []string
	var lastSeedSize int64

	for _, score := range scores {
		if score.Seeds == 0 {
			lastSeedHashes = append(lastSeedHashes, score.Hash)
			lastSeedSize += score.Size
		}
	}

	if len(lastSeedHashes) == 0 {
		return nil
	}

	return &OptimizationOpportunity{
		Type:        "preserve_last_seed",
		Title:       "CRITICAL: Preserve Torrents Where We're The Last Seed",
		Description: fmt.Sprintf("NEVER REMOVE: %d torrents where we are the sole remaining seeder - removing these would make the content permanently unavailable", len(lastSeedHashes)),
		Priority:    "critical",
		Savings:     -lastSeedSize,
		Impact:      100.0,
		Torrents:    lastSeedHashes,
	}
}

// createHighValueOpportunity creates high-value content preservation opportunity
func (es *EconomyService) createHighValueOpportunity(scores []EconomyScore, duplicateHashSet map[string]bool) *OptimizationOpportunity {
	var highValueHashes []string
	var highValueSize int64

	for _, score := range scores {
		isDuplicate := duplicateHashSet[score.Hash]
		isLastSeed := score.Seeds == 0

		if (isDuplicate && score.EconomyScore > 50.0) ||
			(!isDuplicate && score.EconomyScore > 60.0 && score.Seeds < 5) ||
			isLastSeed {
			highValueHashes = append(highValueHashes, score.Hash)
			highValueSize += score.Size
		}
	}

	if len(highValueHashes) == 0 {
		return nil
	}

	return &OptimizationOpportunity{
		Type:        "preserve_rare_content",
		Title:       "Preserve Critical Content",
		Description: fmt.Sprintf("Ensure %d critical torrents (duplicates, rare unique content, and torrents where we're the last seed) are properly seeded and backed up", len(highValueHashes)),
		Priority:    "high",
		Savings:     -highValueSize,
		Impact:      95.0,
		Torrents:    highValueHashes,
	}
}

// calculateStorageOptimization calculates comprehensive storage optimization data
func (es *EconomyService) calculateStorageOptimization(scores []EconomyScore, duplicates map[string][]string, duplicateHashSet map[string]bool, torrentDetails map[string]qbt.Torrent) StorageOptimization {
	// Pre-calculate shared data structures
	scoreMap := createScoreMap(scores)
	// duplicateHashSet is now passed as parameter instead of computed here

	var deduplicationSavings int64
	var oldContentCleanupSavings int64
	var ratioOptimizationSavings int64
	var unusedContentSavings int64

	// Calculate deduplication savings
	deduplicationSavings = es.calculateDeduplicationSavings(duplicates, scoreMap, torrentDetails)

	// Calculate old content cleanup savings
	oldContentCleanupSavings = es.calculateOldContentCleanupSavings(scores, duplicateHashSet)

	// Calculate ratio optimization savings
	ratioOptimizationSavings = es.calculateRatioOptimizationSavings(scores)

	// Calculate unused content savings
	unusedContentSavings = es.calculateUnusedContentSavings(scores)

	totalPotentialSavings := deduplicationSavings + oldContentCleanupSavings + ratioOptimizationSavings + unusedContentSavings

	return StorageOptimization{
		TotalPotentialSavings:    totalPotentialSavings,
		DeduplicationSavings:     deduplicationSavings,
		OldContentCleanupSavings: oldContentCleanupSavings,
		RatioOptimizationSavings: ratioOptimizationSavings,
		UnusedContentSavings:     unusedContentSavings,
	}
}

// calculateDeduplicationSavings calculates savings from removing duplicate content
func (es *EconomyService) calculateDeduplicationSavings(duplicates map[string][]string, scoreMap map[string]*EconomyScore, torrentDetails map[string]qbt.Torrent) int64 {
	var savings int64

	for primaryHash, dupHashes := range duplicates {
		allHashes := append([]string{primaryHash}, dupHashes...)
		bestHash := findBestCopyInGroup(allHashes, scoreMap)
		bestStorageKey := getTorrentStorageKey(torrentDetails, bestHash)

		// Calculate savings from removing all copies except the best one
		for _, hash := range allHashes {
			if hash != bestHash {
				storageKey := getTorrentStorageKey(torrentDetails, hash)
				if storagePathsEqual(bestStorageKey, storageKey) {
					// Removing this torrent won't reclaim disk space because it shares storage with the keeper
					continue
				}
				if score := scoreMap[hash]; score != nil {
					savings += score.Size
				}
			}
		}
	}

	return savings
}

// calculateOldContentCleanupSavings calculates savings from cleaning up old well-seeded content
func (es *EconomyService) calculateOldContentCleanupSavings(scores []EconomyScore, duplicateHashSet map[string]bool) int64 {
	var savings int64

	for _, score := range scores {
		if !duplicateHashSet[score.Hash] && score.Seeds > 10 && score.Age > 60 && score.EconomyScore < 30.0 {
			savings += score.Size
		}
	}

	return savings
}

// calculateRatioOptimizationSavings calculates savings from ratio optimization
func (es *EconomyService) calculateRatioOptimizationSavings(scores []EconomyScore) int64 {
	var savings int64

	for _, score := range scores {
		if score.Ratio < 0.5 && score.State == "seeding" && score.Age > 7 {
			savings += score.Size
		}
	}

	return savings
}

// calculateUnusedContentSavings calculates savings from removing unused content
func (es *EconomyService) calculateUnusedContentSavings(scores []EconomyScore) int64 {
	var savings int64

	for _, score := range scores {
		if score.State == "paused" && score.LastActivity == 0 && score.Age > 30 {
			savings += score.Size
		}
	}

	return savings
} // formatBytes formats bytes into human readable format
func (es *EconomyService) formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// calculateReviewThreshold calculates the dynamic threshold for torrents needing review
func (es *EconomyService) calculateReviewThreshold(scores []EconomyScore) float64 {
	if len(scores) == 0 {
		return 50.0 // Default fallback for retention scores
	}

	// Calculate threshold as the 25th percentile of economy scores
	// This ensures we focus on the worst 25% of torrents (lowest retention scores)
	// Reduced from 40% to improve performance and focus on truly problematic torrents
	sortedScores := make([]float64, len(scores))
	for i, score := range scores {
		sortedScores[i] = score.EconomyScore
	}
	sort.Float64s(sortedScores)

	// 25th percentile (bottom 25% lowest retention scores)
	thresholdIndex := int(float64(len(sortedScores)) * 0.25)
	if thresholdIndex >= len(sortedScores) {
		thresholdIndex = len(sortedScores) - 1
	}

	threshold := sortedScores[thresholdIndex]

	// Ensure threshold is reasonable for the new scoring system
	if threshold < 15.0 {
		threshold = 15.0 // Low retention for unique torrents
	} else if threshold > 100.0 {
		threshold = 100.0 // High retention (shouldn't happen with new penalties)
	}

	return threshold
}

// buildReviewTorrents builds the filtered and sorted list of torrents needing review
func (es *EconomyService) buildReviewTorrents(scores []EconomyScore, threshold float64) []EconomyScore {
	// Filter torrents that need review
	var reviewCandidates []EconomyScore
	for _, score := range scores {
		if score.EconomyScore < threshold {
			reviewCandidates = append(reviewCandidates, score)
		}
	}

	// Limit the number of review candidates to prevent performance issues
	// Keep only the worst performing torrents (lowest economy scores)
	maxReviewTorrents := 500 // Hard limit to prevent performance issues
	if len(reviewCandidates) > maxReviewTorrents {
		// Sort by economy score (lowest first) and keep only the worst
		sort.Slice(reviewCandidates, func(i, j int) bool {
			return reviewCandidates[i].EconomyScore < reviewCandidates[j].EconomyScore
		})
		reviewCandidates = reviewCandidates[:maxReviewTorrents]
	}

	// Sort by review priority (lowest first = highest priority)
	sort.Slice(reviewCandidates, func(i, j int) bool {
		if reviewCandidates[i].ReviewPriority != reviewCandidates[j].ReviewPriority {
			return reviewCandidates[i].ReviewPriority < reviewCandidates[j].ReviewPriority
		}
		// Secondary sort: oldest content first (higher age = more likely to need review)
		return reviewCandidates[i].Age > reviewCandidates[j].Age
	})

	// Remove duplicates from the list (keep only the first occurrence of each hash)
	seenHashes := make(map[string]bool)
	var reviewTorrents []EconomyScore

	for _, torrent := range reviewCandidates {
		if !seenHashes[torrent.Hash] {
			reviewTorrents = append(reviewTorrents, torrent)
			seenHashes[torrent.Hash] = true
		}
	}

	return reviewTorrents
}

// createTorrentGroups groups torrents by their duplicate relationships for review
func (es *EconomyService) createTorrentGroups(reviewTorrents []EconomyScore) [][]EconomyScore {
	var groups [][]EconomyScore
	processed := make(map[string]bool)

	// Create a quick lookup map for review torrents
	reviewTorrentMap := make(map[string]EconomyScore)
	for _, torrent := range reviewTorrents {
		reviewTorrentMap[torrent.Hash] = torrent
	}

	for _, torrent := range reviewTorrents {
		if processed[torrent.Hash] {
			continue
		}

		var group []EconomyScore
		group = append(group, torrent)
		processed[torrent.Hash] = true

		// Add all duplicates of this torrent that are also in review torrents
		if len(torrent.Duplicates) > 0 {
			for _, dupHash := range torrent.Duplicates {
				if dupTorrent, exists := reviewTorrentMap[dupHash]; exists && !processed[dupHash] {
					group = append(group, dupTorrent)
					processed[dupHash] = true
				}
			}
		}

		// Also check if this torrent is listed as a duplicate of others
		// This handles cases where the duplicate relationship might not be bidirectional in the data
		for _, reviewTorrent := range reviewTorrents {
			if processed[reviewTorrent.Hash] {
				continue
			}
			if reviewTorrent.Duplicates != nil {
				for _, dupHash := range reviewTorrent.Duplicates {
					if dupHash == torrent.Hash {
						group = append(group, reviewTorrent)
						processed[reviewTorrent.Hash] = true
						break
					}
				}
			}
		}

		// Sort group by review priority (lowest first = highest priority for review)
		// Then by economy score (highest first = most valuable)
		sort.Slice(group, func(i, j int) bool {
			if group[i].ReviewPriority != group[j].ReviewPriority {
				return group[i].ReviewPriority < group[j].ReviewPriority
			}
			return group[i].EconomyScore > group[j].EconomyScore
		})

		groups = append(groups, group)
	}

	// Sort groups by the priority of their highest-priority member (lowest review priority first)
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i]) == 0 || len(groups[j]) == 0 {
			return len(groups[i]) > len(groups[j])
		}
		// Compare by the most urgent torrent in each group
		return groups[i][0].ReviewPriority < groups[j][0].ReviewPriority
	})

	return groups
}

// createEnhancedTorrentGroups creates enhanced torrent groups with metadata for the frontend
func (es *EconomyService) createEnhancedTorrentGroups(reviewTorrents []EconomyScore, duplicates map[string][]string, duplicateHashSet map[string]bool) []TorrentGroup {
	var enhancedGroups []TorrentGroup
	processed := make(map[string]bool)
	groupID := 1

	// Pre-calculate shared data structures
	reviewTorrentMap := createScoreMap(reviewTorrents)
	// duplicateHashSet is now passed as parameter instead of computed here

	for _, torrent := range reviewTorrents {
		if processed[torrent.Hash] {
			continue
		}

		var groupTorrents []EconomyScore
		groupTorrents = append(groupTorrents, torrent)
		processed[torrent.Hash] = true

		// Add all duplicates of this torrent that are also in review torrents
		if len(torrent.Duplicates) > 0 {
			for _, dupHash := range torrent.Duplicates {
				if dupTorrent := reviewTorrentMap[dupHash]; dupTorrent != nil && !processed[dupHash] {
					groupTorrents = append(groupTorrents, *dupTorrent)
					processed[dupHash] = true
				}
			}
		}

		// Also check if this torrent is listed as a duplicate of others
		for _, reviewTorrent := range reviewTorrents {
			if processed[reviewTorrent.Hash] {
				continue
			}
			if reviewTorrent.Duplicates != nil {
				for _, dupHash := range reviewTorrent.Duplicates {
					if dupHash == torrent.Hash {
						groupTorrents = append(groupTorrents, reviewTorrent)
						processed[reviewTorrent.Hash] = true
						break
					}
				}
			}
		}

		// Sort group members by economy score (highest first = most valuable)
		sort.Slice(groupTorrents, func(i, j int) bool {
			if groupTorrents[i].EconomyScore != groupTorrents[j].EconomyScore {
				return groupTorrents[i].EconomyScore > groupTorrents[j].EconomyScore
			}
			return groupTorrents[i].ReviewPriority < groupTorrents[j].ReviewPriority
		})

		// Determine group type and recommended action
		groupType, recommendedAction := es.determineGroupTypeAndAction(groupTorrents, duplicateHashSet)

		// Calculate sizes and savings
		totalSize := es.calculateGroupTotalSize(groupTorrents)
		deduplicatedSize := groupTorrents[0].Size // Size of the best (first) torrent
		potentialSavings := totalSize - deduplicatedSize
		if potentialSavings < 0 {
			potentialSavings = 0
		}

		// Create the enhanced group
		enhancedGroup := TorrentGroup{
			ID:                fmt.Sprintf("group_%d", groupID),
			Torrents:          groupTorrents,
			PrimaryTorrent:    groupTorrents[0], // Best torrent is first after sorting
			GroupType:         groupType,
			TotalSize:         totalSize,
			DeduplicatedSize:  deduplicatedSize,
			PotentialSavings:  potentialSavings,
			RecommendedAction: recommendedAction,
			Priority:          int(groupTorrents[0].ReviewPriority), // Use best torrent's priority
		}

		enhancedGroups = append(enhancedGroups, enhancedGroup)
		groupID++
	}

	// Sort groups by priority (lowest priority value = highest urgency)
	es.sortEnhancedGroupsByPriority(enhancedGroups)

	// Update priority numbers to be sequential
	for i := range enhancedGroups {
		enhancedGroups[i].Priority = i + 1
	}

	return enhancedGroups
}

// determineGroupTypeAndAction determines the group type and recommended action
func (es *EconomyService) determineGroupTypeAndAction(groupTorrents []EconomyScore, duplicateHashSet map[string]bool) (string, string) {
	groupType := "unique"
	recommendedAction := "review"
	hasLastSeed := false

	for _, t := range groupTorrents {
		if t.Seeds == 0 {
			hasLastSeed = true
			break
		}
	}

	if len(groupTorrents) > 1 {
		groupType = "duplicate"
		if hasLastSeed {
			recommendedAction = "preserve"
		} else {
			recommendedAction = "keep_best"
		}
	} else if hasLastSeed {
		groupType = "last_seed"
		recommendedAction = "preserve"
	} else if duplicateHashSet[groupTorrents[0].Hash] {
		groupType = "duplicate"
		recommendedAction = "keep_best"
	}

	return groupType, recommendedAction
}

// calculateGroupTotalSize calculates the total size of all torrents in a group
func (es *EconomyService) calculateGroupTotalSize(groupTorrents []EconomyScore) int64 {
	var totalSize int64
	for _, t := range groupTorrents {
		totalSize += t.Size
	}
	return totalSize
}

// sortEnhancedGroupsByPriority sorts enhanced groups by priority
func (es *EconomyService) sortEnhancedGroupsByPriority(enhancedGroups []TorrentGroup) {
	sort.Slice(enhancedGroups, func(i, j int) bool {
		// Last seed groups get highest priority
		if enhancedGroups[i].GroupType == "last_seed" && enhancedGroups[j].GroupType != "last_seed" {
			return true
		}
		if enhancedGroups[i].GroupType != "last_seed" && enhancedGroups[j].GroupType == "last_seed" {
			return false
		}
		// Then by review priority
		return enhancedGroups[i].Priority < enhancedGroups[j].Priority
	})
}

// CreatePaginatedReviewTorrents creates a properly paginated PaginatedReviewTorrents structure
func (es *EconomyService) CreatePaginatedReviewTorrents(allTorrents []EconomyScore, allGroups [][]EconomyScore, allEnhancedGroups []TorrentGroup, page, pageSize int) PaginatedReviewTorrents {
	totalItems := len(allTorrents)
	totalPages := (totalItems + pageSize - 1) / pageSize

	// Ensure page is within bounds
	if page < 1 {
		page = 1
	}
	if page > totalPages && totalPages > 0 {
		page = totalPages
	}

	// Calculate start and end indices for the current page
	startIndex := (page - 1) * pageSize
	endIndex := startIndex + pageSize
	if endIndex > totalItems {
		endIndex = totalItems
	}

	// Get torrents for current page
	pageTorrents := allTorrents[startIndex:endIndex]

	// Create groups for current page
	pageGroups := es.createGroupsForPage(pageTorrents, allGroups)

	// Create enhanced groups for current page
	pageEnhancedGroups := es.createEnhancedGroupsForPage(pageTorrents, allEnhancedGroups)

	// Determine if grouping should be enabled
	groupingEnabled := len(pageEnhancedGroups) > 0 && len(pageEnhancedGroups) < len(pageTorrents)

	return PaginatedReviewTorrents{
		Torrents:      pageTorrents,
		Groups:        pageGroups,
		TorrentGroups: pageEnhancedGroups,
		Pagination: PaginationInfo{
			Page:        page,
			PageSize:    pageSize,
			TotalItems:  totalItems,
			TotalPages:  totalPages,
			HasNextPage: page < totalPages,
			HasPrevPage: page > 1,
		},
		GroupingEnabled: groupingEnabled,
	}
}

// createGroupsForPage creates groups for the torrents on the current page
func (es *EconomyService) createGroupsForPage(pageTorrents []EconomyScore, allGroups [][]EconomyScore) [][]EconomyScore {
	var pageGroups [][]EconomyScore
	torrentHashesOnPage := make(map[string]bool)

	// Create a map of hashes on this page
	for _, torrent := range pageTorrents {
		torrentHashesOnPage[torrent.Hash] = true
	}

	// Find complete groups that have members on this page
	for _, group := range allGroups {
		// Check if this group has any members on the current page
		hasMembersOnPage := false
		for _, torrent := range group {
			if torrentHashesOnPage[torrent.Hash] {
				hasMembersOnPage = true
				break
			}
		}

		// If the group has members on this page, include the complete group
		// This ensures groups are shown in full even if some members are on other pages
		if hasMembersOnPage {
			pageGroups = append(pageGroups, group)
		}
	}

	return pageGroups
}

// createEnhancedGroupsForPage creates enhanced groups for the torrents on the current page
func (es *EconomyService) createEnhancedGroupsForPage(pageTorrents []EconomyScore, allEnhancedGroups []TorrentGroup) []TorrentGroup {
	var pageEnhancedGroups []TorrentGroup
	torrentHashesOnPage := make(map[string]bool)

	// Create a map of hashes on this page
	for _, torrent := range pageTorrents {
		torrentHashesOnPage[torrent.Hash] = true
	}

	// Find complete enhanced groups that have members on this page
	for _, group := range allEnhancedGroups {
		// Check if this group has any members on the current page
		hasMembersOnPage := false
		for _, torrent := range group.Torrents {
			if torrentHashesOnPage[torrent.Hash] {
				hasMembersOnPage = true
				break
			}
		}

		// If the group has members on this page, include the complete group
		// This ensures groups are shown in full even if some members are on other pages
		if hasMembersOnPage {
			pageEnhancedGroups = append(pageEnhancedGroups, group)
		}
	}

	return pageEnhancedGroups
}

// applyFiltersToScores applies the given filters to the economy scores
func (es *EconomyService) applyFiltersToScores(scores []EconomyScore, filters FilterOptions) []EconomyScore {
	if len(filters.Status) == 0 && len(filters.Categories) == 0 && len(filters.Tags) == 0 && len(filters.Trackers) == 0 {
		return scores
	}

	var filtered []EconomyScore
	for _, score := range scores {
		// Filter by status
		if len(filters.Status) > 0 {
			statusMatch := false
			for _, status := range filters.Status {
				if strings.EqualFold(score.State, status) {
					statusMatch = true
					break
				}
			}
			if !statusMatch {
				continue
			}
		}

		// Filter by category
		if len(filters.Categories) > 0 {
			categoryMatch := false
			for _, category := range filters.Categories {
				if strings.EqualFold(score.Category, category) {
					categoryMatch = true
					break
				}
			}
			if !categoryMatch {
				continue
			}
		}

		// Filter by tracker
		if len(filters.Trackers) > 0 {
			trackerMatch := false
			for _, tracker := range filters.Trackers {
				if strings.Contains(strings.ToLower(score.Tracker), strings.ToLower(tracker)) {
					trackerMatch = true
					break
				}
			}
			if !trackerMatch {
				continue
			}
		}

		// Note: Tag filtering would require additional metadata from qBittorrent
		// For now, we'll skip tag filtering as it's not available in EconomyScore

		filtered = append(filtered, score)
	}

	return filtered
}

// sortScores sorts the economy scores based on the given field and direction
func (es *EconomyService) sortScores(scores []EconomyScore, sortField string, sortDesc bool) []EconomyScore {
	if len(scores) == 0 {
		return scores
	}

	sorted := make([]EconomyScore, len(scores))
	copy(sorted, scores)

	sort.Slice(sorted, func(i, j int) bool {
		var less bool

		switch strings.ToLower(sortField) {
		case "name":
			less = strings.ToLower(sorted[i].Name) < strings.ToLower(sorted[j].Name)
		case "size":
			less = sorted[i].Size < sorted[j].Size
		case "seeds":
			less = sorted[i].Seeds < sorted[j].Seeds
		case "peers":
			less = sorted[i].Peers < sorted[j].Peers
		case "ratio":
			less = sorted[i].Ratio < sorted[j].Ratio
		case "age":
			less = sorted[i].Age < sorted[j].Age
		case "economyscore", "economy_score", "":
			// Default to economy score
			less = sorted[i].EconomyScore < sorted[j].EconomyScore
		case "storagevalue", "storage_value":
			less = sorted[i].StorageValue < sorted[j].StorageValue
		case "raritybonus", "rarity_bonus":
			less = sorted[i].RarityBonus < sorted[j].RarityBonus
		case "reviewpriority", "review_priority":
			less = sorted[i].ReviewPriority < sorted[j].ReviewPriority
		case "lastactivity", "last_activity":
			less = sorted[i].LastActivity < sorted[j].LastActivity
		case "tracker":
			less = strings.ToLower(sorted[i].Tracker) < strings.ToLower(sorted[j].Tracker)
		case "category":
			less = strings.ToLower(sorted[i].Category) < strings.ToLower(sorted[j].Category)
		case "state":
			less = strings.ToLower(sorted[i].State) < strings.ToLower(sorted[j].State)
		default:
			// Default to economy score
			less = sorted[i].EconomyScore < sorted[j].EconomyScore
		}

		if sortDesc {
			return !less
		}
		return less
	})

	return sorted
}
