// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

const (
	pendingDeletePutChunk    = 200
	pendingDeleteRemoveChunk = 500
)

type PendingDelete struct {
	RuleID            int
	Hash              string
	UnregisteredSince time.Time
}

type PendingDeleteChange struct {
	Put        []PendingDelete
	Remove     []PendingDelete
	Activities []*AutomationActivity
}

var ErrPendingDeleteRuleChanged = errors.New("a rule of the pending deletes no longer keeps them")

func (c PendingDeleteChange) IsEmpty() bool {
	return len(c.Put) == 0 && len(c.Remove) == 0
}

type PendingDeleteStore struct {
	db dbinterface.Querier
}

func NewPendingDeleteStore(db dbinterface.Querier) *PendingDeleteStore {
	return &PendingDeleteStore{db: db}
}

func (s *PendingDeleteStore) ListPendingDeletes(ctx context.Context, instanceID int) ([]PendingDelete, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT rule_id, torrent_hash, unregistered_since
		FROM automation_pending_deletes
		WHERE instance_id = ?
	`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("list pending deletes: %w", err)
	}
	defer rows.Close()

	var pending []PendingDelete
	for rows.Next() {
		var row PendingDelete
		if err := rows.Scan(&row.RuleID, &row.Hash, &row.UnregisteredSince); err != nil {
			return nil, fmt.Errorf("scan pending delete: %w", err)
		}
		row.UnregisteredSince = row.UnregisteredSince.UTC()
		pending = append(pending, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list pending deletes: %w", err)
	}
	return pending, nil
}

func (s *PendingDeleteStore) ApplyPendingDeletes(ctx context.Context, instanceID int, change PendingDeleteChange) error {
	if change.IsEmpty() {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := requireRulesKeepPendingDeletes(ctx, tx, change); err != nil {
		return err
	}
	if err := putPendingDeletes(ctx, tx, instanceID, change.Put); err != nil {
		return err
	}
	if err := removePendingDeletes(ctx, tx, change.Remove); err != nil {
		return err
	}
	ids := make([]int, len(change.Activities))
	for i, activity := range change.Activities {
		if ids[i], err = insertActivity(ctx, tx, activity); err != nil {
			return fmt.Errorf("record activity: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	for i, activity := range change.Activities {
		activity.ID = ids[i]
	}
	return nil
}

func requireRulesKeepPendingDeletes(ctx context.Context, tx dbinterface.TxQuerier, change PendingDeleteChange) error {
	ruleIDs := make(map[int]struct{})
	for _, row := range slices.Concat(change.Put, change.Remove) {
		ruleIDs[row.RuleID] = struct{}{}
	}
	args := make([]any, 0, len(ruleIDs))
	for ruleID := range ruleIDs {
		args = append(args, ruleID)
	}
	rows, err := tx.QueryContext(ctx, `SELECT enabled, dry_run, conditions FROM automations WHERE id IN (`+
		strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")+`)`, args...)
	if err != nil {
		return fmt.Errorf("read the rules of pending deletes: %w", err)
	}
	defer rows.Close()
	keeping := 0
	for rows.Next() {
		var enabled, dryRun int
		var conditionsJSON string
		if err := rows.Scan(&enabled, &dryRun, &conditionsJSON); err != nil {
			return fmt.Errorf("scan the rule of a pending delete: %w", err)
		}
		var conditions ActionConditions
		if err := json.Unmarshal([]byte(conditionsJSON), &conditions); err != nil {
			return fmt.Errorf("read the conditions of the rule of a pending delete: %w", err)
		}
		rule := Automation{Enabled: SQLiteIntToBool(enabled), DryRun: SQLiteIntToBool(dryRun), Conditions: &conditions}
		if rule.KeepsPendingDeletes() {
			keeping++
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read the rules of pending deletes: %w", err)
	}
	if keeping != len(ruleIDs) {
		return ErrPendingDeleteRuleChanged
	}
	return nil
}

func putPendingDeletes(ctx context.Context, tx dbinterface.TxQuerier, instanceID int, rows []PendingDelete) error {
	for chunk := range slices.Chunk(rows, pendingDeletePutChunk) {
		query := dbinterface.BuildQueryWithPlaceholders(`
			INSERT INTO automation_pending_deletes (instance_id, rule_id, torrent_hash, unregistered_since)
			VALUES %s
			ON CONFLICT(rule_id, torrent_hash) DO UPDATE SET unregistered_since = excluded.unregistered_since
		`, 4, len(chunk))
		args := make([]any, 0, 4*len(chunk))
		for _, row := range chunk {
			args = append(args, instanceID, row.RuleID, row.Hash, row.UnregisteredSince.UTC())
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("store pending deletes: %w", err)
		}
	}
	return nil
}

func removePendingDeletes(ctx context.Context, tx dbinterface.TxQuerier, rows []PendingDelete) error {
	byRule := make(map[int][]string)
	for _, row := range rows {
		byRule[row.RuleID] = append(byRule[row.RuleID], row.Hash)
	}
	for ruleID, hashes := range byRule {
		for chunk := range slices.Chunk(hashes, pendingDeleteRemoveChunk) {
			query := `DELETE FROM automation_pending_deletes WHERE rule_id = ? AND torrent_hash IN (` +
				strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",") + `)`
			args := make([]any, 0, 1+len(chunk))
			args = append(args, ruleID)
			for _, hash := range chunk {
				args = append(args, hash)
			}
			if _, err := tx.ExecContext(ctx, query, args...); err != nil {
				return fmt.Errorf("remove pending deletes: %w", err)
			}
		}
	}
	return nil
}

func removeRulePendingDeletes(ctx context.Context, tx dbinterface.TxQuerier, ruleID int) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM automation_pending_deletes WHERE rule_id = ?`, ruleID); err != nil {
		return fmt.Errorf("remove pending deletes: %w", err)
	}
	return nil
}

func (a *Automation) KeepsPendingDeletes() bool {
	return a.Enabled && !a.DryRun && a.Conditions != nil && a.Conditions.Delete != nil && a.Conditions.Delete.Enabled
}
