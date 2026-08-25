// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

type TrackerCustomization struct {
	ID              int       `json:"id"`
	DisplayName     string    `json:"displayName"`
	Domains         []string  `json:"domains"`
	IncludedInStats []string  `json:"includedInStats,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type TrackerCustomizationStore struct {
	db dbinterface.Querier
}

func NewTrackerCustomizationStore(db dbinterface.Querier) *TrackerCustomizationStore {
	return &TrackerCustomizationStore{db: db}
}

func (s *TrackerCustomizationStore) List(ctx context.Context) ([]*TrackerCustomization, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, display_name, domains, included_in_stats, created_at, updated_at
		FROM tracker_customizations
		ORDER BY display_name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var customizations []*TrackerCustomization
	for rows.Next() {
		var c TrackerCustomization
		var domainsStr string
		var includedStr string

		if err := rows.Scan(&c.ID, &c.DisplayName, &domainsStr, &includedStr, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}

		c.Domains = splitDomains(domainsStr)
		c.IncludedInStats = splitDomains(includedStr)
		customizations = append(customizations, &c)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return customizations, nil
}

func (s *TrackerCustomizationStore) Get(ctx context.Context, id int) (*TrackerCustomization, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, display_name, domains, included_in_stats, created_at, updated_at
		FROM tracker_customizations
		WHERE id = ?
	`, id)

	var c TrackerCustomization
	var domainsStr string
	var includedStr string

	if err := row.Scan(&c.ID, &c.DisplayName, &domainsStr, &includedStr, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}

	c.Domains = splitDomains(domainsStr)
	c.IncludedInStats = splitDomains(includedStr)
	return &c, nil
}

func (s *TrackerCustomizationStore) Create(ctx context.Context, c *TrackerCustomization) (*TrackerCustomization, error) {
	if c == nil {
		return nil, errors.New("customization is nil")
	}

	domainsStr := joinDomains(c.Domains)
	includedStr := joinDomains(c.IncludedInStats)

	var id int
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO tracker_customizations (display_name, domains, included_in_stats)
		VALUES (?, ?, ?)
		RETURNING id
	`, c.DisplayName, domainsStr, includedStr).Scan(&id)
	if err != nil {
		return nil, err
	}

	return s.Get(ctx, id)
}

func (s *TrackerCustomizationStore) Update(ctx context.Context, c *TrackerCustomization) (*TrackerCustomization, error) {
	if c == nil {
		return nil, errors.New("customization is nil")
	}

	domainsStr := joinDomains(c.Domains)
	includedStr := joinDomains(c.IncludedInStats)

	res, err := s.db.ExecContext(ctx, `
		UPDATE tracker_customizations
		SET display_name = ?, domains = ?, included_in_stats = ?
		WHERE id = ?
	`, c.DisplayName, domainsStr, includedStr, c.ID)
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

	return s.Get(ctx, c.ID)
}

func (s *TrackerCustomizationStore) Delete(ctx context.Context, id int) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tracker_customizations WHERE id = ?`, id)
	if err != nil {
		return err
	}

	if rows, err := res.RowsAffected(); err == nil && rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func splitDomains(domainsStr string) []string {
	if domainsStr == "" {
		return nil
	}

	parts := strings.Split(domainsStr, ",")
	var domains []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			domains = append(domains, trimmed)
		}
	}
	return domains
}

func joinDomains(domains []string) string {
	var cleaned []string
	seen := make(map[string]struct{})
	for _, d := range domains {
		trimmed := strings.TrimSpace(d)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		cleaned = append(cleaned, trimmed)
	}
	return strings.Join(cleaned, ",")
}

// ResolveTrackerDisplayName resolves a tracker domain to its display name using customizations.
// Fallback chain: customization display name → indexerName → domain.
//
// The domain should be extracted from the tracker announce URL (lowercase, no port).
// indexerName is typically from the cross-seed request and serves as a fallback.
func ResolveTrackerDisplayName(domain string, indexerName string, customizations []*TrackerCustomization) string {
	domain = strings.ToLower(strings.TrimSpace(domain))

	// Look for a matching customization
	for _, c := range customizations {
		for _, d := range c.Domains {
			if strings.ToLower(d) == domain {
				return c.DisplayName
			}
		}
	}

	// Fall back to indexer name if provided
	if indexerName != "" {
		return indexerName
	}

	// Fall back to domain itself
	if domain != "" && domain != "unknown" {
		return domain
	}

	return "Unknown"
}
