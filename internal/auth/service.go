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

func (s *Service) SetupUser(ctx context.Context, username, password string) (*models.User, error) {
	exists, err := s.userStore.Exists(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check user existence: %w", err)
	}
	if exists {
		return nil, models.ErrUserAlreadyExists
	}

	if len(password) < 8 {
		return nil, errors.New("password must be at least 8 characters long")
	}

	hashedPassword, err := HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	user, err := s.userStore.Create(ctx, username, hashedPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	log.Info().Msgf("Initial user '%s' created successfully", username)
	return user, nil
}

func (s *Service) Login(ctx context.Context, username, password string) (*models.User, error) {
	exists, err := s.userStore.Exists(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check user existence: %w", err)
	}
	if !exists {
		return nil, ErrNotSetup
	}

	user, err := s.userStore.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, models.ErrUserNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	valid, err := VerifyPassword(password, user.PasswordHash)
	if err != nil {
		return nil, fmt.Errorf("failed to verify password: %w", err)
	}
	if !valid {
		return nil, ErrInvalidCredentials
	}

	return user, nil
}

func (s *Service) ChangePassword(ctx context.Context, oldPassword, newPassword string) error {
	user, err := s.userStore.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	valid, err := VerifyPassword(oldPassword, user.PasswordHash)
	if err != nil {
		return fmt.Errorf("failed to verify password: %w", err)
	}
	if !valid {
		return ErrInvalidCredentials
	}

	if len(newPassword) < 8 {
		return errors.New("password must be at least 8 characters long")
	}

	hashedPassword, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	if err := s.userStore.UpdatePassword(ctx, hashedPassword); err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	log.Info().Msg("Password changed successfully")
	return nil
}

func (s *Service) CreateAPIKey(ctx context.Context, name string, permissions []models.Permission, instanceIDs []int) (string, *models.APIKey, error) {
	return s.apiKeyStore.Create(ctx, name, permissions, instanceIDs)
}

func (s *Service) UpdateAPIKey(ctx context.Context, id int, name string, permissions []models.Permission, instanceIDs []int) (*models.APIKey, error) {
	return s.apiKeyStore.Update(ctx, id, name, permissions, instanceIDs)
}

func (s *Service) ValidateAPIKey(ctx context.Context, key string) (*models.APIKey, error) {
	return s.apiKeyStore.ValidateAPIKey(ctx, key)
}

func (s *Service) ListAPIKeys(ctx context.Context) ([]*models.APIKey, error) {
	return s.apiKeyStore.List(ctx)
}

func (s *Service) DeleteAPIKey(ctx context.Context, id int) error {
	return s.apiKeyStore.Delete(ctx, id)
}

func (s *Service) IsSetupComplete(ctx context.Context) (bool, error) {
	return s.userStore.Exists(ctx)
}
