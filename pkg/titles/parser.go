// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package titles

import (
	"context"
	"time"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	"github.com/moistari/rls"
)

// ParsedTitle represents parsed title information from a release name
type ParsedTitle struct {
	// Parsed release information
	Type        string   `json:"type"`
	Artist      string   `json:"artist,omitempty"`
	Title       string   `json:"title,omitempty"`
	Subtitle    string   `json:"subtitle,omitempty"`
	Alt         string   `json:"alt,omitempty"`
	Platform    string   `json:"platform,omitempty"`
	Arch        string   `json:"arch,omitempty"`
	Source      string   `json:"source,omitempty"`
	Resolution  string   `json:"resolution,omitempty"`
	Collection  string   `json:"collection,omitempty"`
	Year        int      `json:"year,omitempty"`
	Month       int      `json:"month,omitempty"`
	Day         int      `json:"day,omitempty"`
	Series      int      `json:"series,omitempty"`
	Episode     int      `json:"episode,omitempty"`
	Version     string   `json:"version,omitempty"`
	Disc        string   `json:"disc,omitempty"`
	Codec       []string `json:"codec,omitempty"`
	HDR         []string `json:"hdr,omitempty"`
	Audio       []string `json:"audio,omitempty"`
	Channels    string   `json:"channels,omitempty"`
	Other       []string `json:"other,omitempty"`
	Cut         []string `json:"cut,omitempty"`
	Edition     []string `json:"edition,omitempty"`
	Language    []string `json:"language,omitempty"`
	ReleaseSize string   `json:"releaseSize,omitempty"`
	Region      string   `json:"region,omitempty"`
	Container   string   `json:"container,omitempty"`
	Genre       string   `json:"genre,omitempty"`
	ID          string   `json:"id,omitempty"`
	Group       string   `json:"group,omitempty"`
	Meta        []string `json:"meta,omitempty"`
	Site        string   `json:"site,omitempty"`
	Sum         string   `json:"sum,omitempty"`
	Pass        string   `json:"pass,omitempty"`
	Req         bool     `json:"req,omitempty"`
	Ext         string   `json:"ext,omitempty"`
}

// Parser handles parsing of torrent titles with caching
type Parser struct {
	cache *ttlcache.Cache[string, ParsedTitle]
}

// NewParser creates a new title parser with TTL cache
func NewParser() *Parser {
	return &Parser{
		cache: ttlcache.New(ttlcache.Options[string, ParsedTitle]{}.SetDefaultTTL(5 * time.Minute)),
	}
}

// ParseTitles parses a list of torrent names and returns parsed title information
func (p *Parser) ParseTitles(ctx context.Context, names []string) []ParsedTitle {
	var result []ParsedTitle

	for _, name := range names {
		if name == "" {
			continue
		}

		// Check cache first
		if cached, found := p.cache.Get(name); found {
			result = append(result, cached)
			continue
		}

		// Parse the torrent name using rls
		release := rls.ParseString(name)

		parsed := ParsedTitle{
			// Parsed fields from rls
			Type:        release.Type.String(),
			Artist:      release.Artist,
			Title:       release.Title,
			Subtitle:    release.Subtitle,
			Alt:         release.Alt,
			Platform:    release.Platform,
			Arch:        release.Arch,
			Source:      release.Source,
			Resolution:  release.Resolution,
			Collection:  release.Collection,
			Year:        release.Year,
			Month:       release.Month,
			Day:         release.Day,
			Series:      release.Series,
			Episode:     release.Episode,
			Version:     release.Version,
			Disc:        release.Disc,
			Codec:       release.Codec,
			HDR:         release.HDR,
			Audio:       release.Audio,
			Channels:    release.Channels,
			Other:       release.Other,
			Cut:         release.Cut,
			Edition:     release.Edition,
			Language:    release.Language,
			ReleaseSize: release.Size,
			Region:      release.Region,
			Container:   release.Container,
			Genre:       release.Genre,
			ID:          release.ID,
			Group:       release.Group,
			Meta:        release.Meta,
			Site:        release.Site,
			Sum:         release.Sum,
			Pass:        release.Pass,
			Req:         release.Req,
			Ext:         release.Ext,
		}

		// Cache the parsed title
		p.cache.Set(name, parsed, ttlcache.DefaultTTL)

		result = append(result, parsed)
	}

	return result
}
