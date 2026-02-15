// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/autobrr/qui/pkg/httphelpers"
)

const (
	notifiarrAPIEndpoint         = "https://notifiarr.com/api/v1/notification/qui"
	notifiarrAPIValidateEndpoint = "https://notifiarr.com/api/v1/user/validate"
	notifiarrAPITimeout          = 30 * time.Second
	notifiarrAPIValidateTimeout  = 10 * time.Second
)

type notifiarrAPIPayload struct {
	Event string                  `json:"event"`
	Data  notifiarrAPIPayloadData `json:"data"`
}

type notifiarrAPIPayloadData struct {
	Subject                  string                `json:"subject,omitempty"`
	Message                  string                `json:"message,omitempty"`
	Event                    string                `json:"event"`
	Timestamp                time.Time             `json:"timestamp"`
	CrossSeed                *CrossSeedEventData   `json:"cross_seed,omitempty"`
	Automations              *AutomationsEventData `json:"automations,omitempty"`
	InstanceID               *int                  `json:"instance_id,omitempty"`
	InstanceName             *string               `json:"instance_name,omitempty"`
	TorrentName              *string               `json:"torrent_name,omitempty"`
	TorrentHash              *string               `json:"torrent_hash,omitempty"`
	TrackerDomain            *string               `json:"tracker_domain,omitempty"`
	Category                 *string               `json:"category,omitempty"`
	Tags                     []string              `json:"tags,omitempty"`
	BackupKind               *string               `json:"backup_kind,omitempty"`
	BackupRunID              *int64                `json:"backup_run_id,omitempty"`
	BackupTorrentCount       *int                  `json:"backup_torrent_count,omitempty"`
	DirScanRunID             *int64                `json:"dir_scan_run_id,omitempty"`
	DirScanMatchesFound      *int                  `json:"dir_scan_matches_found,omitempty"`
	DirScanTorrentsAdded     *int                  `json:"dir_scan_torrents_added,omitempty"`
	OrphanScanRunID          *int64                `json:"orphan_scan_run_id,omitempty"`
	OrphanScanFilesDeleted   *int                  `json:"orphan_scan_files_deleted,omitempty"`
	OrphanScanFoldersDeleted *int                  `json:"orphan_scan_folders_deleted,omitempty"`
	ErrorMessage             *string               `json:"error_message,omitempty"`
	ErrorMessages            []string              `json:"error_messages,omitempty"`
	StartedAt                *time.Time            `json:"started_at,omitempty"`
	CompletedAt              *time.Time            `json:"completed_at,omitempty"`
	DurationMs               *int64                `json:"duration_ms,omitempty"`
	Description              string                `json:"description,omitempty"`
	Fields                   []notifiarrField      `json:"fields,omitempty"`
}

type notifiarrAPIConfig struct {
	apiKey   string
	endpoint string
}

func parseNotifiarrAPIConfig(rawURL string) (notifiarrAPIConfig, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return notifiarrAPIConfig{}, err
	}

	apiKey := strings.TrimSpace(parsed.Host)
	if apiKey == "" && parsed.User != nil {
		apiKey = strings.TrimSpace(parsed.User.Username())
	}
	if apiKey == "" {
		return notifiarrAPIConfig{}, errors.New("notifiarr api key required")
	}

	endpoint := notifiarrAPIEndpoint
	if override := strings.TrimSpace(parsed.Query().Get("endpoint")); override != "" {
		overrideURL, err := url.Parse(override)
		if err != nil {
			return notifiarrAPIConfig{}, fmt.Errorf("invalid endpoint: %w", err)
		}
		if overrideURL.Scheme != "http" && overrideURL.Scheme != "https" {
			return notifiarrAPIConfig{}, errors.New("endpoint must be http or https")
		}
		if strings.TrimSpace(overrideURL.Host) == "" {
			return notifiarrAPIConfig{}, errors.New("endpoint host required")
		}
		endpoint = override
	}

	return notifiarrAPIConfig{
		apiKey:   apiKey,
		endpoint: endpoint,
	}, nil
}

func ValidateNotifiarrAPIKey(ctx context.Context, rawURL string) error {
	if targetScheme(rawURL) != "notifiarrapi" {
		return nil
	}

	config, err := parseNotifiarrAPIConfig(rawURL)
	if err != nil {
		return err
	}

	if ctx == nil {
		ctx = context.Background()
	}
	validateCtx, cancel := context.WithTimeout(ctx, notifiarrAPIValidateTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(validateCtx, http.MethodGet, buildNotifiarrAPIValidateURL(config.endpoint), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "qui")
	req.Header.Set("X-API-Key", config.apiKey)

	client := &http.Client{Timeout: notifiarrAPIValidateTimeout}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("notifiarr api validation failed: %w", err)
	}
	defer httphelpers.DrainAndClose(res)

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		message := "notifiarr api key invalid; use the API key only (not the full URL)"
		if trimmed := strings.TrimSpace(string(body)); trimmed != "" {
			message = fmt.Sprintf("%s: %s", message, trimmed)
		}
		return errors.New(message)
	}

	return nil
}

func (s *Service) sendNotifiarrAPI(ctx context.Context, rawURL string, event Event, title, message string) error {
	config, err := parseNotifiarrAPIConfig(rawURL)
	if err != nil {
		return err
	}

	if ctx == nil {
		ctx = context.Background()
	}

	payload := notifiarrAPIPayload{
		Event: buildNotifiarrEventValue(event.Type),
		Data:  s.buildNotifiarrAPIData(ctx, event, title, message),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.endpoint, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "qui")
	req.Header.Set("X-API-Key", config.apiKey)

	client := &http.Client{Timeout: notifiarrAPITimeout}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer httphelpers.DrainAndClose(res)

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("unexpected status: %d body: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}

func (s *Service) buildNotifiarrAPIData(ctx context.Context, event Event, title, message string) notifiarrAPIPayloadData {
	trimmedTitle := strings.TrimSpace(title)
	trimmedMessage := strings.TrimSpace(message)

	data := notifiarrAPIPayloadData{
		Event:     buildNotifiarrEventValue(event.Type),
		Timestamp: time.Now().UTC(),
	}
	if trimmedTitle != "" {
		data.Subject = trimmedTitle
	}
	if trimmedMessage != "" {
		data.Message = trimmedMessage
	}

	if event.CrossSeed != nil {
		data.CrossSeed = event.CrossSeed
	}
	if event.Automations != nil {
		data.Automations = event.Automations
	}

	if event.InstanceID > 0 {
		data.InstanceID = intPtr(event.InstanceID)
	}

	instanceName := strings.TrimSpace(event.InstanceName)
	if instanceName == "" && event.InstanceID > 0 {
		instanceName = strings.TrimSpace(s.resolveInstanceLabel(ctx, event))
	}
	if instanceName != "" {
		data.InstanceName = stringPtr(instanceName)
	}

	if strings.TrimSpace(event.TorrentName) != "" {
		data.TorrentName = stringPtr(event.TorrentName)
	}
	if strings.TrimSpace(event.TorrentHash) != "" {
		data.TorrentHash = stringPtr(event.TorrentHash)
	}
	if strings.TrimSpace(event.TrackerDomain) != "" {
		data.TrackerDomain = stringPtr(event.TrackerDomain)
	}
	if strings.TrimSpace(event.Category) != "" {
		data.Category = stringPtr(event.Category)
	}
	if len(event.Tags) > 0 {
		tags := append([]string(nil), event.Tags...)
		sort.Strings(tags)
		data.Tags = tags
	}
	if strings.TrimSpace(string(event.BackupKind)) != "" {
		data.BackupKind = stringPtr(string(event.BackupKind))
	}
	if event.BackupRunID > 0 {
		data.BackupRunID = int64Ptr(event.BackupRunID)
	}
	if event.BackupTorrentCount > 0 {
		data.BackupTorrentCount = intPtr(event.BackupTorrentCount)
	}
	if event.DirScanRunID > 0 {
		data.DirScanRunID = int64Ptr(event.DirScanRunID)
	}
	if event.DirScanMatchesFound > 0 {
		data.DirScanMatchesFound = intPtr(event.DirScanMatchesFound)
	}
	if event.DirScanTorrentsAdded > 0 {
		data.DirScanTorrentsAdded = intPtr(event.DirScanTorrentsAdded)
	}
	if event.OrphanScanRunID > 0 {
		data.OrphanScanRunID = int64Ptr(event.OrphanScanRunID)
	}
	if event.OrphanScanFilesDeleted > 0 {
		data.OrphanScanFilesDeleted = intPtr(event.OrphanScanFilesDeleted)
	}
	if event.OrphanScanFoldersDeleted > 0 {
		data.OrphanScanFoldersDeleted = intPtr(event.OrphanScanFoldersDeleted)
	}
	if strings.TrimSpace(event.ErrorMessage) != "" {
		data.ErrorMessage = stringPtr(event.ErrorMessage)
	}
	if len(event.ErrorMessages) > 0 {
		data.ErrorMessages = normalizeErrorMessages(event.ErrorMessages)
	}
	if data.ErrorMessage != nil {
		if len(data.ErrorMessages) == 0 {
			data.ErrorMessages = []string{*data.ErrorMessage}
		} else if !slices.Contains(data.ErrorMessages, *data.ErrorMessage) {
			data.ErrorMessages = append([]string{*data.ErrorMessage}, data.ErrorMessages...)
		}
	}

	if event.StartedAt != nil && !event.StartedAt.IsZero() {
		data.StartedAt = event.StartedAt
	}
	if event.CompletedAt != nil && !event.CompletedAt.IsZero() {
		data.CompletedAt = event.CompletedAt
	}
	if data.StartedAt != nil && data.CompletedAt != nil {
		durationMs := data.CompletedAt.Sub(*data.StartedAt).Milliseconds()
		if durationMs >= 0 {
			data.DurationMs = &durationMs
		}
	}

	description, fields := buildStructuredMessage(trimmedMessage)
	if description == "" {
		description = trimmedMessage
	}
	if description != "" {
		data.Description = description
	}
	if len(fields) > 0 {
		data.Fields = buildNotifiarrFields(fields)
	}

	return data
}

func normalizeErrorMessages(messages []string) []string {
	if len(messages) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(messages))
	out := make([]string, 0, minInt(len(messages), 10))
	for _, raw := range messages {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
		if len(out) >= 10 {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func buildNotifiarrAPIValidateURL(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
		return notifiarrAPIValidateEndpoint
	}
	return (&url.URL{
		Scheme: parsed.Scheme,
		Host:   parsed.Host,
		Path:   "/api/v1/user/validate",
	}).String()
}

func buildNotifiarrEventValue(eventType EventType) string {
	value := strings.TrimSpace(string(eventType))
	if value == "" {
		return "test"
	}
	return value
}

func stringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func intPtr(value int) *int {
	if value == 0 {
		return nil
	}
	v := value
	return &v
}

func int64Ptr(value int64) *int64 {
	if value == 0 {
		return nil
	}
	v := value
	return &v
}
