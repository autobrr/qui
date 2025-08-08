package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/gorilla/sessions"
	"github.com/rs/zerolog/log"

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
	store       sessions.Store
}

func NewService(db *sql.DB, sessionSecret string) *Service {
	store := sessions.NewCookieStore([]byte(sessionSecret))

	// Configure session options
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7, // 7 days
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}

	return &Service{
		userStore:   models.NewUserStore(db),
		apiKeyStore: models.NewAPIKeyStore(db),
		store:       store,
	}
}

// SetupUser creates the initial user account
func (s *Service) SetupUser(username, password string) (*models.User, error) {
	// Check if user already exists
	exists, err := s.userStore.Exists()
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
	user, err := s.userStore.Create(username, hashedPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	log.Info().Msgf("Initial user '%s' created successfully", username)
	return user, nil
}

// Login validates credentials and returns the user
func (s *Service) Login(username, password string) (*models.User, error) {
	// Check if setup is complete
	exists, err := s.userStore.Exists()
	if err != nil {
		return nil, fmt.Errorf("failed to check user existence: %w", err)
	}
	if !exists {
		return nil, ErrNotSetup
	}

	// Get user by username
	user, err := s.userStore.GetByUsername(username)
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
func (s *Service) ChangePassword(oldPassword, newPassword string) error {
	// Get the current user
	user, err := s.userStore.Get()
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
	if err := s.userStore.UpdatePassword(hashedPassword); err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	log.Info().Msg("Password changed successfully")
	return nil
}

// API Key Management

// CreateAPIKey generates a new API key
func (s *Service) CreateAPIKey(name string) (string, *models.APIKey, error) {
	return s.apiKeyStore.Create(name)
}

// ValidateAPIKey checks if an API key is valid
func (s *Service) ValidateAPIKey(key string) (*models.APIKey, error) {
	return s.apiKeyStore.ValidateAPIKey(key)
}

// ListAPIKeys returns all API keys
func (s *Service) ListAPIKeys() ([]*models.APIKey, error) {
	return s.apiKeyStore.List()
}

// DeleteAPIKey removes an API key
func (s *Service) DeleteAPIKey(id int) error {
	return s.apiKeyStore.Delete(id)
}

// IsSetupComplete checks if initial setup has been completed
func (s *Service) IsSetupComplete() (bool, error) {
	return s.userStore.Exists()
}

// GetSessionStore returns the session store for use in middleware
func (s *Service) GetSessionStore() sessions.Store {
	return s.store
}
