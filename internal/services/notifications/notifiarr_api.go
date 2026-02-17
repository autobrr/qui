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
	Subject       string                `json:"subject,omitempty"`
	Message       string                `json:"message,omitempty"`
	Event         string                `json:"event"`
	Timestamp     time.Time             `json:"timestamp"`
	Torrent       *notifiarrAPITorrent  `json:"torrent,omitempty"`
	Backup        *notifiarrAPIBackup   `json:"backup,omitempty"`
	DirScan       *notifiarrAPIDirScan  `json:"dir_scan,omitempty"`
	OrphanScan    *notifiarrAPIOrphan   `json:"orphan_scan,omitempty"`
	CrossSeed     *CrossSeedEventData   `json:"cross_seed,omitempty"`
	Automations   *AutomationsEventData `json:"automations,omitempty"`
	InstanceID    *int                  `json:"instance_id,omitempty"`
	InstanceName  *string               `json:"instance_name,omitempty"`
	ErrorMessages []string              `json:"error_messages,omitempty"`
	StartedAt     *time.Time            `json:"started_at,omitempty"`
	CompletedAt   *time.Time            `json:"completed_at,omitempty"`
	DurationMs    *int64                `json:"duration_ms,omitempty"`
	Description   string                `json:"description,omitempty"`
	Fields        []notifiarrField      `json:"fields,omitempty"`
}

type notifiarrAPITorrent struct {
	Name                  *string    `json:"name,omitempty"`
	Hash                  *string    `json:"hash,omitempty"`
	AddedAt               *time.Time `json:"added_at,omitempty"`
	EtaSeconds            *int64     `json:"eta_seconds,omitempty"`
	EstimatedCompletionAt *time.Time `json:"estimated_completion_at,omitempty"`
	State                 *string    `json:"state,omitempty"`
	Progress              *float64   `json:"progress,omitempty"`
	Ratio                 *float64   `json:"ratio,omitempty"`
	TotalSizeBytes        *int64     `json:"total_size_bytes,omitempty"`
	DownloadedBytes       *int64     `json:"downloaded_bytes,omitempty"`
	AmountLeftBytes       *int64     `json:"amount_left_bytes,omitempty"`
	DlSpeedBps            *int64     `json:"dl_speed_bps,omitempty"`
	UpSpeedBps            *int64     `json:"up_speed_bps,omitempty"`
	NumSeeds              *int64     `json:"num_seeds,omitempty"`
	NumLeechs             *int64     `json:"num_leechs,omitempty"`
	TrackerDomain         *string    `json:"tracker_domain,omitempty"`
	Category              *string    `json:"category,omitempty"`
	Tags                  []string   `json:"tags,omitempty"`
}

type notifiarrAPIBackup struct {
	Kind         *string `json:"kind,omitempty"`
	RunID        *int64  `json:"run_id,omitempty"`
	TorrentCount *int    `json:"torrent_count,omitempty"`
}

type notifiarrAPIDirScan struct {
	RunID         *int64 `json:"run_id,omitempty"`
	MatchesFound  *int   `json:"matches_found,omitempty"`
	TorrentsAdded *int   `json:"torrents_added,omitempty"`
}

type notifiarrAPIOrphan struct {
	RunID          *int64 `json:"run_id,omitempty"`
	FilesDeleted   *int   `json:"files_deleted,omitempty"`
	FoldersDeleted *int   `json:"folders_deleted,omitempty"`
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

	var tags []string
	if len(event.Tags) > 0 {
		tags = append([]string(nil), event.Tags...)
		sort.Strings(tags)
	}

	data.Torrent = func() *notifiarrAPITorrent {
		t := &notifiarrAPITorrent{
			Name:          stringPtr(event.TorrentName),
			Hash:          stringPtr(event.TorrentHash),
			TrackerDomain: stringPtr(event.TrackerDomain),
			Category:      stringPtr(event.Category),
		}
		if event.TorrentAddedOn > 0 {
			addedAt := time.Unix(event.TorrentAddedOn, 0).UTC()
			t.AddedAt = &addedAt
		}
		if event.TorrentETASeconds > 0 {
			eta := event.TorrentETASeconds
			t.EtaSeconds = &eta
			estimated := data.Timestamp.Add(time.Duration(eta) * time.Second)
			t.EstimatedCompletionAt = &estimated
		}
		if strings.TrimSpace(event.TorrentState) != "" {
			t.State = stringPtr(event.TorrentState)
		}
		if event.TorrentProgress > 0 {
			progress := event.TorrentProgress
			t.Progress = &progress
		}
		if event.TorrentRatio > 0 {
			ratio := event.TorrentRatio
			t.Ratio = &ratio
		}
		if event.TorrentTotalSizeBytes > 0 {
			total := event.TorrentTotalSizeBytes
			t.TotalSizeBytes = &total
		}
		if event.TorrentDownloadedBytes > 0 {
			downloaded := event.TorrentDownloadedBytes
			t.DownloadedBytes = &downloaded
		}
		if event.TorrentAmountLeftBytes > 0 {
			left := event.TorrentAmountLeftBytes
			t.AmountLeftBytes = &left
		}
		if event.TorrentDlSpeedBps > 0 {
			dl := event.TorrentDlSpeedBps
			t.DlSpeedBps = &dl
		}
		if event.TorrentUpSpeedBps > 0 {
			ul := event.TorrentUpSpeedBps
			t.UpSpeedBps = &ul
		}
		if event.TorrentNumSeeds > 0 {
			seeds := event.TorrentNumSeeds
			t.NumSeeds = &seeds
		}
		if event.TorrentNumLeechs > 0 {
			leechs := event.TorrentNumLeechs
			t.NumLeechs = &leechs
		}
		if len(tags) > 0 {
			t.Tags = append([]string(nil), tags...)
		}
		if t.Name == nil &&
			t.Hash == nil &&
			t.AddedAt == nil &&
			t.EtaSeconds == nil &&
			t.EstimatedCompletionAt == nil &&
			t.State == nil &&
			t.Progress == nil &&
			t.Ratio == nil &&
			t.TotalSizeBytes == nil &&
			t.DownloadedBytes == nil &&
			t.AmountLeftBytes == nil &&
			t.DlSpeedBps == nil &&
			t.UpSpeedBps == nil &&
			t.NumSeeds == nil &&
			t.NumLeechs == nil &&
			t.TrackerDomain == nil &&
			t.Category == nil &&
			len(t.Tags) == 0 {
			return nil
		}
		return t
	}()

	data.Backup = func() *notifiarrAPIBackup {
		b := &notifiarrAPIBackup{
			Kind:  stringPtr(string(event.BackupKind)),
			RunID: int64Ptr(event.BackupRunID),
		}
		if event.BackupTorrentCount > 0 {
			b.TorrentCount = intPtr(event.BackupTorrentCount)
		}
		if b.Kind == nil && b.RunID == nil && b.TorrentCount == nil {
			return nil
		}
		return b
	}()

	data.DirScan = func() *notifiarrAPIDirScan {
		d := &notifiarrAPIDirScan{
			RunID: int64Ptr(event.DirScanRunID),
		}
		if event.DirScanMatchesFound > 0 {
			d.MatchesFound = intPtr(event.DirScanMatchesFound)
		}
		if event.DirScanTorrentsAdded > 0 {
			d.TorrentsAdded = intPtr(event.DirScanTorrentsAdded)
		}
		if d.RunID == nil && d.MatchesFound == nil && d.TorrentsAdded == nil {
			return nil
		}
		return d
	}()

	data.OrphanScan = func() *notifiarrAPIOrphan {
		o := &notifiarrAPIOrphan{
			RunID: int64Ptr(event.OrphanScanRunID),
		}
		if event.OrphanScanFilesDeleted > 0 {
			o.FilesDeleted = intPtr(event.OrphanScanFilesDeleted)
		}
		if event.OrphanScanFoldersDeleted > 0 {
			o.FoldersDeleted = intPtr(event.OrphanScanFoldersDeleted)
		}
		if o.RunID == nil && o.FilesDeleted == nil && o.FoldersDeleted == nil {
			return nil
		}
		return o
	}()

	// Prefer a single stable shape for templates: always emit error_messages (a list).
	// error_message intentionally omitted; use error_messages only.
	errors := normalizeErrorMessages(event.ErrorMessages)
	if msg := strings.TrimSpace(event.ErrorMessage); msg != "" {
		if len(errors) == 0 {
			errors = []string{msg}
		} else if !slices.Contains(errors, msg) {
			errors = append([]string{msg}, errors...)
		}
	}
	if len(errors) > 0 {
		data.ErrorMessages = errors
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
