// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/domain"
)

// Category affix mode constants.
const (
	CategoryAffixModePrefix = "prefix"
	CategoryAffixModeSuffix = "suffix"
)

// CrossSeedAutomationSettings controls automatic cross-seed behaviour.
// Contains both RSS Automation-specific settings and global cross-seed settings.
type CrossSeedAutomationSettings struct {
	// RSS Automation settings
	Enabled            bool    `json:"enabled"`            // Enable/disable RSS automation
	RunIntervalMinutes int     `json:"runIntervalMinutes"` // RSS: interval between RSS feed polls (min: 30 minutes, default: 120)
	StartPaused        bool    `json:"startPaused"`        // RSS: start added torrents paused
	Category           *string `json:"category,omitempty"` // RSS: category for added torrents
	TargetInstanceIDs  []int   `json:"targetInstanceIds"`  // RSS: instances to add cross-seeds to
	TargetIndexerIDs   []int   `json:"targetIndexerIds"`   // RSS: indexers to poll for RSS feeds
	MaxResultsPerRun   int     `json:"maxResultsPerRun"`   // Deprecated: automation processes full feeds; retained for backward compatibility

	// RSS source filtering: filter which LOCAL torrents are considered when checking RSS feeds.
	// Empty arrays mean "all" (no filtering).
	RSSSourceCategories        []string `json:"rssSourceCategories"`        // Only match against torrents in these categories
	RSSSourceTags              []string `json:"rssSourceTags"`              // Only match against torrents with these tags
	RSSSourceExcludeCategories []string `json:"rssSourceExcludeCategories"` // Skip torrents in these categories
	RSSSourceExcludeTags       []string `json:"rssSourceExcludeTags"`       // Skip torrents with these tags

	// Webhook source filtering: filter which LOCAL torrents are considered when checking webhook requests.
	// Empty arrays mean "all" (no filtering).
	WebhookSourceCategories        []string `json:"webhookSourceCategories"`        // Only match against torrents in these categories
	WebhookSourceTags              []string `json:"webhookSourceTags"`              // Only match against torrents with these tags
	WebhookSourceExcludeCategories []string `json:"webhookSourceExcludeCategories"` // Skip torrents in these categories
	WebhookSourceExcludeTags       []string `json:"webhookSourceExcludeTags"`       // Skip torrents with these tags

	// Global cross-seed settings (apply to both RSS Automation and Seeded Torrent Search)
	FindIndividualEpisodes       bool    `json:"findIndividualEpisodes"`       // Match season packs with individual episodes
	SizeMismatchTolerancePercent float64 `json:"sizeMismatchTolerancePercent"` // Size tolerance for matching (default: 5%)
	UseCategoryFromIndexer       bool    `json:"useCategoryFromIndexer"`       // Use indexer name as category for cross-seeds
	RunExternalProgramID         *int    `json:"runExternalProgramId"`         // Optional external program to run after successful cross-seed injection

	// Source-specific tagging: tags applied based on how the cross-seed was discovered.
	// Each defaults to ["cross-seed"]. Users can add source-specific tags like "rss", "seeded-search", etc.
	RSSAutomationTags    []string `json:"rssAutomationTags"`    // Tags for RSS automation results
	SeededSearchTags     []string `json:"seededSearchTags"`     // Tags for seeded torrent search results
	CompletionSearchTags []string `json:"completionSearchTags"` // Tags for completion-triggered search results
	WebhookTags          []string `json:"webhookTags"`          // Tags for /apply webhook results
	InheritSourceTags    bool     `json:"inheritSourceTags"`    // Also copy tags from the matched source torrent

	// Category affix: add prefix or suffix to the original category name
	UseCrossCategoryAffix bool   `json:"useCrossCategoryAffix"` // Enable category affix
	CategoryAffixMode     string `json:"categoryAffixMode"`     // "prefix" or "suffix"
	CategoryAffix         string `json:"categoryAffix"`         // The affix value (default: ".cross")
	// Custom category: use exact user-specified category without any suffixing
	UseCustomCategory bool   `json:"useCustomCategory"` // Use custom category instead of affix or indexer name
	CustomCategory    string `json:"customCategory"`    // Custom category name when UseCustomCategory is true

	// Skip auto-resume settings per source mode.
	// When enabled, torrents remain paused after hash check instead of auto-resuming.
	SkipAutoResumeRSS            bool `json:"skipAutoResumeRss"`            // Skip auto-resume for RSS automation results
	SkipAutoResumeSeededSearch   bool `json:"skipAutoResumeSeededSearch"`   // Skip auto-resume for seeded torrent search results
	SkipAutoResumeCompletion     bool `json:"skipAutoResumeCompletion"`     // Skip auto-resume for completion-triggered search results
	SkipAutoResumeWebhook        bool `json:"skipAutoResumeWebhook"`        // Skip auto-resume for /apply webhook results
	SkipRecheck                  bool `json:"skipRecheck"`                  // Skip cross-seed matches that require a recheck
	SkipPieceBoundarySafetyCheck bool `json:"skipPieceBoundarySafetyCheck"` // Skip piece boundary safety check (risky: may corrupt existing seeded data)

	// Gazelle (OPS/RED) cross-seed settings.
	// When enabled, qui uses the tracker JSON APIs to find matches for OPS/RED torrents
	// instead of Torznab. Keys are stored encrypted and are redacted in API responses.
	GazelleEnabled bool   `json:"gazelleEnabled"`
	RedactedAPIKey string `json:"redactedApiKey,omitempty"`
	OrpheusAPIKey  string `json:"orpheusApiKey,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// CompletionFilterProvider defines the interface for types that provide completion filter fields.
// Used by InstanceCrossSeedCompletionSettings for per-instance completion configuration.
type CompletionFilterProvider interface {
	GetCategories() []string
	GetTags() []string
	GetExcludeCategories() []string
	GetExcludeTags() []string
}

// DefaultCrossSeedAutomationSettings returns sensible defaults for RSS automation.
// RSS automation is disabled by default with a 2-hour interval.
func DefaultCrossSeedAutomationSettings() *CrossSeedAutomationSettings {
	return &CrossSeedAutomationSettings{
		Enabled:            false, // RSS automation disabled by default
		RunIntervalMinutes: 120,   // RSS: default 2 hours between polls
		StartPaused:        true,
		Category:           nil,
		TargetInstanceIDs:  []int{},
		TargetIndexerIDs:   []int{},
		MaxResultsPerRun:   50,
		// RSS source filtering defaults - empty means no filtering (all torrents)
		RSSSourceCategories:        []string{},
		RSSSourceTags:              []string{},
		RSSSourceExcludeCategories: []string{},
		RSSSourceExcludeTags:       []string{},
		// Webhook source filtering defaults - empty means no filtering (all torrents)
		WebhookSourceCategories:        []string{},
		WebhookSourceTags:              []string{},
		WebhookSourceExcludeCategories: []string{},
		WebhookSourceExcludeTags:       []string{},
		FindIndividualEpisodes:         false, // Default to false - only find season packs when searching with season packs
		SizeMismatchTolerancePercent:   5.0,   // Allow 5% size difference by default
		UseCategoryFromIndexer:         false, // Default to false - don't override categories by default
		RunExternalProgramID:           nil,   // No external program by default
		// Source-specific tagging defaults - all sources default to ["cross-seed"]
		RSSAutomationTags:    []string{"cross-seed"},
		SeededSearchTags:     []string{"cross-seed"},
		CompletionSearchTags: []string{"cross-seed"},
		WebhookTags:          []string{"cross-seed"},
		InheritSourceTags:    false, // Don't copy source torrent tags by default
		// Category isolation - default to true with suffix mode and ".cross" for backwards compatibility
		UseCrossCategoryAffix: true,
		CategoryAffixMode:     CategoryAffixModeSuffix,
		CategoryAffix:         ".cross",
		// Custom category - default to false (use affix mode by default)
		UseCustomCategory: false,
		CustomCategory:    "",
		// Skip auto-resume - default to false to preserve existing behavior
		SkipAutoResumeRSS:            false,
		SkipAutoResumeSeededSearch:   false,
		SkipAutoResumeCompletion:     false,
		SkipAutoResumeWebhook:        false,
		SkipRecheck:                  false,
		SkipPieceBoundarySafetyCheck: true, // Skip by default to maximize matches
		GazelleEnabled:               false,
		RedactedAPIKey:               "",
		OrpheusAPIKey:                "",
		CreatedAt:                    time.Now().UTC(),
		UpdatedAt:                    time.Now().UTC(),
	}
}

// CrossSeedSearchSettings stores defaults for manual seeded torrent searches.
type CrossSeedSearchSettings struct {
	InstanceID      *int      `json:"instanceId"`
	Categories      []string  `json:"categories"`
	Tags            []string  `json:"tags"`
	IndexerIDs      []int     `json:"indexerIds"`
	IntervalSeconds int       `json:"intervalSeconds"`
	CooldownMinutes int       `json:"cooldownMinutes"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// DefaultCrossSeedSearchSettings returns defaults for seeded torrent searches.
func DefaultCrossSeedSearchSettings() *CrossSeedSearchSettings {
	now := time.Now().UTC()
	return &CrossSeedSearchSettings{
		InstanceID:      nil,
		Categories:      []string{},
		Tags:            []string{},
		IndexerIDs:      []int{},
		IntervalSeconds: 60,
		CooldownMinutes: 720,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

// CrossSeedRunStatus indicates the outcome of an automation run.
type CrossSeedRunStatus string

const (
	CrossSeedRunStatusPending CrossSeedRunStatus = "pending"
	CrossSeedRunStatusRunning CrossSeedRunStatus = "running"
	CrossSeedRunStatusSuccess CrossSeedRunStatus = "success"
	CrossSeedRunStatusPartial CrossSeedRunStatus = "partial"
	CrossSeedRunStatusFailed  CrossSeedRunStatus = "failed"
)

// CrossSeedRunMode indicates how the run was triggered.
type CrossSeedRunMode string

const (
	CrossSeedRunModeAuto   CrossSeedRunMode = "auto"
	CrossSeedRunModeManual CrossSeedRunMode = "manual"
)

// CrossSeedRunResult summarises the outcome for a single instance.
type CrossSeedRunResult struct {
	InstanceID         int     `json:"instanceId"`
	InstanceName       string  `json:"instanceName"`
	IndexerName        string  `json:"indexerName,omitempty"`
	Success            bool    `json:"success"`
	Status             string  `json:"status"`
	Message            string  `json:"message,omitempty"`
	MatchedTorrentHash *string `json:"matchedTorrentHash,omitempty"`
	MatchedTorrentName *string `json:"matchedTorrentName,omitempty"`
}

// CrossSeedRun stores the persisted automation run metadata.
type CrossSeedRun struct {
	ID              int64                `json:"id"`
	TriggeredBy     string               `json:"triggeredBy"`
	Mode            CrossSeedRunMode     `json:"mode"`
	Status          CrossSeedRunStatus   `json:"status"`
	StartedAt       time.Time            `json:"startedAt"`
	CompletedAt     *time.Time           `json:"completedAt,omitempty"`
	TotalFeedItems  int                  `json:"totalFeedItems"`
	CandidatesFound int                  `json:"candidatesFound"`
	TorrentsAdded   int                  `json:"torrentsAdded"`
	TorrentsFailed  int                  `json:"torrentsFailed"`
	TorrentsSkipped int                  `json:"torrentsSkipped"`
	Message         *string              `json:"message,omitempty"`
	ErrorMessage    *string              `json:"errorMessage,omitempty"`
	Results         []CrossSeedRunResult `json:"results,omitempty"`
	CreatedAt       time.Time            `json:"createdAt"`
}

// CrossSeedSearchRunStatus represents the lifecycle state of an automated search pass.
type CrossSeedSearchRunStatus string

const (
	CrossSeedSearchRunStatusRunning  CrossSeedSearchRunStatus = "running"
	CrossSeedSearchRunStatusSuccess  CrossSeedSearchRunStatus = "success"
	CrossSeedSearchRunStatusFailed   CrossSeedSearchRunStatus = "failed"
	CrossSeedSearchRunStatusCanceled CrossSeedSearchRunStatus = "canceled"
)

// CrossSeedSearchFilters capture how torrents are selected for automated search runs.
type CrossSeedSearchFilters struct {
	Categories []string `json:"categories"`
	Tags       []string `json:"tags"`
}

// CrossSeedSearchResult records the outcome of processing a single torrent during a search run.
type CrossSeedSearchResult struct {
	TorrentHash  string    `json:"torrentHash"`
	TorrentName  string    `json:"torrentName"`
	IndexerName  string    `json:"indexerName"`
	ReleaseTitle string    `json:"releaseTitle"`
	Added        bool      `json:"added"`
	Message      string    `json:"message,omitempty"`
	ProcessedAt  time.Time `json:"processedAt"`
}

// CrossSeedSearchRun stores metadata for library search automation runs.
type CrossSeedSearchRun struct {
	ID              int64                    `json:"id"`
	InstanceID      int                      `json:"instanceId"`
	Status          CrossSeedSearchRunStatus `json:"status"`
	StartedAt       time.Time                `json:"startedAt"`
	CompletedAt     *time.Time               `json:"completedAt,omitempty"`
	TotalTorrents   int                      `json:"totalTorrents"`
	Processed       int                      `json:"processed"`
	TorrentsAdded   int                      `json:"torrentsAdded"`
	TorrentsFailed  int                      `json:"torrentsFailed"`
	TorrentsSkipped int                      `json:"torrentsSkipped"`
	Message         *string                  `json:"message,omitempty"`
	ErrorMessage    *string                  `json:"errorMessage,omitempty"`
	Filters         CrossSeedSearchFilters   `json:"filters"`
	IndexerIDs      []int                    `json:"indexerIds"`
	IntervalSeconds int                      `json:"intervalSeconds"`
	CooldownMinutes int                      `json:"cooldownMinutes"`
	Results         []CrossSeedSearchResult  `json:"results"`
	CreatedAt       time.Time                `json:"createdAt"`
}

// CrossSeedFeedItemStatus tracks processing state for feed items.
type CrossSeedFeedItemStatus string

const (
	CrossSeedFeedItemStatusPending   CrossSeedFeedItemStatus = "pending"
	CrossSeedFeedItemStatusProcessed CrossSeedFeedItemStatus = "processed"
	CrossSeedFeedItemStatusSkipped   CrossSeedFeedItemStatus = "skipped"
	CrossSeedFeedItemStatusFailed    CrossSeedFeedItemStatus = "failed"
)

// CrossSeedFeedItem tracks GUIDs pulled from indexers to avoid duplicates.
type CrossSeedFeedItem struct {
	GUID        string                  `json:"guid"`
	IndexerID   int                     `json:"indexerId"`
	Title       string                  `json:"title"`
	FirstSeenAt time.Time               `json:"firstSeenAt"`
	LastSeenAt  time.Time               `json:"lastSeenAt"`
	LastStatus  CrossSeedFeedItemStatus `json:"lastStatus"`
	LastRunID   *int64                  `json:"lastRunId,omitempty"`
	InfoHash    *string                 `json:"infoHash,omitempty"`
}

// CrossSeedStore persists automation settings, runs, and feed items.
type CrossSeedStore struct {
	db dbinterface.Querier
	// Used to encrypt/decrypt Gazelle API keys stored in cross_seed_settings.
	encryptionKey []byte
}

// NewCrossSeedStore constructs a new automation store.
func NewCrossSeedStore(db dbinterface.Querier, encryptionKey []byte) (*CrossSeedStore, error) {
	if len(encryptionKey) != 32 {
		return nil, errors.New("encryption key must be 32 bytes")
	}
	return &CrossSeedStore{db: db, encryptionKey: encryptionKey}, nil
}

func (s *CrossSeedStore) encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (s *CrossSeedStore) decrypt(ciphertext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("malformed ciphertext")
	}
	nonce, ciphertextBytes := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func (s *CrossSeedStore) apiKeyRedacted(encrypted string) string {
	if strings.TrimSpace(encrypted) == "" {
		return ""
	}
	return domain.RedactedStr
}

// GetSettings returns the current automation settings or defaults.
func (s *CrossSeedStore) GetSettings(ctx context.Context) (*CrossSeedAutomationSettings, error) {
	query := `
		SELECT enabled, run_interval_minutes, start_paused, category,
		       target_instance_ids, target_indexer_ids,
		       max_results_per_run,
		       rss_source_categories, rss_source_tags,
		       rss_source_exclude_categories, rss_source_exclude_tags,
		       webhook_source_categories, webhook_source_tags,
		       webhook_source_exclude_categories, webhook_source_exclude_tags,
		       find_individual_episodes, size_mismatch_tolerance_percent,
		       use_category_from_indexer, run_external_program_id,
		       rss_automation_tags, seeded_search_tags, completion_search_tags,
		       webhook_tags, inherit_source_tags,
		       use_cross_category_affix, category_affix_mode, category_affix,
		       use_custom_category, custom_category,
		       skip_auto_resume_rss, skip_auto_resume_seeded_search,
		       skip_auto_resume_completion, skip_auto_resume_webhook,
		       skip_recheck, skip_piece_boundary_safety_check,
		       gazelle_enabled, redacted_api_key_encrypted, orpheus_api_key_encrypted,
		       created_at, updated_at
		FROM cross_seed_settings
		WHERE id = 1
	`

	row := s.db.QueryRowContext(ctx, query)

	var settings CrossSeedAutomationSettings
	var category sql.NullString
	var instancesJSON, indexersJSON sql.NullString
	var rssSourceCategories, rssSourceTags, rssSourceExcludeCategories, rssSourceExcludeTags sql.NullString
	var webhookSourceCategories, webhookSourceTags, webhookSourceExcludeCategories, webhookSourceExcludeTags sql.NullString
	var rssAutomationTags, seededSearchTags, completionSearchTags, webhookTags sql.NullString
	var runExternalProgramID sql.NullInt64
	var gazelleEnabled bool
	var redactedAPIKeyEncrypted, orpheusAPIKeyEncrypted sql.NullString
	var createdAt, updatedAt sql.NullTime

	err := row.Scan(
		&settings.Enabled,
		&settings.RunIntervalMinutes,
		&settings.StartPaused,
		&category,
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
		&settings.FindIndividualEpisodes,
		&settings.SizeMismatchTolerancePercent,
		&settings.UseCategoryFromIndexer,
		&runExternalProgramID,
		&rssAutomationTags,
		&seededSearchTags,
		&completionSearchTags,
		&webhookTags,
		&settings.InheritSourceTags,
		&settings.UseCrossCategoryAffix,
		&settings.CategoryAffixMode,
		&settings.CategoryAffix,
		&settings.UseCustomCategory,
		&settings.CustomCategory,
		&settings.SkipAutoResumeRSS,
		&settings.SkipAutoResumeSeededSearch,
		&settings.SkipAutoResumeCompletion,
		&settings.SkipAutoResumeWebhook,
		&settings.SkipRecheck,
		&settings.SkipPieceBoundarySafetyCheck,
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

	if category.Valid {
		settings.Category = &category.String
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

	// Decode RSS source filters
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

	// Decode webhook source filters
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

	// Decode source-specific tags with defaults
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

	if createdAt.Valid {
		settings.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		settings.UpdatedAt = updatedAt.Time
	}

	settings.GazelleEnabled = gazelleEnabled
	if redactedAPIKeyEncrypted.Valid {
		settings.RedactedAPIKey = s.apiKeyRedacted(redactedAPIKeyEncrypted.String)
	}
	if orpheusAPIKeyEncrypted.Valid {
		settings.OrpheusAPIKey = s.apiKeyRedacted(orpheusAPIKeyEncrypted.String)
	}

	return &settings, nil
}

// GetDecryptedGazelleAPIKey returns the decrypted Gazelle API key for the given host.
// Supported hosts: redacted.sh, orpheus.network.
func (s *CrossSeedStore) GetDecryptedGazelleAPIKey(ctx context.Context, host string) (string, bool, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return "", false, nil
	}

	col := ""
	switch host {
	case "redacted.sh":
		col = "redacted_api_key_encrypted"
	case "orpheus.network":
		col = "orpheus_api_key_encrypted"
	default:
		return "", false, nil
	}

	var enabled bool
	var encrypted sql.NullString
	q := fmt.Sprintf(`SELECT gazelle_enabled, %s FROM cross_seed_settings WHERE id = 1`, col)
	if err := s.db.QueryRowContext(ctx, q).Scan(&enabled, &encrypted); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	if !enabled || !encrypted.Valid || strings.TrimSpace(encrypted.String) == "" {
		return "", false, nil
	}
	plain, err := s.decrypt(encrypted.String)
	if err != nil {
		return "", false, err
	}
	return plain, true, nil
}

// UpsertSettings saves automation settings and returns the updated value.
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

	// Encode RSS source filters
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

	// Encode webhook source filters
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

	// Encode source-specific tags
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

	var existingRedactedEncrypted string
	var existingOrpheusEncrypted string
	{
		var red, ops sql.NullString
		queryErr := s.db.QueryRowContext(ctx, `
				SELECT redacted_api_key_encrypted, orpheus_api_key_encrypted
				FROM cross_seed_settings
				WHERE id = 1
			`).Scan(&red, &ops)
		// Only required when the caller is explicitly requesting "preserve" behavior.
		// If we can't read the existing encrypted values, fail the update rather than silently clearing secrets.
		if queryErr != nil && !errors.Is(queryErr, sql.ErrNoRows) {
			if strings.TrimSpace(settings.RedactedAPIKey) == domain.RedactedStr || strings.TrimSpace(settings.OrpheusAPIKey) == domain.RedactedStr {
				return nil, fmt.Errorf("load existing gazelle api keys: %w", queryErr)
			}
		}
		if red.Valid {
			existingRedactedEncrypted = red.String
		}
		if ops.Valid {
			existingOrpheusEncrypted = ops.String
		}
	}

	redactedAPIKeyEncrypted := ""
	v := strings.TrimSpace(settings.RedactedAPIKey)
	switch v {
	case "":
		// Clear
	case domain.RedactedStr:
		// Preserve existing value
		redactedAPIKeyEncrypted = existingRedactedEncrypted
	default:
		enc, encErr := s.encrypt(v)
		if encErr != nil {
			return nil, fmt.Errorf("encrypt redacted api key: %w", encErr)
		}
		redactedAPIKeyEncrypted = enc
	}

	orpheusAPIKeyEncrypted := ""
	v = strings.TrimSpace(settings.OrpheusAPIKey)
	switch v {
	case "":
		// Clear
	case domain.RedactedStr:
		// Preserve existing value
		orpheusAPIKeyEncrypted = existingOrpheusEncrypted
	default:
		enc, encErr := s.encrypt(v)
		if encErr != nil {
			return nil, fmt.Errorf("encrypt orpheus api key: %w", encErr)
		}
		orpheusAPIKeyEncrypted = enc
	}

	query := `
		INSERT INTO cross_seed_settings (
			id, enabled, run_interval_minutes, start_paused, category,
			target_instance_ids, target_indexer_ids,
			max_results_per_run,
			rss_source_categories, rss_source_tags,
			rss_source_exclude_categories, rss_source_exclude_tags,
			webhook_source_categories, webhook_source_tags,
			webhook_source_exclude_categories, webhook_source_exclude_tags,
			find_individual_episodes, size_mismatch_tolerance_percent,
			use_category_from_indexer, run_external_program_id,
			rss_automation_tags, seeded_search_tags, completion_search_tags,
			webhook_tags, inherit_source_tags,
			use_cross_category_affix, category_affix_mode, category_affix,
			use_custom_category, custom_category,
			skip_auto_resume_rss, skip_auto_resume_seeded_search,
			skip_auto_resume_completion, skip_auto_resume_webhook,
			skip_recheck, skip_piece_boundary_safety_check,
			gazelle_enabled, redacted_api_key_encrypted, orpheus_api_key_encrypted
		) VALUES (
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		)
		ON CONFLICT(id) DO UPDATE SET
			enabled = excluded.enabled,
			run_interval_minutes = excluded.run_interval_minutes,
			start_paused = excluded.start_paused,
			category = excluded.category,
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
			size_mismatch_tolerance_percent = excluded.size_mismatch_tolerance_percent,
			use_category_from_indexer = excluded.use_category_from_indexer,
			run_external_program_id = excluded.run_external_program_id,
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
			skip_piece_boundary_safety_check = excluded.skip_piece_boundary_safety_check,
			gazelle_enabled = excluded.gazelle_enabled,
			redacted_api_key_encrypted = excluded.redacted_api_key_encrypted,
			orpheus_api_key_encrypted = excluded.orpheus_api_key_encrypted
	`

	// Convert *int to any for proper SQL handling
	var runExternalProgramID any
	if settings.RunExternalProgramID != nil {
		runExternalProgramID = *settings.RunExternalProgramID
	}

	var category any
	if settings.Category != nil {
		category = *settings.Category
	}

	_, err = s.db.ExecContext(ctx, query,
		1,
		settings.Enabled,
		settings.RunIntervalMinutes,
		settings.StartPaused,
		category,
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
		settings.FindIndividualEpisodes,
		settings.SizeMismatchTolerancePercent,
		settings.UseCategoryFromIndexer,
		runExternalProgramID,
		rssAutomationTags,
		seededSearchTags,
		completionSearchTags,
		webhookTags,
		settings.InheritSourceTags,
		settings.UseCrossCategoryAffix,
		settings.CategoryAffixMode,
		settings.CategoryAffix,
		settings.UseCustomCategory,
		settings.CustomCategory,
		settings.SkipAutoResumeRSS,
		settings.SkipAutoResumeSeededSearch,
		settings.SkipAutoResumeCompletion,
		settings.SkipAutoResumeWebhook,
		settings.SkipRecheck,
		settings.SkipPieceBoundarySafetyCheck,
		settings.GazelleEnabled,
		redactedAPIKeyEncrypted,
		orpheusAPIKeyEncrypted,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert settings: %w", err)
	}

	return s.GetSettings(ctx)
}

// GetSearchSettings returns the stored seeded search defaults, or defaults when unset.
func (s *CrossSeedStore) GetSearchSettings(ctx context.Context) (*CrossSeedSearchSettings, error) {
	query := `
		SELECT instance_id, categories, tags, indexer_ids,
		       interval_seconds, cooldown_minutes,
		       created_at, updated_at
		FROM cross_seed_search_settings
		WHERE id = 1
	`

	row := s.db.QueryRowContext(ctx, query)

	var settings CrossSeedSearchSettings
	var instanceID sql.NullInt64
	var categoriesJSON, tagsJSON, indexersJSON sql.NullString
	var createdAt, updatedAt sql.NullTime

	if err := row.Scan(
		&instanceID,
		&categoriesJSON,
		&tagsJSON,
		&indexersJSON,
		&settings.IntervalSeconds,
		&settings.CooldownMinutes,
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
	if err := decodeIntSlice(indexersJSON, &settings.IndexerIDs); err != nil {
		return nil, fmt.Errorf("decode search indexers: %w", err)
	}

	if createdAt.Valid {
		settings.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		settings.UpdatedAt = updatedAt.Time
	}

	return &settings, nil
}

// UpsertSearchSettings saves seeded search defaults.
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
	indexerJSON, err := encodeIntSlice(settings.IndexerIDs)
	if err != nil {
		return nil, fmt.Errorf("encode search indexers: %w", err)
	}

	var instanceID interface{}
	if settings.InstanceID != nil {
		instanceID = *settings.InstanceID
	}

	query := `
		INSERT INTO cross_seed_search_settings (
			id, instance_id, categories, tags, indexer_ids,
			interval_seconds, cooldown_minutes
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			instance_id = excluded.instance_id,
			categories = excluded.categories,
			tags = excluded.tags,
			indexer_ids = excluded.indexer_ids,
			interval_seconds = excluded.interval_seconds,
			cooldown_minutes = excluded.cooldown_minutes
	`

	_, err = s.db.ExecContext(ctx, query,
		1,
		instanceID,
		categoryJSON,
		tagsJSON,
		indexerJSON,
		settings.IntervalSeconds,
		settings.CooldownMinutes,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert search settings: %w", err)
	}

	return s.GetSearchSettings(ctx)
}

// CreateRun inserts a new automation run record.
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
			total_feed_items, candidates_found, torrents_added,
			torrents_failed, torrents_skipped, message,
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
		run.TorrentsAdded,
		run.TorrentsFailed,
		run.TorrentsSkipped,
		run.Message,
		run.ErrorMessage,
		resultsJSON,
	).Scan(&runID)
	if err != nil {
		return nil, fmt.Errorf("insert run: %w", err)
	}

	// Prune old runs, keeping only the 10 most recent
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

// UpdateRun updates an existing run with final statistics.
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
		    candidates_found = ?, torrents_added = ?, torrents_failed = ?,
		    torrents_skipped = ?, message = ?, error_message = ?, results_json = ?
		WHERE id = ?
	`

	_, err = s.db.ExecContext(ctx, query,
		run.Status,
		run.CompletedAt,
		run.TotalFeedItems,
		run.CandidatesFound,
		run.TorrentsAdded,
		run.TorrentsFailed,
		run.TorrentsSkipped,
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

// GetRun fetches a single run by ID.
func (s *CrossSeedStore) GetRun(ctx context.Context, id int64) (*CrossSeedRun, error) {
	query := `
		SELECT id, triggered_by, mode, status, started_at, completed_at,
		       total_feed_items, candidates_found, torrents_added,
		       torrents_failed, torrents_skipped, message, error_message,
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

// GetLatestRun returns the most recent automation run.
func (s *CrossSeedStore) GetLatestRun(ctx context.Context) (*CrossSeedRun, error) {
	query := `
		SELECT id, triggered_by, mode, status, started_at, completed_at,
		       total_feed_items, candidates_found, torrents_added,
		       torrents_failed, torrents_skipped, message, error_message,
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

// ListRuns returns automation run history.
func (s *CrossSeedStore) ListRuns(ctx context.Context, limit, offset int) ([]*CrossSeedRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	query := `
		SELECT id, triggered_by, mode, status, started_at, completed_at,
		       total_feed_items, candidates_found, torrents_added,
		       torrents_failed, torrents_skipped, message, error_message,
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

// CreateSearchRun inserts a new record for a search automation run.
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

	const query = `
		INSERT INTO cross_seed_search_runs (
			instance_id, status, started_at, total_torrents, processed,
			torrents_added, torrents_failed, torrents_skipped, message,
			error_message, filters_json, indexer_ids_json, interval_seconds,
			cooldown_minutes, results_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`

	var insertedID int64
	err = s.db.QueryRowContext(ctx, query,
		run.InstanceID,
		run.Status,
		run.StartedAt,
		run.TotalTorrents,
		run.Processed,
		run.TorrentsAdded,
		run.TorrentsFailed,
		run.TorrentsSkipped,
		run.Message,
		run.ErrorMessage,
		filtersJSON,
		indexersJSON,
		run.IntervalSeconds,
		run.CooldownMinutes,
		resultsJSON,
	).Scan(&insertedID)
	if err != nil {
		return nil, fmt.Errorf("insert search run: %w", err)
	}

	// Prune old runs for this instance, keeping only the 10 most recent
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

// UpdateSearchRun updates persisted metadata for a search run.
func (s *CrossSeedStore) UpdateSearchRun(ctx context.Context, run *CrossSeedSearchRun) (*CrossSeedSearchRun, error) {
	if run == nil {
		return nil, errors.New("run cannot be nil")
	}
	if run.ID == 0 {
		return nil, errors.New("run ID cannot be zero")
	}

	resultsJSON, err := encodeSearchResults(run.Results)
	if err != nil {
		return nil, fmt.Errorf("encode results: %w", err)
	}
	filtersJSON, err := encodeSearchFilters(run.Filters)
	if err != nil {
		return nil, fmt.Errorf("encode filters: %w", err)
	}
	indexersJSON, err := encodeIntSlice(run.IndexerIDs)
	if err != nil {
		return nil, fmt.Errorf("encode indexers: %w", err)
	}

	const query = `
		UPDATE cross_seed_search_runs SET
			status = ?,
			started_at = ?,
			completed_at = ?,
			total_torrents = ?,
			processed = ?,
			torrents_added = ?,
			torrents_failed = ?,
			torrents_skipped = ?,
			message = ?,
			error_message = ?,
			filters_json = ?,
			indexer_ids_json = ?,
			interval_seconds = ?,
			cooldown_minutes = ?,
			results_json = ?
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
		run.TorrentsAdded,
		run.TorrentsFailed,
		run.TorrentsSkipped,
		run.Message,
		run.ErrorMessage,
		filtersJSON,
		indexersJSON,
		run.IntervalSeconds,
		run.CooldownMinutes,
		resultsJSON,
		run.ID,
	); err != nil {
		return nil, fmt.Errorf("update search run: %w", err)
	}

	return s.GetSearchRun(ctx, run.ID)
}

// GetSearchRun loads a specific search run by ID.
func (s *CrossSeedStore) GetSearchRun(ctx context.Context, id int64) (*CrossSeedSearchRun, error) {
	const query = `
		SELECT id, instance_id, status, started_at, completed_at,
		       total_torrents, processed, torrents_added, torrents_failed,
		       torrents_skipped, message, error_message, filters_json,
		       indexer_ids_json, interval_seconds, cooldown_minutes,
		       results_json, created_at
		FROM cross_seed_search_runs
		WHERE id = ?
	`

	row := s.db.QueryRowContext(ctx, query, id)
	return scanCrossSeedSearchRun(row)
}

// ListSearchRuns returns search automation history for an instance.
func (s *CrossSeedStore) ListSearchRuns(ctx context.Context, instanceID, limit, offset int) ([]*CrossSeedSearchRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	const query = `
		SELECT id, instance_id, status, started_at, completed_at,
		       total_torrents, processed, torrents_added, torrents_failed,
		       torrents_skipped, message, error_message, filters_json,
		       indexer_ids_json, interval_seconds, cooldown_minutes,
		       results_json, created_at
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

// UpsertSearchHistory updates the last searched timestamp for a torrent on an instance.
func (s *CrossSeedStore) UpsertSearchHistory(ctx context.Context, instanceID int, torrentHash string, searchedAt time.Time) error {
	if instanceID <= 0 || strings.TrimSpace(torrentHash) == "" {
		return fmt.Errorf("invalid search history parameters")
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

// GetSearchHistory returns the last time a torrent was searched.
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

// HasProcessedFeedItem reports whether a GUID/indexer pair has been handled.
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

// MarkFeedItem updates the state of a feed item.
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

	query := `
		INSERT INTO cross_seed_feed_items (
			guid, indexer_id, title, first_seen_at,
			last_seen_at, last_status, last_run_id, info_hash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(guid, indexer_id) DO UPDATE SET
			title = excluded.title,
			last_seen_at = excluded.last_seen_at,
			last_status = excluded.last_status,
			last_run_id = excluded.last_run_id,
			info_hash = COALESCE(excluded.info_hash, cross_seed_feed_items.info_hash)
	`

	_, err := s.db.ExecContext(ctx, query,
		item.GUID,
		item.IndexerID,
		item.Title,
		item.FirstSeenAt,
		item.LastSeenAt,
		item.LastStatus,
		item.LastRunID,
		item.InfoHash,
	)
	if err != nil {
		return fmt.Errorf("mark feed item: %w", err)
	}

	return nil
}

// PruneFeedItems removes processed feed items older than the provided cutoff.
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
		return 0, nil
	}

	return rows, nil
}

// MarkInterruptedSearchRuns marks any search runs still in 'running' status as failed.
// This should be called at startup to reconcile runs interrupted by a crash/restart.
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

// MarkInterruptedAutomationRuns marks any automation runs still in 'running' status as failed.
// This should be called at startup to reconcile runs interrupted by a crash/restart.
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
		&run.TorrentsAdded,
		&run.TorrentsFailed,
		&run.TorrentsSkipped,
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
	)

	err := scanner.Scan(
		&run.ID,
		&run.InstanceID,
		&run.Status,
		&run.StartedAt,
		&completedAt,
		&run.TotalTorrents,
		&run.Processed,
		&run.TorrentsAdded,
		&run.TorrentsFailed,
		&run.TorrentsSkipped,
		&run.Message,
		&run.ErrorMessage,
		&filtersJSON,
		&indexersJSON,
		&run.IntervalSeconds,
		&run.CooldownMinutes,
		&resultsJSON,
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

	return &run, nil
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

// decodeStringSliceWithDefault decodes a JSON string slice, using defaultVal if the source is null/empty.
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

func normalizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}

	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))

	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}

	return normalized
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
		return fmt.Errorf("destination cannot be nil")
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

func decodeSearchResults(src sql.NullString, dest *[]CrossSeedSearchResult) error {
	if dest == nil {
		return fmt.Errorf("destination cannot be nil")
	}
	if !src.Valid || src.String == "" {
		*dest = []CrossSeedSearchResult{}
		return nil
	}
	var tmp []CrossSeedSearchResult
	if err := json.Unmarshal([]byte(src.String), &tmp); err != nil {
		return err
	}
	*dest = tmp
	return nil
}
