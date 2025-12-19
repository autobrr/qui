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
	DeleteModeNone                         = "none"
	DeleteModeKeepFiles                    = "delete"
	DeleteModeWithFiles                    = "deleteWithFiles"
	DeleteModeWithFilesPreserveCrossSeeds  = "deleteWithFilesPreserveCrossSeeds"
)

type TrackerRule struct {
	ID                      int               `json:"id"`
	InstanceID              int               `json:"instanceId"`
	Name                    string            `json:"name"`
	TrackerPattern          string            `json:"trackerPattern"`
	TrackerDomains          []string          `json:"trackerDomains,omitempty"`
	Categories              []string          `json:"categories,omitempty"`
	Tags                    []string          `json:"tags,omitempty"`
	TagMatchMode            string            `json:"tagMatchMode,omitempty"` // "any" (default) or "all"
	UploadLimitKiB          *int64            `json:"uploadLimitKiB,omitempty"`
	DownloadLimitKiB        *int64            `json:"downloadLimitKiB,omitempty"`
	RatioLimit              *float64          `json:"ratioLimit,omitempty"`
	SeedingTimeLimitMinutes *int64            `json:"seedingTimeLimitMinutes,omitempty"`
	DeleteMode                 *string           `json:"deleteMode,omitempty"` // "none", "delete", "deleteWithFiles", "deleteWithFilesPreserveCrossSeeds"
	DeleteUnregistered         bool              `json:"deleteUnregistered"`
	DeleteUnregisteredMinAge   int64             `json:"deleteUnregisteredMinAge,omitempty"` // minimum age in seconds, 0 = no minimum
	Enabled                 bool              `json:"enabled"`
	SortOrder               int               `json:"sortOrder"`
	Conditions              *ActionConditions `json:"conditions,omitempty"` // expression-based conditions for actions
	CreatedAt               time.Time         `json:"createdAt"`
	UpdatedAt               time.Time         `json:"updatedAt"`
}

// UsesExpressions returns true if this rule uses expression-based conditions
// instead of the legacy static fields.
func (r *TrackerRule) UsesExpressions() bool {
	return r.Conditions != nil && r.Conditions.SchemaVersion != ""
}

type TrackerRuleStore struct {
	db dbinterface.Querier
}

func NewTrackerRuleStore(db dbinterface.Querier) *TrackerRuleStore {
	return &TrackerRuleStore{db: db}
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

func (s *TrackerRuleStore) ListByInstance(ctx context.Context, instanceID int) ([]*TrackerRule, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, instance_id, name, tracker_pattern, category, tag, tag_match_mode, upload_limit_kib, download_limit_kib,
		       ratio_limit, seeding_time_limit_minutes, delete_mode, delete_unregistered, delete_unregistered_min_age, enabled, sort_order, conditions, created_at, updated_at
		FROM tracker_rules
		WHERE instance_id = ?
		ORDER BY sort_order ASC, id ASC
	`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []*TrackerRule
	for rows.Next() {
		var rule TrackerRule
		var categories, tags, tagMatchMode, deleteMode, conditionsJSON sql.NullString
		var upload, download sql.NullInt64
		var ratio sql.NullFloat64
		var seeding sql.NullInt64
		var deleteUnregistered int
		var deleteUnregisteredMinAge sql.NullInt64

		if err := rows.Scan(
			&rule.ID,
			&rule.InstanceID,
			&rule.Name,
			&rule.TrackerPattern,
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
			&rule.Enabled,
			&rule.SortOrder,
			&conditionsJSON,
			&rule.CreatedAt,
			&rule.UpdatedAt,
		); err != nil {
			return nil, err
		}

		if categories.Valid && categories.String != "" {
			rule.Categories = splitPatterns(categories.String)
		}
		if tags.Valid && tags.String != "" {
			rule.Tags = splitPatterns(tags.String)
		}
		if tagMatchMode.Valid && tagMatchMode.String != "" {
			rule.TagMatchMode = tagMatchMode.String
		} else {
			rule.TagMatchMode = TagMatchModeAny
		}
		if upload.Valid {
			rule.UploadLimitKiB = &upload.Int64
		}
		if download.Valid {
			rule.DownloadLimitKiB = &download.Int64
		}
		if ratio.Valid {
			rule.RatioLimit = &ratio.Float64
		}
		if seeding.Valid {
			rule.SeedingTimeLimitMinutes = &seeding.Int64
		}
		if deleteMode.Valid && deleteMode.String != DeleteModeNone {
			rule.DeleteMode = &deleteMode.String
		}
		rule.DeleteUnregistered = deleteUnregistered != 0
		if deleteUnregisteredMinAge.Valid {
			rule.DeleteUnregisteredMinAge = deleteUnregisteredMinAge.Int64
		}

		// Parse conditions JSON if present
		if conditionsJSON.Valid && conditionsJSON.String != "" {
			var conditions ActionConditions
			if err := json.Unmarshal([]byte(conditionsJSON.String), &conditions); err == nil {
				rule.Conditions = &conditions
			}
		}

		rule.TrackerDomains = splitPatterns(rule.TrackerPattern)

		rules = append(rules, &rule)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return rules, nil
}

func (s *TrackerRuleStore) Get(ctx context.Context, id int) (*TrackerRule, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, name, tracker_pattern, category, tag, tag_match_mode, upload_limit_kib, download_limit_kib,
		       ratio_limit, seeding_time_limit_minutes, delete_mode, delete_unregistered, delete_unregistered_min_age, enabled, sort_order, conditions, created_at, updated_at
		FROM tracker_rules
		WHERE id = ?
	`, id)

	var rule TrackerRule
	var categories, tags, tagMatchMode, deleteMode, conditionsJSON sql.NullString
	var upload, download sql.NullInt64
	var ratio sql.NullFloat64
	var seeding sql.NullInt64
	var deleteUnregistered int
	var deleteUnregisteredMinAge sql.NullInt64

	if err := row.Scan(
		&rule.ID,
		&rule.InstanceID,
		&rule.Name,
		&rule.TrackerPattern,
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
		&rule.Enabled,
		&rule.SortOrder,
		&conditionsJSON,
		&rule.CreatedAt,
		&rule.UpdatedAt,
	); err != nil {
		return nil, err
	}

	if categories.Valid && categories.String != "" {
		rule.Categories = splitPatterns(categories.String)
	}
	if tags.Valid && tags.String != "" {
		rule.Tags = splitPatterns(tags.String)
	}
	if tagMatchMode.Valid && tagMatchMode.String != "" {
		rule.TagMatchMode = tagMatchMode.String
	} else {
		rule.TagMatchMode = TagMatchModeAny
	}
	if upload.Valid {
		rule.UploadLimitKiB = &upload.Int64
	}
	if download.Valid {
		rule.DownloadLimitKiB = &download.Int64
	}
	if ratio.Valid {
		rule.RatioLimit = &ratio.Float64
	}
	if seeding.Valid {
		rule.SeedingTimeLimitMinutes = &seeding.Int64
	}
	if deleteMode.Valid && deleteMode.String != DeleteModeNone {
		rule.DeleteMode = &deleteMode.String
	}
	rule.DeleteUnregistered = deleteUnregistered != 0
	if deleteUnregisteredMinAge.Valid {
		rule.DeleteUnregisteredMinAge = deleteUnregisteredMinAge.Int64
	}

	// Parse conditions JSON if present
	if conditionsJSON.Valid && conditionsJSON.String != "" {
		var conditions ActionConditions
		if err := json.Unmarshal([]byte(conditionsJSON.String), &conditions); err == nil {
			rule.Conditions = &conditions
		}
	}

	rule.TrackerDomains = splitPatterns(rule.TrackerPattern)

	return &rule, nil
}

func (s *TrackerRuleStore) nextSortOrder(ctx context.Context, instanceID int) (int, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), 0) FROM tracker_rules WHERE instance_id = ?`, instanceID)
	var maxOrder int
	if err := row.Scan(&maxOrder); err != nil {
		return 0, err
	}
	return maxOrder + 1, nil
}

func (s *TrackerRuleStore) Create(ctx context.Context, rule *TrackerRule) (*TrackerRule, error) {
	if rule == nil {
		return nil, errors.New("rule is nil")
	}

	rule.TrackerPattern = normalizeTrackerPattern(rule.TrackerPattern, rule.TrackerDomains)

	sortOrder := rule.SortOrder
	if sortOrder == 0 {
		next, err := s.nextSortOrder(ctx, rule.InstanceID)
		if err != nil {
			return nil, err
		}
		sortOrder = next
	}

	// Default delete_mode to "none" if not set
	deleteMode := DeleteModeNone
	if rule.DeleteMode != nil && *rule.DeleteMode != "" {
		deleteMode = *rule.DeleteMode
	}

	// Default tag_match_mode to "any" if not set
	tagMatchMode := TagMatchModeAny
	if rule.TagMatchMode != "" {
		tagMatchMode = rule.TagMatchMode
	}

	// Join arrays to comma-separated strings for storage
	categoriesStr := nullableSlice(rule.Categories)
	tagsStr := nullableSlice(rule.Tags)

	// Serialize conditions to JSON if present
	var conditionsJSON any
	if rule.Conditions != nil && !rule.Conditions.IsEmpty() {
		data, err := json.Marshal(rule.Conditions)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal conditions: %w", err)
		}
		conditionsJSON = string(data)
	}

	// 0 means no minimum age, store as NULL
	var deleteUnregMinAge any
	if rule.DeleteUnregisteredMinAge > 0 {
		deleteUnregMinAge = rule.DeleteUnregisteredMinAge
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO tracker_rules
			(instance_id, name, tracker_pattern, category, tag, tag_match_mode, upload_limit_kib, download_limit_kib, ratio_limit, seeding_time_limit_minutes, delete_mode, delete_unregistered, delete_unregistered_min_age, enabled, sort_order, conditions)
		VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, rule.InstanceID, rule.Name, rule.TrackerPattern, categoriesStr, tagsStr, tagMatchMode,
		nullableInt64(rule.UploadLimitKiB), nullableInt64(rule.DownloadLimitKiB), nullableFloat64(rule.RatioLimit),
		nullableInt64(rule.SeedingTimeLimitMinutes), deleteMode, boolToInt(rule.DeleteUnregistered), deleteUnregMinAge, boolToInt(rule.Enabled), sortOrder, conditionsJSON)
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return s.Get(ctx, int(id))
}

func (s *TrackerRuleStore) Update(ctx context.Context, rule *TrackerRule) (*TrackerRule, error) {
	if rule == nil {
		return nil, errors.New("rule is nil")
	}

	rule.TrackerPattern = normalizeTrackerPattern(rule.TrackerPattern, rule.TrackerDomains)

	// Default delete_mode to "none" if not set
	deleteMode := DeleteModeNone
	if rule.DeleteMode != nil && *rule.DeleteMode != "" {
		deleteMode = *rule.DeleteMode
	}

	// Default tag_match_mode to "any" if not set
	tagMatchMode := TagMatchModeAny
	if rule.TagMatchMode != "" {
		tagMatchMode = rule.TagMatchMode
	}

	// Join arrays to comma-separated strings for storage
	categoriesStr := nullableSlice(rule.Categories)
	tagsStr := nullableSlice(rule.Tags)

	// Serialize conditions to JSON if present
	var conditionsJSON any
	if rule.Conditions != nil && !rule.Conditions.IsEmpty() {
		data, err := json.Marshal(rule.Conditions)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal conditions: %w", err)
		}
		conditionsJSON = string(data)
	}

	// 0 means no minimum age, store as NULL
	var deleteUnregMinAge any
	if rule.DeleteUnregisteredMinAge > 0 {
		deleteUnregMinAge = rule.DeleteUnregisteredMinAge
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE tracker_rules
		SET name = ?, tracker_pattern = ?, category = ?, tag = ?, tag_match_mode = ?, upload_limit_kib = ?, download_limit_kib = ?,
		    ratio_limit = ?, seeding_time_limit_minutes = ?, delete_mode = ?, delete_unregistered = ?, delete_unregistered_min_age = ?, enabled = ?, sort_order = ?, conditions = ?
		WHERE id = ? AND instance_id = ?
	`, rule.Name, rule.TrackerPattern, categoriesStr, tagsStr, tagMatchMode,
		nullableInt64(rule.UploadLimitKiB), nullableInt64(rule.DownloadLimitKiB), nullableFloat64(rule.RatioLimit),
		nullableInt64(rule.SeedingTimeLimitMinutes), deleteMode, boolToInt(rule.DeleteUnregistered), deleteUnregMinAge, boolToInt(rule.Enabled), rule.SortOrder, conditionsJSON, rule.ID, rule.InstanceID)
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

	return s.Get(ctx, rule.ID)
}

func (s *TrackerRuleStore) Delete(ctx context.Context, instanceID int, id int) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tracker_rules WHERE id = ? AND instance_id = ?`, id, instanceID)
	if err != nil {
		return err
	}
	if rows, err := res.RowsAffected(); err == nil && rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *TrackerRuleStore) Reorder(ctx context.Context, instanceID int, orderedIDs []int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for idx, id := range orderedIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE tracker_rules SET sort_order = ? WHERE id = ? AND instance_id = ?`, idx+1, id, instanceID); err != nil {
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

// IsEmpty returns true if no actions are configured.
func (ac *ActionConditions) IsEmpty() bool {
	if ac == nil {
		return true
	}
	return ac.SpeedLimits == nil && ac.Pause == nil && ac.Delete == nil
}
