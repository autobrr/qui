// Copyright (c) 2025, s0up and the autobrr contributors.
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

const (
	TagMatchModeAny = "any"
	TagMatchModeAll = "all"
)

// Delete mode constants
const (
	DeleteModeNone                        = "none"
	DeleteModeKeepFiles                   = "delete"
	DeleteModeWithFiles                   = "deleteWithFiles"
	DeleteModeWithFilesPreserveCrossSeeds = "deleteWithFilesPreserveCrossSeeds"
)

// Tag mode constants
const (
	TagModeFull   = "full"   // Add to matches, remove from non-matches
	TagModeAdd    = "add"    // Only add to matches
	TagModeRemove = "remove" // Only remove from non-matches
)

type Automation struct {
	ID                       int               `json:"id"`
	InstanceID               int               `json:"instanceId"`
	Name                     string            `json:"name"`
	TrackerPattern           string            `json:"trackerPattern"`
	TrackerDomains           []string          `json:"trackerDomains,omitempty"`
	Categories               []string          `json:"categories,omitempty"`
	Tags                     []string          `json:"tags,omitempty"`
	TagMatchMode             string            `json:"tagMatchMode,omitempty"` // "any" (default) or "all"
	UploadLimitKiB           *int64            `json:"uploadLimitKiB,omitempty"`
	DownloadLimitKiB         *int64            `json:"downloadLimitKiB,omitempty"`
	RatioLimit               *float64          `json:"ratioLimit,omitempty"`
	SeedingTimeLimitMinutes  *int64            `json:"seedingTimeLimitMinutes,omitempty"`
	DeleteMode               *string           `json:"deleteMode,omitempty"` // "none", "delete", "deleteWithFiles", "deleteWithFilesPreserveCrossSeeds"
	DeleteUnregistered       bool              `json:"deleteUnregistered"`
	DeleteUnregisteredMinAge int64             `json:"deleteUnregisteredMinAge,omitempty"` // minimum age in seconds, 0 = no minimum
	Enabled                  bool              `json:"enabled"`
	SortOrder                int               `json:"sortOrder"`
	Conditions               *ActionConditions `json:"conditions,omitempty"` // expression-based conditions for actions
	CreatedAt                time.Time         `json:"createdAt"`
	UpdatedAt                time.Time         `json:"updatedAt"`
}

// UsesExpressions returns true if this automation uses expression-based conditions
// instead of the legacy static fields.
func (r *Automation) UsesExpressions() bool {
	return r.Conditions != nil && r.Conditions.SchemaVersion != ""
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
		SELECT id, instance_id, name, tracker_pattern, category, tag, tag_match_mode, upload_limit_kib, download_limit_kib,
		       ratio_limit, seeding_time_limit_minutes, delete_mode, delete_unregistered, delete_unregistered_min_age, enabled, sort_order, conditions, created_at, updated_at
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
		var categories, tags, tagMatchMode, deleteMode, conditionsJSON sql.NullString
		var upload, download sql.NullInt64
		var ratio sql.NullFloat64
		var seeding sql.NullInt64
		var deleteUnregistered int
		var deleteUnregisteredMinAge sql.NullInt64

		if err := rows.Scan(
			&automation.ID,
			&automation.InstanceID,
			&automation.Name,
			&automation.TrackerPattern,
			&categories,
			&tags,
			&tagMatchMode,
			&upload,
			&download,
			&ratio,
			&seeding,
			&deleteMode,
			&deleteUnregistered,
			&deleteUnregisteredMinAge,
			&automation.Enabled,
			&automation.SortOrder,
			&conditionsJSON,
			&automation.CreatedAt,
			&automation.UpdatedAt,
		); err != nil {
			return nil, err
		}

		if categories.Valid && categories.String != "" {
			automation.Categories = splitPatterns(categories.String)
		}
		if tags.Valid && tags.String != "" {
			automation.Tags = splitPatterns(tags.String)
		}
		if tagMatchMode.Valid && tagMatchMode.String != "" {
			automation.TagMatchMode = tagMatchMode.String
		} else {
			automation.TagMatchMode = TagMatchModeAny
		}
		if upload.Valid {
			automation.UploadLimitKiB = &upload.Int64
		}
		if download.Valid {
			automation.DownloadLimitKiB = &download.Int64
		}
		if ratio.Valid {
			automation.RatioLimit = &ratio.Float64
		}
		if seeding.Valid {
			automation.SeedingTimeLimitMinutes = &seeding.Int64
		}
		if deleteMode.Valid && deleteMode.String != DeleteModeNone {
			automation.DeleteMode = &deleteMode.String
		}
		automation.DeleteUnregistered = deleteUnregistered != 0
		if deleteUnregisteredMinAge.Valid {
			automation.DeleteUnregisteredMinAge = deleteUnregisteredMinAge.Int64
		}

		// Parse conditions JSON if present
		if conditionsJSON.Valid && conditionsJSON.String != "" {
			var conditions ActionConditions
			if err := json.Unmarshal([]byte(conditionsJSON.String), &conditions); err == nil {
				automation.Conditions = &conditions
			}
		}

		automation.TrackerDomains = splitPatterns(automation.TrackerPattern)

		automations = append(automations, &automation)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return automations, nil
}

func (s *AutomationStore) Get(ctx context.Context, id int) (*Automation, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, name, tracker_pattern, category, tag, tag_match_mode, upload_limit_kib, download_limit_kib,
		       ratio_limit, seeding_time_limit_minutes, delete_mode, delete_unregistered, delete_unregistered_min_age, enabled, sort_order, conditions, created_at, updated_at
		FROM automations
		WHERE id = ?
	`, id)

	var automation Automation
	var categories, tags, tagMatchMode, deleteMode, conditionsJSON sql.NullString
	var upload, download sql.NullInt64
	var ratio sql.NullFloat64
	var seeding sql.NullInt64
	var deleteUnregistered int
	var deleteUnregisteredMinAge sql.NullInt64

	if err := row.Scan(
		&automation.ID,
		&automation.InstanceID,
		&automation.Name,
		&automation.TrackerPattern,
		&categories,
		&tags,
		&tagMatchMode,
		&upload,
		&download,
		&ratio,
		&seeding,
		&deleteMode,
		&deleteUnregistered,
		&deleteUnregisteredMinAge,
		&automation.Enabled,
		&automation.SortOrder,
		&conditionsJSON,
		&automation.CreatedAt,
		&automation.UpdatedAt,
	); err != nil {
		return nil, err
	}

	if categories.Valid && categories.String != "" {
		automation.Categories = splitPatterns(categories.String)
	}
	if tags.Valid && tags.String != "" {
		automation.Tags = splitPatterns(tags.String)
	}
	if tagMatchMode.Valid && tagMatchMode.String != "" {
		automation.TagMatchMode = tagMatchMode.String
	} else {
		automation.TagMatchMode = TagMatchModeAny
	}
	if upload.Valid {
		automation.UploadLimitKiB = &upload.Int64
	}
	if download.Valid {
		automation.DownloadLimitKiB = &download.Int64
	}
	if ratio.Valid {
		automation.RatioLimit = &ratio.Float64
	}
	if seeding.Valid {
		automation.SeedingTimeLimitMinutes = &seeding.Int64
	}
	if deleteMode.Valid && deleteMode.String != DeleteModeNone {
		automation.DeleteMode = &deleteMode.String
	}
	automation.DeleteUnregistered = deleteUnregistered != 0
	if deleteUnregisteredMinAge.Valid {
		automation.DeleteUnregisteredMinAge = deleteUnregisteredMinAge.Int64
	}

	// Parse conditions JSON if present
	if conditionsJSON.Valid && conditionsJSON.String != "" {
		var conditions ActionConditions
		if err := json.Unmarshal([]byte(conditionsJSON.String), &conditions); err == nil {
			automation.Conditions = &conditions
		}
	}

	automation.TrackerDomains = splitPatterns(automation.TrackerPattern)

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

	automation.TrackerPattern = normalizeTrackerPattern(automation.TrackerPattern, automation.TrackerDomains)

	sortOrder := automation.SortOrder
	if sortOrder == 0 {
		next, err := s.nextSortOrder(ctx, automation.InstanceID)
		if err != nil {
			return nil, err
		}
		sortOrder = next
	}

	// Default delete_mode to "none" if not set
	deleteMode := DeleteModeNone
	if automation.DeleteMode != nil && *automation.DeleteMode != "" {
		deleteMode = *automation.DeleteMode
	}

	// Default tag_match_mode to "any" if not set
	tagMatchMode := TagMatchModeAny
	if automation.TagMatchMode != "" {
		tagMatchMode = automation.TagMatchMode
	}

	// Join arrays to comma-separated strings for storage
	categoriesStr := nullableSlice(automation.Categories)
	tagsStr := nullableSlice(automation.Tags)

	// Serialize conditions to JSON if present
	var conditionsJSON any
	if automation.Conditions != nil && !automation.Conditions.IsEmpty() {
		data, err := json.Marshal(automation.Conditions)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal conditions: %w", err)
		}
		conditionsJSON = string(data)
	}

	// 0 means no minimum age, store as NULL
	var deleteUnregMinAge any
	if automation.DeleteUnregisteredMinAge > 0 {
		deleteUnregMinAge = automation.DeleteUnregisteredMinAge
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO automations
			(instance_id, name, tracker_pattern, category, tag, tag_match_mode, upload_limit_kib, download_limit_kib, ratio_limit, seeding_time_limit_minutes, delete_mode, delete_unregistered, delete_unregistered_min_age, enabled, sort_order, conditions)
		VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, automation.InstanceID, automation.Name, automation.TrackerPattern, categoriesStr, tagsStr, tagMatchMode,
		nullableInt64(automation.UploadLimitKiB), nullableInt64(automation.DownloadLimitKiB), nullableFloat64(automation.RatioLimit),
		nullableInt64(automation.SeedingTimeLimitMinutes), deleteMode, boolToInt(automation.DeleteUnregistered), deleteUnregMinAge, boolToInt(automation.Enabled), sortOrder, conditionsJSON)
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return s.Get(ctx, int(id))
}

func (s *AutomationStore) Update(ctx context.Context, automation *Automation) (*Automation, error) {
	if automation == nil {
		return nil, errors.New("automation is nil")
	}

	automation.TrackerPattern = normalizeTrackerPattern(automation.TrackerPattern, automation.TrackerDomains)

	// Default delete_mode to "none" if not set
	deleteMode := DeleteModeNone
	if automation.DeleteMode != nil && *automation.DeleteMode != "" {
		deleteMode = *automation.DeleteMode
	}

	// Default tag_match_mode to "any" if not set
	tagMatchMode := TagMatchModeAny
	if automation.TagMatchMode != "" {
		tagMatchMode = automation.TagMatchMode
	}

	// Join arrays to comma-separated strings for storage
	categoriesStr := nullableSlice(automation.Categories)
	tagsStr := nullableSlice(automation.Tags)

	// Serialize conditions to JSON if present
	var conditionsJSON any
	if automation.Conditions != nil && !automation.Conditions.IsEmpty() {
		data, err := json.Marshal(automation.Conditions)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal conditions: %w", err)
		}
		conditionsJSON = string(data)
	}

	// 0 means no minimum age, store as NULL
	var deleteUnregMinAge any
	if automation.DeleteUnregisteredMinAge > 0 {
		deleteUnregMinAge = automation.DeleteUnregisteredMinAge
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE automations
		SET name = ?, tracker_pattern = ?, category = ?, tag = ?, tag_match_mode = ?, upload_limit_kib = ?, download_limit_kib = ?,
		    ratio_limit = ?, seeding_time_limit_minutes = ?, delete_mode = ?, delete_unregistered = ?, delete_unregistered_min_age = ?, enabled = ?, sort_order = ?, conditions = ?
		WHERE id = ? AND instance_id = ?
	`, automation.Name, automation.TrackerPattern, categoriesStr, tagsStr, tagMatchMode,
		nullableInt64(automation.UploadLimitKiB), nullableInt64(automation.DownloadLimitKiB), nullableFloat64(automation.RatioLimit),
		nullableInt64(automation.SeedingTimeLimitMinutes), deleteMode, boolToInt(automation.DeleteUnregistered), deleteUnregMinAge, boolToInt(automation.Enabled), automation.SortOrder, conditionsJSON, automation.ID, automation.InstanceID)
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

	return s.Get(ctx, automation.ID)
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

func nullableSlice(values []string) any {
	if len(values) == 0 {
		return nil
	}
	return strings.Join(values, ",")
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableFloat64(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
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
	FieldName        ConditionField = "NAME"
	FieldHash        ConditionField = "HASH"
	FieldCategory    ConditionField = "CATEGORY"
	FieldTags        ConditionField = "TAGS"
	FieldSavePath    ConditionField = "SAVE_PATH"
	FieldContentPath ConditionField = "CONTENT_PATH"
	FieldState       ConditionField = "STATE"
	FieldTracker     ConditionField = "TRACKER"
	FieldComment     ConditionField = "COMMENT"

	// Numeric fields (bytes)
	FieldSize       ConditionField = "SIZE"
	FieldTotalSize  ConditionField = "TOTAL_SIZE"
	FieldDownloaded ConditionField = "DOWNLOADED"
	FieldUploaded   ConditionField = "UPLOADED"
	FieldAmountLeft ConditionField = "AMOUNT_LEFT"

	// Numeric fields (timestamps/seconds)
	FieldAddedOn      ConditionField = "ADDED_ON"
	FieldCompletionOn ConditionField = "COMPLETION_ON"
	FieldLastActivity ConditionField = "LAST_ACTIVITY"
	FieldSeedingTime  ConditionField = "SEEDING_TIME"
	FieldTimeActive   ConditionField = "TIME_ACTIVE"

	// Numeric fields (float64)
	FieldRatio        ConditionField = "RATIO"
	FieldProgress     ConditionField = "PROGRESS"
	FieldAvailability ConditionField = "AVAILABILITY"

	// Numeric fields (speeds)
	FieldDlSpeed ConditionField = "DL_SPEED"
	FieldUpSpeed ConditionField = "UP_SPEED"

	// Numeric fields (counts)
	FieldNumSeeds      ConditionField = "NUM_SEEDS"
	FieldNumLeechs     ConditionField = "NUM_LEECHS"
	FieldNumComplete   ConditionField = "NUM_COMPLETE"
	FieldNumIncomplete ConditionField = "NUM_INCOMPLETE"
	FieldTrackersCount ConditionField = "TRACKERS_COUNT"

	// Boolean fields
	FieldPrivate        ConditionField = "PRIVATE"
	FieldIsUnregistered ConditionField = "IS_UNREGISTERED"
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
)

// RuleCondition represents a condition or group of conditions for filtering torrents.
type RuleCondition struct {
	Field      ConditionField    `json:"field,omitempty"`
	Operator   ConditionOperator `json:"operator"`
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
	SchemaVersion string            `json:"schemaVersion"`
	SpeedLimits   *SpeedLimitAction `json:"speedLimits,omitempty"`
	Pause         *PauseAction      `json:"pause,omitempty"`
	Delete        *DeleteAction     `json:"delete,omitempty"`
	Tag           *TagAction        `json:"tag,omitempty"`
}

// SpeedLimitAction configures speed limit application with optional conditions.
type SpeedLimitAction struct {
	Enabled     bool           `json:"enabled"`
	UploadKiB   *int64         `json:"uploadKiB,omitempty"`
	DownloadKiB *int64         `json:"downloadKiB,omitempty"`
	Condition   *RuleCondition `json:"condition,omitempty"`
}

// PauseAction configures pause action with conditions.
type PauseAction struct {
	Enabled   bool           `json:"enabled"`
	Condition *RuleCondition `json:"condition,omitempty"`
}

// DeleteAction configures deletion with mode and conditions.
type DeleteAction struct {
	Enabled   bool           `json:"enabled"`
	Mode      string         `json:"mode"` // "delete", "deleteWithFiles", "deleteWithFilesPreserveCrossSeeds"
	Condition *RuleCondition `json:"condition,omitempty"`
}

// TagAction configures tagging with smart add/remove logic.
type TagAction struct {
	Enabled   bool           `json:"enabled"`
	Tags      []string       `json:"tags"`              // Tags to manage
	Mode      string         `json:"mode"`              // "full", "add", "remove"
	Condition *RuleCondition `json:"condition,omitempty"`
}

// IsEmpty returns true if no actions are configured.
func (ac *ActionConditions) IsEmpty() bool {
	if ac == nil {
		return true
	}
	return ac.SpeedLimits == nil && ac.Pause == nil && ac.Delete == nil && ac.Tag == nil
}
