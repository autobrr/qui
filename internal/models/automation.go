// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"regexp"
	"strconv"
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
	// FreeSpaceSourcePath reads free space at a path through the instance's
	// filesystem backend, local or SSH.
	FreeSpaceSourcePath FreeSpaceSourceType = "path"
)

// FreeSpaceSource configures how FREE_SPACE conditions obtain available disk space.
type FreeSpaceSource struct {
	Type FreeSpaceSourceType `json:"type" doc:"'qbittorrent' uses the free space that qBittorrent reports. 'path' reads the free space at 'path'."`
	Path string              `json:"path,omitempty" doc:"With type 'path': the path to read. Needs filesystem access on the instance."`
}

// ScoreRuleType defines the type of score rule.
type ScoreRuleType string

const (
	ScoreRuleTypeFieldMultiplier ScoreRuleType = "field_multiplier"
	ScoreRuleTypeConditional     ScoreRuleType = "conditional"
)

type SortDirection string

const (
	SortDirectionASC  SortDirection = "ASC"
	SortDirectionDESC SortDirection = "DESC"
)

type SortingType string

const (
	SortingTypeSimple SortingType = "simple"
	SortingTypeScore  SortingType = "score"
)

// FieldMultiplierScoreRule applies a multiplier to a numeric field.
type FieldMultiplierScoreRule struct {
	Field      ConditionField `json:"field" doc:"The condition field whose value is multiplied."`
	Multiplier float64        `json:"multiplier" doc:"The number to multiply the field value by. The result is added to the score."`
}

// ConditionalScoreRule adds a flat score if a condition is met.
type ConditionalScoreRule struct {
	Condition *RuleCondition `json:"condition" doc:"When the torrent matches this condition, score is added."`
	Score     float64        `json:"score" doc:"The score to add."`
}

// ScoreRule defines a single weighted rule for scoring torrents.
type ScoreRule struct {
	Type            ScoreRuleType             `json:"type" doc:"The kind of rule."`
	FieldMultiplier *FieldMultiplierScoreRule `json:"fieldMultiplier,omitempty" doc:"With type 'field_multiplier': the field and multiplier."`
	Conditional     *ConditionalScoreRule     `json:"conditional,omitempty" doc:"With type 'conditional': the condition and score."`
}

// SortingConfig defines how torrents are sorted before processing.
// This is the top-level structure stored in the `sorting_config` JSON column.
type SortingConfig struct {
	SchemaVersion string         `json:"schemaVersion" doc:"Must be '1'."`
	Type          SortingType    `json:"type" doc:"'simple' sorts by one field. 'score' sorts by the sum of 'scoreRules'."`
	Direction     SortDirection  `json:"direction" doc:"'ASC' or 'DESC'."`
	Field         ConditionField `json:"field,omitempty" doc:"With type 'simple': the condition field to sort by."`
	ScoreRules    []ScoreRule    `json:"scoreRules,omitempty" doc:"With type 'score': the rules that add up to the score of each torrent."`
}

func (c *SortingConfig) Validate() error {
	if c == nil {
		return nil
	}

	if c.SchemaVersion != "1" {
		return fmt.Errorf("invalid schema version: %s", c.SchemaVersion)
	}

	if c.Direction == "" {
		return errors.New("direction is required")
	}

	if c.Direction != SortDirectionASC && c.Direction != SortDirectionDESC {
		return fmt.Errorf("invalid direction: %s", c.Direction)
	}

	switch c.Type {
	case SortingTypeSimple:
		if c.Field == "" {
			return errors.New("simple sort requires field")
		}
		if !c.Field.IsNumeric() && !c.Field.IsString() {
			return fmt.Errorf("unknown sort field: %s", c.Field)
		}
	case SortingTypeScore:

		if len(c.ScoreRules) == 0 {
			return errors.New("score sort requires at least one rule")
		}

		for i, r := range c.ScoreRules {
			switch r.Type {
			case ScoreRuleTypeFieldMultiplier:
				if r.FieldMultiplier == nil {
					return fmt.Errorf("score rule %d: content missing for field multiplier", i)
				}
				if r.FieldMultiplier.Field == "" {
					return fmt.Errorf("score rule %d: field required for field multiplier", i)
				}
				if !r.FieldMultiplier.Field.IsNumeric() {
					return fmt.Errorf("field multiplier requires numeric field, got: %s", r.FieldMultiplier.Field)
				}
			case ScoreRuleTypeConditional:
				if r.Conditional == nil {
					return fmt.Errorf("score rule %d: content missing for conditional", i)
				}
				if r.Conditional.Condition == nil {
					return fmt.Errorf("score rule %d: condition missing", i)
				}
			default:
				return fmt.Errorf("score rule %d: unknown type %s", i, r.Type)
			}
		}
	case "":
		return errors.New("sorting config type is required")
	default:
		return fmt.Errorf("unknown sorting type: %s", c.Type)
	}

	return nil
}

type Automation struct {
	ID              int               `json:"id"`
	InstanceID      int               `json:"instanceId"`
	Name            string            `json:"name"`
	TrackerPattern  string            `json:"trackerPattern"`
	Conditions      *ActionConditions `json:"conditions"`
	FreeSpaceSource *FreeSpaceSource  `json:"freeSpaceSource,omitempty"` // nil = default qBittorrent free space
	SortingConfig   *SortingConfig    `json:"sortingConfig,omitempty"`   // nil = default sorting (oldest first)
	Enabled         bool              `json:"enabled"`
	DryRun          bool              `json:"dryRun"`
	Notify          bool              `json:"notify"`
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

func normalizeTrackerPattern(pattern string) string {
	return strings.Join(splitPatterns(pattern), ",")
}

func (s *AutomationStore) ListByInstance(ctx context.Context, instanceID int) ([]*Automation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, instance_id, name, tracker_pattern, conditions, enabled, dry_run, notify, sort_order, interval_seconds, free_space_source, sorting_config, created_at, updated_at
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
		var sortingConfigJSON sql.NullString
		var enabled, dryRun, notify int

		if err := rows.Scan(
			&automation.ID,
			&automation.InstanceID,
			&automation.Name,
			&automation.TrackerPattern,
			&conditionsJSON,
			&enabled,
			&dryRun,
			&notify,
			&automation.SortOrder,
			&intervalSeconds,
			&freeSpaceSourceJSON,
			&sortingConfigJSON,
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

		automation.Enabled = SQLiteIntToBool(enabled)
		automation.DryRun = SQLiteIntToBool(dryRun)
		automation.Notify = SQLiteIntToBool(notify)

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

		if sortingConfigJSON.Valid && sortingConfigJSON.String != "" {
			var sortingConfig SortingConfig
			if err := json.Unmarshal([]byte(sortingConfigJSON.String), &sortingConfig); err != nil {
				return nil, fmt.Errorf("failed to unmarshal sorting_config for automation %d: %w", automation.ID, err)
			}
			automation.SortingConfig = &sortingConfig
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
		SELECT id, instance_id, name, tracker_pattern, conditions, enabled, dry_run, notify, sort_order, interval_seconds, free_space_source, sorting_config, created_at, updated_at
		FROM automations
		WHERE id = ? AND instance_id = ?
	`, id, instanceID)

	var automation Automation
	var conditionsJSON string
	var intervalSeconds sql.NullInt64
	var freeSpaceSourceJSON sql.NullString
	var sortingConfigJSON sql.NullString
	var enabled, dryRun, notify int

	if err := row.Scan(
		&automation.ID,
		&automation.InstanceID,
		&automation.Name,
		&automation.TrackerPattern,
		&conditionsJSON,
		&enabled,
		&dryRun,
		&notify,
		&automation.SortOrder,
		&intervalSeconds,
		&freeSpaceSourceJSON,
		&sortingConfigJSON,
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

	automation.Enabled = SQLiteIntToBool(enabled)
	automation.DryRun = SQLiteIntToBool(dryRun)
	automation.Notify = SQLiteIntToBool(notify)

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

	if sortingConfigJSON.Valid && sortingConfigJSON.String != "" {
		var sortingConfig SortingConfig
		if err := json.Unmarshal([]byte(sortingConfigJSON.String), &sortingConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal sorting_config for automation %d: %w", automation.ID, err)
		}
		automation.SortingConfig = &sortingConfig
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
	if err := automation.Conditions.ExportToInstance.Validate(); err != nil {
		return nil, fmt.Errorf("invalid export to instance action: %w", err)
	}

	if automation.SortingConfig != nil {
		if err := automation.SortingConfig.Validate(); err != nil {
			return nil, fmt.Errorf("invalid sorting config: %w", err)
		}
	}

	automation.TrackerPattern = normalizeTrackerPattern(automation.TrackerPattern)

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

	var sortingConfigJSON sql.NullString
	if automation.SortingConfig != nil {
		data, marshalErr := json.Marshal(automation.SortingConfig)
		if marshalErr != nil {
			return nil, fmt.Errorf("failed to marshal sorting_config: %w", marshalErr)
		}
		sortingConfigJSON = sql.NullString{String: string(data), Valid: true}
	}
	var id int
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO automations
			(instance_id, name, tracker_pattern, conditions, enabled, dry_run, notify, sort_order, interval_seconds, free_space_source, sorting_config)
		VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`, automation.InstanceID, automation.Name, automation.TrackerPattern, string(conditionsJSON), boolToInt(automation.Enabled), boolToInt(automation.DryRun), boolToInt(automation.Notify), sortOrder, intervalSeconds, freeSpaceSourceJSON, sortingConfigJSON).Scan(&id)
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
	if err := automation.Conditions.ExportToInstance.Validate(); err != nil {
		return nil, fmt.Errorf("invalid export to instance action: %w", err)
	}

	if automation.SortingConfig != nil {
		if err := automation.SortingConfig.Validate(); err != nil {
			return nil, fmt.Errorf("invalid sorting config: %w", err)
		}
	}

	automation.TrackerPattern = normalizeTrackerPattern(automation.TrackerPattern)

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

	var sortingConfigJSON sql.NullString
	if automation.SortingConfig != nil {
		data, marshalErr := json.Marshal(automation.SortingConfig)
		if marshalErr != nil {
			return nil, fmt.Errorf("failed to marshal sorting_config: %w", marshalErr)
		}
		sortingConfigJSON = sql.NullString{String: string(data), Valid: true}
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE automations
		SET name = ?, tracker_pattern = ?, conditions = ?, enabled = ?, dry_run = ?, notify = ?, sort_order = ?, interval_seconds = ?, free_space_source = ?, sorting_config = ?
		WHERE id = ? AND instance_id = ?
	`, automation.Name, automation.TrackerPattern, string(conditionsJSON), boolToInt(automation.Enabled), boolToInt(automation.DryRun), boolToInt(automation.Notify), automation.SortOrder, intervalSeconds, freeSpaceSourceJSON, sortingConfigJSON, automation.ID, automation.InstanceID)
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
	defer func() { _ = tx.Rollback() }()

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
// Returns all automations referencing the program, regardless of whether the action is enabled.
func (s *AutomationStore) FindByExternalProgramID(ctx context.Context, programID int) ([]*AutomationReference, error) {
	dialect := dbinterface.DialectOf(s.db)
	programIDArg := any(programID)
	if dialect == "postgres" {
		programIDArg = strconv.Itoa(programID)
	}

	rows, err := s.db.QueryContext(ctx, findByExternalProgramIDQuery(dialect), programIDArg)
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
	dialect := dbinterface.DialectOf(s.db)
	programIDArg := any(programID)
	if dialect == "postgres" {
		programIDArg = strconv.Itoa(programID)
	}

	res, err := s.db.ExecContext(ctx, clearExternalProgramActionQuery(dialect), programIDArg)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func findByExternalProgramIDQuery(dialect string) string {
	if dialect == "postgres" {
		return `
		SELECT id, instance_id, name
		FROM automations
		WHERE (conditions::jsonb -> 'externalProgram' ->> 'programId') = ?
	`
	}

	return `
		SELECT id, instance_id, name
		FROM automations
		WHERE json_extract(conditions, '$.externalProgram.programId') = ?
	`
}

func clearExternalProgramActionQuery(dialect string) string {
	if dialect == "postgres" {
		return `
		UPDATE automations
		SET conditions = (conditions::jsonb - 'externalProgram')::text
		WHERE (conditions::jsonb -> 'externalProgram' ->> 'programId') = ?
	`
	}

	return `
		UPDATE automations
		SET conditions = json_remove(conditions, '$.externalProgram')
		WHERE json_extract(conditions, '$.externalProgram.programId') = ?
	`
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
	FieldRlsSource      ConditionField = "RLS_SOURCE"
	FieldRlsResolution  ConditionField = "RLS_RESOLUTION"
	FieldRlsCodec       ConditionField = "RLS_CODEC"
	FieldRlsHDR         ConditionField = "RLS_HDR"
	FieldRlsAudio       ConditionField = "RLS_AUDIO"
	FieldRlsChannels    ConditionField = "RLS_CHANNELS"
	FieldRlsGroup       ConditionField = "RLS_GROUP"
	FieldRlsYear        ConditionField = "RLS_YEAR"
	FieldState          ConditionField = "STATE"
	FieldTracker        ConditionField = "TRACKER"
	FieldTrackerStatus  ConditionField = "TRACKER_STATUS"
	FieldTrackerMessage ConditionField = "TRACKER_MESSAGE"
	FieldComment        ConditionField = "COMMENT"

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
	FieldRatio            ConditionField = "RATIO"
	FieldRatioLimit       ConditionField = "RATIO_LIMIT"
	FieldMaxRatio         ConditionField = "MAX_RATIO"
	FieldUploadedOverSize ConditionField = "UPLOADED_OVER_SIZE"
	FieldProgress         ConditionField = "PROGRESS"
	FieldAvailability     ConditionField = "AVAILABILITY"
	FieldPopularity       ConditionField = "POPULARITY"

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
	FieldPrivate                ConditionField = "PRIVATE"
	FieldAutoManaged            ConditionField = "AUTO_MANAGED"
	FieldFirstLastPiecePrio     ConditionField = "FIRST_LAST_PIECE_PRIO"
	FieldForceStart             ConditionField = "FORCE_START"
	FieldSequentialDownload     ConditionField = "SEQUENTIAL_DOWNLOAD"
	FieldSuperSeeding           ConditionField = "SUPER_SEEDING"
	FieldIsUnregistered         ConditionField = "IS_UNREGISTERED"
	FieldHasMissingFiles        ConditionField = "HAS_MISSING_FILES"
	FieldHasSkippedFiles        ConditionField = "HAS_SKIPPED_FILES"
	FieldIsGrouped              ConditionField = "IS_GROUPED"
	FieldExistsOnOtherInstance  ConditionField = "EXISTS_ON_OTHER_INSTANCE"
	FieldSeedingOnOtherInstance ConditionField = "SEEDING_ON_OTHER_INSTANCE"
	FieldExistsOnSameInstance   ConditionField = "EXISTS_ON_SAME_INSTANCE"
	FieldSeedingOnSameInstance  ConditionField = "SEEDING_ON_SAME_INSTANCE"
	FieldCrossSeedTags          ConditionField = "CROSS_SEED_TAGS"

	// Season pack fields
	FieldSeasonPackStatus            ConditionField = "SEASON_PACK_STATUS"
	FieldSeasonPackStatusAnyInstance ConditionField = "SEASON_PACK_STATUS_ANY_INSTANCE"

	// System time fields
	FieldSystemHour      ConditionField = "SYSTEM_HOUR"
	FieldSystemMinute    ConditionField = "SYSTEM_MINUTE"
	FieldSystemDayOfWeek ConditionField = "SYSTEM_DAY_OF_WEEK"
	FieldSystemDay       ConditionField = "SYSTEM_DAY"
	FieldSystemMonth     ConditionField = "SYSTEM_MONTH"
	FieldSystemYear      ConditionField = "SYSTEM_YEAR"

	// Enum-like fields
	FieldHardlinkScope      ConditionField = "HARDLINK_SCOPE"
	FieldHardlinkScopeCross ConditionField = "HARDLINK_SCOPE_CROSS"
)

//nolint:exhaustive // Only sortable numeric fields belong here.
func (f ConditionField) IsNumeric() bool {
	switch f {
	case FieldSize, FieldTotalSize, FieldDownloaded, FieldUploaded, FieldAmountLeft, FieldFreeSpace,
		FieldAddedOn, FieldCompletionOn, FieldLastActivity, FieldSeedingTime, FieldTimeActive,
		FieldAddedOnAge, FieldCompletionOnAge, FieldLastActivityAge,
		FieldRatio, FieldProgress, FieldAvailability,
		FieldDlSpeed, FieldUpSpeed,
		FieldNumSeeds, FieldNumLeechs, FieldNumComplete, FieldNumIncomplete, FieldTrackersCount,
		FieldSystemHour, FieldSystemMinute, FieldSystemDayOfWeek, FieldSystemDay, FieldSystemMonth, FieldSystemYear:
		return true
	default:
		return false
	}
}

// WrapsBetween reports whether a BETWEEN range on this field wraps when the
// minimum is greater than the maximum. Clock fields cycle, so 20 to 6 on the
// hour means the night that crosses midnight. A wrapped range stops before its
// maximum, see matchesBetween. SYSTEM_DAY and SYSTEM_YEAR do not wrap: a month
// boundary depends on the month length, and years do not cycle.
//
//nolint:exhaustive // Only cyclic clock fields belong here.
func (f ConditionField) WrapsBetween() bool {
	switch f {
	case FieldSystemHour, FieldSystemMinute, FieldSystemDayOfWeek, FieldSystemMonth:
		return true
	default:
		return false
	}
}

//nolint:exhaustive // Only sortable string fields belong here.
func (f ConditionField) IsString() bool {
	switch f {
	case FieldName, FieldCategory, FieldTags, FieldTracker, FieldState, FieldSavePath, FieldContentPath, FieldComment:
		return true
	default:
		return false
	}
}

// Hardlink scope values (wire format - stable API values).
// The stored per-torrent scope is one of none/torrents_only/outside_qbittorrent/both,
// a partition of the two independent facts "linked inside the torrent set" and
// "linked outside it". As condition values, outside_qbittorrent also matches "both"
// (its historical meaning), and inside_qbittorrent matches torrents_only or "both".
const (
	HardlinkScopeNone               = "none"                // No file has link count > 1
	HardlinkScopeTorrentsOnly       = "torrents_only"       // Links exist only between torrents in the set
	HardlinkScopeOutsideQBitTorrent = "outside_qbittorrent" // Links outside the torrent set (stored: outside only; condition: also matches "both")
	HardlinkScopeBoth               = "both"                // Links both inside and outside the torrent set
	HardlinkScopeInsideQBitTorrent  = "inside_qbittorrent"  // Condition-only: linked to other torrents, even if also outside (#1177)
)

// Season pack status values. Empty when the name parses without exactly one season.
const (
	SeasonPackStatusPack     = "pack"     // A whole single season
	SeasonPackStatusPacked   = "packed"   // An episode whose season pack of the same release is in the client
	SeasonPackStatusUnpacked = "unpacked" // An episode with no such pack
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
	Field      ConditionField    `json:"field,omitempty" doc:"The condition field to test. Leave it out on an 'AND' or 'OR' group."`
	Operator   ConditionOperator `json:"operator" doc:"An operator that the field allows, or 'AND' or 'OR' for a group."`
	GroupID    string            `json:"groupId,omitempty" doc:"For 'GROUP_SIZE' and 'IS_GROUPED': the group to use. A built-in group ID or the id of a group in 'conditions.grouping'. Default: 'conditions.grouping.defaultGroupId'."`
	Value      string            `json:"value,omitempty" doc:"The value to compare, always as a JSON string, also for numbers and yes/no values. Not used with 'BETWEEN'."`
	MinValue   *float64          `json:"minValue,omitempty" doc:"For 'BETWEEN': the lower end of the range, as a JSON number."`
	MaxValue   *float64          `json:"maxValue,omitempty" doc:"For 'BETWEEN': the upper end of the range, as a JSON number. On 'SYSTEM_HOUR', 'SYSTEM_MINUTE', 'SYSTEM_DAY_OF_WEEK' and 'SYSTEM_MONTH', a 'minValue' above 'maxValue' wraps past the end of the field."`
	Regex      bool              `json:"regex,omitempty" doc:"When 'true', 'value' is a regular expression. Applies to string fields. 'MATCHES' always uses a regular expression."`
	Negate     bool              `json:"negate,omitempty" doc:"When 'true', the condition matches when the test is false."`
	Conditions []*RuleCondition  `json:"conditions,omitempty" doc:"For an 'AND' or 'OR' group: the child conditions."`
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
	SchemaVersion    string                  `json:"schemaVersion" doc:"Must be '1'."`
	Grouping         *GroupingConfig         `json:"grouping,omitempty" doc:"Group definitions for group-aware actions and conditions."`
	SpeedLimits      *SpeedLimitAction       `json:"speedLimits,omitempty" doc:"Set upload and download speed limits."`
	ShareLimits      *ShareLimitsAction      `json:"shareLimits,omitempty" doc:"Set ratio and seeding time limits."`
	Pause            *PauseAction            `json:"pause,omitempty" doc:"Pause matching torrents."`
	Resume           *ResumeAction           `json:"resume,omitempty" doc:"Resume matching torrents."`
	Recheck          *RecheckAction          `json:"recheck,omitempty" doc:"Force a recheck of matching torrents."`
	Reannounce       *ReannounceAction       `json:"reannounce,omitempty" doc:"Force a reannounce of matching torrents."`
	Delete           *DeleteAction           `json:"delete,omitempty" doc:"Delete matching torrents. A rule with 'delete' has no other action."`
	Tag              *TagAction              `json:"tag,omitempty" doc:"-"` // Legacy single-tag action (backward compatible alias for first entry in Tags)
	Tags             []*TagAction            `json:"tags,omitempty" doc:"Tag actions. Each entry manages its own set of tags."`
	Category         *CategoryAction         `json:"category,omitempty" doc:"Set the category of matching torrents."`
	Move             *MoveAction             `json:"move,omitempty" doc:"Move matching torrents to a different save path."`
	ExternalProgram  *ExternalProgramAction  `json:"externalProgram,omitempty" doc:"Run an external program for each matching torrent."`
	AutoManagement   *AutoManagementAction   `json:"autoManagement,omitempty" doc:"Turn automatic torrent management on or off."`
	ExportToInstance *ExportToInstanceAction `json:"exportToInstance,omitempty" doc:"Add matching torrents to a different qBittorrent instance."`
}

// SpeedLimitAction configures speed limit application with optional conditions.
type SpeedLimitAction struct {
	Enabled     bool           `json:"enabled" doc:"Whether the action runs."`
	UploadKiB   *int64         `json:"uploadKiB,omitempty" doc:"Upload limit in KiB/s. '0' removes the limit. Leave it out to keep the current limit."`
	DownloadKiB *int64         `json:"downloadKiB,omitempty" doc:"Download limit in KiB/s. '0' removes the limit. Leave it out to keep the current limit."`
	Condition   *RuleCondition `json:"condition,omitempty" doc:"Optional. When set, the action applies only to torrents that also match this condition."`
}

// ShareLimitsAction configures share limit (ratio/seeding time) application with optional conditions.
type ShareLimitsAction struct {
	Enabled            bool           `json:"enabled" doc:"Whether the action runs."`
	RatioLimit         *float64       `json:"ratioLimit,omitempty" doc:"Ratio limit. '-2' uses the global setting, '-1' means no limit. Leave it out to keep the current limit."`
	SeedingTimeMinutes *int64         `json:"seedingTimeMinutes,omitempty" doc:"Seeding time limit in minutes. '-2' uses the global setting, '-1' means no limit. Leave it out to keep the current limit."`
	ShareLimitAction   *string        `json:"shareLimitAction,omitempty" doc:"What qBittorrent does when a limit is reached. Needs qBittorrent Web API 2.15.1+. Leave it out to use the global setting."`
	ShareLimitsMode    *string        `json:"shareLimitsMode,omitempty" doc:"Whether any limit or all limits must be reached. Needs qBittorrent Web API 2.16.0+. Leave it out to use the global setting."`
	Condition          *RuleCondition `json:"condition,omitempty" doc:"Optional. When set, the action applies only to torrents that also match this condition."`
}

// PauseAction configures pause action with conditions.
type PauseAction struct {
	Enabled   bool           `json:"enabled" doc:"Whether the action runs."`
	Condition *RuleCondition `json:"condition,omitempty" doc:"Optional. When set, the action applies only to torrents that also match this condition."`
}

// ResumeAction configures resume action with conditions.
type ResumeAction struct {
	Enabled   bool           `json:"enabled" doc:"Whether the action runs."`
	Condition *RuleCondition `json:"condition,omitempty" doc:"Optional. When set, the action applies only to torrents that also match this condition."`
}

// RecheckAction configures force recheck action with optional conditions.
type RecheckAction struct {
	Enabled   bool           `json:"enabled" doc:"Whether the action runs."`
	Condition *RuleCondition `json:"condition,omitempty" doc:"Optional. When set, the action applies only to torrents that also match this condition."`
}

// ReannounceAction configures force reannounce action with optional conditions.
type ReannounceAction struct {
	Enabled   bool           `json:"enabled" doc:"Whether the action runs."`
	Condition *RuleCondition `json:"condition,omitempty" doc:"Optional. When set, the action applies only to torrents that also match this condition."`
}

// AutoManagementAction configures automatic torrent management (ATM) with optional conditions.
type AutoManagementAction struct {
	Enabled   bool           `json:"enabled" doc:"'true' turns automatic torrent management on, 'false' turns it off."`
	Condition *RuleCondition `json:"condition,omitempty" doc:"Optional. When set, the action applies only to torrents that also match this condition."`
}

// DeleteAction configures deletion with mode and conditions.
type DeleteAction struct {
	Enabled          bool           `json:"enabled" doc:"Whether the action runs."`
	Mode             string         `json:"mode" doc:"What to delete. Default: 'delete'. When 'condition' uses 'FREE_SPACE', the mode must not be 'delete', because a delete that keeps the files frees no disk space."`
	IncludeHardlinks bool           `json:"includeHardlinks,omitempty" doc:"Also delete torrents that share files through hardlinks. Only with mode 'deleteWithFilesIncludeCrossSeeds', and needs local filesystem access."`
	GroupID          string         `json:"groupId,omitempty" doc:"Only with mode 'delete'. Delete each matching torrent together with the other torrents in its group. A built-in group ID or the id of a group in 'conditions.grouping'."`
	Atomic           string         `json:"atomic,omitempty" doc:"With 'groupId': 'all' deletes the group only when every torrent in it matches."`
	Condition        *RuleCondition `json:"condition,omitempty" doc:"Required when 'enabled' is 'true'. The action deletes only torrents that match this condition."`
}

// TagAction configures tagging with smart add/remove logic.
type TagAction struct {
	Enabled           bool           `json:"enabled" doc:"Whether the action runs."`
	Tags              []string       `json:"tags" doc:"The tags that this action manages. With 'useTrackerAsTag', qui uses them when a torrent has no tracker domain."`
	Mode              string         `json:"mode" doc:"'full' adds the tags to matching torrents and removes them from the others. 'add' only adds. 'remove' only removes them from matching torrents."`
	DeleteFromClient  bool           `json:"deleteFromClient,omitempty" doc:"Delete the managed tags from qBittorrent before the rule tags the matches. Needs explicit tags."`
	UseTrackerAsTag   bool           `json:"useTrackerAsTag,omitempty" doc:"Use the tracker domain of each torrent as its tag."`
	UseDisplayName    bool           `json:"useDisplayName,omitempty" doc:"With 'useTrackerAsTag': use the tracker display name in place of the domain."`
	IncludeCrossSeeds bool           `json:"includeCrossSeeds,omitempty" doc:"Also apply the tag changes to the cross-seeds of each matching torrent. Ignored with 'useTrackerAsTag'."`
	Condition         *RuleCondition `json:"condition,omitempty" doc:"Optional. When set, the action applies only to torrents that also match this condition."`
}

// CategoryAction configures category assignment with optional conditions.
type CategoryAction struct {
	Enabled           bool   `json:"enabled" doc:"Whether the action runs."`
	Category          string `json:"category" doc:"The category to set. An empty string removes the category."`
	IncludeCrossSeeds bool   `json:"includeCrossSeeds,omitempty" doc:"Also move the cross-seeds of each matching torrent to the category."`
	GroupID           string `json:"groupId,omitempty" doc:"Change the category of each matching torrent together with the other torrents in its group. A built-in group ID or the id of a group in 'conditions.grouping'."`
	// BlockIfCrossSeedInCategories prevents category changes when any other cross-seed torrent
	// (same ContentPath + SavePath) is found in one of the listed categories.
	BlockIfCrossSeedInCategories []string       `json:"blockIfCrossSeedInCategories,omitempty" doc:"Skip a torrent when one of its cross-seeds is in one of these categories."`
	Condition                    *RuleCondition `json:"condition,omitempty" doc:"Optional. When set, the action applies only to torrents that also match this condition."`
}

type MoveAction struct {
	Enabled          bool           `json:"enabled" doc:"Whether the action runs."`
	Path             string         `json:"path" doc:"The new save path."`
	BlockIfCrossSeed bool           `json:"blockIfCrossSeed,omitempty" doc:"Skip a torrent when one of its cross-seeds does not match 'condition'. Not used when 'groupId' is set."`
	GroupID          string         `json:"groupId,omitempty" doc:"Move each matching torrent together with the other torrents in its group. A built-in group ID or the id of a group in 'conditions.grouping'."`
	Atomic           string         `json:"atomic,omitempty" doc:"With 'groupId': 'all' moves the group only when every torrent in it matches."`
	Condition        *RuleCondition `json:"condition,omitempty" doc:"Optional. When set, the action applies only to torrents that also match this condition."`
}

// GroupingConfig defines how torrents can be grouped for group-aware actions and conditions.
// Group definitions are per-rule (stored inside the rule's conditions JSON).
type GroupingConfig struct {
	DefaultGroupID string            `json:"defaultGroupId,omitempty" doc:"The group that 'GROUP_SIZE' and 'IS_GROUPED' use when the condition has no 'groupId'. A built-in group ID or the id of a group in 'groups'."`
	Groups         []GroupDefinition `json:"groups,omitempty" doc:"Custom group definitions."`
}

// GroupDefinition defines a single grouping strategy.
type GroupDefinition struct {
	ID                    string   `json:"id" doc:"The group ID. Conditions and actions refer to the group by this ID."`
	Keys                  []string `json:"keys" doc:"Torrents with equal values for all these keys are in the same group."`
	AmbiguousPolicy       string   `json:"ambiguousPolicy,omitempty" doc:"What the delete, category, and move actions do when they expand to the group, 'contentPath' is one of the keys, and a torrent content path equals its save path. 'verify_overlap' compares the files. 'skip' does not expand the action to the group. 'GROUP_SIZE' and 'IS_GROUPED' ignore this setting."`
	MinFileOverlapPercent int      `json:"minFileOverlapPercent,omitempty" doc:"With 'verify_overlap': the percentage of files that must overlap. Default '90'."`
}

// ExternalProgramAction configures external program execution with optional conditions.
type ExternalProgramAction struct {
	Enabled   bool           `json:"enabled" doc:"Whether the action runs."`
	ProgramID int            `json:"programId" doc:"The ID of the external program in this qui. An export keeps the ID of the qui that made it."` // FK to external_programs table
	Condition *RuleCondition `json:"condition,omitempty" doc:"Optional. When set, the action applies only to torrents that also match this condition."`
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

// ExportToInstanceAction configures exporting a torrent to a different qBittorrent instance.
type ExportToInstanceAction struct {
	Enabled          bool           `json:"enabled" doc:"Whether the action runs."`
	TargetInstanceID int            `json:"targetInstanceId" doc:"The ID of the target instance in this qui. The ID is in the URL of the instance page, '/instances/ID'."`
	SavePath         string         `json:"savePath" doc:"The save path on the target instance. Supports Go templates."`
	Category         string         `json:"category,omitempty" doc:"The category on the target instance."`
	Tags             []string       `json:"tags,omitempty" doc:"The tags on the target instance."`
	Paused           bool           `json:"paused,omitempty" doc:"Add the torrent paused."`
	SkipChecking     *bool          `json:"skipChecking,omitempty" doc:"Skip the hash check on the target instance. Default 'true'."`
	ContentLayout    string         `json:"contentLayout,omitempty" doc:"The content layout on the target instance. Leave it out to use the default layout of the target instance."`
	Condition        *RuleCondition `json:"condition,omitempty" doc:"Optional. When set, the action applies only to torrents that also match this condition."`
}

// SkipCheckingEnabled returns the effective skip_checking value (defaults true).
func (a *ExportToInstanceAction) SkipCheckingEnabled() bool {
	if a.SkipChecking == nil {
		return true
	}
	return *a.SkipChecking
}

// Validate checks that the ExportToInstanceAction has valid configuration.
func (a *ExportToInstanceAction) Validate() error {
	if a == nil {
		return nil
	}
	if a.Enabled && a.TargetInstanceID <= 0 {
		return errors.New("enabled export to instance action requires valid targetInstanceId")
	}
	return nil
}

// IsEmpty returns true if no actions are configured.
func (ac *ActionConditions) IsEmpty() bool {
	for range ac.Conditions() {
		return false
	}
	return true
}

// ActionCondition is one configured action's condition, as ActionConditions.Conditions yields it.
type ActionCondition struct {
	Path      string // JSON pointer to the condition, for example "/conditions/move/condition"
	Enabled   bool
	Condition *RuleCondition
}

// Conditions yields every configured action with its condition, which can be nil. Each caller
// decides whether a disabled action counts. An AutoManagement action counts as enabled whenever
// it exists, because its Enabled field is the ATM value that it sets.
func (ac *ActionConditions) Conditions() iter.Seq[ActionCondition] {
	return func(yield func(ActionCondition) bool) {
		if ac == nil {
			return
		}
		emit := func(name string, enabled bool, cond *RuleCondition) bool {
			return yield(ActionCondition{Path: "/conditions/" + name + "/condition", Enabled: enabled, Condition: cond})
		}
		if ac.SpeedLimits != nil && !emit("speedLimits", ac.SpeedLimits.Enabled, ac.SpeedLimits.Condition) {
			return
		}
		if ac.ShareLimits != nil && !emit("shareLimits", ac.ShareLimits.Enabled, ac.ShareLimits.Condition) {
			return
		}
		if ac.Pause != nil && !emit("pause", ac.Pause.Enabled, ac.Pause.Condition) {
			return
		}
		if ac.Resume != nil && !emit("resume", ac.Resume.Enabled, ac.Resume.Condition) {
			return
		}
		if ac.Recheck != nil && !emit("recheck", ac.Recheck.Enabled, ac.Recheck.Condition) {
			return
		}
		if ac.Reannounce != nil && !emit("reannounce", ac.Reannounce.Enabled, ac.Reannounce.Condition) {
			return
		}
		if ac.Delete != nil && !emit("delete", ac.Delete.Enabled, ac.Delete.Condition) {
			return
		}
		for i, tag := range ac.TagActions() {
			var enabled bool
			var cond *RuleCondition
			if tag != nil {
				enabled, cond = tag.Enabled, tag.Condition
			}
			if !emit("tags/"+strconv.Itoa(i), enabled, cond) {
				return
			}
		}
		if ac.Category != nil && !emit("category", ac.Category.Enabled, ac.Category.Condition) {
			return
		}
		if ac.Move != nil && !emit("move", ac.Move.Enabled, ac.Move.Condition) {
			return
		}
		if ac.ExternalProgram != nil && !emit("externalProgram", ac.ExternalProgram.Enabled, ac.ExternalProgram.Condition) {
			return
		}
		if ac.AutoManagement != nil && !emit("autoManagement", true, ac.AutoManagement.Condition) {
			return
		}
		if ac.ExportToInstance != nil {
			emit("exportToInstance", ac.ExportToInstance.Enabled, ac.ExportToInstance.Condition)
		}
	}
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
