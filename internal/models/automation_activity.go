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

type ActivityAction string

const (
	ActivityActionDeletedRatio        ActivityAction = "deleted_ratio"
	ActivityActionDeletedSeeding      ActivityAction = "deleted_seeding"
	ActivityActionDeletedUnregistered ActivityAction = "deleted_unregistered"
	ActivityActionDeletedCondition    ActivityAction = "deleted_condition"
	ActivityActionDeleteFailed        ActivityAction = "delete_failed"
	ActivityActionLimitFailed         ActivityAction = "limit_failed"
	ActivityActionTagsChanged         ActivityAction = "tags_changed"
	ActivityActionCategoryChanged     ActivityAction = "category_changed"
	ActivityActionSpeedLimitsChanged  ActivityAction = "speed_limits_changed"
	ActivityActionShareLimitsChanged  ActivityAction = "share_limits_changed"
	ActivityActionPaused              ActivityAction = "paused"
	ActivityActionResumed             ActivityAction = "resumed"
	ActivityActionRechecked           ActivityAction = "rechecked"
	ActivityActionReannounced         ActivityAction = "reannounced"
	ActivityActionMoved               ActivityAction = "moved"
	ActivityActionAutoManaged         ActivityAction = "auto_managed"
	ActivityActionExternalProgram     ActivityAction = "external_program"
	ActivityActionExportedToInstance  ActivityAction = "exported_to_instance"
	ActivityActionDryRunNoMatch       ActivityAction = "dry_run_no_match"
	ActivityActionRuleSkipped         ActivityAction = "rule_skipped"
)

const ActivityActionTransferredToInstance ActivityAction = "transferred_to_instance"

const ActivityActionDeleteWaiting ActivityAction = "delete_waiting"

const (
	ActivityActionUnregisteredWaitStarted ActivityAction = "unregistered_wait_started"
	ActivityActionUnregisteredWaitEnded   ActivityAction = "unregistered_wait_ended"
)

type ActivityOutcome string

const (
	ActivityOutcomeSuccess ActivityOutcome = "success"
	ActivityOutcomeFailed  ActivityOutcome = "failed"
	ActivityOutcomeDryRun  ActivityOutcome = "dry-run"
	ActivityOutcomeSkipped ActivityOutcome = "skipped"
)

type AutomationActivity struct {
	ID            int             `json:"id"`
	InstanceID    int             `json:"instanceId"`
	Hash          string          `json:"hash"`
	TorrentName   string          `json:"torrentName,omitempty"`
	TrackerDomain string          `json:"trackerDomain,omitempty"`
	Action        ActivityAction  `json:"action"`
	RuleID        *int            `json:"ruleId,omitempty"`
	RuleName      string          `json:"ruleName,omitempty"`
	Outcome       ActivityOutcome `json:"outcome"`
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

	id, err := s.insert(ctx, activity)
	activity.ID = id
	return err
}

func (s *AutomationActivityStore) CreateWithID(ctx context.Context, activity *AutomationActivity) (int, error) {
	if activity == nil {
		return 0, nil
	}

	id, err := s.insert(ctx, activity)
	if err != nil {
		return 0, err
	}

	activity.ID = id
	return id, nil
}

func (s *AutomationActivityStore) insert(ctx context.Context, activity *AutomationActivity) (int, error) {
	if s == nil || s.db == nil || activity == nil {
		return 0, nil
	}
	return insertActivity(ctx, s.db, activity)
}

type activityWriter interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	dbinterface.DialectProvider
}

func insertActivity(ctx context.Context, db activityWriter, activity *AutomationActivity) (int, error) {
	var detailsStr sql.NullString
	if len(activity.Details) > 0 {
		detailsStr = sql.NullString{String: string(activity.Details), Valid: true}
	}

	var ruleID sql.NullInt64
	if activity.RuleID != nil {
		ruleID = sql.NullInt64{Int64: int64(*activity.RuleID), Valid: true}
	}

	if dbinterface.DialectOf(db) != "postgres" {
		res, err := db.ExecContext(ctx, `
		INSERT INTO automation_activity
			(instance_id, hash, torrent_name, tracker_domain, action, rule_id, rule_name, outcome, reason, details)
		VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, activity.InstanceID, activity.Hash, activity.TorrentName, activity.TrackerDomain, string(activity.Action),
			ruleID, activity.RuleName, string(activity.Outcome), activity.Reason, detailsStr)
		if err != nil {
			return 0, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return 0, err
		}
		return int(id), nil
	}

	var id int
	err := db.QueryRowContext(ctx, `
		INSERT INTO automation_activity
			(instance_id, hash, torrent_name, tracker_domain, action, rule_id, rule_name, outcome, reason, details)
		VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`, activity.InstanceID, activity.Hash, activity.TorrentName, activity.TrackerDomain, string(activity.Action),
		ruleID, activity.RuleName, string(activity.Outcome), activity.Reason, detailsStr).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
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

	cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM automation_activity
		WHERE instance_id = ? AND created_at < ?
	`, instanceID, cutoff)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

func (s *AutomationActivityStore) Prune(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		retentionDays = 7
	}

	cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM automation_activity
		WHERE created_at < ?
	`, cutoff)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}
