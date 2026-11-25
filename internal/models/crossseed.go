// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/domain"
)

const (
	CategoryAffixModePrefix = "prefix"
	CategoryAffixModeSuffix = "suffix"
)

type SeasonPackCategoryRule struct {
	Resolution string `json:"resolution"`
	Source     string `json:"source"`
	Category   string `json:"category"`
}

type CategoryMappingRule struct {
	Categories  []string `json:"categories"`
	ContentType string   `json:"contentType"`
}

type CrossSeedAutomationSettings struct {
	Enabled            bool  `json:"enabled"`
	RunIntervalMinutes int   `json:"runIntervalMinutes"`
	TargetInstanceIDs  []int `json:"targetInstanceIds"`
	TargetIndexerIDs   []int `json:"targetIndexerIds"`
	MaxResultsPerRun   int   `json:"maxResultsPerRun"` // Deprecated: automation processes full feeds; retained for backward compatibility

	RSSSourceCategories        []string `json:"rssSourceCategories"`
	RSSSourceTags              []string `json:"rssSourceTags"`
	RSSSourceExcludeCategories []string `json:"rssSourceExcludeCategories"`
	RSSSourceExcludeTags       []string `json:"rssSourceExcludeTags"`

	WebhookSourceCategories        []string `json:"webhookSourceCategories"`
	WebhookSourceTags              []string `json:"webhookSourceTags"`
	WebhookSourceExcludeCategories []string `json:"webhookSourceExcludeCategories"`
	WebhookSourceExcludeTags       []string `json:"webhookSourceExcludeTags"`

	FindIndividualEpisodes         bool `json:"findIndividualEpisodes"`
	AutoResumeMaxDownloadMB        int  `json:"autoResumeMaxDownloadMb"`
	PooledPartialCompletionEnabled bool `json:"pooledPartialCompletionEnabled"`
	UseCategoryFromIndexer         bool `json:"useCategoryFromIndexer"`
	RunExternalProgramID           *int `json:"runExternalProgramId"`

	CategoryMappingRules []CategoryMappingRule `json:"categoryMappingRules"`

	RSSAutomationTags    []string `json:"rssAutomationTags"`
	SeededSearchTags     []string `json:"seededSearchTags"`
	CompletionSearchTags []string `json:"completionSearchTags"`
	WebhookTags          []string `json:"webhookTags"`
	InheritSourceTags    bool     `json:"inheritSourceTags"`

	UseCrossCategoryAffix bool   `json:"useCrossCategoryAffix"`
	CategoryAffixMode     string `json:"categoryAffixMode"`
	CategoryAffix         string `json:"categoryAffix"`
	UseCustomCategory     bool   `json:"useCustomCategory"`
	CustomCategory        string `json:"customCategory"`

	SkipAutoResumeRSS            bool `json:"skipAutoResumeRss"`
	SkipAutoResumeSeededSearch   bool `json:"skipAutoResumeSeededSearch"`
	SkipAutoResumeCompletion     bool `json:"skipAutoResumeCompletion"`
	SkipAutoResumeWebhook        bool `json:"skipAutoResumeWebhook"`
	SkipRecheck                  bool `json:"skipRecheck"`
	RescueTitleMismatches        bool `json:"rescueTitleMismatches"`
	SkipPieceBoundarySafetyCheck bool `json:"skipPieceBoundarySafetyCheck"`

	SeasonPackSkipRepackCompare  bool                     `json:"seasonPackSkipRepackCompare"`
	SeasonPackSimplifyHDRCompare bool                     `json:"seasonPackSimplifyHdrCompare"`
	SeasonPackSimplifyWEBCompare bool                     `json:"seasonPackSimplifyWebCompare"`
	SeasonPackSkipYearCompare    bool                     `json:"seasonPackSkipYearCompare"`
	SeasonPackEnabled            bool                     `json:"seasonPackEnabled"`
	SeasonPackAutomationEnabled  bool                     `json:"seasonPackAutomationEnabled"`
	SeasonPackCoverageThreshold  float64                  `json:"seasonPackCoverageThreshold"`
	SeasonPackTags               []string                 `json:"seasonPackTags"`
	SeasonPackCategory           string                   `json:"seasonPackCategory"`
	SeasonPackCategoryRules      []SeasonPackCategoryRule `json:"seasonPackCategoryRules"`
	SeasonPackTVDBAPIKey         string                   `json:"seasonPackTvdbApiKey,omitempty"`
	SeasonPackTVDBPIN            string                   `json:"seasonPackTvdbPin,omitempty"`

	GazelleEnabled bool   `json:"gazelleEnabled"`
	RedactedAPIKey string `json:"redactedApiKey,omitempty"`
	OrpheusAPIKey  string `json:"orpheusApiKey,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

const DefaultAutoResumeMaxDownloadMB = 50

func DefaultCrossSeedAutomationSettings() *CrossSeedAutomationSettings {
	return &CrossSeedAutomationSettings{
		Enabled:                        false,
		RunIntervalMinutes:             120,
		TargetInstanceIDs:              []int{},
		TargetIndexerIDs:               []int{},
		MaxResultsPerRun:               50,
		RSSSourceCategories:            []string{},
		RSSSourceTags:                  []string{},
		RSSSourceExcludeCategories:     []string{},
		RSSSourceExcludeTags:           []string{},
		WebhookSourceCategories:        []string{},
		WebhookSourceTags:              []string{},
		WebhookSourceExcludeCategories: []string{},
		WebhookSourceExcludeTags:       []string{},
		FindIndividualEpisodes:         false,
		AutoResumeMaxDownloadMB:        DefaultAutoResumeMaxDownloadMB,
		PooledPartialCompletionEnabled: false,
		UseCategoryFromIndexer:         false,
		RunExternalProgramID:           nil,
		CategoryMappingRules:           []CategoryMappingRule{},
		RSSAutomationTags:              []string{"cross-seed"},
		SeededSearchTags:               []string{"cross-seed"},
		CompletionSearchTags:           []string{"cross-seed"},
		WebhookTags:                    []string{"cross-seed"},
		InheritSourceTags:              false,
		UseCrossCategoryAffix:          true,
		CategoryAffixMode:              CategoryAffixModeSuffix,
		CategoryAffix:                  ".cross",
		UseCustomCategory:              false,
		CustomCategory:                 "",
		SkipAutoResumeRSS:              false,
		SkipAutoResumeSeededSearch:     false,
		SkipAutoResumeCompletion:       false,
		SkipAutoResumeWebhook:          false,
		SkipRecheck:                    false,
		RescueTitleMismatches:          false,
		SkipPieceBoundarySafetyCheck:   true,
		SeasonPackSkipRepackCompare:    true,
		SeasonPackSimplifyHDRCompare:   false,
		SeasonPackSimplifyWEBCompare:   false,
		SeasonPackSkipYearCompare:      false,
		SeasonPackEnabled:              false,
		SeasonPackAutomationEnabled:    false,
		SeasonPackCoverageThreshold:    0.75,
		SeasonPackTags:                 []string{"cross-seed"},
		SeasonPackCategory:             "",
		SeasonPackCategoryRules:        []SeasonPackCategoryRule{},
		GazelleEnabled:                 false,
		RedactedAPIKey:                 "",
		OrpheusAPIKey:                  "",
		CreatedAt:                      time.Now().UTC(),
		UpdatedAt:                      time.Now().UTC(),
	}
}

type CrossSeedSearchSettings struct {
	InstanceID             *int      `json:"instanceId"`
	Categories             []string  `json:"categories"`
	Tags                   []string  `json:"tags"`
	ExcludeCategories      []string  `json:"excludeCategories"`
	ExcludeTags            []string  `json:"excludeTags"`
	IndexerIDs             []int     `json:"indexerIds"`
	IntervalSeconds        int       `json:"intervalSeconds"`
	CooldownMinutes        int       `json:"cooldownMinutes"`
	MaxAddedAgeDays        int       `json:"maxAddedAgeDays"`
	SkipIndividualEpisodes bool      `json:"skipIndividualEpisodes"`
	TorznabEnabled         bool      `json:"torznabEnabled"`
	ScheduleEnabled        bool      `json:"scheduleEnabled"`
	ScheduleIntervalHours  int       `json:"scheduleIntervalHours"`
	ScheduleMaxSearches    int       `json:"scheduleMaxSearches"`
	ScheduleMaxAgeDays     int       `json:"scheduleMaxAgeDays"`
	CreatedAt              time.Time `json:"createdAt"`
	UpdatedAt              time.Time `json:"updatedAt"`
}

func DefaultCrossSeedSearchSettings() *CrossSeedSearchSettings {
	now := time.Now().UTC()
	return &CrossSeedSearchSettings{
		InstanceID:            nil,
		Categories:            []string{},
		Tags:                  []string{},
		ExcludeCategories:     []string{},
		ExcludeTags:           []string{},
		IndexerIDs:            []int{},
		IntervalSeconds:       60,
		CooldownMinutes:       720,
		TorznabEnabled:        true,
		ScheduleIntervalHours: 24,
		ScheduleMaxSearches:   400,
		ScheduleMaxAgeDays:    14,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
}

type CrossSeedRunStatus string

const (
	CrossSeedRunStatusPending CrossSeedRunStatus = "pending"
	CrossSeedRunStatusRunning CrossSeedRunStatus = "running"
	CrossSeedRunStatusSuccess CrossSeedRunStatus = "success"
	CrossSeedRunStatusPartial CrossSeedRunStatus = "partial"
	CrossSeedRunStatusFailed  CrossSeedRunStatus = "failed"
)

type CrossSeedRunMode string

const (
	CrossSeedRunModeAuto   CrossSeedRunMode = "auto"
	CrossSeedRunModeManual CrossSeedRunMode = "manual"
)

type CrossSeedRunResult struct {
	InstanceID            int     `json:"instanceId"`
	InstanceName          string  `json:"instanceName"`
	IndexerName           string  `json:"indexerName,omitempty"`
	Success               bool    `json:"success"`
	Status                string  `json:"status"`
	Message               string  `json:"message,omitempty"`
	MatchedTorrentHash    *string `json:"matchedTorrentHash,omitempty"`
	MatchedTorrentName    *string `json:"matchedTorrentName,omitempty"`
	CrossSeedInstanceID   int     `json:"crossSeedInstanceId,omitzero"`
	CrossSeedInstanceName string  `json:"crossSeedInstanceName,omitempty"`
}

type CrossSeedRun struct {
	ID                int64                `json:"id"`
	TriggeredBy       string               `json:"triggeredBy"`
	Mode              CrossSeedRunMode     `json:"mode"`
	Status            CrossSeedRunStatus   `json:"status"`
	StartedAt         time.Time            `json:"startedAt"`
	CompletedAt       *time.Time           `json:"completedAt,omitempty"`
	TotalFeedItems    int                  `json:"totalFeedItems"`
	CandidatesFound   int                  `json:"candidatesFound"`
	CrossSeedsAdded   int                  `json:"crossSeedsAdded"`
	CandidatesFailed  int                  `json:"candidatesFailed"`
	CandidatesSkipped int                  `json:"candidatesSkipped"`
	Message           *string              `json:"message,omitempty"`
	ErrorMessage      *string              `json:"errorMessage,omitempty"`
	Results           []CrossSeedRunResult `json:"results,omitempty"`
	CreatedAt         time.Time            `json:"createdAt"`
}

type CrossSeedSearchRunStatus string

const (
	CrossSeedSearchRunStatusRunning  CrossSeedSearchRunStatus = "running"
	CrossSeedSearchRunStatusSuccess  CrossSeedSearchRunStatus = "success"
	CrossSeedSearchRunStatusFailed   CrossSeedSearchRunStatus = "failed"
	CrossSeedSearchRunStatusCanceled CrossSeedSearchRunStatus = "canceled"
)

type CrossSeedSearchFilters struct {
	Categories        []string `json:"categories"`
	Tags              []string `json:"tags"`
	ExcludeCategories []string `json:"excludeCategories,omitempty"`
	ExcludeTags       []string `json:"excludeTags,omitempty"`
}

type CrossSeedSearchRunKind string

const (
	CrossSeedSearchRunKindManual    CrossSeedSearchRunKind = "manual"
	CrossSeedSearchRunKindScheduled CrossSeedSearchRunKind = "scheduled"
)

type CrossSeedSearchResultStatus string

const (
	CrossSeedSearchResultStatusAdded   CrossSeedSearchResultStatus = "added"
	CrossSeedSearchResultStatusSkipped CrossSeedSearchResultStatus = "skipped"
	CrossSeedSearchResultStatusFailed  CrossSeedSearchResultStatus = "failed"
)

type CrossSeedSearchResult struct {
	TorrentHash           string                      `json:"torrentHash"`
	TorrentName           string                      `json:"torrentName"`
	IndexerName           string                      `json:"indexerName"`
	ReleaseTitle          string                      `json:"releaseTitle"`
	Status                CrossSeedSearchResultStatus `json:"status"`
	Message               string                      `json:"message,omitempty"`
	ProcessedAt           time.Time                   `json:"processedAt"`
	CrossSeedInstanceID   int                         `json:"crossSeedInstanceId,omitzero"`
	CrossSeedInstanceName string                      `json:"crossSeedInstanceName,omitempty"`
}

type CrossSeedSearchRun struct {
	ID                     int64                         `json:"id"`
	InstanceID             int                           `json:"instanceId"`
	Kind                   CrossSeedSearchRunKind        `json:"kind"`
	Status                 CrossSeedSearchRunStatus      `json:"status"`
	StartedAt              time.Time                     `json:"startedAt"`
	CompletedAt            *time.Time                    `json:"completedAt,omitempty"`
	TotalTorrents          int                           `json:"totalTorrents"`
	Processed              int                           `json:"processed"`
	TorrentsWithCrossSeeds int                           `json:"torrentsWithCrossSeeds"`
	CrossSeedsAdded        int                           `json:"crossSeedsAdded"`
	TorrentsFailed         int                           `json:"torrentsFailed"`
	TorrentsSkipped        int                           `json:"torrentsSkipped"`
	Message                *string                       `json:"message,omitempty"`
	ErrorMessage           *string                       `json:"errorMessage,omitempty"`
	Filters                CrossSeedSearchFilters        `json:"filters"`
	IndexerIDs             []int                         `json:"indexerIds"`
	IntervalSeconds        int                           `json:"intervalSeconds"`
	CooldownMinutes        int                           `json:"cooldownMinutes"`
	Results                []CrossSeedSearchResult       `json:"results"`
	GazelleSummary         []CrossSeedGazelleSiteSummary `json:"gazelleSummary,omitempty"`
	SearchLimit            int                           `json:"searchLimit,omitzero"`
	WaitingTorrents        int                           `json:"waitingTorrents,omitzero"`
	CreatedAt              time.Time                     `json:"createdAt"`
}

type CrossSeedGazelleSiteSummary struct {
	Site     string         `json:"site"`
	Name     string         `json:"name"`
	Asked    int            `json:"asked"`
	Matched  int            `json:"matched"`
	Failed   int            `json:"failed"`
	NotAsked map[string]int `json:"notAsked,omitempty"`
}

type CrossSeedFeedItemStatus string

const (
	CrossSeedFeedItemStatusPending   CrossSeedFeedItemStatus = "pending"
	CrossSeedFeedItemStatusProcessed CrossSeedFeedItemStatus = "processed"
	CrossSeedFeedItemStatusSkipped   CrossSeedFeedItemStatus = "skipped"
	CrossSeedFeedItemStatusFailed    CrossSeedFeedItemStatus = "failed"
)

type CrossSeedFeedItem struct {
	GUID        string                  `json:"guid"`
	IndexerID   int                     `json:"indexerId"`
	Title       string                  `json:"title"`
	FirstSeenAt time.Time               `json:"firstSeenAt"`
	LastSeenAt  time.Time               `json:"lastSeenAt"`
	LastStatus  CrossSeedFeedItemStatus `json:"lastStatus"`
	InfoHash    *string                 `json:"infoHash,omitempty"`
}

type CrossSeedStore struct {
	db     dbinterface.Querier
	cipher *CredentialCipher
}

func NewCrossSeedStore(db dbinterface.Querier, encryptionKey []byte, opts ...CredentialCipherOption) (*CrossSeedStore, error) {
	credentialCipher, err := NewCredentialCipher(encryptionKey, opts...)
	if err != nil {
		return nil, err
	}

	return &CrossSeedStore{db: db, cipher: credentialCipher}, nil
}

func (s *CrossSeedStore) encrypt(plaintext string) (string, error) {
	return s.cipher.Encrypt(plaintext, nil)
}

func (s *CrossSeedStore) decrypt(ciphertext string) (string, error) {
	return s.cipher.Decrypt(ciphertext, nil)
}

func (s *CrossSeedStore) RewriteLegacyCredentials(ctx context.Context) (int, error) {
	return s.cipher.rewriteLegacyRows(ctx, s.db, legacyCredentialTable{
		table: "cross_seed_settings",
		columns: []string{
			"season_pack_tvdb_api_key_encrypted",
			"season_pack_tvdb_pin_encrypted",
			"redacted_api_key_encrypted",
			"orpheus_api_key_encrypted",
		},
	})
}

func (s *CrossSeedStore) apiKeyRedacted(encrypted string) string {
	if strings.TrimSpace(encrypted) == "" {
		return ""
	}
	if _, err := s.decrypt(encrypted); err != nil {
		return ""
	}
	return domain.RedactedStr
}

func (s *CrossSeedStore) GetSettings(ctx context.Context) (*CrossSeedAutomationSettings, error) {
	query := `
		SELECT enabled, run_interval_minutes,
		       target_instance_ids, target_indexer_ids,
		       max_results_per_run,
		       rss_source_categories, rss_source_tags,
		       rss_source_exclude_categories, rss_source_exclude_tags,
		       webhook_source_categories, webhook_source_tags,
		       webhook_source_exclude_categories, webhook_source_exclude_tags,
		       find_individual_episodes,
		       auto_resume_max_download_mb,
		       pooled_partial_completion_enabled,
		       use_category_from_indexer, run_external_program_id,
		       category_mapping_rules,
		       rss_automation_tags, seeded_search_tags, completion_search_tags,
		       webhook_tags, inherit_source_tags,
		       use_cross_category_affix, category_affix_mode, category_affix,
		       use_custom_category, custom_category,
		       skip_auto_resume_rss, skip_auto_resume_seeded_search,
		       skip_auto_resume_completion, skip_auto_resume_webhook,
		       skip_recheck, rescue_title_mismatches, skip_piece_boundary_safety_check,
		       season_pack_skip_repack_compare, season_pack_simplify_hdr_compare,
		       season_pack_simplify_web_compare, season_pack_skip_year_compare,
		       season_pack_enabled, season_pack_automation_enabled, season_pack_coverage_threshold, season_pack_tags, season_pack_category,
		       season_pack_category_rules,
		       season_pack_tvdb_api_key_encrypted, season_pack_tvdb_pin_encrypted,
		       gazelle_enabled, redacted_api_key_encrypted, orpheus_api_key_encrypted,
		       created_at, updated_at
		FROM cross_seed_settings
		WHERE id = 1
	`

	row := s.db.QueryRowContext(ctx, query)

	var settings CrossSeedAutomationSettings
	var instancesJSON, indexersJSON sql.NullString
	var rssSourceCategories, rssSourceTags, rssSourceExcludeCategories, rssSourceExcludeTags sql.NullString
	var webhookSourceCategories, webhookSourceTags, webhookSourceExcludeCategories, webhookSourceExcludeTags sql.NullString
	var rssAutomationTags, seededSearchTags, completionSearchTags, webhookTags sql.NullString
	var runExternalProgramID sql.NullInt64
	var enabled int
	var findIndividualEpisodes, useCategoryFromIndexer int
	var pooledPartialCompletionEnabled int
	var inheritSourceTags, useCrossCategoryAffix, useCustomCategory int
	var skipAutoResumeRSS, skipAutoResumeSeededSearch, skipAutoResumeCompletion, skipAutoResumeWebhook int
	var skipRecheck, rescueTitleMismatches, skipPieceBoundarySafetyCheck int
	var seasonPackSkipRepackCompare, seasonPackSimplifyHDRCompare, seasonPackSimplifyWEBCompare, seasonPackSkipYearCompare int
	var seasonPackEnabled int
	var seasonPackAutomationEnabled int
	var seasonPackTags, seasonPackCategory sql.NullString
	var seasonPackCategoryRules sql.NullString
	var categoryMappingRules sql.NullString
	var seasonPackTVDBAPIKeyEncrypted, seasonPackTVDBPINEncrypted sql.NullString
	var gazelleEnabled int
	var redactedAPIKeyEncrypted, orpheusAPIKeyEncrypted sql.NullString
	var createdAt, updatedAt sql.NullTime

	err := row.Scan(
		&enabled,
		&settings.RunIntervalMinutes,
		&instancesJSON,
		&indexersJSON,
		&settings.MaxResultsPerRun,
		&rssSourceCategories,
		&rssSourceTags,
		&rssSourceExcludeCategories,
		&rssSourceExcludeTags,
		&webhookSourceCategories,
		&webhookSourceTags,
		&webhookSourceExcludeCategories,
		&webhookSourceExcludeTags,
		&findIndividualEpisodes,
		&settings.AutoResumeMaxDownloadMB,
		&pooledPartialCompletionEnabled,
		&useCategoryFromIndexer,
		&runExternalProgramID,
		&categoryMappingRules,
		&rssAutomationTags,
		&seededSearchTags,
		&completionSearchTags,
		&webhookTags,
		&inheritSourceTags,
		&useCrossCategoryAffix,
		&settings.CategoryAffixMode,
		&settings.CategoryAffix,
		&useCustomCategory,
		&settings.CustomCategory,
		&skipAutoResumeRSS,
		&skipAutoResumeSeededSearch,
		&skipAutoResumeCompletion,
		&skipAutoResumeWebhook,
		&skipRecheck,
		&rescueTitleMismatches,
		&skipPieceBoundarySafetyCheck,
		&seasonPackSkipRepackCompare,
		&seasonPackSimplifyHDRCompare,
		&seasonPackSimplifyWEBCompare,
		&seasonPackSkipYearCompare,
		&seasonPackEnabled,
		&seasonPackAutomationEnabled,
		&settings.SeasonPackCoverageThreshold,
		&seasonPackTags,
		&seasonPackCategory,
		&seasonPackCategoryRules,
		&seasonPackTVDBAPIKeyEncrypted,
		&seasonPackTVDBPINEncrypted,
		&gazelleEnabled,
		&redactedAPIKeyEncrypted,
		&orpheusAPIKeyEncrypted,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DefaultCrossSeedAutomationSettings(), nil
		}
		return nil, fmt.Errorf("query settings: %w", err)
	}

	if runExternalProgramID.Valid {
		id := int(runExternalProgramID.Int64)
		settings.RunExternalProgramID = &id
	}

	if err := decodeIntSlice(instancesJSON, &settings.TargetInstanceIDs); err != nil {
		return nil, fmt.Errorf("decode target instances: %w", err)
	}
	if err := decodeIntSlice(indexersJSON, &settings.TargetIndexerIDs); err != nil {
		return nil, fmt.Errorf("decode target indexers: %w", err)
	}

	if err := decodeStringSlice(rssSourceCategories, &settings.RSSSourceCategories); err != nil {
		return nil, fmt.Errorf("decode rss source categories: %w", err)
	}
	if err := decodeStringSlice(rssSourceTags, &settings.RSSSourceTags); err != nil {
		return nil, fmt.Errorf("decode rss source tags: %w", err)
	}
	if err := decodeStringSlice(rssSourceExcludeCategories, &settings.RSSSourceExcludeCategories); err != nil {
		return nil, fmt.Errorf("decode rss source exclude categories: %w", err)
	}
	if err := decodeStringSlice(rssSourceExcludeTags, &settings.RSSSourceExcludeTags); err != nil {
		return nil, fmt.Errorf("decode rss source exclude tags: %w", err)
	}

	if err := decodeStringSlice(webhookSourceCategories, &settings.WebhookSourceCategories); err != nil {
		return nil, fmt.Errorf("decode webhook source categories: %w", err)
	}
	if err := decodeStringSlice(webhookSourceTags, &settings.WebhookSourceTags); err != nil {
		return nil, fmt.Errorf("decode webhook source tags: %w", err)
	}
	if err := decodeStringSlice(webhookSourceExcludeCategories, &settings.WebhookSourceExcludeCategories); err != nil {
		return nil, fmt.Errorf("decode webhook source exclude categories: %w", err)
	}
	if err := decodeStringSlice(webhookSourceExcludeTags, &settings.WebhookSourceExcludeTags); err != nil {
		return nil, fmt.Errorf("decode webhook source exclude tags: %w", err)
	}

	defaults := DefaultCrossSeedAutomationSettings()
	if err := decodeStringSliceWithDefault(rssAutomationTags, &settings.RSSAutomationTags, defaults.RSSAutomationTags); err != nil {
		return nil, fmt.Errorf("decode rss automation tags: %w", err)
	}
	if err := decodeStringSliceWithDefault(seededSearchTags, &settings.SeededSearchTags, defaults.SeededSearchTags); err != nil {
		return nil, fmt.Errorf("decode seeded search tags: %w", err)
	}
	if err := decodeStringSliceWithDefault(completionSearchTags, &settings.CompletionSearchTags, defaults.CompletionSearchTags); err != nil {
		return nil, fmt.Errorf("decode completion search tags: %w", err)
	}
	if err := decodeStringSliceWithDefault(webhookTags, &settings.WebhookTags, defaults.WebhookTags); err != nil {
		return nil, fmt.Errorf("decode webhook tags: %w", err)
	}
	if err := decodeStringSliceWithDefault(seasonPackTags, &settings.SeasonPackTags, defaults.SeasonPackTags); err != nil {
		return nil, fmt.Errorf("decode season pack tags: %w", err)
	}
	if seasonPackCategory.Valid {
		settings.SeasonPackCategory = seasonPackCategory.String
	}
	if err := decodeJSONSlice(seasonPackCategoryRules, &settings.SeasonPackCategoryRules); err != nil {
		return nil, fmt.Errorf("decode season pack category rules: %w", err)
	}
	if err := decodeJSONSlice(categoryMappingRules, &settings.CategoryMappingRules); err != nil {
		return nil, fmt.Errorf("decode category mapping rules: %w", err)
	}
	settings.CategoryMappingRules = slices.DeleteFunc(settings.CategoryMappingRules, func(rule CategoryMappingRule) bool {
		return len(rule.Categories) == 0
	})

	if createdAt.Valid {
		settings.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		settings.UpdatedAt = updatedAt.Time
	}

	settings.Enabled = SQLiteIntToBool(enabled)
	settings.FindIndividualEpisodes = SQLiteIntToBool(findIndividualEpisodes)
	settings.PooledPartialCompletionEnabled = SQLiteIntToBool(pooledPartialCompletionEnabled)
	settings.UseCategoryFromIndexer = SQLiteIntToBool(useCategoryFromIndexer)
	settings.InheritSourceTags = SQLiteIntToBool(inheritSourceTags)
	settings.UseCrossCategoryAffix = SQLiteIntToBool(useCrossCategoryAffix)
	settings.UseCustomCategory = SQLiteIntToBool(useCustomCategory)
	settings.SkipAutoResumeRSS = SQLiteIntToBool(skipAutoResumeRSS)
	settings.SkipAutoResumeSeededSearch = SQLiteIntToBool(skipAutoResumeSeededSearch)
	settings.SkipAutoResumeCompletion = SQLiteIntToBool(skipAutoResumeCompletion)
	settings.SkipAutoResumeWebhook = SQLiteIntToBool(skipAutoResumeWebhook)
	settings.SkipRecheck = SQLiteIntToBool(skipRecheck)
	settings.RescueTitleMismatches = SQLiteIntToBool(rescueTitleMismatches)
	settings.SkipPieceBoundarySafetyCheck = SQLiteIntToBool(skipPieceBoundarySafetyCheck)
	settings.SeasonPackSkipRepackCompare = SQLiteIntToBool(seasonPackSkipRepackCompare)
	settings.SeasonPackSimplifyHDRCompare = SQLiteIntToBool(seasonPackSimplifyHDRCompare)
	settings.SeasonPackSimplifyWEBCompare = SQLiteIntToBool(seasonPackSimplifyWEBCompare)
	settings.SeasonPackSkipYearCompare = SQLiteIntToBool(seasonPackSkipYearCompare)
	settings.SeasonPackEnabled = SQLiteIntToBool(seasonPackEnabled)
	settings.SeasonPackAutomationEnabled = SQLiteIntToBool(seasonPackAutomationEnabled)
	settings.GazelleEnabled = SQLiteIntToBool(gazelleEnabled)
	if redactedAPIKeyEncrypted.Valid {
		settings.RedactedAPIKey = s.apiKeyRedacted(redactedAPIKeyEncrypted.String)
	}
	if orpheusAPIKeyEncrypted.Valid {
		settings.OrpheusAPIKey = s.apiKeyRedacted(orpheusAPIKeyEncrypted.String)
	}
	if seasonPackTVDBAPIKeyEncrypted.Valid {
		settings.SeasonPackTVDBAPIKey = s.apiKeyRedacted(seasonPackTVDBAPIKeyEncrypted.String)
	}
	if seasonPackTVDBPINEncrypted.Valid {
		settings.SeasonPackTVDBPIN = s.apiKeyRedacted(seasonPackTVDBPINEncrypted.String)
	}

	return &settings, nil
}

const (
	GazelleKeyRedacted = "redacted"
	GazelleKeyOrpheus  = "orpheus"
)

func (s *CrossSeedAutomationSettings) GazelleAPIKey(name string) string {
	switch name {
	case GazelleKeyRedacted:
		return s.RedactedAPIKey
	case GazelleKeyOrpheus:
		return s.OrpheusAPIKey
	default:
		return ""
	}
}

type GazelleAPIKeys struct {
	Enabled bool
	Keys    map[string]string
	Errs    map[string]error
}

func (s *CrossSeedStore) GetDecryptedGazelleAPIKeys(ctx context.Context) (GazelleAPIKeys, error) {
	return s.gazelleAPIKeys(ctx, false)
}

func (s *CrossSeedStore) GetStoredGazelleAPIKeys(ctx context.Context) (GazelleAPIKeys, error) {
	return s.gazelleAPIKeys(ctx, true)
}

func (s *CrossSeedStore) gazelleAPIKeys(ctx context.Context, evenWhenOff bool) (GazelleAPIKeys, error) {
	var enabled int
	var redacted, orpheus sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT gazelle_enabled, redacted_api_key_encrypted, orpheus_api_key_encrypted FROM cross_seed_settings WHERE id = 1`).
		Scan(&enabled, &redacted, &orpheus)
	if errors.Is(err, sql.ErrNoRows) {
		return GazelleAPIKeys{}, nil
	}
	if err != nil {
		return GazelleAPIKeys{}, err
	}
	keys := GazelleAPIKeys{Enabled: SQLiteIntToBool(enabled), Keys: map[string]string{}, Errs: map[string]error{}}
	if !keys.Enabled && !evenWhenOff {
		return keys, nil
	}
	for name, encrypted := range map[string]sql.NullString{GazelleKeyRedacted: redacted, GazelleKeyOrpheus: orpheus} {
		if !encrypted.Valid || strings.TrimSpace(encrypted.String) == "" {
			continue
		}
		plain, err := s.decrypt(encrypted.String)
		if err != nil {
			keys.Errs[name] = err
			continue
		}
		keys.Keys[name] = plain
	}
	return keys, nil
}

var ErrUndecryptableCredential = errors.New("stored credential does not decrypt")

func (s *CrossSeedStore) GetDecryptedSeasonPackTVDBCredentials(ctx context.Context) (apiKey, pin string, err error) {
	var keyEnc, pinEnc sql.NullString
	err = s.db.QueryRowContext(ctx, `
		SELECT season_pack_tvdb_api_key_encrypted, season_pack_tvdb_pin_encrypted
		FROM cross_seed_settings WHERE id = 1
	`).Scan(&keyEnc, &pinEnc)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", nil
		}
		return "", "", err
	}
	if keyEnc.Valid && strings.TrimSpace(keyEnc.String) != "" {
		apiKey, err = s.decrypt(keyEnc.String)
		if err != nil {
			return "", "", fmt.Errorf("decrypt tvdb api key: %w: %w", ErrUndecryptableCredential, err)
		}
	}
	if pinEnc.Valid && strings.TrimSpace(pinEnc.String) != "" {
		if pin, err = s.decrypt(pinEnc.String); err != nil {
			log.Warn().Err(err).Msg("Ignoring the stored TVDB PIN: it does not decrypt, most likely because sessionSecret changed")
			pin = ""
		}
	}
	return apiKey, pin, nil
}

func (s *CrossSeedStore) GetSeasonPackTVDBCredentialsUpdatedAt(ctx context.Context) (time.Time, error) {
	var updatedAt time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT updated_at
		FROM cross_seed_settings WHERE id = 1
	`).Scan(&updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}

	return updatedAt, nil
}

type crossSeedSettingsSecrets struct {
	redactedAPIKey string
	orpheusAPIKey  string
	tvdbAPIKey     string
	tvdbPIN        string
}

func (s *CrossSeedStore) loadExistingSettingsSecrets(ctx context.Context, settings *CrossSeedAutomationSettings) (crossSeedSettingsSecrets, error) {
	var red, ops, tvdbKey, tvdbPin sql.NullString
	queryErr := s.db.QueryRowContext(ctx, `
			SELECT redacted_api_key_encrypted, orpheus_api_key_encrypted,
			       season_pack_tvdb_api_key_encrypted, season_pack_tvdb_pin_encrypted
			FROM cross_seed_settings
			WHERE id = 1
		`).Scan(&red, &ops, &tvdbKey, &tvdbPin)
	if queryErr != nil && !errors.Is(queryErr, sql.ErrNoRows) {
		if strings.TrimSpace(settings.RedactedAPIKey) == domain.RedactedStr || strings.TrimSpace(settings.OrpheusAPIKey) == domain.RedactedStr ||
			strings.TrimSpace(settings.SeasonPackTVDBAPIKey) == domain.RedactedStr || strings.TrimSpace(settings.SeasonPackTVDBPIN) == domain.RedactedStr {
			return crossSeedSettingsSecrets{}, fmt.Errorf("load existing encrypted keys: %w", queryErr)
		}
	}
	return crossSeedSettingsSecrets{
		redactedAPIKey: red.String,
		orpheusAPIKey:  ops.String,
		tvdbAPIKey:     tvdbKey.String,
		tvdbPIN:        tvdbPin.String,
	}, nil
}

func (s *CrossSeedStore) resolveSettingsSecret(raw, existingEncrypted, label string) (string, error) {
	v := strings.TrimSpace(raw)
	switch v {
	case "":
		return "", nil
	case domain.RedactedStr:
		return existingEncrypted, nil
	default:
		enc, err := s.encrypt(v)
		if err != nil {
			return "", fmt.Errorf("encrypt %s: %w", label, err)
		}
		return enc, nil
	}
}

func (s *CrossSeedStore) UpsertSettings(ctx context.Context, settings *CrossSeedAutomationSettings) (*CrossSeedAutomationSettings, error) {
	if settings == nil {
		return nil, errors.New("settings cannot be nil")
	}

	instanceJSON, err := encodeIntSlice(settings.TargetInstanceIDs)
	if err != nil {
		return nil, fmt.Errorf("encode target instances: %w", err)
	}
	indexerJSON, err := encodeIntSlice(settings.TargetIndexerIDs)
	if err != nil {
		return nil, fmt.Errorf("encode target indexers: %w", err)
	}

	rssSourceCategoriesJSON, err := encodeStringSlice(settings.RSSSourceCategories)
	if err != nil {
		return nil, fmt.Errorf("encode rss source categories: %w", err)
	}
	rssSourceTagsJSON, err := encodeStringSlice(settings.RSSSourceTags)
	if err != nil {
		return nil, fmt.Errorf("encode rss source tags: %w", err)
	}
	rssSourceExcludeCategoriesJSON, err := encodeStringSlice(settings.RSSSourceExcludeCategories)
	if err != nil {
		return nil, fmt.Errorf("encode rss source exclude categories: %w", err)
	}
	rssSourceExcludeTagsJSON, err := encodeStringSlice(settings.RSSSourceExcludeTags)
	if err != nil {
		return nil, fmt.Errorf("encode rss source exclude tags: %w", err)
	}

	webhookSourceCategoriesJSON, err := encodeStringSlice(settings.WebhookSourceCategories)
	if err != nil {
		return nil, fmt.Errorf("encode webhook source categories: %w", err)
	}
	webhookSourceTagsJSON, err := encodeStringSlice(settings.WebhookSourceTags)
	if err != nil {
		return nil, fmt.Errorf("encode webhook source tags: %w", err)
	}
	webhookSourceExcludeCategoriesJSON, err := encodeStringSlice(settings.WebhookSourceExcludeCategories)
	if err != nil {
		return nil, fmt.Errorf("encode webhook source exclude categories: %w", err)
	}
	webhookSourceExcludeTagsJSON, err := encodeStringSlice(settings.WebhookSourceExcludeTags)
	if err != nil {
		return nil, fmt.Errorf("encode webhook source exclude tags: %w", err)
	}

	rssAutomationTags, err := encodeStringSlice(settings.RSSAutomationTags)
	if err != nil {
		return nil, fmt.Errorf("encode rss automation tags: %w", err)
	}
	seededSearchTags, err := encodeStringSlice(settings.SeededSearchTags)
	if err != nil {
		return nil, fmt.Errorf("encode seeded search tags: %w", err)
	}
	completionSearchTags, err := encodeStringSlice(settings.CompletionSearchTags)
	if err != nil {
		return nil, fmt.Errorf("encode completion search tags: %w", err)
	}
	webhookTags, err := encodeStringSlice(settings.WebhookTags)
	if err != nil {
		return nil, fmt.Errorf("encode webhook tags: %w", err)
	}
	seasonPackTags, err := encodeStringSlice(settings.SeasonPackTags)
	if err != nil {
		return nil, fmt.Errorf("encode season pack tags: %w", err)
	}
	seasonPackCategoryRules, err := encodeJSONSlice(settings.SeasonPackCategoryRules)
	if err != nil {
		return nil, fmt.Errorf("encode season pack category rules: %w", err)
	}
	categoryMappingRules, err := encodeJSONSlice(settings.CategoryMappingRules)
	if err != nil {
		return nil, fmt.Errorf("encode category mapping rules: %w", err)
	}

	existing, err := s.loadExistingSettingsSecrets(ctx, settings)
	if err != nil {
		return nil, err
	}

	redactedAPIKeyEncrypted, err := s.resolveSettingsSecret(settings.RedactedAPIKey, existing.redactedAPIKey, "redacted api key")
	if err != nil {
		return nil, err
	}
	orpheusAPIKeyEncrypted, err := s.resolveSettingsSecret(settings.OrpheusAPIKey, existing.orpheusAPIKey, "orpheus api key")
	if err != nil {
		return nil, err
	}
	seasonPackTVDBAPIKeyEncrypted, err := s.resolveSettingsSecret(settings.SeasonPackTVDBAPIKey, existing.tvdbAPIKey, "tvdb api key")
	if err != nil {
		return nil, err
	}
	seasonPackTVDBPINEncrypted, err := s.resolveSettingsSecret(settings.SeasonPackTVDBPIN, existing.tvdbPIN, "tvdb pin")
	if err != nil {
		return nil, err
	}

	query := `
		INSERT INTO cross_seed_settings (
			id, enabled, run_interval_minutes,
			target_instance_ids, target_indexer_ids,
			max_results_per_run,
			rss_source_categories, rss_source_tags,
			rss_source_exclude_categories, rss_source_exclude_tags,
			webhook_source_categories, webhook_source_tags,
			webhook_source_exclude_categories, webhook_source_exclude_tags,
			find_individual_episodes,
			auto_resume_max_download_mb,
			pooled_partial_completion_enabled,
			use_category_from_indexer, run_external_program_id,
			category_mapping_rules,
			rss_automation_tags, seeded_search_tags, completion_search_tags,
			webhook_tags, inherit_source_tags,
			use_cross_category_affix, category_affix_mode, category_affix,
			use_custom_category, custom_category,
			skip_auto_resume_rss, skip_auto_resume_seeded_search,
			skip_auto_resume_completion, skip_auto_resume_webhook,
			skip_recheck, rescue_title_mismatches, skip_piece_boundary_safety_check,
			season_pack_skip_repack_compare, season_pack_simplify_hdr_compare,
			season_pack_simplify_web_compare, season_pack_skip_year_compare,
			season_pack_enabled, season_pack_automation_enabled, season_pack_coverage_threshold, season_pack_tags, season_pack_category,
			season_pack_category_rules,
			season_pack_tvdb_api_key_encrypted, season_pack_tvdb_pin_encrypted,
			gazelle_enabled, redacted_api_key_encrypted, orpheus_api_key_encrypted
		) VALUES (
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		)
		ON CONFLICT(id) DO UPDATE SET
			enabled = excluded.enabled,
			run_interval_minutes = excluded.run_interval_minutes,
			target_instance_ids = excluded.target_instance_ids,
			target_indexer_ids = excluded.target_indexer_ids,
			max_results_per_run = excluded.max_results_per_run,
			rss_source_categories = excluded.rss_source_categories,
			rss_source_tags = excluded.rss_source_tags,
			rss_source_exclude_categories = excluded.rss_source_exclude_categories,
			rss_source_exclude_tags = excluded.rss_source_exclude_tags,
			webhook_source_categories = excluded.webhook_source_categories,
			webhook_source_tags = excluded.webhook_source_tags,
			webhook_source_exclude_categories = excluded.webhook_source_exclude_categories,
			webhook_source_exclude_tags = excluded.webhook_source_exclude_tags,
			find_individual_episodes = excluded.find_individual_episodes,
			auto_resume_max_download_mb = excluded.auto_resume_max_download_mb,
			pooled_partial_completion_enabled = excluded.pooled_partial_completion_enabled,
			use_category_from_indexer = excluded.use_category_from_indexer,
			run_external_program_id = excluded.run_external_program_id,
			category_mapping_rules = excluded.category_mapping_rules,
			rss_automation_tags = excluded.rss_automation_tags,
			seeded_search_tags = excluded.seeded_search_tags,
			completion_search_tags = excluded.completion_search_tags,
			webhook_tags = excluded.webhook_tags,
			inherit_source_tags = excluded.inherit_source_tags,
			use_cross_category_affix = excluded.use_cross_category_affix,
			category_affix_mode = excluded.category_affix_mode,
			category_affix = excluded.category_affix,
			use_custom_category = excluded.use_custom_category,
			custom_category = excluded.custom_category,
			skip_auto_resume_rss = excluded.skip_auto_resume_rss,
			skip_auto_resume_seeded_search = excluded.skip_auto_resume_seeded_search,
			skip_auto_resume_completion = excluded.skip_auto_resume_completion,
			skip_auto_resume_webhook = excluded.skip_auto_resume_webhook,
			skip_recheck = excluded.skip_recheck,
			rescue_title_mismatches = excluded.rescue_title_mismatches,
			skip_piece_boundary_safety_check = excluded.skip_piece_boundary_safety_check,
			season_pack_skip_repack_compare = excluded.season_pack_skip_repack_compare,
			season_pack_simplify_hdr_compare = excluded.season_pack_simplify_hdr_compare,
			season_pack_simplify_web_compare = excluded.season_pack_simplify_web_compare,
			season_pack_skip_year_compare = excluded.season_pack_skip_year_compare,
			season_pack_enabled = excluded.season_pack_enabled,
			season_pack_automation_enabled = excluded.season_pack_automation_enabled,
			season_pack_coverage_threshold = excluded.season_pack_coverage_threshold,
			season_pack_tags = excluded.season_pack_tags,
			season_pack_category = excluded.season_pack_category,
			season_pack_category_rules = excluded.season_pack_category_rules,
			season_pack_tvdb_api_key_encrypted = excluded.season_pack_tvdb_api_key_encrypted,
			season_pack_tvdb_pin_encrypted = excluded.season_pack_tvdb_pin_encrypted,
			gazelle_enabled = excluded.gazelle_enabled,
			redacted_api_key_encrypted = excluded.redacted_api_key_encrypted,
			orpheus_api_key_encrypted = excluded.orpheus_api_key_encrypted
	`

	var runExternalProgramID any
	if settings.RunExternalProgramID != nil {
		runExternalProgramID = *settings.RunExternalProgramID
	}

	_, err = s.db.ExecContext(ctx, query,
		1,
		BoolToSQLite(settings.Enabled),
		settings.RunIntervalMinutes,
		instanceJSON,
		indexerJSON,
		settings.MaxResultsPerRun,
		rssSourceCategoriesJSON,
		rssSourceTagsJSON,
		rssSourceExcludeCategoriesJSON,
		rssSourceExcludeTagsJSON,
		webhookSourceCategoriesJSON,
		webhookSourceTagsJSON,
		webhookSourceExcludeCategoriesJSON,
		webhookSourceExcludeTagsJSON,
		BoolToSQLite(settings.FindIndividualEpisodes),
		settings.AutoResumeMaxDownloadMB,
		BoolToSQLite(settings.PooledPartialCompletionEnabled),
		BoolToSQLite(settings.UseCategoryFromIndexer),
		runExternalProgramID,
		categoryMappingRules,
		rssAutomationTags,
		seededSearchTags,
		completionSearchTags,
		webhookTags,
		BoolToSQLite(settings.InheritSourceTags),
		BoolToSQLite(settings.UseCrossCategoryAffix),
		settings.CategoryAffixMode,
		settings.CategoryAffix,
		BoolToSQLite(settings.UseCustomCategory),
		settings.CustomCategory,
		BoolToSQLite(settings.SkipAutoResumeRSS),
		BoolToSQLite(settings.SkipAutoResumeSeededSearch),
		BoolToSQLite(settings.SkipAutoResumeCompletion),
		BoolToSQLite(settings.SkipAutoResumeWebhook),
		BoolToSQLite(settings.SkipRecheck),
		BoolToSQLite(settings.RescueTitleMismatches),
		BoolToSQLite(settings.SkipPieceBoundarySafetyCheck),
		BoolToSQLite(settings.SeasonPackSkipRepackCompare),
		BoolToSQLite(settings.SeasonPackSimplifyHDRCompare),
		BoolToSQLite(settings.SeasonPackSimplifyWEBCompare),
		BoolToSQLite(settings.SeasonPackSkipYearCompare),
		BoolToSQLite(settings.SeasonPackEnabled),
		BoolToSQLite(settings.SeasonPackAutomationEnabled),
		settings.SeasonPackCoverageThreshold,
		seasonPackTags,
		settings.SeasonPackCategory,
		seasonPackCategoryRules,
		seasonPackTVDBAPIKeyEncrypted,
		seasonPackTVDBPINEncrypted,
		BoolToSQLite(settings.GazelleEnabled),
		redactedAPIKeyEncrypted,
		orpheusAPIKeyEncrypted,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert settings: %w", err)
	}

	return s.GetSettings(ctx)
}

func (s *CrossSeedStore) GetSearchSettings(ctx context.Context) (*CrossSeedSearchSettings, error) {
	query := `
		SELECT instance_id, categories, tags, exclude_categories, exclude_tags, indexer_ids,
		       interval_seconds, cooldown_minutes, max_added_age_days,
		       skip_individual_episodes, torznab_enabled, schedule_enabled,
		       schedule_interval_hours, schedule_max_searches, schedule_max_age_days,
		       created_at, updated_at
		FROM cross_seed_search_settings
		WHERE id = 1
	`

	row := s.db.QueryRowContext(ctx, query)

	var settings CrossSeedSearchSettings
	var instanceID sql.NullInt64
	var categoriesJSON, tagsJSON, excludeCategoriesJSON, excludeTagsJSON, indexersJSON sql.NullString
	var skipIndividualEpisodes, torznabEnabled, scheduleEnabled int
	var createdAt, updatedAt sql.NullTime

	if err := row.Scan(
		&instanceID,
		&categoriesJSON,
		&tagsJSON,
		&excludeCategoriesJSON,
		&excludeTagsJSON,
		&indexersJSON,
		&settings.IntervalSeconds,
		&settings.CooldownMinutes,
		&settings.MaxAddedAgeDays,
		&skipIndividualEpisodes,
		&torznabEnabled,
		&scheduleEnabled,
		&settings.ScheduleIntervalHours,
		&settings.ScheduleMaxSearches,
		&settings.ScheduleMaxAgeDays,
		&createdAt,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DefaultCrossSeedSearchSettings(), nil
		}
		return nil, fmt.Errorf("query search settings: %w", err)
	}

	if instanceID.Valid {
		id := int(instanceID.Int64)
		settings.InstanceID = &id
	}

	if err := decodeStringSlice(categoriesJSON, &settings.Categories); err != nil {
		return nil, fmt.Errorf("decode search categories: %w", err)
	}
	if err := decodeStringSlice(tagsJSON, &settings.Tags); err != nil {
		return nil, fmt.Errorf("decode search tags: %w", err)
	}
	if err := decodeStringSlice(excludeCategoriesJSON, &settings.ExcludeCategories); err != nil {
		return nil, fmt.Errorf("decode search exclude categories: %w", err)
	}
	if err := decodeStringSlice(excludeTagsJSON, &settings.ExcludeTags); err != nil {
		return nil, fmt.Errorf("decode search exclude tags: %w", err)
	}
	if err := decodeIntSlice(indexersJSON, &settings.IndexerIDs); err != nil {
		return nil, fmt.Errorf("decode search indexers: %w", err)
	}
	settings.SkipIndividualEpisodes = SQLiteIntToBool(skipIndividualEpisodes)
	settings.TorznabEnabled = SQLiteIntToBool(torznabEnabled)
	settings.ScheduleEnabled = SQLiteIntToBool(scheduleEnabled)

	if createdAt.Valid {
		settings.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		settings.UpdatedAt = updatedAt.Time
	}

	return &settings, nil
}

func (s *CrossSeedStore) UpsertSearchSettings(ctx context.Context, settings *CrossSeedSearchSettings) (*CrossSeedSearchSettings, error) {
	if settings == nil {
		return nil, errors.New("settings cannot be nil")
	}

	categoryJSON, err := encodeStringSlice(settings.Categories)
	if err != nil {
		return nil, fmt.Errorf("encode search categories: %w", err)
	}
	tagsJSON, err := encodeStringSlice(settings.Tags)
	if err != nil {
		return nil, fmt.Errorf("encode search tags: %w", err)
	}
	excludeCategoriesJSON, err := encodeStringSlice(settings.ExcludeCategories)
	if err != nil {
		return nil, fmt.Errorf("encode search exclude categories: %w", err)
	}
	excludeTagsJSON, err := encodeStringSlice(settings.ExcludeTags)
	if err != nil {
		return nil, fmt.Errorf("encode search exclude tags: %w", err)
	}
	indexerJSON, err := encodeIntSlice(settings.IndexerIDs)
	if err != nil {
		return nil, fmt.Errorf("encode search indexers: %w", err)
	}

	var instanceID any
	if settings.InstanceID != nil {
		instanceID = *settings.InstanceID
	}

	query := `
		INSERT INTO cross_seed_search_settings (
			id, instance_id, categories, tags, exclude_categories, exclude_tags, indexer_ids,
			interval_seconds, cooldown_minutes, max_added_age_days,
			skip_individual_episodes, torznab_enabled, schedule_enabled,
			schedule_interval_hours, schedule_max_searches, schedule_max_age_days
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			instance_id = excluded.instance_id,
			categories = excluded.categories,
			tags = excluded.tags,
			exclude_categories = excluded.exclude_categories,
			exclude_tags = excluded.exclude_tags,
			indexer_ids = excluded.indexer_ids,
			interval_seconds = excluded.interval_seconds,
			cooldown_minutes = excluded.cooldown_minutes,
			max_added_age_days = excluded.max_added_age_days,
			skip_individual_episodes = excluded.skip_individual_episodes,
			torznab_enabled = excluded.torznab_enabled,
			schedule_enabled = excluded.schedule_enabled,
			schedule_interval_hours = excluded.schedule_interval_hours,
			schedule_max_searches = excluded.schedule_max_searches,
			schedule_max_age_days = excluded.schedule_max_age_days
	`

	_, err = s.db.ExecContext(ctx, query,
		1,
		instanceID,
		categoryJSON,
		tagsJSON,
		excludeCategoriesJSON,
		excludeTagsJSON,
		indexerJSON,
		settings.IntervalSeconds,
		settings.CooldownMinutes,
		settings.MaxAddedAgeDays,
		BoolToSQLite(settings.SkipIndividualEpisodes),
		BoolToSQLite(settings.TorznabEnabled),
		BoolToSQLite(settings.ScheduleEnabled),
		settings.ScheduleIntervalHours,
		settings.ScheduleMaxSearches,
		settings.ScheduleMaxAgeDays,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert search settings: %w", err)
	}

	return s.GetSearchSettings(ctx)
}

func (s *CrossSeedStore) LastScheduledSearchRun(ctx context.Context) (time.Time, bool, error) {
	var last sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT last_scheduled_run_at FROM cross_seed_search_settings WHERE id = 1`).Scan(&last)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("query last scheduled search run: %w", err)
	}
	return last.Time, last.Valid, nil
}

func (s *CrossSeedStore) SetLastScheduledSearchRun(ctx context.Context, startedAt time.Time) error {
	const query = `
		INSERT INTO cross_seed_search_settings (id, last_scheduled_run_at) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET last_scheduled_run_at = excluded.last_scheduled_run_at
	`
	if _, err := s.db.ExecContext(ctx, query, startedAt); err != nil {
		return fmt.Errorf("save last scheduled search run: %w", err)
	}
	return nil
}

func (s *CrossSeedStore) ClearLastScheduledSearchRun(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE cross_seed_search_settings SET last_scheduled_run_at = NULL WHERE id = 1`); err != nil {
		return fmt.Errorf("clear last scheduled search run: %w", err)
	}
	return nil
}

func (s *CrossSeedStore) CreateRun(ctx context.Context, run *CrossSeedRun) (*CrossSeedRun, error) {
	if run == nil {
		return nil, errors.New("run cannot be nil")
	}
	now := time.Now().UTC()
	if run.StartedAt.IsZero() {
		run.StartedAt = now
	}

	resultsJSON, err := encodeRunResults(run.Results)
	if err != nil {
		return nil, fmt.Errorf("encode results: %w", err)
	}

	query := `
		INSERT INTO cross_seed_runs (
			triggered_by, mode, status, started_at,
			total_feed_items, candidates_found, cross_seeds_added,
			candidates_failed, candidates_skipped, message,
			error_message, results_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`

	var runID int64
	err = s.db.QueryRowContext(ctx, query,
		run.TriggeredBy,
		run.Mode,
		run.Status,
		run.StartedAt,
		run.TotalFeedItems,
		run.CandidatesFound,
		run.CrossSeedsAdded,
		run.CandidatesFailed,
		run.CandidatesSkipped,
		run.Message,
		run.ErrorMessage,
		resultsJSON,
	).Scan(&runID)
	if err != nil {
		return nil, fmt.Errorf("insert run: %w", err)
	}

	const pruneQuery = `
		DELETE FROM cross_seed_runs
		WHERE id NOT IN (
			SELECT id FROM cross_seed_runs
			ORDER BY started_at DESC
			LIMIT 10
		)
	`
	if _, err := s.db.ExecContext(ctx, pruneQuery); err != nil {
		return nil, fmt.Errorf("prune old runs: %w", err)
	}

	return s.GetRun(ctx, runID)
}

func (s *CrossSeedStore) UpdateRun(ctx context.Context, run *CrossSeedRun) (*CrossSeedRun, error) {
	if run == nil {
		return nil, errors.New("run cannot be nil")
	}
	if run.ID == 0 {
		return nil, errors.New("run ID cannot be zero")
	}

	resultsJSON, err := encodeRunResults(run.Results)
	if err != nil {
		return nil, fmt.Errorf("encode results: %w", err)
	}

	query := `
		UPDATE cross_seed_runs
		SET status = ?, completed_at = ?, total_feed_items = ?,
		    candidates_found = ?, cross_seeds_added = ?, candidates_failed = ?,
		    candidates_skipped = ?, message = ?, error_message = ?, results_json = ?
		WHERE id = ?
	`

	_, err = s.db.ExecContext(ctx, query,
		run.Status,
		run.CompletedAt,
		run.TotalFeedItems,
		run.CandidatesFound,
		run.CrossSeedsAdded,
		run.CandidatesFailed,
		run.CandidatesSkipped,
		run.Message,
		run.ErrorMessage,
		resultsJSON,
		run.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("update run: %w", err)
	}

	return s.GetRun(ctx, run.ID)
}

func (s *CrossSeedStore) GetRun(ctx context.Context, id int64) (*CrossSeedRun, error) {
	query := `
		SELECT id, triggered_by, mode, status, started_at, completed_at,
		       total_feed_items, candidates_found, cross_seeds_added,
		       candidates_failed, candidates_skipped, message, error_message,
		       results_json, created_at
		FROM cross_seed_runs
		WHERE id = ?
	`

	row := s.db.QueryRowContext(ctx, query, id)
	run, err := scanCrossSeedRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return run, err
}

func (s *CrossSeedStore) GetLatestRun(ctx context.Context) (*CrossSeedRun, error) {
	query := `
		SELECT id, triggered_by, mode, status, started_at, completed_at,
		       total_feed_items, candidates_found, cross_seeds_added,
		       candidates_failed, candidates_skipped, message, error_message,
		       results_json, created_at
		FROM cross_seed_runs
		ORDER BY started_at DESC
		LIMIT 1
	`

	row := s.db.QueryRowContext(ctx, query)
	run, err := scanCrossSeedRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return run, err
}

func (s *CrossSeedStore) ListRuns(ctx context.Context, limit, offset int) ([]*CrossSeedRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	query := `
		SELECT id, triggered_by, mode, status, started_at, completed_at,
		       total_feed_items, candidates_found, cross_seeds_added,
		       candidates_failed, candidates_skipped, message, error_message,
		       results_json, created_at
		FROM cross_seed_runs
		ORDER BY started_at DESC
		LIMIT ? OFFSET ?
	`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	var runs []*CrossSeedRun
	for rows.Next() {
		run, err := scanCrossSeedRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runs: %w", err)
	}

	return runs, nil
}

func (s *CrossSeedStore) CreateSearchRun(ctx context.Context, run *CrossSeedSearchRun) (*CrossSeedSearchRun, error) {
	if run == nil {
		return nil, errors.New("run cannot be nil")
	}
	if run.InstanceID <= 0 {
		return nil, errors.New("instance id must be positive")
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}
	if run.Kind == "" {
		run.Kind = CrossSeedSearchRunKindManual
	}

	filtersJSON, err := encodeSearchFilters(run.Filters)
	if err != nil {
		return nil, fmt.Errorf("encode filters: %w", err)
	}
	indexersJSON, err := encodeIntSlice(run.IndexerIDs)
	if err != nil {
		return nil, fmt.Errorf("encode indexers: %w", err)
	}
	resultsJSON, err := encodeSearchResults(run.Results)
	if err != nil {
		return nil, fmt.Errorf("encode results: %w", err)
	}
	gazelleJSON, err := encodeGazelleSummary(run.GazelleSummary)
	if err != nil {
		return nil, fmt.Errorf("encode RED/OPS summary: %w", err)
	}

	const query = `
		INSERT INTO cross_seed_search_runs (
			instance_id, kind, status, started_at, total_torrents, processed,
			torrents_with_cross_seeds, cross_seeds_added, torrents_failed, torrents_skipped, message,
			error_message, filters_json, indexer_ids_json, interval_seconds,
			cooldown_minutes, results_json, gazelle_summary_json, search_limit, waiting_torrents
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`

	var insertedID int64
	err = s.db.QueryRowContext(ctx, query,
		run.InstanceID,
		run.Kind,
		run.Status,
		run.StartedAt,
		run.TotalTorrents,
		run.Processed,
		run.TorrentsWithCrossSeeds,
		run.CrossSeedsAdded,
		run.TorrentsFailed,
		run.TorrentsSkipped,
		run.Message,
		run.ErrorMessage,
		filtersJSON,
		indexersJSON,
		run.IntervalSeconds,
		run.CooldownMinutes,
		resultsJSON,
		gazelleJSON,
		run.SearchLimit,
		run.WaitingTorrents,
	).Scan(&insertedID)
	if err != nil {
		return nil, fmt.Errorf("insert search run: %w", err)
	}

	const pruneQuery = `
		DELETE FROM cross_seed_search_runs
		WHERE instance_id = ? AND id NOT IN (
			SELECT id FROM cross_seed_search_runs
			WHERE instance_id = ?
			ORDER BY started_at DESC
			LIMIT 10
		)
	`
	if _, err := s.db.ExecContext(ctx, pruneQuery, run.InstanceID, run.InstanceID); err != nil {
		return nil, fmt.Errorf("prune old search runs: %w", err)
	}

	return s.GetSearchRun(ctx, insertedID)
}

func (s *CrossSeedStore) UpdateSearchRun(ctx context.Context, run *CrossSeedSearchRun) error {
	if run == nil {
		return errors.New("run cannot be nil")
	}
	if run.ID == 0 {
		return errors.New("run ID cannot be zero")
	}

	resultsJSON, err := encodeSearchResults(run.Results)
	if err != nil {
		return fmt.Errorf("encode results: %w", err)
	}
	filtersJSON, err := encodeSearchFilters(run.Filters)
	if err != nil {
		return fmt.Errorf("encode filters: %w", err)
	}
	indexersJSON, err := encodeIntSlice(run.IndexerIDs)
	if err != nil {
		return fmt.Errorf("encode indexers: %w", err)
	}
	gazelleJSON, err := encodeGazelleSummary(run.GazelleSummary)
	if err != nil {
		return fmt.Errorf("encode RED/OPS summary: %w", err)
	}

	const query = `
		UPDATE cross_seed_search_runs SET
			status = ?,
			started_at = ?,
			completed_at = ?,
			total_torrents = ?,
			processed = ?,
			torrents_with_cross_seeds = ?,
			cross_seeds_added = ?,
			torrents_failed = ?,
			torrents_skipped = ?,
			message = ?,
			error_message = ?,
			filters_json = ?,
			indexer_ids_json = ?,
			interval_seconds = ?,
			cooldown_minutes = ?,
			results_json = ?,
			gazelle_summary_json = ?,
			waiting_torrents = ?
		WHERE id = ?
	`

	var completed any
	if run.CompletedAt != nil {
		completed = run.CompletedAt
	}

	if _, err := s.db.ExecContext(ctx, query,
		run.Status,
		run.StartedAt,
		completed,
		run.TotalTorrents,
		run.Processed,
		run.TorrentsWithCrossSeeds,
		run.CrossSeedsAdded,
		run.TorrentsFailed,
		run.TorrentsSkipped,
		run.Message,
		run.ErrorMessage,
		filtersJSON,
		indexersJSON,
		run.IntervalSeconds,
		run.CooldownMinutes,
		resultsJSON,
		gazelleJSON,
		run.WaitingTorrents,
		run.ID,
	); err != nil {
		return fmt.Errorf("update search run: %w", err)
	}

	return nil
}

func (s *CrossSeedStore) UpdateSearchRunProgress(ctx context.Context, run *CrossSeedSearchRun) error {
	gazelleJSON, err := encodeGazelleSummary(run.GazelleSummary)
	if err != nil {
		return fmt.Errorf("encode RED/OPS summary: %w", err)
	}

	const query = `
		UPDATE cross_seed_search_runs SET
			status = ?,
			total_torrents = ?,
			processed = ?,
			torrents_with_cross_seeds = ?,
			cross_seeds_added = ?,
			torrents_failed = ?,
			torrents_skipped = ?,
			gazelle_summary_json = ?
		WHERE id = ?
	`

	if _, err := s.db.ExecContext(ctx, query,
		run.Status,
		run.TotalTorrents,
		run.Processed,
		run.TorrentsWithCrossSeeds,
		run.CrossSeedsAdded,
		run.TorrentsFailed,
		run.TorrentsSkipped,
		gazelleJSON,
		run.ID,
	); err != nil {
		return fmt.Errorf("update search run progress: %w", err)
	}

	return nil
}

func (s *CrossSeedStore) GetSearchRun(ctx context.Context, id int64) (*CrossSeedSearchRun, error) {
	const query = `
		SELECT id, instance_id, kind, status, started_at, completed_at,
		       total_torrents, processed, torrents_with_cross_seeds, cross_seeds_added,
		       torrents_failed, torrents_skipped, message, error_message, filters_json,
		       indexer_ids_json, interval_seconds, cooldown_minutes,
		       results_json, gazelle_summary_json, search_limit, waiting_torrents, created_at
		FROM cross_seed_search_runs
		WHERE id = ?
	`

	row := s.db.QueryRowContext(ctx, query, id)
	return scanCrossSeedSearchRun(row)
}

func (s *CrossSeedStore) ListSearchRuns(ctx context.Context, instanceID, limit, offset int) ([]*CrossSeedSearchRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	const query = `
		SELECT id, instance_id, kind, status, started_at, completed_at,
		       total_torrents, processed, torrents_with_cross_seeds, cross_seeds_added,
		       torrents_failed, torrents_skipped, message, error_message, filters_json,
		       indexer_ids_json, interval_seconds, cooldown_minutes,
		       results_json, gazelle_summary_json, search_limit, waiting_torrents, created_at
		FROM cross_seed_search_runs
		WHERE instance_id = ?
		ORDER BY started_at DESC
		LIMIT ? OFFSET ?
	`

	rows, err := s.db.QueryContext(ctx, query, instanceID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list search runs: %w", err)
	}
	defer rows.Close()

	var runs []*CrossSeedSearchRun
	for rows.Next() {
		run, err := scanCrossSeedSearchRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan search run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search runs: %w", err)
	}

	return runs, nil
}

func (s *CrossSeedStore) UpsertSearchHistory(ctx context.Context, instanceID int, torrentHash string, searchedAt time.Time) error {
	if instanceID <= 0 || strings.TrimSpace(torrentHash) == "" {
		return errors.New("invalid search history parameters")
	}

	const query = `
		INSERT INTO cross_seed_search_history (instance_id, torrent_hash, last_searched_at)
		VALUES (?, ?, ?)
		ON CONFLICT(instance_id, torrent_hash) DO UPDATE SET
			last_searched_at = excluded.last_searched_at
	`

	if _, err := s.db.ExecContext(ctx, query, instanceID, torrentHash, searchedAt); err != nil {
		return fmt.Errorf("upsert search history: %w", err)
	}
	return nil
}

func (s *CrossSeedStore) GetSearchHistory(ctx context.Context, instanceID int, torrentHash string) (time.Time, bool, error) {
	const query = `
		SELECT last_searched_at
		FROM cross_seed_search_history
		WHERE instance_id = ? AND torrent_hash = ?
	`

	var last time.Time
	err := s.db.QueryRowContext(ctx, query, instanceID, torrentHash).Scan(&last)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, fmt.Errorf("get search history: %w", err)
	}

	return last, true, nil
}

func (s *CrossSeedStore) GetLatestSearchHistory(ctx context.Context, torrentHash string) (time.Time, bool, error) {
	const query = `
		SELECT last_searched_at
		FROM cross_seed_search_history
		WHERE torrent_hash = ?
		ORDER BY last_searched_at DESC
		LIMIT 1
	`

	var last time.Time
	err := s.db.QueryRowContext(ctx, query, torrentHash).Scan(&last)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, fmt.Errorf("get latest search history: %w", err)
	}

	return last, true, nil
}

func (s *CrossSeedStore) UpsertIndexerSearchHistory(ctx context.Context, instanceID int, torrentHash string, indexerIDs []int, searchedAt time.Time) error {
	if instanceID <= 0 || strings.TrimSpace(torrentHash) == "" {
		return errors.New("invalid search history parameters")
	}
	if len(indexerIDs) == 0 {
		return nil
	}
	indexerIDs = slices.Compact(slices.Sorted(slices.Values(indexerIDs)))

	var query strings.Builder
	query.WriteString(`
		INSERT INTO cross_seed_search_history_indexers (instance_id, torrent_hash, indexer_id, last_searched_at)
		VALUES `)
	args := make([]any, 0, len(indexerIDs)*4)
	for i, indexerID := range indexerIDs {
		if i > 0 {
			query.WriteString(", ")
		}
		query.WriteString("(?, ?, ?, ?)")
		args = append(args, instanceID, torrentHash, indexerID, searchedAt)
	}
	query.WriteString(`
		ON CONFLICT(instance_id, torrent_hash, indexer_id) DO UPDATE SET
			last_searched_at = excluded.last_searched_at
	`)

	if _, err := s.db.ExecContext(ctx, query.String(), args...); err != nil {
		return fmt.Errorf("upsert indexer search history: %w", err)
	}
	return nil
}

func (s *CrossSeedStore) GetIndexerSearchHistory(ctx context.Context, instanceID int, torrentHash string) (map[int]time.Time, error) {
	const query = `
		SELECT indexer_id, last_searched_at
		FROM cross_seed_search_history_indexers
		WHERE instance_id = ? AND torrent_hash = ?
	`

	rows, err := s.db.QueryContext(ctx, query, instanceID, torrentHash)
	if err != nil {
		return nil, fmt.Errorf("get indexer search history: %w", err)
	}
	defer rows.Close()

	history := make(map[int]time.Time)
	for rows.Next() {
		var indexerID int
		var last time.Time
		if err := rows.Scan(&indexerID, &last); err != nil {
			return nil, fmt.Errorf("scan indexer search history: %w", err)
		}
		history[indexerID] = last
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate indexer search history: %w", err)
	}
	return history, nil
}

func (s *CrossSeedStore) HasProcessedFeedItem(ctx context.Context, guid string, indexerID int) (bool, CrossSeedFeedItemStatus, error) {
	query := `
		SELECT last_status
		FROM cross_seed_feed_items
		WHERE guid = ? AND indexer_id = ?
	`

	var status string
	err := s.db.QueryRowContext(ctx, query, guid, indexerID).Scan(&status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, CrossSeedFeedItemStatusPending, nil
		}
		return false, CrossSeedFeedItemStatusPending, fmt.Errorf("query feed item: %w", err)
	}

	return true, CrossSeedFeedItemStatus(status), nil
}

func (s *CrossSeedStore) MarkFeedItem(ctx context.Context, item *CrossSeedFeedItem) error {
	if item == nil {
		return errors.New("item cannot be nil")
	}
	if item.GUID == "" || item.IndexerID == 0 {
		return errors.New("item must include GUID and indexer ID")
	}

	now := time.Now().UTC()
	if item.FirstSeenAt.IsZero() {
		item.FirstSeenAt = now
	}
	if item.LastSeenAt.IsZero() {
		item.LastSeenAt = now
	}

	lastSeenAt := item.LastSeenAt.Truncate(24 * time.Hour)

	query := `
		INSERT INTO cross_seed_feed_items (
			guid, indexer_id, title, first_seen_at,
			last_seen_at, last_status, info_hash
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(guid, indexer_id) DO UPDATE SET
			title = excluded.title,
			last_seen_at = excluded.last_seen_at,
			last_status = excluded.last_status,
			info_hash = COALESCE(excluded.info_hash, cross_seed_feed_items.info_hash)
	`

	_, err := s.db.ExecContext(ctx, query,
		item.GUID,
		item.IndexerID,
		item.Title,
		item.FirstSeenAt,
		lastSeenAt,
		item.LastStatus,
		item.InfoHash,
	)
	if err != nil {
		return fmt.Errorf("mark feed item: %w", err)
	}

	return nil
}

func (s *CrossSeedStore) TouchFeedItem(ctx context.Context, guid string, indexerID int, seenAt time.Time) error {
	day := seenAt.UTC().Truncate(24 * time.Hour)
	query := `
		UPDATE cross_seed_feed_items
		SET last_seen_at = ?
		WHERE guid = ? AND indexer_id = ? AND last_seen_at < ?
	`

	if _, err := s.db.ExecContext(ctx, query, day, guid, indexerID, day); err != nil {
		return fmt.Errorf("touch feed item: %w", err)
	}

	return nil
}

func (s *CrossSeedStore) PruneFeedItems(ctx context.Context, olderThan time.Time) (int64, error) {
	query := `
		DELETE FROM cross_seed_feed_items
		WHERE last_seen_at < ? AND last_status IN ('processed', 'skipped', 'failed')
	`

	result, err := s.db.ExecContext(ctx, query, olderThan)
	if err != nil {
		return 0, fmt.Errorf("prune feed items: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, nil //nolint:nilerr // the delete succeeded; the row count is informational only
	}

	return rows, nil
}

func (s *CrossSeedStore) MarkInterruptedSearchRuns(ctx context.Context, completedAt time.Time, message string) (int64, error) {
	query := `
		UPDATE cross_seed_search_runs
		SET status = 'failed', completed_at = ?, error_message = ?
		WHERE status = 'running'
	`

	result, err := s.db.ExecContext(ctx, query, completedAt, message)
	if err != nil {
		return 0, fmt.Errorf("mark interrupted search runs: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("get rows affected: %w", err)
	}

	return rows, nil
}

func (s *CrossSeedStore) MarkInterruptedAutomationRuns(ctx context.Context, completedAt time.Time, message string) (int64, error) {
	query := `
		UPDATE cross_seed_runs
		SET status = 'failed', completed_at = ?, error_message = ?
		WHERE status = 'running'
	`

	result, err := s.db.ExecContext(ctx, query, completedAt, message)
	if err != nil {
		return 0, fmt.Errorf("mark interrupted automation runs: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("get rows affected: %w", err)
	}

	return rows, nil
}

func scanCrossSeedRun(scanner interface {
	Scan(dest ...any) error
}) (*CrossSeedRun, error) {
	var run CrossSeedRun
	var completedAt sql.NullTime
	var resultsJSON sql.NullString

	err := scanner.Scan(
		&run.ID,
		&run.TriggeredBy,
		&run.Mode,
		&run.Status,
		&run.StartedAt,
		&completedAt,
		&run.TotalFeedItems,
		&run.CandidatesFound,
		&run.CrossSeedsAdded,
		&run.CandidatesFailed,
		&run.CandidatesSkipped,
		&run.Message,
		&run.ErrorMessage,
		&resultsJSON,
		&run.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}

	if err := decodeRunResults(resultsJSON, &run.Results); err != nil {
		return nil, fmt.Errorf("decode run results: %w", err)
	}

	return &run, nil
}

func scanCrossSeedSearchRun(scanner interface {
	Scan(dest ...any) error
}) (*CrossSeedSearchRun, error) {
	var (
		run          CrossSeedSearchRun
		completedAt  sql.NullTime
		filtersJSON  sql.NullString
		indexersJSON sql.NullString
		resultsJSON  sql.NullString
		gazelleJSON  sql.NullString
	)

	err := scanner.Scan(
		&run.ID,
		&run.InstanceID,
		&run.Kind,
		&run.Status,
		&run.StartedAt,
		&completedAt,
		&run.TotalTorrents,
		&run.Processed,
		&run.TorrentsWithCrossSeeds,
		&run.CrossSeedsAdded,
		&run.TorrentsFailed,
		&run.TorrentsSkipped,
		&run.Message,
		&run.ErrorMessage,
		&filtersJSON,
		&indexersJSON,
		&run.IntervalSeconds,
		&run.CooldownMinutes,
		&resultsJSON,
		&gazelleJSON,
		&run.SearchLimit,
		&run.WaitingTorrents,
		&run.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}
	if err := decodeSearchFilters(filtersJSON, &run.Filters); err != nil {
		return nil, fmt.Errorf("decode filters: %w", err)
	}
	if err := decodeIntSlice(indexersJSON, &run.IndexerIDs); err != nil {
		return nil, fmt.Errorf("decode indexer IDs: %w", err)
	}
	if err := decodeSearchResults(resultsJSON, &run.Results); err != nil {
		return nil, fmt.Errorf("decode search results: %w", err)
	}
	if gazelleJSON.Valid && gazelleJSON.String != "" {
		if err := jsonv2.Unmarshal([]byte(gazelleJSON.String), &run.GazelleSummary); err != nil {
			return nil, fmt.Errorf("decode RED/OPS summary: %w", err)
		}
		run.GazelleSummary = withoutRetiredGazelleReasons(run.GazelleSummary)
	}

	return &run, nil
}

func withoutRetiredGazelleReasons(summary []CrossSeedGazelleSiteSummary) []CrossSeedGazelleSiteSummary {
	var kept []CrossSeedGazelleSiteSummary
	for _, site := range summary {
		delete(site.NotAsked, "disabled")
		delete(site.NotAsked, "no_api_key")
		if len(site.NotAsked) == 0 {
			site.NotAsked = nil
		}
		if site.Asked == 0 && site.Matched == 0 && site.Failed == 0 && site.NotAsked == nil {
			continue
		}
		kept = append(kept, site)
	}
	return kept
}

func encodeGazelleSummary(summary []CrossSeedGazelleSiteSummary) (any, error) {
	if len(summary) == 0 {
		return nil, nil
	}
	data, err := jsonv2.Marshal(summary)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func encodeStringSlice(values []string) (string, error) {
	if values == nil {
		values = []string{}
	}
	data, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func encodeIntSlice(values []int) (string, error) {
	if values == nil {
		values = []int{}
	}
	data, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeStringSlice(src sql.NullString, dest *[]string) error {
	if !src.Valid || src.String == "" {
		*dest = []string{}
		return nil
	}
	var tmp []string
	if err := json.Unmarshal([]byte(src.String), &tmp); err != nil {
		return err
	}
	*dest = tmp
	return nil
}

func decodeStringSliceWithDefault(src sql.NullString, dest *[]string, defaultVal []string) error {
	if !src.Valid || src.String == "" {
		*dest = defaultVal
		return nil
	}
	var tmp []string
	if err := json.Unmarshal([]byte(src.String), &tmp); err != nil {
		return err
	}
	*dest = tmp
	return nil
}

func encodeJSONSlice[T any](items []T) (string, error) {
	if items == nil {
		items = []T{}
	}
	data, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeJSONSlice[T any](src sql.NullString, dest *[]T) error {
	if !src.Valid || src.String == "" {
		*dest = []T{}
		return nil
	}
	var tmp []T
	if err := json.Unmarshal([]byte(src.String), &tmp); err != nil {
		return err
	}
	*dest = tmp
	return nil
}

func decodeIntSlice(src sql.NullString, dest *[]int) error {
	if !src.Valid || src.String == "" {
		*dest = []int{}
		return nil
	}
	var tmp []int
	if err := json.Unmarshal([]byte(src.String), &tmp); err != nil {
		return err
	}
	*dest = tmp
	return nil
}

func encodeRunResults(results []CrossSeedRunResult) (string, error) {
	if results == nil {
		results = []CrossSeedRunResult{}
	}
	data, err := json.Marshal(results)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeRunResults(src sql.NullString, dest *[]CrossSeedRunResult) error {
	if !src.Valid || src.String == "" {
		*dest = []CrossSeedRunResult{}
		return nil
	}
	var tmp []CrossSeedRunResult
	if err := json.Unmarshal([]byte(src.String), &tmp); err != nil {
		return err
	}
	*dest = tmp
	return nil
}

func encodeSearchFilters(filters CrossSeedSearchFilters) (string, error) {
	data, err := json.Marshal(filters)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeSearchFilters(src sql.NullString, dest *CrossSeedSearchFilters) error {
	if dest == nil {
		return errors.New("destination cannot be nil")
	}
	if !src.Valid || src.String == "" {
		*dest = CrossSeedSearchFilters{}
		return nil
	}
	var tmp CrossSeedSearchFilters
	if err := json.Unmarshal([]byte(src.String), &tmp); err != nil {
		return err
	}
	*dest = tmp
	return nil
}

func encodeSearchResults(results []CrossSeedSearchResult) (string, error) {
	if results == nil {
		results = []CrossSeedSearchResult{}
	}
	data, err := json.Marshal(results)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

type crossSeedSearchResultPayload struct {
	TorrentHash  string                      `json:"torrentHash"`
	TorrentName  string                      `json:"torrentName"`
	IndexerName  string                      `json:"indexerName"`
	ReleaseTitle string                      `json:"releaseTitle"`
	Added        *bool                       `json:"added,omitempty"`
	Status       CrossSeedSearchResultStatus `json:"status,omitempty"`
	Message      string                      `json:"message,omitempty"`
	ProcessedAt  time.Time                   `json:"processedAt"`

	CrossSeedInstanceID   int    `json:"crossSeedInstanceId,omitzero"`
	CrossSeedInstanceName string `json:"crossSeedInstanceName,omitempty"`
}

func decodeSearchResults(src sql.NullString, dest *[]CrossSeedSearchResult) error {
	if dest == nil {
		return errors.New("destination cannot be nil")
	}
	if !src.Valid || src.String == "" {
		*dest = []CrossSeedSearchResult{}
		return nil
	}
	var payloads []crossSeedSearchResultPayload
	if err := json.Unmarshal([]byte(src.String), &payloads); err != nil {
		return err
	}
	results := make([]CrossSeedSearchResult, 0, len(payloads))
	for _, payload := range payloads {
		status := payload.Status
		if status == "" {
			status = legacyCrossSeedSearchResultStatus(payload.Added, payload.Message)
		}
		results = append(results, CrossSeedSearchResult{
			TorrentHash:  payload.TorrentHash,
			TorrentName:  payload.TorrentName,
			IndexerName:  payload.IndexerName,
			ReleaseTitle: payload.ReleaseTitle,
			Status:       status,
			Message:      payload.Message,
			ProcessedAt:  payload.ProcessedAt,

			CrossSeedInstanceID:   payload.CrossSeedInstanceID,
			CrossSeedInstanceName: payload.CrossSeedInstanceName,
		})
	}
	*dest = results
	return nil
}

func legacyCrossSeedSearchResultStatus(added *bool, message string) CrossSeedSearchResultStatus {
	if added == nil {
		return ""
	}
	if *added {
		return CrossSeedSearchResultStatusAdded
	}
	if isLegacyCrossSeedSearchFailure(message) {
		return CrossSeedSearchResultStatusFailed
	}
	return CrossSeedSearchResultStatusSkipped
}

func isLegacyCrossSeedSearchFailure(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	failurePrefixes := []string{
		"resolve indexers:",
		"analyze torrent:",
		"search failed:",
		"download failed:",
		"cross-seed failed:",
	}
	for _, prefix := range failurePrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}
