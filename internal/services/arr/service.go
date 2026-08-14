// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package arr

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/moistari/rls"
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
	Titles        []string            `json:"titles,omitempty"`
	TitlesKnown   bool                `json:"-"`
	FromCache     bool                `json:"from_cache"`
	ArrInstanceID *int                `json:"arr_instance_id,omitempty"`
	ContentType   ContentType         `json:"content_type"`
	Source        string              `json:"source,omitempty"`
}

// SeasonEpisodeTotalResult contains the resolved episode count for a Sonarr season,
// plus the show's alternate titles usable for that season (series-wide + same-season).
type SeasonEpisodeTotalResult struct {
	TotalEpisodes int      `json:"total_episodes"`
	ArrInstanceID *int     `json:"arr_instance_id,omitempty"`
	Titles        []string `json:"titles,omitempty"`
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

	titleHash := models.ComputeTitleHash(title)

	cacheResult, err := s.lookupCache(ctx, titleHash, title, contentType)
	if err != nil {
		return nil, err
	}
	if cacheResult != nil && (cacheResult.IDs == nil || cacheResult.IDs.IsEmpty() || cacheResult.TitlesKnown) {
		return cacheResult, nil
	}

	// Cache miss - determine which ARR type(s) to query
	instances, err := s.enabledInstancesForContent(ctx, title, contentType)
	if err != nil {
		return nil, err
	}

	if len(instances) == 0 {
		if cacheResult != nil {
			return cacheResult, nil
		}
		return nil, nil
	}

	result, err := s.lookupExternalIDsFromParse(ctx, titleHash, title, contentType, instances, cacheResult == nil)
	if err != nil {
		// Instances unreachable: fall back to the cached IDs that triggered this
		// re-query (e.g. a legacy entry awaiting title hydration) rather than
		// discarding known-good data over a transient outage.
		if cacheResult != nil {
			return cacheResult, nil
		}
		return nil, err
	}
	if result.IDs == nil || result.IDs.IsEmpty() {
		if cacheResult != nil {
			return cacheResult, nil
		}
		return result, nil
	}
	return result, nil
}

func (s *Service) enabledInstancesForContent(ctx context.Context, title string, contentType ContentType) ([]*models.ArrInstance, error) {
	arrType := s.getArrTypeForContent(contentType)
	if arrType == "" {
		return nil, nil
	}

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
	}

	return instances, nil
}

func (s *Service) lookupCache(ctx context.Context, titleHash, title string, contentType ContentType) (*ExternalIDsResult, error) {
	cacheEntry, err := s.cacheStore.Get(ctx, titleHash, string(contentType))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		log.Warn().Err(err).
			Str("title", title).
			Str("contentType", string(contentType)).
			Msg("[ARR-LOOKUP] Cache read error, proceeding as cache miss")
	}
	if err != nil || cacheEntry == nil {
		return nil, nil
	}
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
			Source:        "cache",
		}, nil
	}
	log.Debug().
		Str("title", title).
		Str("contentType", string(contentType)).
		Str("imdbId", cacheEntry.ExternalIDs.IMDbID).
		Int("tmdbId", cacheEntry.ExternalIDs.TMDbID).
		Int("tvdbId", cacheEntry.ExternalIDs.TVDbID).
		Int("tvmazeId", cacheEntry.ExternalIDs.TVMazeID).
		Int("titles", len(cacheEntry.Titles)).
		Strs("arrTitles", cacheEntry.Titles).
		Msg("[ARR-LOOKUP] Cache hit (positive)")

	return &ExternalIDsResult{
		IDs:           &cacheEntry.ExternalIDs,
		Titles:        cacheEntry.Titles,
		TitlesKnown:   cacheEntry.HasTitles,
		FromCache:     true,
		ArrInstanceID: cacheEntry.ArrInstanceID,
		ContentType:   contentType,
		Source:        "cache",
	}, nil
}

func (s *Service) lookupExternalIDsFromParse(ctx context.Context, titleHash, title string, contentType ContentType, instances []*models.ArrInstance, cacheNegative bool) (*ExternalIDsResult, error) {
	anyQueried := false
	// Parse mis-reads some names (e.g. a yearless title whose group tag ends in
	// digits parses with a bogus year), so when it yields no IDs, retry as a plain
	// title search against the instance's lookup endpoint. The rls year (when the
	// name carries one) disambiguates same-title remakes among lookup candidates.
	parsedRelease := rls.ParseString(title)
	lookupTerm := strings.TrimSpace(parsedRelease.Title)
	lookupYear := parsedRelease.Year
	for _, instance := range instances {
		client := s.clientForInstance(instance)
		if client == nil {
			continue
		}
		result, err := client.ParseTitleLookupResult(ctx, title)
		parseAnswered := err == nil
		if err != nil {
			log.Debug().Err(err).
				Int("instanceId", instance.ID).
				Str("instanceName", instance.Name).
				Str("title", title).
				Msg("[ARR-LOOKUP] Parse request failed")
			result = nil
		}

		if result != nil && result.IDs != nil && !result.IDs.IsEmpty() {
			return s.cacheAndBuildResult(ctx, titleHash, title, contentType, instance, result, "parse"), nil
		}

		// anyQueried gates negative caching and the total-failure error: an
		// instance counts as queried only when its lookup cycle reached an
		// authoritative empty answer. A parse-empty followed by a lookup error
		// is inconclusive — parse-empty is exactly the state the title-lookup
		// fallback exists to distrust.
		if lookupTerm != "" {
			lookupResult, lookupErr := client.LookupByTerm(ctx, lookupTerm, lookupYear)
			if lookupErr != nil {
				log.Debug().Err(lookupErr).
					Int("instanceId", instance.ID).
					Str("instanceName", instance.Name).
					Str("term", lookupTerm).
					Msg("[ARR-LOOKUP] Title lookup failed")
				continue
			}
			anyQueried = true
			if lookupResult != nil && lookupResult.IDs != nil && !lookupResult.IDs.IsEmpty() {
				return s.cacheAndBuildResult(ctx, titleHash, title, contentType, instance, lookupResult, "lookup"), nil
			}
		} else if parseAnswered {
			anyQueried = true
		}

		log.Debug().
			Int("instanceId", instance.ID).
			Str("instanceName", instance.Name).
			Str("title", title).
			Msg("[ARR-LOOKUP] No IDs returned from instance")
	}

	if !anyQueried {
		// Every instance failed before giving an authoritative answer: surface
		// the outage instead of a look-alike "no IDs" result, and cache nothing.
		return nil, fmt.Errorf("arr lookup: all %d instance(s) failed to answer", len(instances))
	}

	if cacheNegative {
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
		Source:      "none",
	}, nil
}

func (s *Service) cacheAndBuildResult(ctx context.Context, titleHash, title string, contentType ContentType, instance *models.ArrInstance, result *ExternalIDsLookupResult, source string) *ExternalIDsResult {
	instanceID := instance.ID
	titles := append([]string{}, result.Titles...)
	if err := s.cacheStore.SetWithTitles(ctx, titleHash, string(contentType), &instanceID, result.IDs, titles, false, s.positiveTTL); err != nil {
		log.Warn().Err(err).Msg("[ARR-LOOKUP] Failed to cache positive result")
	}

	log.Debug().
		Str("title", title).
		Int("instanceId", instance.ID).
		Str("instanceName", instance.Name).
		Str("source", source).
		Str("imdbId", result.IDs.IMDbID).
		Int("tmdbId", result.IDs.TMDbID).
		Int("tvdbId", result.IDs.TVDbID).
		Int("tvmazeId", result.IDs.TVMazeID).
		Int("titles", len(result.Titles)).
		Strs("arrTitles", result.Titles).
		Msg("[ARR-LOOKUP] IDs found")

	return &ExternalIDsResult{
		IDs:           result.IDs,
		Titles:        result.Titles,
		TitlesKnown:   true,
		FromCache:     false,
		ArrInstanceID: &instanceID,
		ContentType:   contentType,
		Source:        source,
	}
}

func (s *Service) clientForInstance(instance *models.ArrInstance) *Client {
	apiKey, err := s.instanceStore.GetDecryptedAPIKey(instance)
	if err != nil {
		log.Warn().Err(err).
			Int("instanceId", instance.ID).
			Str("instanceName", instance.Name).
			Msg("[ARR-LOOKUP] Failed to decrypt API key")
		return nil
	}

	var basicPass string
	if instance.BasicUsername != nil && *instance.BasicUsername != "" {
		basicPass, err = s.instanceStore.GetDecryptedBasicPassword(instance)
		if err != nil {
			log.Warn().Err(err).
				Int("instanceId", instance.ID).
				Str("instanceName", instance.Name).
				Msg("[ARR-LOOKUP] Failed to decrypt basic auth password")
			return nil
		}
	}
	basicPassPtr := &basicPass
	if basicPass == "" {
		basicPassPtr = nil
	}

	return NewClient(instance.BaseURL, apiKey, instance.BasicUsername, basicPassPtr, instance.Type, instance.TimeoutSeconds)
}

// LookupSeasonEpisodeTotal queries Sonarr instances for the episode count of a specific
// season, plus the show's alternate titles usable for that season. When the series
// resolves but the season's episode rows are unavailable (anime is often stored as one
// absolute-numbered season), it returns a partial result with TotalEpisodes 0 and the
// titles intact, so callers must check TotalEpisodes before trusting the count.
func (s *Service) LookupSeasonEpisodeTotal(ctx context.Context, title string, seasonNumber int) (*SeasonEpisodeTotalResult, error) {
	if title == "" {
		return nil, errors.New("title cannot be empty")
	}
	if seasonNumber <= 0 {
		return nil, nil
	}
	if s == nil || s.instanceStore == nil {
		return nil, nil
	}

	instances, err := s.instanceStore.ListEnabledByType(ctx, models.ArrInstanceTypeSonarr)
	if err != nil {
		return nil, err
	}
	if len(instances) == 0 {
		return nil, nil
	}

	var partial *SeasonEpisodeTotalResult
	for _, instance := range instances {
		client := s.clientForInstance(instance)
		if client == nil {
			continue
		}

		parseResp, err := client.ParseSonarrTitle(ctx, title)
		if err != nil {
			log.Debug().Err(err).
				Int("instanceId", instance.ID).
				Str("instanceName", instance.Name).
				Str("title", title).
				Msg("[ARR-LOOKUP] Sonarr parse request failed for season total")
			continue
		}
		if parseResp == nil || parseResp.Series == nil || parseResp.Series.ID <= 0 {
			continue
		}

		// Sonarr's /parse embeds the series WITHOUT alternateTitles: only the series
		// endpoints populate them (SeriesController.PopulateAlternateTitles, from scene
		// mappings). Hydrate via /series/{id} so alias matching sees them; on failure
		// fall back to the canonical title alone.
		series := parseResp.Series
		if full, hydrateErr := client.sonarrSeriesByID(ctx, series.ID); hydrateErr == nil && full != nil && full.ID > 0 {
			series = full
		} else if hydrateErr != nil {
			log.Debug().Err(hydrateErr).
				Int("instanceId", instance.ID).
				Str("instanceName", instance.Name).
				Str("title", title).
				Msg("[ARR-LOOKUP] Sonarr series hydration failed; alias matching limited to canonical title")
		}
		titles := seasonLookupTitles(series, title, seasonNumber)

		episodes, err := client.GetSonarrSeasonEpisodes(ctx, series.ID, seasonNumber)
		if err != nil {
			log.Debug().Err(err).
				Int("instanceId", instance.ID).
				Str("instanceName", instance.Name).
				Str("title", title).
				Int("seasonNumber", seasonNumber).
				Msg("[ARR-LOOKUP] Sonarr season episode lookup failed")
		}
		if err != nil || len(episodes) == 0 {
			// The series resolved but the season has no episode rows (common for anime
			// stored as one absolute-numbered season) or the call failed. Keep the alias
			// titles so the caller's metadata-total fallback can still alias-match.
			if partial == nil {
				instanceID := instance.ID
				partial = &SeasonEpisodeTotalResult{
					ArrInstanceID: &instanceID,
					Titles:        titles,
				}
			}
			continue
		}

		instanceID := instance.ID
		return &SeasonEpisodeTotalResult{
			TotalEpisodes: len(episodes),
			ArrInstanceID: &instanceID,
			Titles:        titles,
		}, nil
	}

	return partial, nil
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
