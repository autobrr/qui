// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package problemreport

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

const (
	statusWindow = 90 * 24 * time.Hour
	callTimeout  = 5 * time.Second
)

var ErrNotFound = errors.New("no sent report has this id")

type Issue struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	Closed bool   `json:"closed"`
}

type ListedReport struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	SentAt          time.Time `json:"sentAt"`
	Status          string    `json:"status"`
	Issue           *Issue    `json:"issue,omitzero"`
	Reason          string    `json:"reason,omitempty"`
	StatusChangedAt time.Time `json:"statusChangedAt"`
}

func (s *Service) SentReports(ctx context.Context) ([]ListedReport, error) {
	rows, err := s.store.SentReports(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	var ids []string
	for _, row := range rows {
		if now.Sub(row.SentAt) <= statusWindow {
			ids = append(ids, row.BrrbinID)
		}
	}
	if len(ids) > 0 {
		if err := s.refreshStatuses(ctx, rows, ids); err != nil {
			return nil, err
		}
	}

	listed := make([]ListedReport, len(rows))
	for i, row := range rows {
		listed[i] = listedReport(row)
	}
	return listed, nil
}

func (s *Service) refreshStatuses(ctx context.Context, rows []models.SentProblemReport, ids []string) error {
	installKey, err := s.store.InstallKey(ctx)
	if err != nil {
		return err
	}
	statuses, err := s.fetchStatuses(ctx, installKey, ids)
	if err != nil {
		log.Warn().Err(err).Msg("Cannot read Report statuses from brrbin, showing the stored ones")
		return nil
	}
	changedAt := s.now().UTC()
	for i := range rows {
		status, ok := statuses[rows[i].BrrbinID]
		if !ok || !status.apply(&rows[i]) {
			continue
		}
		rows[i].StatusChangedAt = changedAt
		if err := s.store.UpdateSentReportStatus(ctx, rows[i]); err != nil {
			return err
		}
	}
	return nil
}

type brrbinStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Issue  *Issue `json:"issue"`
	Reason string `json:"reason"`
}

func (a brrbinStatus) apply(row *models.SentProblemReport) bool {
	next := *row
	next.Status = a.Status
	next.IssueNumber, next.IssueURL, next.IssueClosed, next.CloseReason = nil, nil, false, nil
	switch a.Status {
	case models.ProblemReportReceived:
	case models.ProblemReportIssue:
		if a.Issue == nil || a.Issue.Number <= 0 {
			return false
		}
		next.IssueNumber = new(a.Issue.Number)
		if isHTTPSURL(a.Issue.URL) {
			next.IssueURL = new(a.Issue.URL)
		}
		next.IssueClosed = a.Issue.Closed
	case models.ProblemReportClosed:
		if a.Reason != "" {
			next.CloseReason = new(a.Reason)
		}
	default:
		return false
	}
	if sameStatus(*row, next) {
		return false
	}
	*row = next
	return true
}

func sameStatus(a, b models.SentProblemReport) bool {
	return a.Status == b.Status && a.IssueClosed == b.IssueClosed &&
		samePointee(a.IssueNumber, b.IssueNumber) && samePointee(a.IssueURL, b.IssueURL) && samePointee(a.CloseReason, b.CloseReason)
}

func samePointee[T comparable](a, b *T) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

func isHTTPSURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func listedReport(row models.SentProblemReport) ListedReport {
	listed := ListedReport{
		ID:              row.BrrbinID,
		Title:           row.Title,
		SentAt:          row.SentAt.UTC(),
		Status:          row.Status,
		StatusChangedAt: row.StatusChangedAt.UTC(),
	}
	if row.IssueNumber != nil {
		listed.Issue = &Issue{Number: *row.IssueNumber, Closed: row.IssueClosed}
		if row.IssueURL != nil {
			listed.Issue.URL = *row.IssueURL
		}
	}
	if row.CloseReason != nil {
		listed.Reason = *row.CloseReason
	}
	return listed
}

func (s *Service) fetchStatuses(ctx context.Context, installKey string, ids []string) (map[string]brrbinStatus, error) {
	escaped := make([]string, len(ids))
	for i, id := range ids {
		escaped[i] = url.QueryEscape(id)
	}
	status, body, err := s.callBrrbin(ctx, installKey, http.MethodGet, "/api/install/reports?ids="+strings.Join(escaped, ","), "", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("brrbin answered %d", status)
	}
	var answers []brrbinStatus
	if err := json.Unmarshal(body, &answers); err != nil {
		return nil, fmt.Errorf("read the Report statuses: %w", err)
	}
	byID := make(map[string]brrbinStatus, len(answers))
	for _, answer := range answers {
		byID[answer.ID] = answer
	}
	return byID, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	found, err := s.store.HasSentReport(ctx, id)
	if err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	installKey, err := s.store.InstallKey(ctx)
	if err != nil {
		return err
	}
	ctx = context.WithoutCancel(ctx)
	status, _, err := s.callBrrbin(ctx, installKey, http.MethodDelete, "/api/reports/"+url.PathEscape(id), "", nil)
	switch {
	case err != nil:
		log.Warn().Err(err).Str("reportID", id).Msg("brrbin did not answer a Problem report delete")
		return &RefusalError{Status: http.StatusBadGateway, Reason: ReasonUnavailable}
	case status == http.StatusNotFound:
	case status < 200 || status > 299:
		log.Warn().Int("status", status).Str("reportID", id).Msg("brrbin refused a Problem report delete")
		return &RefusalError{Status: http.StatusBadGateway, Reason: ReasonUnavailable}
	}
	if err := s.store.DeleteSentReport(ctx, id); err != nil {
		return err
	}
	log.Info().Str("reportID", id).Msg("Problem report deleted")
	return nil
}

func (s *Service) callBrrbin(ctx context.Context, installKey, method, path, token string, jsonBody []byte) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(s.baseURL, "/")+path, bytes.NewReader(jsonBody))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("X-Install-Key", installKey)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if jsonBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("read the brrbin answer: %w", err)
	}
	return resp.StatusCode, body, nil
}
