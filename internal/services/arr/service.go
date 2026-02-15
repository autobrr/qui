// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package arr

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

const (
	// DefaultPositiveCacheTTL is the default TTL for positive cache entries (IDs found)
	// Long TTL because external IDs (IMDb, TMDb, TVDb, TVMaze) are immutable
	DefaultPositiveCacheTTL = 30 * 24 * time.Hour // 30 days

	// DefaultNegativeCacheTTL is the default TTL for negative cache entries (no IDs found)
	// Short TTL because content may be added to *arr instances later
	DefaultNegativeCacheTTL = 1 * time.Hour

	// cacheCleanupInterval is how often to run cache cleanup (opportunistically)
	cacheCleanupInterval = 30 * time.Minute
)

// ContentType represents the type of content being searched
type ContentType string

const (
	ContentTypeMovie   ContentType = "movie"
	ContentTypeTV      ContentType = "tv"
	ContentTypeAnime   ContentType = "anime"
	ContentTypeUnknown ContentType = "unknown"
)

// ExternalIDsResult contains the result of an ID lookup
type ExternalIDsResult struct {
	IDs           *models.ExternalIDs `json:"ids,omitempty"`
	FromCache     bool                `json:"from_cache"`
	ArrInstanceID *int                `json:"arr_instance_id,omitempty"`
	ContentType   ContentType         `json:"content_type"`
}

// Service orchestrates ARR ID lookups with caching
type Service struct {
	instanceStore *models.ArrInstanceStore
	cacheStore    *models.ArrIDCacheStore
	positiveTTL   time.Duration
	negativeTTL   time.Duration

	// Cache cleanup scheduling
	cleanupMu        sync.Mutex
	nextCacheCleanup time.Time
}

// NewService creates a new ARR service
func NewService(instanceStore *models.ArrInstanceStore, cacheStore *models.ArrIDCacheStore) *Service {
	return &Service{
		instanceStore: instanceStore,
		cacheStore:    cacheStore,
		positiveTTL:   DefaultPositiveCacheTTL,
		negativeTTL:   DefaultNegativeCacheTTL,
	}
}

// WithPositiveTTL sets the TTL for positive cache entries
func (s *Service) WithPositiveTTL(ttl time.Duration) *Service {
	s.positiveTTL = ttl
	return s
}

// WithNegativeTTL sets the TTL for negative cache entries
func (s *Service) WithNegativeTTL(ttl time.Duration) *Service {
	s.negativeTTL = ttl
	return s
}

// LookupExternalIDs queries ARR instances for external IDs based on content type.
// It checks the cache first, then queries ARR instances in priority order.
func (s *Service) LookupExternalIDs(ctx context.Context, title string, contentType ContentType) (*ExternalIDsResult, error) {
	if title == "" {
		return nil, errors.New("title cannot be empty")
	}

	// Skip lookup for unknown content type
	if contentType == ContentTypeUnknown || contentType == "" {
		return nil, nil
	}

	// Schedule opportunistic cache cleanup
	defer s.maybeScheduleCacheCleanup()

	// Compute cache key
	titleHash := models.ComputeTitleHash(title)

	// Check cache first
	cacheEntry, err := s.cacheStore.Get(ctx, titleHash, string(contentType))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		// Log real DB errors (not just "not found")
		log.Warn().Err(err).
			Str("title", title).
			Str("contentType", string(contentType)).
			Msg("[ARR-LOOKUP] Cache read error, proceeding as cache miss")
	}
	if err == nil && cacheEntry != nil {
		// Cache hit
		if cacheEntry.IsNegative {
			log.Debug().
				Str("title", title).
				Str("contentType", string(contentType)).
				Msg("[ARR-LOOKUP] Cache hit (negative)")
			return &ExternalIDsResult{
				IDs:           nil,
				FromCache:     true,
				ArrInstanceID: cacheEntry.ArrInstanceID,
				ContentType:   contentType,
			}, nil
		}

		log.Debug().
			Str("title", title).
			Str("contentType", string(contentType)).
			Str("imdbId", cacheEntry.ExternalIDs.IMDbID).
			Int("tmdbId", cacheEntry.ExternalIDs.TMDbID).
			Int("tvdbId", cacheEntry.ExternalIDs.TVDbID).
			Int("tvmazeId", cacheEntry.ExternalIDs.TVMazeID).
			Msg("[ARR-LOOKUP] Cache hit (positive)")

		return &ExternalIDsResult{
			IDs:           &cacheEntry.ExternalIDs,
			FromCache:     true,
			ArrInstanceID: cacheEntry.ArrInstanceID,
			ContentType:   contentType,
		}, nil
	}

	// Cache miss - determine which ARR type(s) to query
	arrType := s.getArrTypeForContent(contentType)
	if arrType == "" {
		return nil, nil
	}

	// Get enabled instances of the appropriate type, ordered by priority
	instances, err := s.instanceStore.ListEnabledByType(ctx, arrType)
	if err != nil {
		return nil, err
	}

	if len(instances) == 0 {
		log.Debug().
			Str("title", title).
			Str("contentType", string(contentType)).
			Str("arrType", string(arrType)).
			Msg("[ARR-LOOKUP] No enabled ARR instances found")
		return nil, nil
	}

	// Query instances in priority order until we find IDs
	// Track whether we successfully queried at least one instance
	anyQueried := false

	for _, instance := range instances {
		apiKey, err := s.instanceStore.GetDecryptedAPIKey(instance)
		if err != nil {
			log.Warn().Err(err).
				Int("instanceId", instance.ID).
				Str("instanceName", instance.Name).
				Msg("[ARR-LOOKUP] Failed to decrypt API key")
			continue
		}

		var basicPass string
		if instance.BasicUsername != nil && *instance.BasicUsername != "" {
			basicPass, err = s.instanceStore.GetDecryptedBasicPassword(instance)
			if err != nil {
				log.Warn().Err(err).
					Int("instanceId", instance.ID).
					Str("instanceName", instance.Name).
					Msg("[ARR-LOOKUP] Failed to decrypt basic auth password")
				continue
			}
		}
		basicPassPtr := &basicPass
		if basicPass == "" {
			basicPassPtr = nil
		}

		client := NewClient(instance.BaseURL, apiKey, instance.BasicUsername, basicPassPtr, instance.Type, instance.TimeoutSeconds)

		ids, err := client.ParseTitle(ctx, title)
		if err != nil {
			log.Debug().Err(err).
				Int("instanceId", instance.ID).
				Str("instanceName", instance.Name).
				Str("title", title).
				Msg("[ARR-LOOKUP] Parse request failed")
			continue
		}

		// Successfully queried this instance
		anyQueried = true

		if ids != nil && !ids.IsEmpty() {
			// Found IDs - cache and return
			instanceID := instance.ID
			if err := s.cacheStore.Set(ctx, titleHash, string(contentType), &instanceID, ids, false, s.positiveTTL); err != nil {
				log.Warn().Err(err).Msg("[ARR-LOOKUP] Failed to cache positive result")
			}

			log.Debug().
				Str("title", title).
				Int("instanceId", instance.ID).
				Str("instanceName", instance.Name).
				Str("imdbId", ids.IMDbID).
				Int("tmdbId", ids.TMDbID).
				Int("tvdbId", ids.TVDbID).
				Int("tvmazeId", ids.TVMazeID).
				Msg("[ARR-LOOKUP] IDs found")

			return &ExternalIDsResult{
				IDs:           ids,
				FromCache:     false,
				ArrInstanceID: &instanceID,
				ContentType:   contentType,
			}, nil
		}

		log.Debug().
			Int("instanceId", instance.ID).
			Str("instanceName", instance.Name).
			Str("title", title).
			Msg("[ARR-LOOKUP] No IDs returned from instance")
	}

	// Only cache negative result if we successfully queried at least one instance
	// (don't cache if all instances failed to connect/decrypt)
	if anyQueried {
		if err := s.cacheStore.Set(ctx, titleHash, string(contentType), nil, nil, true, s.negativeTTL); err != nil {
			log.Warn().Err(err).Msg("[ARR-LOOKUP] Failed to cache negative result")
		}
	}

	log.Debug().
		Str("title", title).
		Str("contentType", string(contentType)).
		Int("instancesQueried", len(instances)).
		Msg("[ARR-LOOKUP] No IDs found from any instance")

	return &ExternalIDsResult{
		IDs:         nil,
		FromCache:   false,
		ContentType: contentType,
	}, nil
}

// TestConnection tests connectivity to an ARR instance.
func (s *Service) TestConnection(ctx context.Context, baseURL, apiKey string, basicUsername, basicPassword *string, instanceType models.ArrInstanceType) error {
	client := NewClient(baseURL, apiKey, basicUsername, basicPassword, instanceType, 15)
	return client.Ping(ctx)
}

// TestInstance tests connectivity to an existing ARR instance by ID
func (s *Service) TestInstance(ctx context.Context, instanceID int) error {
	instance, err := s.instanceStore.Get(ctx, instanceID)
	if err != nil {
		return err
	}

	apiKey, err := s.instanceStore.GetDecryptedAPIKey(instance)
	if err != nil {
		return err
	}

	var basicPass string
	if instance.BasicUsername != nil && *instance.BasicUsername != "" {
		basicPass, err = s.instanceStore.GetDecryptedBasicPassword(instance)
		if err != nil {
			return err
		}
	}
	basicPassPtr := &basicPass
	if basicPass == "" {
		basicPassPtr = nil
	}

	client := NewClient(instance.BaseURL, apiKey, instance.BasicUsername, basicPassPtr, instance.Type, instance.TimeoutSeconds)
	err = client.Ping(ctx)

	// Update test status
	status := "ok"
	var errorMsg *string
	if err != nil {
		status = "error"
		errStr := err.Error()
		errorMsg = &errStr
	}

	if updateErr := s.instanceStore.UpdateTestStatus(ctx, instanceID, status, errorMsg); updateErr != nil {
		log.Warn().Err(updateErr).Int("instanceId", instanceID).Msg("Failed to update test status")
	}

	return err
}

// DebugResolve resolves a title to IDs and returns detailed debug info
func (s *Service) DebugResolve(ctx context.Context, title string, contentType ContentType) (*DebugResolveResult, error) {
	result := &DebugResolveResult{
		Title:       title,
		ContentType: contentType,
	}

	// Check cache
	titleHash := models.ComputeTitleHash(title)
	result.TitleHash = titleHash

	cacheEntry, err := s.cacheStore.Get(ctx, titleHash, string(contentType))
	if err == nil && cacheEntry != nil {
		result.CacheHit = true
		result.CacheEntry = cacheEntry
	} else if !errors.Is(err, sql.ErrNoRows) && err != nil {
		result.Error = err.Error()
	}

	// Lookup fresh (bypassing cache)
	arrType := s.getArrTypeForContent(contentType)
	if arrType != "" {
		instances, err := s.instanceStore.ListEnabledByType(ctx, arrType)
		if err == nil {
			result.InstancesAvailable = len(instances)

			for _, instance := range instances {
				instanceResult := DebugInstanceResult{
					InstanceID:   instance.ID,
					InstanceName: instance.Name,
					InstanceType: string(instance.Type),
				}

				apiKey, err := s.instanceStore.GetDecryptedAPIKey(instance)
				if err != nil {
					instanceResult.Error = "failed to decrypt API key: " + err.Error()
					result.InstanceResults = append(result.InstanceResults, instanceResult)
					continue
				}

				var basicPass string
				if instance.BasicUsername != nil && *instance.BasicUsername != "" {
					basicPass, err = s.instanceStore.GetDecryptedBasicPassword(instance)
					if err != nil {
						instanceResult.Error = "failed to decrypt basic auth password: " + err.Error()
						result.InstanceResults = append(result.InstanceResults, instanceResult)
						continue
					}
				}
				basicPassPtr := &basicPass
				if basicPass == "" {
					basicPassPtr = nil
				}

				client := NewClient(instance.BaseURL, apiKey, instance.BasicUsername, basicPassPtr, instance.Type, instance.TimeoutSeconds)
				ids, err := client.ParseTitle(ctx, title)
				if err != nil {
					instanceResult.Error = err.Error()
				} else {
					instanceResult.IDs = ids
				}

				result.InstanceResults = append(result.InstanceResults, instanceResult)
			}
		}
	}

	return result, nil
}

// getArrTypeForContent maps content type to the appropriate ARR instance type
func (s *Service) getArrTypeForContent(contentType ContentType) models.ArrInstanceType {
	switch contentType {
	case ContentTypeMovie:
		return models.ArrInstanceTypeRadarr
	case ContentTypeTV, ContentTypeAnime:
		return models.ArrInstanceTypeSonarr
	default:
		return ""
	}
}

// CleanupExpiredCache removes expired cache entries
func (s *Service) CleanupExpiredCache(ctx context.Context) (int64, error) {
	return s.cacheStore.CleanupExpired(ctx)
}

// DebugResolveResult contains detailed debug information about an ID resolution
type DebugResolveResult struct {
	Title              string                  `json:"title"`
	TitleHash          string                  `json:"title_hash"`
	ContentType        ContentType             `json:"content_type"`
	CacheHit           bool                    `json:"cache_hit"`
	CacheEntry         *models.ArrIDCacheEntry `json:"cache_entry,omitempty"`
	InstancesAvailable int                     `json:"instances_available"`
	InstanceResults    []DebugInstanceResult   `json:"instance_results,omitempty"`
	Error              string                  `json:"error,omitempty"`
}

// DebugInstanceResult contains the result of querying a single ARR instance
type DebugInstanceResult struct {
	InstanceID   int                 `json:"instance_id"`
	InstanceName string              `json:"instance_name"`
	InstanceType string              `json:"instance_type"`
	IDs          *models.ExternalIDs `json:"ids,omitempty"`
	Error        string              `json:"error,omitempty"`
}

// maybeScheduleCacheCleanup triggers cache cleanup if enough time has passed since the last cleanup.
// This runs opportunistically during lookups to prevent unbounded table growth.
func (s *Service) maybeScheduleCacheCleanup() {
	if s == nil || s.cacheStore == nil {
		return
	}

	s.cleanupMu.Lock()
	if time.Now().Before(s.nextCacheCleanup) {
		s.cleanupMu.Unlock()
		return
	}
	s.nextCacheCleanup = time.Now().Add(cacheCleanupInterval)
	s.cleanupMu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if deleted, err := s.cacheStore.CleanupExpired(ctx); err != nil {
			log.Debug().Err(err).Msg("[ARR-LOOKUP] Failed to cleanup expired cache entries")
		} else if deleted > 0 {
			log.Debug().Int64("deleted", deleted).Msg("[ARR-LOOKUP] Cleaned up expired cache entries")
		}
	}()
}
