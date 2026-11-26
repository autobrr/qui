// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/autobrr/qui/internal/models"
)

func TestCanMatchTorrents(t *testing.T) {
	tests := []struct {
		name     string
		settings *models.InstanceReannounceSettings
		want     bool
	}{
		{name: "Nil", settings: nil, want: false},
		{name: "Disabled", settings: &models.InstanceReannounceSettings{MonitorAll: true}, want: false},
		{name: "Empty scope", settings: &models.InstanceReannounceSettings{Enabled: true}, want: false},
		{name: "Exclusions only", settings: &models.InstanceReannounceSettings{
			Enabled:           true,
			ExcludeCategories: true,
			Categories:        []string{"tv"},
		}, want: false},
		{name: "MonitorAll", settings: &models.InstanceReannounceSettings{Enabled: true, MonitorAll: true}, want: true},
		{name: "Category include", settings: &models.InstanceReannounceSettings{Enabled: true, Categories: []string{"tv"}}, want: true},
		{name: "Tag include", settings: &models.InstanceReannounceSettings{Enabled: true, Tags: []string{"tagA"}}, want: true},
		{name: "Tracker include", settings: &models.InstanceReannounceSettings{Enabled: true, Trackers: []string{"tracker.example.com"}}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.settings.CanMatchTorrents())
		})
	}
}
