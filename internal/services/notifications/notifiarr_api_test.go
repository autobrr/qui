// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package notifications

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateNotifiarrAPIKeySkipsNonNotifiarrAPI(t *testing.T) {
	t.Parallel()

	err := ValidateNotifiarrAPIKey(context.Background(), "discord://token@channel")
	require.NoError(t, err)
}

func TestValidateNotifiarrAPIKeyValid(t *testing.T) {
	t.Parallel()

	var (
		hits    int
		gotKey  string
		gotPath string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		gotKey = r.Header.Get("X-API-Key")
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	endpoint := fmt.Sprintf("%s/api/v1/notification/qui", server.URL)
	rawURL := fmt.Sprintf("notifiarrapi://abc123?endpoint=%s", url.QueryEscape(endpoint))

	err := ValidateNotifiarrAPIKey(context.Background(), rawURL)
	require.NoError(t, err)
	require.Equal(t, 1, hits)
	require.Equal(t, "abc123", gotKey)
	require.Equal(t, "/api/v1/user/validate", gotPath)
}

func TestValidateNotifiarrAPIKeyInvalid(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid key"))
	}))
	t.Cleanup(server.Close)

	endpoint := fmt.Sprintf("%s/api/v1/notification/qui", server.URL)
	rawURL := fmt.Sprintf("notifiarrapi://abc123?endpoint=%s", url.QueryEscape(endpoint))

	err := ValidateNotifiarrAPIKey(context.Background(), rawURL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "notifiarr api key invalid")
	require.Contains(t, err.Error(), "invalid key")
}
