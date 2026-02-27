// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

// Delete mode constants
const (
	DeleteModeNone                        = "none"
	DeleteModeKeepFiles                   = "delete"
	DeleteModeWithFiles                   = "deleteWithFiles"
	DeleteModeWithFilesPreserveCrossSeeds = "deleteWithFilesPreserveCrossSeeds"
	DeleteModeWithFilesIncludeCrossSeeds  = "deleteWithFilesIncludeCrossSeeds"
)

// Tag mode constants
const (
	TagModeFull   = "full"   // Add to matches, remove from non-matches
	TagModeAdd    = "add"    // Only add to matches
	TagModeRemove = "remove" // Only remove from matches
)

// FreeSpaceSourceType defines the source for free space checks in workflows.
type FreeSpaceSourceType string

const (
	// FreeSpaceSourceQBittorrent uses qBittorrent's reported free space (default download dir).
	FreeSpaceSourceQBittorrent FreeSpaceSourceType = "qbittorrent"
	// FreeSpaceSourcePath reads free space from a local filesystem path.
	FreeSpaceSourcePath FreeSpaceSourceType = "path"
	// Future: FreeSpaceSourceAgentPath for remote agent-based free space checks.
)

// FreeSpaceSource configures how FREE_SPACE conditions obtain available disk space.
type FreeSpaceSource struct {
	Type FreeSpaceSourceType `json:"type"`           // "qbittorrent" or "path"
	Path string              `json:"path,omitempty"` // Required when Type == "path"
}

type Automation struct {
	ID              int               `json:"id"`
	InstanceID      int               `json:"instanceId"`
	Name            string            `json:"name"`
	TrackerPattern  string            `json:"trackerPattern"`
	TrackerDomains  []string          `json:"trackerDomains,omitempty"`
	Conditions      *ActionConditions `json:"conditions"`
	FreeSpaceSource *FreeSpaceSource  `json:"freeSpaceSource,omitempty"` // nil = default qBittorrent free space
	Enabled         bool              `json:"enabled"`
	DryRun          bool              `json:"dryRun"`
	SortOrder       int               `json:"sortOrder"`
	IntervalSeconds *int              `json:"intervalSeconds,omitempty"` // nil = use DefaultRuleInterval (15m)
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}

type AutomationStore struct {
	db dbinterface.Querier
}

func NewAutomationStore(db dbinterface.Querier) *AutomationStore {
	return &AutomationStore{db: db}
}

func splitPatterns(pattern string) []string {
	if pattern == "" {
		return nil
	}

	rawParts := strings.FieldsFunc(pattern, func(r rune) bool {
		return r == ',' || r == ';' || r == '|'
	})

	seen := make(map[string]struct{})
	var parts []string
	for _, raw := range rawParts {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		if _, exists := seen[p]; exists {
			continue
		}
		seen[p] = struct{}{}
		parts = append(parts, p)
	}
	return parts
}

func normalizeTrackerPattern(pattern string, domains []string) string {
	if len(domains) > 0 {
		pattern = strings.Join(domains, ",")
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return ""
	}
	parts := splitPatterns(pattern)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ",")
}

func (s *AutomationStore) ListByInstance(ctx context.Context, instanceID int) ([]*Automation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, instance_id, name, tracker_pattern, conditions, enabled, dry_run, sort_order, interval_seconds, free_space_source, created_at, updated_at
		FROM automations
		WHERE instance_id = ?
		ORDER BY sort_order ASC, id ASC
	`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var automations []*Automation
	for rows.Next() {
		var automation Automation
		var conditionsJSON string
		var intervalSeconds sql.NullInt64
		var freeSpaceSourceJSON sql.NullString

		if err := rows.Scan(
			&automation.ID,
			&automation.InstanceID,
			&automation.Name,
			&automation.TrackerPattern,
			&conditionsJSON,
			&automation.Enabled,
			&automation.DryRun,
			&automation.SortOrder,
			&intervalSeconds,
			&freeSpaceSourceJSON,
			&automation.CreatedAt,
			&automation.UpdatedAt,
		); err != nil {
			return nil, err
		}

		var conditions ActionConditions
		if err := json.Unmarshal([]byte(conditionsJSON), &conditions); err != nil {
			return nil, fmt.Errorf("failed to unmarshal conditions for automation %d: %w", automation.ID, err)
		}
		conditions.Normalize()
		automation.Conditions = &conditions

		automation.TrackerDomains = splitPatterns(automation.TrackerPattern)

		if intervalSeconds.Valid {
			v := int(intervalSeconds.Int64)
			automation.IntervalSeconds = &v
		}

		if freeSpaceSourceJSON.Valid && freeSpaceSourceJSON.String != "" {
			var freeSpaceSource FreeSpaceSource
			if err := json.Unmarshal([]byte(freeSpaceSourceJSON.String), &freeSpaceSource); err != nil {
				return nil, fmt.Errorf("failed to unmarshal free_space_source for automation %d: %w", automation.ID, err)
			}
			automation.FreeSpaceSource = &freeSpaceSource
		}

		automations = append(automations, &automation)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return automations, nil
}

func (s *AutomationStore) Get(ctx context.Context, instanceID, id int) (*Automation, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, name, tracker_pattern, conditions, enabled, dry_run, sort_order, interval_seconds, free_space_source, created_at, updated_at
		FROM automations
		WHERE id = ? AND instance_id = ?
	`, id, instanceID)

	var automation Automation
	var conditionsJSON string
	var intervalSeconds sql.NullInt64
	var freeSpaceSourceJSON sql.NullString

	if err := row.Scan(
		&automation.ID,
		&automation.InstanceID,
		&automation.Name,
		&automation.TrackerPattern,
		&conditionsJSON,
		&automation.Enabled,
		&automation.DryRun,
		&automation.SortOrder,
		&intervalSeconds,
		&freeSpaceSourceJSON,
		&automation.CreatedAt,
		&automation.UpdatedAt,
	); err != nil {
		return nil, err
	}

	var conditions ActionConditions
	if err := json.Unmarshal([]byte(conditionsJSON), &conditions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal conditions for automation %d: %w", automation.ID, err)
	}
	conditions.Normalize()
	automation.Conditions = &conditions

	automation.TrackerDomains = splitPatterns(automation.TrackerPattern)

	if intervalSeconds.Valid {
		v := int(intervalSeconds.Int64)
		automation.IntervalSeconds = &v
	}

	if freeSpaceSourceJSON.Valid && freeSpaceSourceJSON.String != "" {
		var freeSpaceSource FreeSpaceSource
		if err := json.Unmarshal([]byte(freeSpaceSourceJSON.String), &freeSpaceSource); err != nil {
			return nil, fmt.Errorf("failed to unmarshal free_space_source for automation %d: %w", automation.ID, err)
		}
		automation.FreeSpaceSource = &freeSpaceSource
	}

	return &automation, nil
}

func (s *AutomationStore) nextSortOrder(ctx context.Context, instanceID int) (int, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), 0) FROM automations WHERE instance_id = ?`, instanceID)
	var maxOrder int
	if err := row.Scan(&maxOrder); err != nil {
		return 0, err
	}
	return maxOrder + 1, nil
}

func (s *AutomationStore) Create(ctx context.Context, automation *Automation) (*Automation, error) {
	if automation == nil {
		return nil, errors.New("automation is nil")
	}
	automation.Conditions.Normalize()
	if automation.Conditions == nil || automation.Conditions.IsEmpty() {
		return nil, errors.New("automation must have conditions")
	}
	if err := automation.Conditions.ExternalProgram.Validate(); err != nil {
		return nil, fmt.Errorf("invalid external program action: %w", err)
	}

	automation.TrackerPattern = normalizeTrackerPattern(automation.TrackerPattern, automation.TrackerDomains)

	sortOrder := automation.SortOrder
	if sortOrder == 0 {
		next, err := s.nextSortOrder(ctx, automation.InstanceID)
		if err != nil {
			return nil, err
		}
		sortOrder = next
	}

	conditionsJSON, err := json.Marshal(automation.Conditions)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal conditions: %w", err)
	}

	var intervalSeconds sql.NullInt64
	if automation.IntervalSeconds != nil {
		intervalSeconds = sql.NullInt64{Int64: int64(*automation.IntervalSeconds), Valid: true}
	}

	var freeSpaceSourceJSON sql.NullString
	if automation.FreeSpaceSource != nil {
		data, marshalErr := json.Marshal(automation.FreeSpaceSource)
		if marshalErr != nil {
			return nil, fmt.Errorf("failed to marshal free_space_source: %w", marshalErr)
		}
		freeSpaceSourceJSON = sql.NullString{String: string(data), Valid: true}
	}

	var id int
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO automations
			(instance_id, name, tracker_pattern, conditions, enabled, dry_run, sort_order, interval_seconds, free_space_source)
		VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`, automation.InstanceID, automation.Name, automation.TrackerPattern, string(conditionsJSON), boolToInt(automation.Enabled), boolToInt(automation.DryRun), sortOrder, intervalSeconds, freeSpaceSourceJSON).Scan(&id)
	if err != nil {
		return nil, err
	}

	return s.Get(ctx, automation.InstanceID, id)
}

func (s *AutomationStore) Update(ctx context.Context, automation *Automation) (*Automation, error) {
	if automation == nil {
		return nil, errors.New("automation is nil")
	}
	automation.Conditions.Normalize()
	if automation.Conditions == nil || automation.Conditions.IsEmpty() {
		return nil, errors.New("automation must have conditions")
	}
	if err := automation.Conditions.ExternalProgram.Validate(); err != nil {
		return nil, fmt.Errorf("invalid external program action: %w", err)
	}

	automation.TrackerPattern = normalizeTrackerPattern(automation.TrackerPattern, automation.TrackerDomains)

	conditionsJSON, err := json.Marshal(automation.Conditions)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal conditions: %w", err)
	}

	var intervalSeconds sql.NullInt64
	if automation.IntervalSeconds != nil {
		intervalSeconds = sql.NullInt64{Int64: int64(*automation.IntervalSeconds), Valid: true}
	}

	var freeSpaceSourceJSON sql.NullString
	if automation.FreeSpaceSource != nil {
		data, marshalErr := json.Marshal(automation.FreeSpaceSource)
		if marshalErr != nil {
			return nil, fmt.Errorf("failed to marshal free_space_source: %w", marshalErr)
		}
		freeSpaceSourceJSON = sql.NullString{String: string(data), Valid: true}
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE automations
		SET name = ?, tracker_pattern = ?, conditions = ?, enabled = ?, dry_run = ?, sort_order = ?, interval_seconds = ?, free_space_source = ?
		WHERE id = ? AND instance_id = ?
	`, automation.Name, automation.TrackerPattern, string(conditionsJSON), boolToInt(automation.Enabled), boolToInt(automation.DryRun), automation.SortOrder, intervalSeconds, freeSpaceSourceJSON, automation.ID, automation.InstanceID)
	if err != nil {
		return nil, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, sql.ErrNoRows
	}

	return s.Get(ctx, automation.InstanceID, automation.ID)
}

func (s *AutomationStore) Delete(ctx context.Context, instanceID int, id int) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM automations WHERE id = ? AND instance_id = ?`, id, instanceID)
	if err != nil {
		return err
	}
	if rows, err := res.RowsAffected(); err == nil && rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *AutomationStore) Reorder(ctx context.Context, instanceID int, orderedIDs []int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for idx, id := range orderedIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE automations SET sort_order = ? WHERE id = ? AND instance_id = ?`, idx+1, id, instanceID); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// AutomationReference represents a minimal automation reference for cascade delete warnings.
type AutomationReference struct {
	ID         int    `json:"id"`
	InstanceID int    `json:"instanceId"`
	Name       string `json:"name"`
}

// FindByExternalProgramID returns automations that reference the given external program ID.
// Uses SQLite's json_extract to query the conditions JSON column.
// Returns all automations referencing the program, regardless of whether the action is enabled.
func (s *AutomationStore) FindByExternalProgramID(ctx context.Context, programID int) ([]*AutomationReference, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, instance_id, name
		FROM automations
		WHERE json_extract(conditions, '$.externalProgram.programId') = ?
	`, programID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []*AutomationReference
	for rows.Next() {
		var ref AutomationReference
		if err := rows.Scan(&ref.ID, &ref.InstanceID, &ref.Name); err != nil {
			return nil, err
		}
		refs = append(refs, &ref)
	}
	return refs, rows.Err()
}

// ClearExternalProgramAction removes the external program action from all automations
// that reference the given program ID. This is used for cascade delete.
// Clears references regardless of whether the action is enabled or disabled.
func (s *AutomationStore) ClearExternalProgramAction(ctx context.Context, programID int) (int64, error) {
	// Set externalProgram to null in the conditions JSON for all matching automations
	res, err := s.db.ExecContext(ctx, `
		UPDATE automations
		SET conditions = json_remove(conditions, '$.externalProgram')
		WHERE json_extract(conditions, '$.externalProgram.programId') = ?
	`, programID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// ConditionField represents a field from the qBittorrent Torrent struct that can be filtered on.
type ConditionField string

const (
	// String fields
	FieldName          ConditionField = "NAME"
	FieldHash          ConditionField = "HASH"
	FieldInfohashV1    ConditionField = "INFOHASH_V1"
	FieldInfohashV2    ConditionField = "INFOHASH_V2"
	FieldMagnetURI     ConditionField = "MAGNET_URI"
	FieldCategory      ConditionField = "CATEGORY"
	FieldTags          ConditionField = "TAGS"
	FieldSavePath      ConditionField = "SAVE_PATH"
	FieldContentPath   ConditionField = "CONTENT_PATH"
	FieldDownloadPath  ConditionField = "DOWNLOAD_PATH"
	FieldCreatedBy     ConditionField = "CREATED_BY"
	FieldTrackers      ConditionField = "TRACKERS"
	FieldContentType   ConditionField = "CONTENT_TYPE"
	FieldEffectiveName ConditionField = "EFFECTIVE_NAME"

	// RLS-derived specifiers (from torrent name parsing)
	FieldRlsSource     ConditionField = "RLS_SOURCE"
	FieldRlsResolution ConditionField = "RLS_RESOLUTION"
	FieldRlsCodec      ConditionField = "RLS_CODEC"
	FieldRlsHDR        ConditionField = "RLS_HDR"
	FieldRlsAudio      ConditionField = "RLS_AUDIO"
	FieldRlsChannels   ConditionField = "RLS_CHANNELS"
	FieldRlsGroup      ConditionField = "RLS_GROUP"
	FieldState         ConditionField = "STATE"
	FieldTracker       ConditionField = "TRACKER"
	FieldComment       ConditionField = "COMMENT"

	// Numeric fields (bytes)
	FieldSize              ConditionField = "SIZE"
	FieldTotalSize         ConditionField = "TOTAL_SIZE"
	FieldCompleted         ConditionField = "COMPLETED"
	FieldDownloaded        ConditionField = "DOWNLOADED"
	FieldDownloadedSession ConditionField = "DOWNLOADED_SESSION"
	FieldUploaded          ConditionField = "UPLOADED"
	FieldUploadedSession   ConditionField = "UPLOADED_SESSION"
	FieldAmountLeft        ConditionField = "AMOUNT_LEFT"
	FieldFreeSpace         ConditionField = "FREE_SPACE"

	// Time fields (timestamp-backed ages + duration seconds)
	FieldAddedOn                  ConditionField = "ADDED_ON"
	FieldCompletionOn             ConditionField = "COMPLETION_ON"
	FieldLastActivity             ConditionField = "LAST_ACTIVITY"
	FieldSeenComplete             ConditionField = "SEEN_COMPLETE"
	FieldETA                      ConditionField = "ETA"
	FieldReannounce               ConditionField = "REANNOUNCE"
	FieldSeedingTime              ConditionField = "SEEDING_TIME"
	FieldTimeActive               ConditionField = "TIME_ACTIVE"
	FieldMaxSeedingTime           ConditionField = "MAX_SEEDING_TIME"
	FieldMaxInactiveSeedingTime   ConditionField = "MAX_INACTIVE_SEEDING_TIME"
	FieldSeedingTimeLimit         ConditionField = "SEEDING_TIME_LIMIT"
	FieldInactiveSeedingTimeLimit ConditionField = "INACTIVE_SEEDING_TIME_LIMIT"

	// Legacy age aliases (computed as nowUnix - timestamp)
	FieldAddedOnAge      ConditionField = "ADDED_ON_AGE"
	FieldCompletionOnAge ConditionField = "COMPLETION_ON_AGE"
	FieldLastActivityAge ConditionField = "LAST_ACTIVITY_AGE"

	// Numeric fields (float64)
	FieldRatio        ConditionField = "RATIO"
	FieldRatioLimit   ConditionField = "RATIO_LIMIT"
	FieldMaxRatio     ConditionField = "MAX_RATIO"
	FieldProgress     ConditionField = "PROGRESS"
	FieldAvailability ConditionField = "AVAILABILITY"
	FieldPopularity   ConditionField = "POPULARITY"

	// Numeric fields (speeds)
	FieldDlSpeed ConditionField = "DL_SPEED"
	FieldUpSpeed ConditionField = "UP_SPEED"
	FieldDlLimit ConditionField = "DL_LIMIT"
	FieldUpLimit ConditionField = "UP_LIMIT"

	// Numeric fields (counts/misc)
	FieldNumSeeds      ConditionField = "NUM_SEEDS"
	FieldNumLeechs     ConditionField = "NUM_LEECHS"
	FieldNumComplete   ConditionField = "NUM_COMPLETE"
	FieldNumIncomplete ConditionField = "NUM_INCOMPLETE"
	FieldTrackersCount ConditionField = "TRACKERS_COUNT"
	FieldPriority      ConditionField = "PRIORITY"
	FieldGroupSize     ConditionField = "GROUP_SIZE"

	// Boolean fields
	FieldPrivate            ConditionField = "PRIVATE"
	FieldAutoManaged        ConditionField = "AUTO_MANAGED"
	FieldFirstLastPiecePrio ConditionField = "FIRST_LAST_PIECE_PRIO"
	FieldForceStart         ConditionField = "FORCE_START"
	FieldSequentialDownload ConditionField = "SEQUENTIAL_DOWNLOAD"
	FieldSuperSeeding       ConditionField = "SUPER_SEEDING"
	FieldIsUnregistered     ConditionField = "IS_UNREGISTERED"
	FieldHasMissingFiles    ConditionField = "HAS_MISSING_FILES"
	FieldIsGrouped          ConditionField = "IS_GROUPED"

	// Enum-like fields
	FieldHardlinkScope ConditionField = "HARDLINK_SCOPE"
)

// Hardlink scope values (wire format - stable API values)
const (
	HardlinkScopeNone               = "none"                // No file has link count > 1
	HardlinkScopeTorrentsOnly       = "torrents_only"       // All links are within qBittorrent's torrent set
	HardlinkScopeOutsideQBitTorrent = "outside_qbittorrent" // At least one file has links outside the torrent set
)

// ConditionOperator represents operators for comparing field values.
type ConditionOperator string

const (
	// Logical operators (for groups)
	OperatorAnd ConditionOperator = "AND"
	OperatorOr  ConditionOperator = "OR"

	// Comparison operators
	OperatorEqual              ConditionOperator = "EQUAL"
	OperatorNotEqual           ConditionOperator = "NOT_EQUAL"
	OperatorContains           ConditionOperator = "CONTAINS"
	OperatorNotContains        ConditionOperator = "NOT_CONTAINS"
	OperatorStartsWith         ConditionOperator = "STARTS_WITH"
	OperatorEndsWith           ConditionOperator = "ENDS_WITH"
	OperatorGreaterThan        ConditionOperator = "GREATER_THAN"
	OperatorGreaterThanOrEqual ConditionOperator = "GREATER_THAN_OR_EQUAL"
	OperatorLessThan           ConditionOperator = "LESS_THAN"
	OperatorLessThanOrEqual    ConditionOperator = "LESS_THAN_OR_EQUAL"
	OperatorBetween            ConditionOperator = "BETWEEN"
	OperatorMatches            ConditionOperator = "MATCHES" // regex

	// Cross-category lookup operators (NAME field only)
	OperatorExistsIn   ConditionOperator = "EXISTS_IN"   // exact name match in target category
	OperatorContainsIn ConditionOperator = "CONTAINS_IN" // partial name match in target category
)

// RuleCondition represents a condition or group of conditions for filtering torrents.
type RuleCondition struct {
	Field      ConditionField    `json:"field,omitempty"`
	Operator   ConditionOperator `json:"operator"`
	GroupID    string            `json:"groupId,omitempty"` // Optional grouping ID for GROUP_SIZE/IS_GROUPED conditions
	Value      string            `json:"value,omitempty"`
	MinValue   *float64          `json:"minValue,omitempty"`
	MaxValue   *float64          `json:"maxValue,omitempty"`
	Regex      bool              `json:"regex,omitempty"`
	Negate     bool              `json:"negate,omitempty"`
	Conditions []*RuleCondition  `json:"conditions,omitempty"`
	Compiled   *regexp.Regexp    `json:"-"` // compiled regex, not serialized
}

// IsGroup returns true if this condition is an AND/OR group containing child conditions.
func (c *RuleCondition) IsGroup() bool {
	return len(c.Conditions) > 0 && (c.Operator == OperatorAnd || c.Operator == OperatorOr)
}

// CompileRegex compiles the regex pattern if needed. Safe to call multiple times.
func (c *RuleCondition) CompileRegex() error {
	if c.Compiled != nil {
		return nil
	}
	if !c.Regex && c.Operator != OperatorMatches {
		return nil
	}
	var err error
	c.Compiled, err = regexp.Compile("(?i)" + c.Value)
	return err
}

// ActionConditions holds per-action conditions with action configuration.
// This is the top-level structure stored in the `conditions` JSON column.
type ActionConditions struct {
	SchemaVersion   string                 `json:"schemaVersion"`
	Grouping        *GroupingConfig        `json:"grouping,omitempty"`
	SpeedLimits     *SpeedLimitAction      `json:"speedLimits,omitempty"`
	ShareLimits     *ShareLimitsAction     `json:"shareLimits,omitempty"`
	Pause           *PauseAction           `json:"pause,omitempty"`
	Resume          *ResumeAction          `json:"resume,omitempty"`
	Recheck         *RecheckAction         `json:"recheck,omitempty"`
	Reannounce      *ReannounceAction      `json:"reannounce,omitempty"`
	Delete          *DeleteAction          `json:"delete,omitempty"`
	Tag             *TagAction             `json:"tag,omitempty"`  // Legacy single-tag action (backward compatible alias for first entry in Tags)
	Tags            []*TagAction           `json:"tags,omitempty"` // Preferred multi-tag actions
	Category        *CategoryAction        `json:"category,omitempty"`
	Move            *MoveAction            `json:"move,omitempty"`
	ExternalProgram *ExternalProgramAction `json:"externalProgram,omitempty"`
}

// SpeedLimitAction configures speed limit application with optional conditions.
type SpeedLimitAction struct {
	Enabled     bool           `json:"enabled"`
	UploadKiB   *int64         `json:"uploadKiB,omitempty"`
	DownloadKiB *int64         `json:"downloadKiB,omitempty"`
	Condition   *RuleCondition `json:"condition,omitempty"`
}

// ShareLimitsAction configures share limit (ratio/seeding time) application with optional conditions.
type ShareLimitsAction struct {
	Enabled            bool           `json:"enabled"`
	RatioLimit         *float64       `json:"ratioLimit,omitempty"`
	SeedingTimeMinutes *int64         `json:"seedingTimeMinutes,omitempty"`
	Condition          *RuleCondition `json:"condition,omitempty"`
}

// PauseAction configures pause action with conditions.
type PauseAction struct {
	Enabled   bool           `json:"enabled"`
	Condition *RuleCondition `json:"condition,omitempty"`
}

// ResumeAction configures resume action with conditions.
type ResumeAction struct {
	Enabled   bool           `json:"enabled"`
	Condition *RuleCondition `json:"condition,omitempty"`
}

// RecheckAction configures force recheck action with optional conditions.
type RecheckAction struct {
	Enabled   bool           `json:"enabled"`
	Condition *RuleCondition `json:"condition,omitempty"`
}

// ReannounceAction configures force reannounce action with optional conditions.
type ReannounceAction struct {
	Enabled   bool           `json:"enabled"`
	Condition *RuleCondition `json:"condition,omitempty"`
}

// DeleteAction configures deletion with mode and conditions.
type DeleteAction struct {
	Enabled          bool           `json:"enabled"`
	Mode             string         `json:"mode"`                       // "delete", "deleteWithFiles", "deleteWithFilesPreserveCrossSeeds", "deleteWithFilesIncludeCrossSeeds"
	IncludeHardlinks bool           `json:"includeHardlinks,omitempty"` // Only valid when mode is "deleteWithFilesIncludeCrossSeeds" and instance has local filesystem access
	GroupID          string         `json:"groupId,omitempty"`          // Optional grouping ID for expanding/atomically applying deletes
	Atomic           string         `json:"atomic,omitempty"`           // Optional atomic policy: "all" (apply only if all group members match)
	Condition        *RuleCondition `json:"condition,omitempty"`
}

// TagAction configures tagging with smart add/remove logic.
type TagAction struct {
	Enabled          bool           `json:"enabled"`
	Tags             []string       `json:"tags"`                       // Tags to manage (fallback if UseTrackerAsTag has no domains)
	Mode             string         `json:"mode"`                       // "full", "add", "remove"
	DeleteFromClient bool           `json:"deleteFromClient,omitempty"` // Delete managed tags from qBittorrent before applying matches
	UseTrackerAsTag  bool           `json:"useTrackerAsTag,omitempty"`  // Derive tag from torrent's tracker domain
	UseDisplayName   bool           `json:"useDisplayName,omitempty"`   // Use tracker customization display name instead of raw domain
	Condition        *RuleCondition `json:"condition,omitempty"`
}

// CategoryAction configures category assignment with optional conditions.
type CategoryAction struct {
	Enabled           bool   `json:"enabled"`
	Category          string `json:"category"`                    // Target category name
	IncludeCrossSeeds bool   `json:"includeCrossSeeds,omitempty"` // Also move cross-seeds to same category
	GroupID           string `json:"groupId,omitempty"`           // Optional grouping ID for expanding category changes
	// BlockIfCrossSeedInCategories prevents category changes when any other cross-seed torrent
	// (same ContentPath + SavePath) is found in one of the listed categories.
	BlockIfCrossSeedInCategories []string       `json:"blockIfCrossSeedInCategories,omitempty"`
	Condition                    *RuleCondition `json:"condition,omitempty"`
}

type MoveAction struct {
	Enabled          bool           `json:"enabled"`
	Path             string         `json:"path"`
	BlockIfCrossSeed bool           `json:"blockIfCrossSeed,omitempty"`
	GroupID          string         `json:"groupId,omitempty"` // Optional grouping ID for move cross-seed protection/atomicity
	Atomic           string         `json:"atomic,omitempty"`  // Optional atomic policy: "all" (apply only if all group members match)
	Condition        *RuleCondition `json:"condition,omitempty"`
}

// GroupingConfig defines how torrents can be grouped for group-aware actions and conditions.
// Group definitions are per-rule (stored inside the rule's conditions JSON).
type GroupingConfig struct {
	// DefaultGroupID is the group to use for group-derived condition fields (e.g. GROUP_SIZE, IS_GROUPED).
	DefaultGroupID string            `json:"defaultGroupId,omitempty"`
	Groups         []GroupDefinition `json:"groups,omitempty"`
}

// GroupDefinition defines a single grouping strategy.
type GroupDefinition struct {
	ID string `json:"id"`
	// Keys are combined to form a group key.
	// Supported keys are documented in the automations service (built-ins include contentPath, savePath, effectiveName, contentType, tracker, rlsSource, rlsResolution, rlsCodec, rlsGroup, hardlinkSignature).
	Keys []string `json:"keys"`
	// AmbiguousPolicy controls how groups are handled when ContentPath is ambiguous (ContentPath == SavePath).
	// Valid: "verify_overlap" (default for contentPath group), "skip".
	AmbiguousPolicy string `json:"ambiguousPolicy,omitempty"`
	// MinFileOverlapPercent applies when AmbiguousPolicy == "verify_overlap". Defaults to 90.
	MinFileOverlapPercent int `json:"minFileOverlapPercent,omitempty"`
}

// ExternalProgramAction configures external program execution with optional conditions.
type ExternalProgramAction struct {
	Enabled   bool           `json:"enabled"`
	ProgramID int            `json:"programId"` // FK to external_programs table
	Condition *RuleCondition `json:"condition,omitempty"`
}

// Validate checks that the ExternalProgramAction has valid configuration.
func (a *ExternalProgramAction) Validate() error {
	if a == nil {
		return nil
	}
	if a.Enabled && a.ProgramID <= 0 {
		return errors.New("enabled external program action requires valid programId")
	}
	return nil
}

// IsEmpty returns true if no actions are configured.
func (ac *ActionConditions) IsEmpty() bool {
	if ac == nil {
		return true
	}
	return ac.SpeedLimits == nil &&
		ac.ShareLimits == nil &&
		ac.Pause == nil &&
		ac.Resume == nil &&
		ac.Recheck == nil &&
		ac.Reannounce == nil &&
		ac.Delete == nil &&
		len(ac.TagActions()) == 0 &&
		ac.Category == nil &&
		ac.Move == nil &&
		ac.ExternalProgram == nil
}

// Normalize normalizes legacy/new action fields for in-memory use.
func (ac *ActionConditions) Normalize() {
	if ac == nil {
		return
	}

	if len(ac.Tags) == 0 && ac.Tag != nil {
		ac.Tags = []*TagAction{ac.Tag}
	}

	normalized := make([]*TagAction, 0, len(ac.Tags))
	for _, action := range ac.Tags {
		if action == nil {
			continue
		}
		normalized = append(normalized, action)
	}
	ac.Tags = normalized

	if len(ac.Tags) > 0 {
		ac.Tag = ac.Tags[0]
	}
}

// TagActions returns all configured tag actions (multi-tag aware).
func (ac *ActionConditions) TagActions() []*TagAction {
	if ac == nil {
		return nil
	}
	if len(ac.Tags) > 0 {
		return ac.Tags
	}
	if ac.Tag != nil {
		return []*TagAction{ac.Tag}
	}
	return nil
}
