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
	"strings"
	"time"

	"github.com/autobrr/qui/pkg/httphelpers"
)

const (
	webhookTimeout = 30 * time.Second
)

type webhookPayload struct {
	SourceApp     string                `json:"source_app"`
	Type          string                `json:"type"`
	InstanceID    int                   `json:"instance_id,omitempty"`
	InstanceName  string                `json:"instance_name,omitempty"`
	Timestamp     time.Time             `json:"timestamp"`
	Torrent       *notifiarrAPITorrent  `json:"torrent,omitempty"`
	Backup        *notifiarrAPIBackup   `json:"backup,omitempty"`
	DirScan       *notifiarrAPIDirScan  `json:"dir_scan,omitempty"`
	OrphanScan    *notifiarrAPIOrphan   `json:"orphan_scan,omitempty"`
	CrossSeed     *CrossSeedEventData   `json:"cross_seed,omitempty"`
	Automations   *AutomationsEventData `json:"automations,omitempty"`
	ErrorMessages []string              `json:"error_messages,omitempty"`
}

func parseWebhookURL(rawURL string) (string, error) {
	target := strings.TrimPrefix(rawURL, "webhook://")
	parsed, err := url.Parse(target)
	if err != nil {
		return "", fmt.Errorf("invalid webhook url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("webhook target must be http or https")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return "", errors.New("webhook target host required")
	}
	return target, nil
}

func (s *Service) sendWebhook(ctx context.Context, rawURL string, event Event, title, message string) error {
	target, err := parseWebhookURL(rawURL)
	if err != nil {
		return err
	}

	if ctx == nil {
		ctx = context.Background()
	}

	payload := s.buildWebhookPayload(ctx, event, title, message)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "qui")

	client := &http.Client{Timeout: webhookTimeout}
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

func (s *Service) buildWebhookPayload(ctx context.Context, event Event, title, message string) webhookPayload {
	data := s.buildNotifiarrAPIData(ctx, event, title, message)

	payload := webhookPayload{
		SourceApp:     "qui",
		Type:          buildNotifiarrEventValue(event.Type),
		Timestamp:     data.Timestamp,
		Torrent:       data.Torrent,
		Backup:        data.Backup,
		DirScan:       data.DirScan,
		OrphanScan:    data.OrphanScan,
		CrossSeed:     data.CrossSeed,
		Automations:   data.Automations,
		ErrorMessages: data.ErrorMessages,
	}

	if data.InstanceID != nil {
		payload.InstanceID = *data.InstanceID
	}
	if data.InstanceName != nil {
		payload.InstanceName = *data.InstanceName
	}

	return payload
}
