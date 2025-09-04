// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Error types for categorization
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
	db *sql.DB
}

func NewInstanceErrorStore(db *sql.DB) *InstanceErrorStore {
	return &InstanceErrorStore{
		db: db,
	}
}

// isContextError checks if an error is a standard context error that should be ignored
func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// RecordError stores an error for an instance with simple deduplication
func (s *InstanceErrorStore) RecordError(ctx context.Context, instanceID int, err error) error {
	// Skip context cancellation/timeout errors - these are expected operational conditions
	if isContextError(err) {
		return nil
	}

	errorType := categorizeError(err)
	errorMessage := err.Error()

	// Simple deduplication: check if same error was recorded in last minute
	var count int
	checkQuery := `SELECT COUNT(*) FROM instance_errors 
                   WHERE instance_id = ? AND error_type = ? AND error_message = ? 
                   AND occurred_at > datetime('now', '-1 minute')`

	if err := s.db.QueryRowContext(ctx, checkQuery, instanceID, errorType, errorMessage).Scan(&count); err == nil && count > 0 {
		return nil // Skip duplicate
	}

	// Insert the error (trigger will handle cleanup of old errors)
	query := `INSERT INTO instance_errors (instance_id, error_type, error_message) 
              VALUES (?, ?, ?)`
	_, execErr := s.db.ExecContext(ctx, query, instanceID, errorType, errorMessage)
	return execErr
}

// GetRecentErrors retrieves the last N errors for an instance
func (s *InstanceErrorStore) GetRecentErrors(ctx context.Context, instanceID int, limit int) ([]InstanceError, error) {
	query := `SELECT id, instance_id, error_type, error_message, occurred_at 
              FROM instance_errors 
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
	return errors, rows.Err()
}

// ClearErrors removes all errors for an instance (called on successful connection)
func (s *InstanceErrorStore) ClearErrors(ctx context.Context, instanceID int) error {
	query := `DELETE FROM instance_errors WHERE instance_id = ?`
	_, err := s.db.ExecContext(ctx, query, instanceID)
	return err
}

// categorizeError determines error type based on error message patterns
func categorizeError(err error) string {
	if err == nil {
		return ErrorTypeAPI
	}

	errorStr := strings.ToLower(err.Error())

	// Check for ban-related errors
	if strings.Contains(errorStr, "ip is banned") ||
		strings.Contains(errorStr, "too many failed login attempts") ||
		strings.Contains(errorStr, "banned") ||
		strings.Contains(errorStr, "rate limit") ||
		strings.Contains(errorStr, "403") ||
		strings.Contains(errorStr, "forbidden") {
		return ErrorTypeBan
	}

	// Check for authentication errors
	if strings.Contains(errorStr, "unauthorized") ||
		strings.Contains(errorStr, "401") ||
		strings.Contains(errorStr, "login") ||
		strings.Contains(errorStr, "authentication") ||
		strings.Contains(errorStr, "credential") {
		return ErrorTypeAuthentication
	}

	// Check for connection errors
	if strings.Contains(errorStr, "connection refused") ||
		strings.Contains(errorStr, "no such host") ||
		strings.Contains(errorStr, "network") ||
		strings.Contains(errorStr, "dial") ||
		strings.Contains(errorStr, "connect") {
		return ErrorTypeConnection
	}

	// Default to API error for everything else
	return ErrorTypeAPI
}
