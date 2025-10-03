// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrWebhookNotFound = errors.New("webhook not found")
var ErrWebhookAlreadyExists = errors.New("webhook already exists for this instance")

type Webhook struct {
	ID                           int       `json:"id"`
	InstanceID                   int       `json:"instanceId"`
	APIKeyID                     int       `json:"apiKeyId"`
	Enabled                      bool      `json:"enabled"`
	AutorunEnabled               bool      `json:"autorunEnabled"`
	AutorunOnTorrentAddedEnabled bool      `json:"autorunOnTorrentAddedEnabled"`
	QuiURL                       string    `json:"quiUrl"`
	CreatedAt                    time.Time `json:"createdAt"`
	UpdatedAt                    time.Time `json:"updatedAt"`
}

type WebhookStore struct {
	db *sql.DB
}

func NewWebhookStore(db *sql.DB) *WebhookStore {
	return &WebhookStore{db: db}
}

// Create creates a new webhook configuration
func (s *WebhookStore) Create(ctx context.Context, instanceID int, apiKeyID int, enabled bool, autorunEnabled bool, autorunOnTorrentAddedEnabled bool, quiURL string) (*Webhook, error) {
	query := `
		INSERT INTO webhooks (instance_id, api_key_id, enabled, autorun_enabled, autorun_on_torrent_added_enabled, qui_url) 
		VALUES (?, ?, ?, ?, ?, ?)
		RETURNING id, instance_id, api_key_id, enabled, autorun_enabled, autorun_on_torrent_added_enabled, qui_url, created_at, updated_at
	`

	webhook := &Webhook{}
	err := s.db.QueryRowContext(ctx, query, instanceID, apiKeyID, enabled, autorunEnabled, autorunOnTorrentAddedEnabled, quiURL).Scan(
		&webhook.ID,
		&webhook.InstanceID,
		&webhook.APIKeyID,
		&webhook.Enabled,
		&webhook.AutorunEnabled,
		&webhook.AutorunOnTorrentAddedEnabled,
		&webhook.QuiURL,
		&webhook.CreatedAt,
		&webhook.UpdatedAt,
	)

	if err != nil {
		// Check if it's a unique constraint violation
		if err.Error() == "UNIQUE constraint failed: webhooks.instance_id" {
			return nil, ErrWebhookAlreadyExists
		}
		return nil, err
	}

	return webhook, nil
}

// GetByInstanceID gets a webhook configuration by instance ID
func (s *WebhookStore) GetByInstanceID(ctx context.Context, instanceID int) (*Webhook, error) {
	query := `
		SELECT id, instance_id, api_key_id, enabled, autorun_enabled, autorun_on_torrent_added_enabled, qui_url, created_at, updated_at 
		FROM webhooks 
		WHERE instance_id = ?
	`

	webhook := &Webhook{}
	err := s.db.QueryRowContext(ctx, query, instanceID).Scan(
		&webhook.ID,
		&webhook.InstanceID,
		&webhook.APIKeyID,
		&webhook.Enabled,
		&webhook.AutorunEnabled,
		&webhook.AutorunOnTorrentAddedEnabled,
		&webhook.QuiURL,
		&webhook.CreatedAt,
		&webhook.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWebhookNotFound
		}
		return nil, err
	}

	return webhook, nil
}

// GetByAPIKeyID gets webhooks by API key ID
func (s *WebhookStore) GetByAPIKeyID(ctx context.Context, apiKeyID int) ([]*Webhook, error) {
	query := `
		SELECT id, instance_id, api_key_id, enabled, autorun_enabled, autorun_on_torrent_added_enabled, qui_url, created_at, updated_at 
		FROM webhooks 
		WHERE api_key_id = ?
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, apiKeyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	webhooks := make([]*Webhook, 0)
	for rows.Next() {
		webhook := &Webhook{}
		err := rows.Scan(
			&webhook.ID,
			&webhook.InstanceID,
			&webhook.APIKeyID,
			&webhook.Enabled,
			&webhook.AutorunEnabled,
			&webhook.AutorunOnTorrentAddedEnabled,
			&webhook.QuiURL,
			&webhook.CreatedAt,
			&webhook.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		webhooks = append(webhooks, webhook)
	}

	return webhooks, rows.Err()
}

// List returns all webhook configurations
func (s *WebhookStore) List(ctx context.Context) ([]*Webhook, error) {
	query := `
		SELECT id, instance_id, api_key_id, enabled, autorun_enabled, autorun_on_torrent_added_enabled, qui_url, created_at, updated_at 
		FROM webhooks 
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	webhooks := make([]*Webhook, 0)
	for rows.Next() {
		webhook := &Webhook{}
		err := rows.Scan(
			&webhook.ID,
			&webhook.InstanceID,
			&webhook.APIKeyID,
			&webhook.Enabled,
			&webhook.AutorunEnabled,
			&webhook.AutorunOnTorrentAddedEnabled,
			&webhook.QuiURL,
			&webhook.CreatedAt,
			&webhook.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		webhooks = append(webhooks, webhook)
	}

	return webhooks, rows.Err()
}

// Update updates a webhook configuration
func (s *WebhookStore) Update(ctx context.Context, instanceID int, apiKeyID int, enabled bool, autorunEnabled bool, autorunOnTorrentAddedEnabled bool, quiURL string) (*Webhook, error) {
	query := `
		UPDATE webhooks 
		SET api_key_id = ?, enabled = ?, autorun_enabled = ?, autorun_on_torrent_added_enabled = ?, qui_url = ?, updated_at = CURRENT_TIMESTAMP 
		WHERE instance_id = ?
		RETURNING id, instance_id, api_key_id, enabled, autorun_enabled, autorun_on_torrent_added_enabled, qui_url, created_at, updated_at
	`

	webhook := &Webhook{}
	err := s.db.QueryRowContext(ctx, query, apiKeyID, enabled, autorunEnabled, autorunOnTorrentAddedEnabled, quiURL, instanceID).Scan(
		&webhook.ID,
		&webhook.InstanceID,
		&webhook.APIKeyID,
		&webhook.Enabled,
		&webhook.AutorunEnabled,
		&webhook.AutorunOnTorrentAddedEnabled,
		&webhook.QuiURL,
		&webhook.CreatedAt,
		&webhook.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWebhookNotFound
		}
		return nil, err
	}

	return webhook, nil
}

// Upsert inserts a new webhook or updates an existing one
func (s *WebhookStore) Upsert(ctx context.Context, instanceID int, apiKeyID int, enabled bool, autorunEnabled bool, autorunOnTorrentAddedEnabled bool, quiURL string) (*Webhook, error) {
	query := `
		INSERT INTO webhooks (instance_id, api_key_id, enabled, autorun_enabled, autorun_on_torrent_added_enabled, qui_url) 
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(instance_id) DO UPDATE SET
			api_key_id = excluded.api_key_id,
			enabled = excluded.enabled,
			autorun_enabled = excluded.autorun_enabled,
			autorun_on_torrent_added_enabled = excluded.autorun_on_torrent_added_enabled,
			qui_url = excluded.qui_url,
			updated_at = CURRENT_TIMESTAMP
		RETURNING id, instance_id, api_key_id, enabled, autorun_enabled, autorun_on_torrent_added_enabled, qui_url, created_at, updated_at
	`

	webhook := &Webhook{}
	err := s.db.QueryRowContext(ctx, query, instanceID, apiKeyID, enabled, autorunEnabled, autorunOnTorrentAddedEnabled, quiURL).Scan(
		&webhook.ID,
		&webhook.InstanceID,
		&webhook.APIKeyID,
		&webhook.Enabled,
		&webhook.AutorunEnabled,
		&webhook.AutorunOnTorrentAddedEnabled,
		&webhook.QuiURL,
		&webhook.CreatedAt,
		&webhook.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return webhook, nil
}

// Delete deletes a webhook configuration
func (s *WebhookStore) Delete(ctx context.Context, instanceID int) error {
	query := `DELETE FROM webhooks WHERE instance_id = ?`

	result, err := s.db.ExecContext(ctx, query, instanceID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return ErrWebhookNotFound
	}

	return nil
}
