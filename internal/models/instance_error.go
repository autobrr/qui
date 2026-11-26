// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

const (
	ErrorTypeConnection     = "connection"
	ErrorTypeAuthentication = "authentication"
	ErrorTypeBan            = "ban"
	ErrorTypeAPI            = "api"
)

type InstanceError struct {
	ID           int       `json:"id"`
	InstanceID   int       `json:"instanceId"`
	ErrorType    string    `json:"errorType"`
	ErrorMessage string    `json:"errorMessage"`
	OccurredAt   time.Time `json:"occurredAt"`
}

type InstanceErrorStore struct {
	db dbinterface.Querier
}

func NewInstanceErrorStore(db dbinterface.Querier) *InstanceErrorStore {
	return &InstanceErrorStore{db: db}
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (s *InstanceErrorStore) RecordError(ctx context.Context, instanceID int, err error) error {
	if isContextError(err) {
		return nil
	}

	errorType := categorizeError(err)
	errorMessage := err.Error()

	tx, txErr := s.db.BeginTx(ctx, nil)
	if txErr != nil {
		return fmt.Errorf("failed to begin transaction: %w", txErr)
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	existsQuery := `SELECT COUNT(*) FROM instances WHERE id = ?`
	scanErr := tx.QueryRowContext(ctx, existsQuery, instanceID).Scan(&exists)
	if scanErr != nil {
		if scanErr == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("failed to check instance existence: %w", scanErr)
	}
	if exists == 0 {
		return nil
	}

	var count int
	cutoff := time.Now().UTC().Add(-time.Minute)
	checkQuery := `SELECT COUNT(*) FROM instance_errors_view
                   WHERE instance_id = ? AND error_type = ? AND error_message = ?
                   AND occurred_at > ?`

	if err := tx.QueryRowContext(ctx, checkQuery, instanceID, errorType, errorMessage, cutoff).Scan(&count); err == nil && count > 0 {
		return nil
	}

	ids, err := dbinterface.InternStringNullable(ctx, tx, &errorType, &errorMessage)
	if err != nil {
		return fmt.Errorf("failed to intern error strings: %w", err)
	}

	_, execErr := tx.ExecContext(ctx, `INSERT INTO instance_errors (instance_id, error_type_id, error_message_id) VALUES (?, ?, ?)`,
		instanceID, ids[0], ids[1])

	if isForeignKeyConstraintError(execErr) {
		return nil
	}

	if execErr != nil {
		return execErr
	}

	return tx.Commit()
}

func (s *InstanceErrorStore) GetRecentErrors(ctx context.Context, instanceID int, limit int) ([]InstanceError, error) {
	query := `SELECT id, instance_id, error_type, error_message, occurred_at
              FROM instance_errors_view
              WHERE instance_id = ?
              ORDER BY occurred_at DESC
              LIMIT ?`

	rows, err := s.db.QueryContext(ctx, query, instanceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var errors []InstanceError
	for rows.Next() {
		var e InstanceError
		if err := rows.Scan(&e.ID, &e.InstanceID, &e.ErrorType, &e.ErrorMessage, &e.OccurredAt); err != nil {
			return nil, err
		}
		errors = append(errors, e)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return errors, nil
}

func (s *InstanceErrorStore) ClearErrors(ctx context.Context, instanceID int) error {
	query := `DELETE FROM instance_errors WHERE instance_id = ?`
	_, err := s.db.ExecContext(ctx, query, instanceID)
	return err
}

func categorizeError(err error) string {
	if err == nil {
		return ErrorTypeAPI
	}

	errorStr := strings.ToLower(err.Error())

	if strings.Contains(errorStr, "ip is banned") ||
		strings.Contains(errorStr, "too many failed login attempts") ||
		strings.Contains(errorStr, "banned") ||
		strings.Contains(errorStr, "rate limit") ||
		strings.Contains(errorStr, "403") ||
		strings.Contains(errorStr, "forbidden") {
		return ErrorTypeBan
	}

	if strings.Contains(errorStr, "unauthorized") ||
		strings.Contains(errorStr, "401") ||
		strings.Contains(errorStr, "login") ||
		strings.Contains(errorStr, "authentication") ||
		strings.Contains(errorStr, "credential") {
		return ErrorTypeAuthentication
	}

	if strings.Contains(errorStr, "connection refused") ||
		strings.Contains(errorStr, "no such host") ||
		strings.Contains(errorStr, "network") ||
		strings.Contains(errorStr, "dial") ||
		strings.Contains(errorStr, "connect") {
		return ErrorTypeConnection
	}

	return ErrorTypeAPI
}
