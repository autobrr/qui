// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

var ErrNotificationTargetNotFound = errors.New("notification target not found")

// NotificationTarget represents a configured notification destination.
type NotificationTarget struct {
	ID         int       `json:"id"`
	Name       string    `json:"name"`
	URL        string    `json:"url"`
	Enabled    bool      `json:"enabled"`
	EventTypes []string  `json:"eventTypes"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// NotificationTargetCreate represents data needed to create a notification target.
type NotificationTargetCreate struct {
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	Enabled    bool     `json:"enabled"`
	EventTypes []string `json:"eventTypes"`
}

// NotificationTargetUpdate represents data needed to update a notification target.
type NotificationTargetUpdate struct {
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	Enabled    bool     `json:"enabled"`
	EventTypes []string `json:"eventTypes"`
}

// NotificationTargetStore manages persistence for notification targets.
type NotificationTargetStore struct {
	db dbinterface.Querier
}

func NewNotificationTargetStore(db dbinterface.Querier) *NotificationTargetStore {
	return &NotificationTargetStore{db: db}
}

func (s *NotificationTargetStore) List(ctx context.Context) ([]*NotificationTarget, error) {
	query := `
		SELECT id, name, url, enabled, event_types, created_at, updated_at
		FROM notification_targets
		ORDER BY name ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query notification targets: %w", err)
	}
	defer rows.Close()

	var targets []*NotificationTarget
	for rows.Next() {
		var target NotificationTarget
		var enabled int
		var eventTypesJSON string
		if err := rows.Scan(
			&target.ID,
			&target.Name,
			&target.URL,
			&enabled,
			&eventTypesJSON,
			&target.CreatedAt,
			&target.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan notification target: %w", err)
		}
		target.Enabled = enabled == 1
		if err := unmarshalEventTypes(eventTypesJSON, &target.EventTypes); err != nil {
			return nil, err
		}
		targets = append(targets, &target)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notification targets: %w", err)
	}

	return targets, nil
}

func (s *NotificationTargetStore) ListEnabled(ctx context.Context) ([]*NotificationTarget, error) {
	query := `
		SELECT id, name, url, enabled, event_types, created_at, updated_at
		FROM notification_targets
		WHERE enabled = 1
		ORDER BY name ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query enabled notification targets: %w", err)
	}
	defer rows.Close()

	var targets []*NotificationTarget
	for rows.Next() {
		var target NotificationTarget
		var enabled int
		var eventTypesJSON string
		if err := rows.Scan(
			&target.ID,
			&target.Name,
			&target.URL,
			&enabled,
			&eventTypesJSON,
			&target.CreatedAt,
			&target.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan enabled notification target: %w", err)
		}
		target.Enabled = enabled == 1
		if err := unmarshalEventTypes(eventTypesJSON, &target.EventTypes); err != nil {
			return nil, err
		}
		targets = append(targets, &target)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate enabled notification targets: %w", err)
	}

	return targets, nil
}

func (s *NotificationTargetStore) GetByID(ctx context.Context, id int) (*NotificationTarget, error) {
	query := `
		SELECT id, name, url, enabled, event_types, created_at, updated_at
		FROM notification_targets
		WHERE id = ?
	`

	row := s.db.QueryRowContext(ctx, query, id)
	return scanNotificationTarget(row)
}

func (s *NotificationTargetStore) Create(ctx context.Context, create *NotificationTargetCreate) (*NotificationTarget, error) {
	if create == nil {
		return nil, errors.New("create payload required")
	}

	eventTypesJSON, err := json.Marshal(create.EventTypes)
	if err != nil {
		return nil, fmt.Errorf("marshal event types: %w", err)
	}

	query := `
		INSERT INTO notification_targets (name, url, enabled, event_types, created_at, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING id, name, url, enabled, event_types, created_at, updated_at
	`

	enabledInt := 0
	if create.Enabled {
		enabledInt = 1
	}

	row := s.db.QueryRowContext(ctx, query, strings.TrimSpace(create.Name), strings.TrimSpace(create.URL), enabledInt, string(eventTypesJSON))
	return scanNotificationTarget(row)
}

func (s *NotificationTargetStore) Update(ctx context.Context, id int, update *NotificationTargetUpdate) (*NotificationTarget, error) {
	if update == nil {
		return nil, errors.New("update payload required")
	}

	eventTypesJSON, err := json.Marshal(update.EventTypes)
	if err != nil {
		return nil, fmt.Errorf("marshal event types: %w", err)
	}

	query := `
		UPDATE notification_targets
		SET name = ?, url = ?, enabled = ?, event_types = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`

	enabledInt := 0
	if update.Enabled {
		enabledInt = 1
	}

	result, err := s.db.ExecContext(ctx, query, strings.TrimSpace(update.Name), strings.TrimSpace(update.URL), enabledInt, string(eventTypesJSON), id)
	if err != nil {
		return nil, fmt.Errorf("update notification target: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return nil, ErrNotificationTargetNotFound
	}

	return s.GetByID(ctx, id)
}

func (s *NotificationTargetStore) Delete(ctx context.Context, id int) error {
	query := `DELETE FROM notification_targets WHERE id = ?`

	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete notification target: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotificationTargetNotFound
	}

	return nil
}

func scanNotificationTarget(scanner interface{ Scan(dest ...any) error }) (*NotificationTarget, error) {
	var target NotificationTarget
	var enabled int
	var eventTypesJSON string

	if err := scanner.Scan(
		&target.ID,
		&target.Name,
		&target.URL,
		&enabled,
		&eventTypesJSON,
		&target.CreatedAt,
		&target.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotificationTargetNotFound
		}
		return nil, fmt.Errorf("scan notification target: %w", err)
	}

	target.Enabled = enabled == 1
	if err := unmarshalEventTypes(eventTypesJSON, &target.EventTypes); err != nil {
		return nil, err
	}

	return &target, nil
}

func unmarshalEventTypes(raw string, dest *[]string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "[]" {
		*dest = nil
		return nil
	}

	if err := json.Unmarshal([]byte(trimmed), dest); err != nil {
		return fmt.Errorf("unmarshal event types: %w", err)
	}

	return nil
}
