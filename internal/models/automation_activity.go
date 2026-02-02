// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

// Activity action types
const (
	ActivityActionDeletedRatio        = "deleted_ratio"
	ActivityActionDeletedSeeding      = "deleted_seeding"
	ActivityActionDeletedUnregistered = "deleted_unregistered"
	ActivityActionDeletedCondition    = "deleted_condition" // Expression-based deletion
	ActivityActionDeleteFailed        = "delete_failed"
	ActivityActionLimitFailed         = "limit_failed"
	ActivityActionTagsChanged         = "tags_changed"         // Batch tag operation
	ActivityActionCategoryChanged     = "category_changed"     // Batch category operation
	ActivityActionSpeedLimitsChanged  = "speed_limits_changed" // Batch speed limit operation
	ActivityActionShareLimitsChanged  = "share_limits_changed" // Batch share limit operation
	ActivityActionPaused              = "paused"               // Batch pause operation
	ActivityActionMoved               = "moved"                // Batch move operation
)

// Activity outcome types
const (
	ActivityOutcomeSuccess = "success"
	ActivityOutcomeFailed  = "failed"
	ActivityOutcomeDryRun  = "dry-run"
)

type AutomationActivity struct {
	ID            int             `json:"id"`
	InstanceID    int             `json:"instanceId"`
	Hash          string          `json:"hash"`
	TorrentName   string          `json:"torrentName,omitempty"`
	TrackerDomain string          `json:"trackerDomain,omitempty"`
	Action        string          `json:"action"`
	RuleID        *int            `json:"ruleId,omitempty"`
	RuleName      string          `json:"ruleName,omitempty"`
	Outcome       string          `json:"outcome"`
	Reason        string          `json:"reason,omitempty"`
	Details       json.RawMessage `json:"details,omitempty"`
	CreatedAt     time.Time       `json:"createdAt"`
}

type AutomationActivityStore struct {
	db dbinterface.Querier
}

func NewAutomationActivityStore(db dbinterface.Querier) *AutomationActivityStore {
	return &AutomationActivityStore{db: db}
}

func (s *AutomationActivityStore) Create(ctx context.Context, activity *AutomationActivity) error {
	if activity == nil {
		return nil
	}

	_, err := s.insert(ctx, activity)
	return err
}

func (s *AutomationActivityStore) CreateWithID(ctx context.Context, activity *AutomationActivity) (int, error) {
	if activity == nil {
		return 0, nil
	}

	res, err := s.insert(ctx, activity)
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	return int(id), nil
}

func (s *AutomationActivityStore) insert(ctx context.Context, activity *AutomationActivity) (sql.Result, error) {
	if activity == nil {
		return nil, nil
	}

	var detailsStr sql.NullString
	if len(activity.Details) > 0 {
		detailsStr = sql.NullString{String: string(activity.Details), Valid: true}
	}

	var ruleID sql.NullInt64
	if activity.RuleID != nil {
		ruleID = sql.NullInt64{Int64: int64(*activity.RuleID), Valid: true}
	}

	return s.db.ExecContext(ctx, `
		INSERT INTO automation_activity
			(instance_id, hash, torrent_name, tracker_domain, action, rule_id, rule_name, outcome, reason, details)
		VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, activity.InstanceID, activity.Hash, activity.TorrentName, activity.TrackerDomain, activity.Action,
		ruleID, activity.RuleName, activity.Outcome, activity.Reason, detailsStr)
}

func (s *AutomationActivityStore) ListByInstance(ctx context.Context, instanceID int, limit int) ([]*AutomationActivity, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, instance_id, hash, torrent_name, tracker_domain, action, rule_id, rule_name, outcome, reason, details, created_at
		FROM automation_activity
		WHERE instance_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, instanceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var activities []*AutomationActivity
	for rows.Next() {
		var a AutomationActivity
		var torrentName, trackerDomain, ruleName, reason, details sql.NullString
		var ruleID sql.NullInt64

		if err := rows.Scan(
			&a.ID,
			&a.InstanceID,
			&a.Hash,
			&torrentName,
			&trackerDomain,
			&a.Action,
			&ruleID,
			&ruleName,
			&a.Outcome,
			&reason,
			&details,
			&a.CreatedAt,
		); err != nil {
			return nil, err
		}

		if torrentName.Valid {
			a.TorrentName = torrentName.String
		}
		if trackerDomain.Valid {
			a.TrackerDomain = trackerDomain.String
		}
		if ruleID.Valid {
			id := int(ruleID.Int64)
			a.RuleID = &id
		}
		if ruleName.Valid {
			a.RuleName = ruleName.String
		}
		if reason.Valid {
			a.Reason = reason.String
		}
		if details.Valid && details.String != "" {
			a.Details = json.RawMessage(details.String)
		}

		activities = append(activities, &a)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return activities, nil
}

func (s *AutomationActivityStore) DeleteOlderThan(ctx context.Context, instanceID int, days int) (int64, error) {
	// days == 0 means delete ALL activity for this instance
	if days == 0 {
		res, err := s.db.ExecContext(ctx, `DELETE FROM automation_activity WHERE instance_id = ?`, instanceID)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}

	if days < 0 {
		days = 7
	}

	res, err := s.db.ExecContext(ctx, `
		DELETE FROM automation_activity
		WHERE instance_id = ? AND created_at < datetime('now', '-' || ? || ' days')
	`, instanceID, days)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

func (s *AutomationActivityStore) Prune(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		retentionDays = 7
	}

	res, err := s.db.ExecContext(ctx, `
		DELETE FROM automation_activity
		WHERE created_at < datetime('now', '-' || ? || ' days')
	`, retentionDays)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}
