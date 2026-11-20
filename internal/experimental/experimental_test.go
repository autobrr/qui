// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package experimental_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/experimental"
)

const (
	testAlpha experimental.Feature = "testAlpha"
	testBravo experimental.Feature = "testBravo"
)

var testKnown = []experimental.Feature{testAlpha, testBravo}

func captureLogs(t *testing.T) *strings.Builder {
	t.Helper()
	var logs strings.Builder
	previous := log.Logger
	log.Logger = zerolog.New(&logs)
	t.Cleanup(func() { log.Logger = previous })
	return &logs
}

func TestNamesFromConfigReachThePackage(t *testing.T) {
	tests := []struct {
		name string
		file string
		env  string
	}{
		{name: "config_file", file: `experimentalFeatures = ["testBravo"]`},
		{name: "env_trims_spaces_around_each_name", env: " testBravo , nothing "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file+"\n"), 0o600))
			if tt.env != "" {
				t.Setenv("QUI__EXPERIMENTAL_FEATURES", tt.env)
			}

			cfg, err := config.New(path, "test")
			require.NoError(t, err)
			features := experimental.NewWithKnown(testKnown, cfg.Config.ExperimentalFeatures)

			assert.True(t, features.Enabled(testBravo))
			assert.False(t, features.Enabled(testAlpha))
		})
	}
}

func TestUnknownNameLogsOneWarning(t *testing.T) {
	logs := captureLogs(t)

	features := experimental.NewWithKnown(testKnown, []string{"testAlpha", "nothing", "TestBeta", "nothing"})

	assert.Equal(t, []string{"testAlpha"}, features.Names(), "names match exactly, and case matters")
	assert.Equal(t, 1, strings.Count(logs.String(), `"name":"nothing"`), logs.String())
	assert.Equal(t, 1, strings.Count(logs.String(), `"name":"TestBeta"`), logs.String())
	assert.Equal(t, 1, strings.Count(logs.String(), `"level":"info"`), logs.String())
	assert.Contains(t, logs.String(), `"experimentalFeatures":["testAlpha"]`)
}

func TestNoInfoLineWhenNoFeatureIsOn(t *testing.T) {
	logs := captureLogs(t)

	features := experimental.NewWithKnown(testKnown, nil)

	assert.Empty(t, logs.String())
	assert.Equal(t, []string{}, features.Names())
}

func TestRequireGuardsRouteGroup(t *testing.T) {
	tests := []struct {
		name       string
		configured []string
		wantStatus int
	}{
		{name: "off_answers_404", configured: nil, wantStatus: http.StatusNotFound},
		{name: "on_reaches_handler", configured: []string{"testAlpha"}, wantStatus: http.StatusTeapot},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			features := experimental.NewWithKnown(testKnown, tt.configured)
			router := chi.NewRouter()
			router.Group(func(r chi.Router) {
				r.Use(features.Require(testAlpha))
				r.Get("/api/alpha", func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusTeapot)
				})
			})

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/alpha", nil))

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}
