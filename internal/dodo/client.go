// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dodo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var (
	ErrInvalidLicenseKey       = errors.New("license key is invalid")
	ErrLicenseNotFound         = errors.New("license key not found")
	ErrActivationLimitExceeded = errors.New("license activation limit exceeded")
	ErrInstanceNotFound        = errors.New("license activation instance not found")
	ErrRateLimitExceeded       = errors.New("rate limit exceeded")
)

const (
	// Dodo "live" base URL per vendor docs.
	dodoAPIBaseURL      = "https://live.dodopayments.com"
	dodoTestBaseURL     = "https://test.dodopayments.com"
	requestTimeout      = 30 * time.Second
	maxErrorBodyBytes   = 64 * 1024
	truncatedBodySuffix = " (truncated)"
)

type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("dodo api error (status %d)", e.StatusCode)
	}
	return fmt.Sprintf("dodo api error (status %d): %s", e.StatusCode, e.Message)
}

type Client struct {
	baseURL    string
	userAgent  string
	httpClient *http.Client
}

type OptFunc func(*Client)

// WithEnvironment sets the Dodo Payments environment ("test_mode" or "live_mode").
func WithEnvironment(env string) OptFunc {
	return func(c *Client) {
		switch strings.TrimSpace(strings.ToLower(env)) {
		case "test_mode", "test", "sandbox", "staging":
			c.baseURL = dodoTestBaseURL
		case "live_mode", "live", "prod", "production":
			c.baseURL = dodoAPIBaseURL
		}
	}
}

func WithBaseURL(baseURL string) OptFunc {
	return func(c *Client) {
		if baseURL != "" {
			c.baseURL = baseURL
		}
	}
}

func WithUserAgent(userAgent string) OptFunc {
	return func(c *Client) {
		c.userAgent = userAgent
	}
}

func WithHTTPClient(httpClient *http.Client) OptFunc {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

func NewClient(opts ...OptFunc) *Client {
	c := &Client{
		baseURL:   dodoAPIBaseURL,
		userAgent: "dodo-go",
		httpClient: &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     30 * time.Second,
			},
		},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

func (c *Client) BaseURL() string {
	return c.baseURL
}

type ActivateRequest struct {
	LicenseKey string `json:"license_key"`
	Name       string `json:"name,omitempty"`
}

type ActivateResponse struct {
	ID         string     `json:"id,omitempty"`
	InstanceID string     `json:"instance_id,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

type ValidateRequest struct {
	LicenseKey           string `json:"license_key"`
	LicenseKeyInstanceID string `json:"license_key_instance_id,omitempty"`
}

type ValidateResponse struct {
	Valid      bool       `json:"valid"`
	Status     string     `json:"status,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	InstanceID string     `json:"instance_id,omitempty"`
}

type DeactivateRequest struct {
	LicenseKey           string `json:"license_key"`
	LicenseKeyInstanceID string `json:"license_key_instance_id,omitempty"`
	InstanceID           string `json:"instance_id,omitempty"`
}

type DeactivateResponse struct {
	Success bool `json:"success"`
}

func (c *Client) Activate(ctx context.Context, req ActivateRequest) (*ActivateResponse, error) {
	var resp ActivateResponse
	if err := c.doRequest(ctx, http.MethodPost, "/licenses/activate", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) Validate(ctx context.Context, req ValidateRequest) (*ValidateResponse, error) {
	var resp ValidateResponse
	if err := c.doRequest(ctx, http.MethodPost, "/licenses/validate", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) Deactivate(ctx context.Context, req DeactivateRequest) (*DeactivateResponse, error) {
	var resp DeactivateResponse
	if err := c.doRequest(ctx, http.MethodPost, "/licenses/deactivate", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Detail  string `json:"detail"`
}

func (c *Client) doRequest(ctx context.Context, method, path string, requestBody any, responseBody any) error {
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseError(resp)
	}

	if responseBody == nil {
		return nil
	}

	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(responseBody); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	return nil
}

func parseError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes+1))
	truncated := len(body) > maxErrorBodyBytes
	if truncated {
		body = body[:maxErrorBodyBytes]
	}

	message := strings.TrimSpace(string(body))

	var payload errorResponse
	if err := json.Unmarshal(body, &payload); err == nil {
		switch {
		case payload.Message != "":
			message = payload.Message
		case payload.Detail != "":
			message = payload.Detail
		case payload.Error != "":
			message = payload.Error
		}
	}

	if truncated {
		if message == "" {
			message = strings.TrimSpace(truncatedBodySuffix)
		} else {
			message += truncatedBodySuffix
		}
	}

	lower := strings.ToLower(message)

	switch {
	case strings.Contains(lower, "license key instance") || (strings.Contains(lower, "instance") && strings.Contains(lower, "not found")):
		return wrapError(ErrInstanceNotFound, message)
	case resp.StatusCode == http.StatusNotFound || strings.Contains(lower, "not found"):
		return wrapError(ErrLicenseNotFound, message)
	case resp.StatusCode == http.StatusTooManyRequests:
		return wrapError(ErrRateLimitExceeded, message)
	case strings.Contains(lower, "activation limit") || (strings.Contains(lower, "activation") && strings.Contains(lower, "limit")):
		return wrapError(ErrActivationLimitExceeded, message)
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnprocessableEntity || strings.Contains(lower, "invalid"):
		return wrapError(ErrInvalidLicenseKey, message)
	}

	return &APIError{
		StatusCode: resp.StatusCode,
		Message:    message,
	}
}

func wrapError(base error, message string) error {
	if message == "" {
		return base
	}
	return fmt.Errorf("%w: %s", base, message)
}
