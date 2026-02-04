// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
)

const (
	SessionName = "qui_user_session"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrNotSetup           = errors.New("initial setup required")
)

type Service struct {
	userStore   *models.UserStore
	apiKeyStore *models.APIKeyStore
}

func NewService(db dbinterface.Querier) *Service {
	return &Service{
		userStore:   models.NewUserStore(db),
		apiKeyStore: models.NewAPIKeyStore(db),
	}
}

// SetupUser creates the initial user account
func (s *Service) SetupUser(ctx context.Context, username, password string) (*models.User, error) {
	// Check if user already exists
	exists, err := s.userStore.Exists(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check user existence: %w", err)
	}
	if exists {
		return nil, models.ErrUserAlreadyExists
	}

	// Validate password strength
	if len(password) < 8 {
		return nil, errors.New("password must be at least 8 characters long")
	}

	// Hash password
	hashedPassword, err := HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Create user
	user, err := s.userStore.Create(ctx, username, hashedPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	log.Info().Msgf("Initial user '%s' created successfully", username)
	return user, nil
}

// Login validates credentials and returns the user
func (s *Service) Login(ctx context.Context, username, password string) (*models.User, error) {
	// Check if setup is complete
	exists, err := s.userStore.Exists(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check user existence: %w", err)
	}
	if !exists {
		return nil, ErrNotSetup
	}

	// Get user by username
	user, err := s.userStore.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, models.ErrUserNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Verify password
	valid, err := VerifyPassword(password, user.PasswordHash)
	if err != nil {
		return nil, fmt.Errorf("failed to verify password: %w", err)
	}
	if !valid {
		return nil, ErrInvalidCredentials
	}

	return user, nil
}

// ChangePassword updates the user's password
func (s *Service) ChangePassword(ctx context.Context, oldPassword, newPassword string) error {
	// Get the current user
	user, err := s.userStore.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	// Verify old password
	valid, err := VerifyPassword(oldPassword, user.PasswordHash)
	if err != nil {
		return fmt.Errorf("failed to verify password: %w", err)
	}
	if !valid {
		return ErrInvalidCredentials
	}

	// Validate new password strength
	if len(newPassword) < 8 {
		return errors.New("password must be at least 8 characters long")
	}

	// Hash new password
	hashedPassword, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Update password
	if err := s.userStore.UpdatePassword(ctx, hashedPassword); err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	log.Info().Msg("Password changed successfully")
	return nil
}

// API Key Management

// CreateAPIKey generates a new API key
func (s *Service) CreateAPIKey(ctx context.Context, name string) (string, *models.APIKey, error) {
	return s.apiKeyStore.Create(ctx, name)
}

// ValidateAPIKey checks if an API key is valid
func (s *Service) ValidateAPIKey(ctx context.Context, key string) (*models.APIKey, error) {
	return s.apiKeyStore.ValidateAPIKey(ctx, key)
}

// ListAPIKeys returns all API keys
func (s *Service) ListAPIKeys(ctx context.Context) ([]*models.APIKey, error) {
	return s.apiKeyStore.List(ctx)
}

// DeleteAPIKey removes an API key
func (s *Service) DeleteAPIKey(ctx context.Context, id int) error {
	return s.apiKeyStore.Delete(ctx, id)
}

// IsSetupComplete checks if initial setup has been completed
func (s *Service) IsSetupComplete(ctx context.Context) (bool, error) {
	return s.userStore.Exists(ctx)
}
