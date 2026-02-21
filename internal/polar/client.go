// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package polar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

var (
	ErrNoOrganizationID        = errors.New("organization ID not configured")
	ErrLicenseExpired          = errors.New("license expired")
	ErrLicenseNotActivated     = errors.New("license not activated")
	ErrInvalidLicenseKey       = errors.New("license key is not valid")
	ErrConditionMismatch       = errors.New("license key does not match required conditions")
	ErrActivationLimitExceeded = errors.New("license key activation limit already reached")
	ErrBadRequestData          = errors.New("bad request data")
	ErrCouldNotUnmarshalData   = errors.New("could not unmarshal data")
	ErrRateLimitExceeded       = errors.New("rate limit exceeded")
	ErrDataValidationError     = errors.New("data validation error")
)

const (
	polarAPIBaseURL        = "https://api.autobrr.com"
	polarSandboxAPIBaseURL = "https://sandbox-api.autobrr.com"
	validateEndpoint       = "/v1/customer-portal/license-keys/validate"
	activateEndpoint       = "/v1/customer-portal/license-keys/activate"
	deactivateEndpoint     = "/v1/customer-portal/license-keys/deactivate"

	requestTimeout = 30 * time.Second

	OrgIDNotConfigMsg = "Organization ID not configured"
	LicenseFailedMsg  = "Failed to validate license"
	ActivateFailedMsg = "Failed to activate license"
	InvalidRespMsg    = "Invalid license response"
)

// ValidationResponse represents the response from the validate endpoint
type ValidationResponse struct {
	ID               string     `json:"id"`
	BenefitID        string     `json:"benefit_id"`
	CustomerID       string     `json:"customer_id"`
	Key              string     `json:"key"`
	Status           string     `json:"status"`
	ExpiresAt        *time.Time `json:"expires_at"`
	LimitActivations int        `json:"limit_activations"`
	Usage            int        `json:"usage"`
	Validations      int        `json:"validations"`
}

type ValidateResp struct {
	Id               string    `json:"id"`
	OrganizationId   string    `json:"organization_id"`
	UserId           string    `json:"user_id"`
	BenefitId        string    `json:"benefit_id"`
	Key              string    `json:"key"`
	DisplayKey       string    `json:"display_key"`
	Status           string    `json:"status"`
	LimitActivations int       `json:"limit_activations"`
	Usage            int       `json:"usage"`
	LimitUsage       int       `json:"limit_usage"`
	Validations      int       `json:"validations"`
	LastValidatedAt  time.Time `json:"last_validated_at"`
	ExpiresAt        time.Time `json:"expires_at"`
	Activation       struct {
		Id           string         `json:"id"`
		LicenseKeyId string         `json:"license_key_id"`
		Label        string         `json:"label"`
		Meta         map[string]any `json:"meta"`
		CreatedAt    time.Time      `json:"created_at"`
		ModifiedAt   interface{}    `json:"modified_at"`
	} `json:"activation"`
}

func (v *ValidateResp) ValidLicense() bool {
	if v.Status == "granted" {
		return true
	}

	return false
}

// ActivationResponse represents the response from the activate endpoint
type ActivationResponse struct {
	LicenseKey LicenseKeyData `json:"license_key"`
}

type ActivateKeyResponse struct {
	Id           string         `json:"id"`
	LicenseKeyID string         `json:"license_key_id"`
	Label        string         `json:"label"`
	Meta         map[string]any `json:"meta"`
	CreatedAt    time.Time      `json:"created_at"`
	ModifiedAt   time.Time      `json:"modified_at"`
	LicenseKey   struct {
		ID               string     `json:"id"`
		OrganizationID   string     `json:"organization_id"`
		CustomerID       string     `json:"customer_id"`
		UserID           string     `json:"user_id"`
		BenefitID        string     `json:"benefit_id"`
		Key              string     `json:"key"`
		DisplayKey       string     `json:"display_key"`
		Status           string     `json:"status"`
		LimitActivations int        `json:"limit_activations"`
		Usage            int        `json:"usage"`
		LimitUsage       int        `json:"limit_usage"`
		Validations      int        `json:"validations"`
		LastValidatedAt  *time.Time `json:"last_validated_at"`
		ExpiresAt        *time.Time `json:"expires_at"`
	} `json:"license_key"`
}

// LicenseKeyData represents the nested license key data in activation response
type LicenseKeyData struct {
	ID               string     `json:"id"`
	BenefitID        string     `json:"benefit_id"`
	CustomerID       string     `json:"customer_id"`
	Key              string     `json:"key"`
	Status           string     `json:"status"`
	ExpiresAt        *time.Time `json:"expires_at"`
	LimitActivations int        `json:"limit_activations"`
	Usage            int        `json:"usage"`
}

type ErrorResponse struct {
	Error  string `json:"error"`
	Detail string `json:"detail"`
}

// Client wraps the Polar API for license management
type Client struct {
	baseURL        string
	environment    string
	organizationID string
	userAgent      string

	httpClient *http.Client
}

type OptFunc func(*Client)

// WithOrganizationID sets the organization ID to use for requests.
func WithOrganizationID(organizationID string) OptFunc {
	return func(c *Client) {
		c.organizationID = organizationID
	}
}

// WithEnvironment sets the environment to use for requests.
// Valid values are "production", "sandbox" and "development".
func WithEnvironment(env string) OptFunc {
	return func(c *Client) {
		switch env {
		case "production":
			c.baseURL = polarAPIBaseURL
			c.environment = env
		case "sandbox":
			c.baseURL = polarSandboxAPIBaseURL
			c.environment = env
		case "development":
			c.baseURL = "http://localhost:8080"
			c.environment = env
		}
	}
}

func WithUserAgent(userAgent string) OptFunc {
	return func(c *Client) {
		c.userAgent = userAgent
	}
}

// WithHTTPClient sets a custom HTTP client to use for requests
func WithHTTPClient(httpClient *http.Client) OptFunc {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

// NewClient creates a new Polar API client with the default HTTP client
func NewClient(opts ...OptFunc) *Client {
	c := &Client{
		baseURL:        polarAPIBaseURL,
		environment:    "production",
		organizationID: "",
		userAgent:      "polar-go",

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

type ActivateRequest struct {
	// License key
	Key string `json:"key"`

	// Set a label to associate with this specific activation.
	Label string `json:"label"`

	// Organization ID
	OrganizationID string `json:"organization_id"`

	// JSON object with custom conditions to validate against in the future, e.g IP, mac address, major version etc.
	Conditions map[string]any `json:"conditions,omitempty"`

	// JSON object with metadata to store for the users activation.
	Meta map[string]any `json:"meta,omitempty"`
}

func (r *ActivateRequest) Validate() []error {
	var err []error
	if r.Key == "" {
		err = append(err, errors.New("key is required"))
	}
	if r.Label == "" {
		err = append(err, errors.New("label is required"))
	}
	if r.OrganizationID == "" {
		err = append(err, ErrNoOrganizationID)
	}

	return err
}

func (r *ActivateRequest) SetMeta(k string, v any) {
	if r.Meta == nil {
		r.Meta = make(map[string]any)
	}
	r.Meta[k] = v
}

func (r *ActivateRequest) SetCondition(k string, v any) {
	if r.Conditions == nil {
		r.Conditions = make(map[string]any)
	}
	r.Conditions[k] = v
}

// Activate activates a license key against Polar API
func (c *Client) Activate(ctx context.Context, activateReq ActivateRequest) (*ActivateKeyResponse, error) {
	if activateReq.OrganizationID == "" {
		activateReq.OrganizationID = c.organizationID
	}

	if err := activateReq.Validate(); len(err) > 0 {
		return nil, errors.Wrap(ErrBadRequestData, fmt.Sprintf("invalid request: %v", err))
	}

	jsonData, err := json.Marshal(activateReq)
	if err != nil {
		return nil, ErrBadRequestData
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+activateEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
		break

	case http.StatusForbidden:
		var response ErrorResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, ErrCouldNotUnmarshalData
		}

		if response.Detail == "License key activation limit already reached" {
			return nil, ErrActivationLimitExceeded
		}

		return nil, errors.Wrapf(errors.New(response.Detail), "%s", response.Error)

	case http.StatusNotFound:
		return nil, ErrInvalidLicenseKey

	case http.StatusTooManyRequests:
		return nil, ErrActivationLimitExceeded

	default:
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var response ActivateKeyResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, ErrCouldNotUnmarshalData
	}

	return &response, nil
}

type ValidateRequest struct {
	Key            string         `json:"key"`
	ActivationID   string         `json:"activation_id,omitempty"`
	OrganizationID string         `json:"organization_id"`
	Conditions     map[string]any `json:"conditions,omitempty"`
	IncrementUsage int            `json:"increment_usage,omitempty"`
}

func (r *ValidateRequest) SetCondition(k string, v any) {
	if r.Conditions == nil {
		r.Conditions = make(map[string]any)
	}
	r.Conditions[k] = v
}

func (r *ValidateRequest) Validate() []error {
	var err []error
	if r.Key == "" {
		err = append(err, errors.New("key is required"))
	}
	if r.OrganizationID == "" {
		err = append(err, ErrNoOrganizationID)
	}

	return err
}

// Validate a license key against Polar API
func (c *Client) Validate(ctx context.Context, validateReq ValidateRequest) (*ValidateResp, error) {
	if validateReq.OrganizationID == "" {
		validateReq.OrganizationID = c.organizationID
	}

	if err := validateReq.Validate(); len(err) > 0 {
		return nil, errors.Wrap(ErrBadRequestData, fmt.Sprintf("invalid request: %v", err))
	}

	jsonData, err := json.Marshal(validateReq)
	if err != nil {
		return nil, ErrBadRequestData
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+validateEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
		break

	case http.StatusForbidden:
		var response ErrorResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, ErrCouldNotUnmarshalData
		}

		if response.Detail == "License key activation limit already reached" {
			return nil, ErrActivationLimitExceeded
		}

		return nil, errors.Wrapf(errors.New(response.Detail), "%s", response.Error)

	case http.StatusNotFound:
		var response ErrorResponse
		if err := json.Unmarshal(body, &response); err == nil {
			if response.Detail == "License key does not match required conditions" {
				return nil, ErrConditionMismatch
			}
		}
		return nil, ErrInvalidLicenseKey

	case http.StatusTooManyRequests:
		return nil, ErrRateLimitExceeded

	default:
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var response ValidateResp
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, ErrCouldNotUnmarshalData
	}

	return &response, nil
}

type DeactivateRequest struct {
	Key            string `json:"key"`
	ActivationID   string `json:"activation_id"`
	OrganizationID string `json:"organization_id"`
}

func (r *DeactivateRequest) Validate() []error {
	var err []error
	if r.Key == "" {
		err = append(err, errors.New("key is required"))
	}
	if r.ActivationID == "" {
		err = append(err, errors.New("activation_id is required"))
	}
	if r.OrganizationID == "" {
		err = append(err, ErrNoOrganizationID)
	}

	return err
}

func (c *Client) Deactivate(ctx context.Context, deactivateReq DeactivateRequest) error {
	if deactivateReq.OrganizationID == "" {
		deactivateReq.OrganizationID = c.organizationID
	}

	if err := deactivateReq.Validate(); len(err) > 0 {
		return errors.Wrap(ErrBadRequestData, fmt.Sprintf("invalid request: %v", err))
	}

	log.Debug().
		Str("licenseKey", maskLicenseKey(deactivateReq.Key)).
		Str("activationId", maskID(deactivateReq.ActivationID)).
		Str("baseURL", c.baseURL).
		Msg("Deactivating Polar license activation")

	jsonData, err := json.Marshal(deactivateReq)
	if err != nil {
		return ErrBadRequestData
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+deactivateEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	log.Trace().
		Int("status", resp.StatusCode).
		Str("licenseKey", maskLicenseKey(deactivateReq.Key)).
		Str("activationId", maskID(deactivateReq.ActivationID)).
		Msg("Polar license deactivation response received")

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent:
		log.Info().
			Str("licenseKey", maskLicenseKey(deactivateReq.Key)).
			Str("activationId", maskID(deactivateReq.ActivationID)).
			Msg("Polar license deactivated successfully")
		return nil
	case http.StatusForbidden:
		var response ErrorResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return ErrCouldNotUnmarshalData
		}
		log.Warn().
			Str("licenseKey", maskLicenseKey(deactivateReq.Key)).
			Str("activationId", maskID(deactivateReq.ActivationID)).
			Str("detail", response.Detail).
			Msg("Polar license deactivation forbidden")
		return errors.Wrapf(errors.New(response.Detail), "%s", response.Error)
	case http.StatusNotFound:
		var response ErrorResponse
		if err := json.Unmarshal(body, &response); err == nil && response.Detail != "" {
			if strings.Contains(strings.ToLower(response.Detail), "activation") {
				log.Info().
					Str("licenseKey", maskLicenseKey(deactivateReq.Key)).
					Str("activationId", maskID(deactivateReq.ActivationID)).
					Msg("Polar license activation not found (already deactivated)")
				return ErrLicenseNotActivated
			}
		}
		return ErrInvalidLicenseKey
	case http.StatusTooManyRequests:
		log.Warn().
			Str("licenseKey", maskLicenseKey(deactivateReq.Key)).
			Str("activationId", maskID(deactivateReq.ActivationID)).
			Msg("Polar license deactivation rate limited")
		return ErrRateLimitExceeded
	default:
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
}

// Helper functions

// maskLicenseKey masks a license key for logging (shows first 8 chars + ***)
func maskLicenseKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:8] + "***"
}

// maskID masks an ID for logging (shows first 8 chars + ***)
func maskID(id string) string {
	if len(id) <= 8 {
		return "***"
	}
	return id[:8] + "***"
}

// IsClientConfigured checks if the Polar client is properly configured
func (c *Client) IsClientConfigured() bool {
	return c.organizationID != ""
}
