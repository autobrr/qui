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

// QualityGroupField names the rls.Release fields that can be used to identify "same content".
// These fields are combined to form a group key — torrents with the same group key represent
// the same piece of content and become candidates for quality comparison.
const (
	QualityGroupFieldTitle      = "title"
	QualityGroupFieldSubtitle   = "subtitle"
	QualityGroupFieldYear       = "year"
	QualityGroupFieldMonth      = "month"
	QualityGroupFieldDay        = "day"
	QualityGroupFieldSeries     = "series"
	QualityGroupFieldEpisode    = "episode"
	QualityGroupFieldArtist     = "artist"
	QualityGroupFieldPlatform   = "platform"
	QualityGroupFieldCollection = "collection"
)

// QualityRankField names the rls.Release fields that can be used as quality ranking criteria.
const (
	QualityRankFieldResolution = "resolution"
	QualityRankFieldSource     = "source"
	QualityRankFieldCodec      = "codec"
	QualityRankFieldHDR        = "hdr"
	QualityRankFieldAudio      = "audio"
	QualityRankFieldChannels   = "channels"
	QualityRankFieldContainer  = "container"
	QualityRankFieldOther      = "other"
	QualityRankFieldCut        = "cut"
	QualityRankFieldEdition    = "edition"
	QualityRankFieldLanguage   = "language"
	QualityRankFieldRegion     = "region"
	QualityRankFieldGroup      = "group"
)

// RankingTier defines one priority level in a quality comparison. The slice-valued fields
// (codec, hdr, audio, etc.) are matched against any element in the release's corresponding
// []string slice.
type RankingTier struct {
	// Field is one of the QualityRankField* constants.
	Field string `json:"field"`
	// ValueOrder lists values from best (index 0) to worst. Values absent from this list
	// are treated as the lowest rank (after all explicit values).
	ValueOrder []string `json:"valueOrder"`
}

// QualityProfile defines how to group "same content" torrents and rank them by quality.
type QualityProfile struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// GroupFields are QualityGroupField* constants that form the group key.
	// Torrents with the same group key are treated as the same content.
	GroupFields []string `json:"groupFields"`
	// RankingTiers are evaluated in order (index 0 is highest priority).
	RankingTiers []RankingTier `json:"rankingTiers"`
	CreatedAt    time.Time     `json:"createdAt"`
	UpdatedAt    time.Time     `json:"updatedAt"`
}

// Validate returns a non-nil error if the profile is missing required data.
func (p *QualityProfile) Validate() error {
	if p == nil {
		return errors.New("quality profile is nil")
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return errors.New("quality profile name is required")
	}
	if len(p.GroupFields) == 0 {
		return errors.New("quality profile must have at least one group field")
	}
	if len(p.RankingTiers) == 0 {
		return errors.New("quality profile must have at least one ranking tier")
	}
	for i, tier := range p.RankingTiers {
		if strings.TrimSpace(tier.Field) == "" {
			return fmt.Errorf("ranking tier %d has no field", i)
		}
		if len(tier.ValueOrder) == 0 {
			return fmt.Errorf("ranking tier %d (%s) has no value order", i, tier.Field)
		}
	}
	return nil
}

// QualityProfileStore handles persistence for quality profiles.
type QualityProfileStore struct {
	db dbinterface.Querier
}

// NewQualityProfileStore returns a new QualityProfileStore backed by db.
func NewQualityProfileStore(db dbinterface.Querier) *QualityProfileStore {
	return &QualityProfileStore{db: db}
}

func scanQualityProfile(dest *QualityProfile, groupFieldsJSON, rankingTiersJSON string) error {
	if err := json.Unmarshal([]byte(groupFieldsJSON), &dest.GroupFields); err != nil {
		return fmt.Errorf("unmarshal group_fields: %w", err)
	}
	if err := json.Unmarshal([]byte(rankingTiersJSON), &dest.RankingTiers); err != nil {
		return fmt.Errorf("unmarshal ranking_tiers: %w", err)
	}
	if dest.GroupFields == nil {
		dest.GroupFields = []string{}
	}
	if dest.RankingTiers == nil {
		dest.RankingTiers = []RankingTier{}
	}
	return nil
}

// List returns all quality profiles ordered by name.
func (s *QualityProfileStore) List(ctx context.Context) ([]*QualityProfile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, description, group_fields, ranking_tiers, created_at, updated_at
		FROM quality_profiles
		ORDER BY name ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var profiles []*QualityProfile
	for rows.Next() {
		var p QualityProfile
		var groupFieldsJSON, rankingTiersJSON string
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &groupFieldsJSON, &rankingTiersJSON, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		if err := scanQualityProfile(&p, groupFieldsJSON, rankingTiersJSON); err != nil {
			return nil, fmt.Errorf("quality profile %d: %w", p.ID, err)
		}
		profiles = append(profiles, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return profiles, nil
}

// Get returns the quality profile with the given id, or sql.ErrNoRows if not found.
func (s *QualityProfileStore) Get(ctx context.Context, id int) (*QualityProfile, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, group_fields, ranking_tiers, created_at, updated_at
		FROM quality_profiles
		WHERE id = ?
	`, id)

	var p QualityProfile
	var groupFieldsJSON, rankingTiersJSON string
	if err := row.Scan(&p.ID, &p.Name, &p.Description, &groupFieldsJSON, &rankingTiersJSON, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	if err := scanQualityProfile(&p, groupFieldsJSON, rankingTiersJSON); err != nil {
		return nil, fmt.Errorf("quality profile %d: %w", p.ID, err)
	}
	return &p, nil
}

// Create inserts a new quality profile and returns it with the generated ID.
func (s *QualityProfileStore) Create(ctx context.Context, p *QualityProfile) (*QualityProfile, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}

	groupFieldsJSON, err := json.Marshal(p.GroupFields)
	if err != nil {
		return nil, fmt.Errorf("marshal group_fields: %w", err)
	}
	rankingTiersJSON, err := json.Marshal(p.RankingTiers)
	if err != nil {
		return nil, fmt.Errorf("marshal ranking_tiers: %w", err)
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO quality_profiles (name, description, group_fields, ranking_tiers)
		VALUES (?, ?, ?, ?)
	`, strings.TrimSpace(p.Name), strings.TrimSpace(p.Description), string(groupFieldsJSON), string(rankingTiersJSON))
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, int(id))
}

// Update replaces the mutable fields of an existing quality profile.
func (s *QualityProfileStore) Update(ctx context.Context, p *QualityProfile) (*QualityProfile, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}

	groupFieldsJSON, err := json.Marshal(p.GroupFields)
	if err != nil {
		return nil, fmt.Errorf("marshal group_fields: %w", err)
	}
	rankingTiersJSON, err := json.Marshal(p.RankingTiers)
	if err != nil {
		return nil, fmt.Errorf("marshal ranking_tiers: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE quality_profiles
		SET name = ?, description = ?, group_fields = ?, ranking_tiers = ?
		WHERE id = ?
	`, strings.TrimSpace(p.Name), strings.TrimSpace(p.Description), string(groupFieldsJSON), string(rankingTiersJSON), p.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("quality profile %d not found", p.ID)
		}
		return nil, err
	}
	return s.Get(ctx, p.ID)
}

// Delete removes the quality profile with the given id.
func (s *QualityProfileStore) Delete(ctx context.Context, id int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM quality_profiles WHERE id = ?`, id)
	return err
}
