// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package problemreport owns the outbound path of Problem reports: it builds
// the facts that qui's server adds, removes keys and secrets, keeps the
// Install key and the sign-in, sends each report to brrbin, and stores Sent
// reports. See ADR 0058.
package problemreport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/systemnotice"
	"github.com/autobrr/qui/pkg/version"
)

const (
	StartedFromMenu   = "menu"
	StartedFromNotice = "notice"
	StartedFromSearch = "search"
)

const (
	LogScopeWarnings = "warnings"
	LogScopeAll      = "all"
	LogScopeNone     = "none"
)

const warningLines = 200

var ErrInvalidRequest = errors.New("invalid problem report request")

type QuiFacts struct {
	Version              string        `json:"version"`
	Commit               string        `json:"commit,omitempty"`
	StartedAt            time.Time     `json:"startedAt"`
	NewerRelease         *NewerRelease `json:"newerRelease,omitempty"`
	OS                   string        `json:"os"`
	Arch                 string        `json:"arch"`
	AppContainer         bool          `json:"appContainer"`
	Database             string        `json:"database"`
	LogLevel             string        `json:"logLevel"`
	ExperimentalFeatures []string      `json:"experimentalFeatures"`
}

type NewerRelease struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

type LogFacts struct {
	Warnings []string `json:"warnings"`
	All      []string `json:"all"`
}

type Facts struct {
	Qui         QuiFacts                          `json:"qui"`
	Instances   []qbittorrent.InstanceReportFacts `json:"instances"`
	Notices     []systemnotice.Notice             `json:"notices"`
	Logs        LogFacts                          `json:"logs"`
	MatchPolicy *crossseed.ReportMatchPolicy      `json:"matchPolicy,omitempty"`
	SearchScope *crossseed.ReportSearchScope      `json:"searchScope,omitempty"`
}

type FactsRequest struct {
	StartedFrom string
	InstanceID  int
	Hash        string
	Search      crossseed.TorrentSearchOptions
}

type InstanceLister interface {
	List(ctx context.Context) ([]*models.Instance, error)
}

type InstanceFactsReader interface {
	ReportFacts(instanceID int, active bool) qbittorrent.InstanceReportFacts
}

type NoticeLister interface {
	List() []systemnotice.Notice
}

type LogReader interface {
	HistoryFrom(minLevel zerolog.Level, n int) []string
}

type ReleaseReader interface {
	GetLatestRelease(ctx context.Context) *version.Release
}

type SearchFactsReader interface {
	SearchReportFacts(ctx context.Context, instanceID int, hash string, opts crossseed.TorrentSearchOptions) (*crossseed.SearchReportFacts, error)
}

type SecretReader interface {
	ReportSecrets(ctx context.Context) (Secrets, error)
}

type Sources struct {
	// Qui returns the facts about qui, except the newer release.
	Qui       func() QuiFacts
	Instances InstanceLister
	Pool      InstanceFactsReader
	Notices   NoticeLister
	Logs      LogReader
	Releases  ReleaseReader
	// Search is nil when cross-seed is not set up.
	Search  SearchFactsReader
	Secrets SecretReader
}

type Service struct {
	store   *models.ProblemReportStore
	baseURL string
	client  *http.Client
	sources Sources
	now     func() time.Time

	signInMu sync.Mutex
	signIn   *signInAttempt
}

func NewService(baseURL string, client *http.Client, store *models.ProblemReportStore, sources Sources) *Service {
	return &Service{store: store, baseURL: baseURL, client: client, sources: sources, now: time.Now}
}

// Facts returns the facts for a starting point, with every text part
// cleaned. It sends nothing to brrbin. For a search it reads the torrent from
// qBittorrent, as the manual search does.
func (s *Service) Facts(ctx context.Context, req FactsRequest) (*Facts, error) {
	switch req.StartedFrom {
	case StartedFromMenu, StartedFromNotice:
	case StartedFromSearch:
		if req.InstanceID <= 0 || req.Hash == "" {
			return nil, fmt.Errorf("%w: a search report needs instanceId and hash", ErrInvalidRequest)
		}
		if s.sources.Search == nil {
			return nil, fmt.Errorf("%w: cross-seed is not set up", ErrInvalidRequest)
		}
	default:
		return nil, fmt.Errorf("%w: startedFrom must be menu, notice or search", ErrInvalidRequest)
	}

	c, err := s.cleaner(ctx)
	if err != nil {
		return nil, err
	}

	facts := &Facts{
		Qui:       s.sources.Qui(),
		Instances: []qbittorrent.InstanceReportFacts{},
		Notices:   s.sources.Notices.List(),
		Logs:      s.logFacts(c),
	}
	if release := s.sources.Releases.GetLatestRelease(ctx); release != nil {
		facts.Qui.NewerRelease = &NewerRelease{Version: release.TagName, URL: release.HTMLURL}
	}
	for i := range facts.Notices {
		facts.Notices[i].Cause = c.clean(facts.Notices[i].Cause)
	}

	instances, err := s.sources.Instances.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}
	for _, instance := range instances {
		facts.Instances = append(facts.Instances, s.sources.Pool.ReportFacts(instance.ID, instance.IsActive))
	}

	if req.StartedFrom == StartedFromSearch {
		search, err := s.sources.Search.SearchReportFacts(ctx, req.InstanceID, req.Hash, req.Search)
		if err != nil {
			return nil, fmt.Errorf("read the search facts: %w", err)
		}
		for _, indexers := range [][]crossseed.ReportIndexer{search.SearchScope.TorznabIndexers, search.SearchScope.TorznabSkipped} {
			for i := range indexers {
				indexers[i].Name = c.clean(indexers[i].Name)
			}
		}
		facts.MatchPolicy = &search.MatchPolicy
		facts.SearchScope = &search.SearchScope
	}
	return facts, nil
}

// logFacts cleans each line of the log buffer once. The warning lines are a
// part of the buffer, so they reuse the cleaned lines.
func (s *Service) logFacts(c cleaner) LogFacts {
	all := s.sources.Logs.HistoryFrom(zerolog.TraceLevel, 0)
	cleaned := make(map[string]string, len(all))
	logs := LogFacts{All: make([]string, len(all))}
	for i, line := range all {
		logs.All[i] = c.clean(line)
		cleaned[line] = logs.All[i]
	}
	warnings := s.sources.Logs.HistoryFrom(zerolog.WarnLevel, warningLines)
	logs.Warnings = make([]string, len(warnings))
	for i, line := range warnings {
		clean, ok := cleaned[line]
		if !ok {
			clean = c.clean(line) // written after the first read
		}
		logs.Warnings[i] = clean
	}
	return logs
}

func (s *Service) cleaner(ctx context.Context) (cleaner, error) {
	secrets, err := s.sources.Secrets.ReportSecrets(ctx)
	if err != nil {
		return cleaner{}, fmt.Errorf("read the secrets to remove: %w", err)
	}
	return newCleaner(secrets), nil
}
