// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/dbinterface"
)

const (
	ProblemReportReceived = "received"
	ProblemReportIssue    = "issue"
	ProblemReportClosed   = "closed"
)

type ProblemReportSignIn struct {
	Token    string
	Provider string
	Name     string
}

type SentProblemReport struct {
	BrrbinID        string
	SubmissionID    string
	Title           string
	SentAt          time.Time
	Status          string
	IssueNumber     *int
	IssueURL        *string
	IssueClosed     bool
	CloseReason     *string
	StatusChangedAt time.Time
}

type ProblemReportStore struct {
	db     dbinterface.Querier
	cipher *CredentialCipher
}

func NewProblemReportStore(db dbinterface.Querier, encryptionKey []byte) (*ProblemReportStore, error) {
	credentialCipher, err := NewCredentialCipher(encryptionKey)
	if err != nil {
		return nil, err
	}
	return &ProblemReportStore{db: db, cipher: credentialCipher}, nil
}

func (s *ProblemReportStore) InstallKey(ctx context.Context) (string, error) {
	var encrypted string
	err := s.db.QueryRowContext(ctx, `SELECT install_key_encrypted FROM problem_report_install WHERE id = 1`).Scan(&encrypted)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return s.createInstallKey(ctx)
	case err != nil:
		return "", fmt.Errorf("read install key: %w", err)
	}

	key, err := s.cipher.Decrypt(encrypted, nil)
	if err == nil {
		return key, nil
	}
	log.Warn().Err(err).Msg("Cannot decrypt the Problem report Install key, making a new one")
	key, replacement, err := s.newInstallKey()
	if err != nil {
		return "", err
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE problem_report_install
		SET install_key_encrypted = ?, sign_in_token_encrypted = NULL, sign_in_provider = NULL, sign_in_name = NULL
		WHERE id = 1 AND install_key_encrypted = ?`, replacement, encrypted)
	if err != nil {
		return "", fmt.Errorf("replace install key: %w", err)
	}
	if replaced, err := result.RowsAffected(); err != nil {
		return "", fmt.Errorf("replace install key: %w", err)
	} else if replaced == 0 {
		return s.InstallKey(ctx)
	}
	return key, nil
}

func (s *ProblemReportStore) createInstallKey(ctx context.Context) (string, error) {
	_, encrypted, err := s.newInstallKey()
	if err != nil {
		return "", err
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO problem_report_install (id, install_key_encrypted) VALUES (1, ?)
		ON CONFLICT (id) DO NOTHING`, encrypted); err != nil {
		return "", fmt.Errorf("store install key: %w", err)
	}
	return s.InstallKey(ctx)
}

func (s *ProblemReportStore) newInstallKey() (key, encrypted string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("make install key: %w", err)
	}
	key = base64.RawURLEncoding.EncodeToString(raw)
	encrypted, err = s.cipher.Encrypt(key, nil)
	if err != nil {
		return "", "", fmt.Errorf("encrypt install key: %w", err)
	}
	return key, encrypted, nil
}

func (s *ProblemReportStore) SignIn(ctx context.Context) (*ProblemReportSignIn, error) {
	var token, provider, name sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT sign_in_token_encrypted, sign_in_provider, sign_in_name
		FROM problem_report_install WHERE id = 1`).Scan(&token, &provider, &name)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !token.Valid) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read sign-in: %w", err)
	}

	plain, err := s.cipher.Decrypt(token.String, nil)
	if err != nil {
		log.Warn().Err(err).Msg("Cannot decrypt the brrbin sign-in, dropping it")
		return nil, s.ClearSignIn(ctx)
	}
	return &ProblemReportSignIn{Token: plain, Provider: provider.String, Name: name.String}, nil
}

func (s *ProblemReportStore) SaveSignIn(ctx context.Context, signIn ProblemReportSignIn) error {
	if _, err := s.InstallKey(ctx); err != nil {
		return err
	}
	encrypted, err := s.cipher.Encrypt(signIn.Token, nil)
	if err != nil {
		return fmt.Errorf("encrypt sign-in: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE problem_report_install
		SET sign_in_token_encrypted = ?, sign_in_provider = ?, sign_in_name = ?
		WHERE id = 1`, encrypted, signIn.Provider, signIn.Name); err != nil {
		return fmt.Errorf("store sign-in: %w", err)
	}
	return nil
}

func (s *ProblemReportStore) ClearSignIn(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE problem_report_install
		SET sign_in_token_encrypted = NULL, sign_in_provider = NULL, sign_in_name = NULL
		WHERE id = 1`); err != nil {
		return fmt.Errorf("clear sign-in: %w", err)
	}
	return nil
}

func (s *ProblemReportStore) AddSentReport(ctx context.Context, report SentProblemReport) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO problem_reports (
			brrbin_id, submission_id, title, sent_at, status,
			issue_number, issue_url, issue_closed, close_reason, status_changed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`,
		report.BrrbinID, report.SubmissionID, report.Title, report.SentAt, report.Status,
		report.IssueNumber, report.IssueURL, BoolToSQLite(report.IssueClosed), report.CloseReason, report.StatusChangedAt,
	); err != nil {
		return fmt.Errorf("store sent report: %w", err)
	}
	return nil
}

func (s *ProblemReportStore) SentReports(ctx context.Context) ([]SentProblemReport, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT brrbin_id, submission_id, title, sent_at, status,
		       issue_number, issue_url, issue_closed, close_reason, status_changed_at
		FROM problem_reports
		ORDER BY sent_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list sent reports: %w", err)
	}
	defer rows.Close()

	var reports []SentProblemReport
	for rows.Next() {
		var report SentProblemReport
		var issueNumber sql.NullInt64
		var issueURL, closeReason sql.NullString
		var issueClosed int
		if err := rows.Scan(&report.BrrbinID, &report.SubmissionID, &report.Title, &report.SentAt, &report.Status,
			&issueNumber, &issueURL, &issueClosed, &closeReason, &report.StatusChangedAt); err != nil {
			return nil, fmt.Errorf("scan sent report: %w", err)
		}
		if issueNumber.Valid {
			number := int(issueNumber.Int64)
			report.IssueNumber = &number
		}
		if issueURL.Valid {
			report.IssueURL = &issueURL.String
		}
		if closeReason.Valid {
			report.CloseReason = &closeReason.String
		}
		report.IssueClosed = SQLiteIntToBool(issueClosed)
		reports = append(reports, report)
	}
	return reports, rows.Err()
}

func (s *ProblemReportStore) UpdateSentReportStatus(ctx context.Context, report SentProblemReport) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE problem_reports
		SET status = ?, issue_number = ?, issue_url = ?, issue_closed = ?, close_reason = ?, status_changed_at = ?
		WHERE brrbin_id = ?`,
		report.Status, report.IssueNumber, report.IssueURL, BoolToSQLite(report.IssueClosed), report.CloseReason, report.StatusChangedAt,
		report.BrrbinID,
	); err != nil {
		return fmt.Errorf("store report status: %w", err)
	}
	return nil
}

func (s *ProblemReportStore) HasSentReport(ctx context.Context, brrbinID string) (bool, error) {
	var found int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM problem_reports WHERE brrbin_id = ?`, brrbinID).Scan(&found)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("find sent report: %w", err)
	}
	return true, nil
}

func (s *ProblemReportStore) DeleteSentReport(ctx context.Context, brrbinID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM problem_reports WHERE brrbin_id = ?`, brrbinID); err != nil {
		return fmt.Errorf("delete sent report: %w", err)
	}
	return nil
}
