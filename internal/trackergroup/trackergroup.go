// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package trackergroup

import (
	"strings"

	"github.com/autobrr/qui/internal/models"
)

type Kind int

const (
	Public Kind = iota
	NoTracker
	Customization
	Domain
)

type Group struct {
	Kind Kind
	Name string
}

type Mapper struct {
	byDomain map[string]*models.TrackerCustomization
}

func New(customizations []*models.TrackerCustomization) *Mapper {
	byDomain := make(map[string]*models.TrackerCustomization)
	for _, custom := range customizations {
		for _, domain := range custom.Domains {
			if domain = normalize(domain); domain != "" {
				byDomain[domain] = custom
			}
		}
	}
	return &Mapper{byDomain: byDomain}
}

func (m *Mapper) Of(domains []string, private *bool) Group {
	if private != nil && !*private {
		return Group{Kind: Public, Name: "Public"}
	}
	stored := m.StoredDomain(domains)
	if stored == "" {
		return Group{Kind: NoTracker, Name: "No tracker"}
	}
	if custom := m.byDomain[normalize(stored)]; custom != nil {
		if name := strings.TrimSpace(custom.DisplayName); name != "" {
			return Group{Kind: Customization, Name: name}
		}
		return Group{Kind: Customization, Name: stored}
	}
	return Group{Kind: Domain, Name: stored}
}

func (m *Mapper) StoredDomain(domains []string) string {
	first, firstHeld := "", ""
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		if first == "" || domain < first {
			first = domain
		}
		if m.byDomain[normalize(domain)] != nil && (firstHeld == "" || domain < firstHeld) {
			firstHeld = domain
		}
	}
	if firstHeld != "" {
		return firstHeld
	}
	return first
}

func normalize(domain string) string {
	return strings.ToLower(strings.TrimSpace(domain))
}
