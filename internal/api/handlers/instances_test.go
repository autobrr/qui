// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestGetInstanceCapabilitiesPreservesBackoffMessage(t *testing.T) {
	qbtServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer qbtServer.Close()

	db := testdb.NewMigratedSQLite(t, "instance-capabilities-backoff")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	errorStore := models.NewInstanceErrorStore(db)
	clientPool, err := internalqbittorrent.NewClientPool(instanceStore, errorStore, 60*time.Second)
	require.NoError(t, err)
	defer clientPool.Close()

	instance, err := instanceStore.Create(
		context.Background(),
		"blocked",
		qbtServer.URL,
		"admin",
		"password",
		nil,
		nil,
		false,
		nil,
	)
	require.NoError(t, err)

	handler := NewInstancesHandler(instanceStore, nil, nil, clientPool, nil, nil)
	router := chi.NewRouter()
	router.Get("/api/instances/{instanceID}/capabilities", handler.GetInstanceCapabilities)

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequestWithContext(t.Context(), http.MethodGet, fmt.Sprintf("/api/instances/%d/capabilities", instance.ID), nil))
	require.Equal(t, http.StatusServiceUnavailable, first.Code)

	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequestWithContext(t.Context(), http.MethodGet, fmt.Sprintf("/api/instances/%d/capabilities", instance.ID), nil))
	require.Equal(t, http.StatusServiceUnavailable, second.Code)

	var body ErrorResponse
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &body))
	require.Contains(t, body.Error, "health-check backoff")
	require.Contains(t, body.Error, "retrying in")
	require.NotEqual(t, "Failed to load instance capabilities", body.Error)
}

func TestInstanceCapabilitiesClientErrorMessagePreservesHealthBlocker(t *testing.T) {
	err := fmt.Errorf("failed to get client: %w", &internalqbittorrent.InstanceHealthBlockerError{
		Kind:       internalqbittorrent.InstanceHealthBlockerBackoff,
		InstanceID: 7,
		RetryAfter: 12 * time.Second,
	})

	message := instanceCapabilitiesClientErrorMessage(err)

	require.Contains(t, message, "health-check backoff")
	require.Contains(t, message, "retrying in 12s")
	require.NotEqual(t, "Failed to load instance capabilities", message)
}

func TestInstanceCapabilitiesClientErrorMessagePreservesInFlightProbe(t *testing.T) {
	err := fmt.Errorf("failed to get client: %w", &internalqbittorrent.InstanceHealthBlockerError{
		Kind:       internalqbittorrent.InstanceHealthBlockerHealthCheckInProgress,
		InstanceID: 7,
	})

	message := instanceCapabilitiesClientErrorMessage(err)

	require.Contains(t, message, "already running a health check")
	require.Contains(t, message, "retry shortly")
	require.NotEqual(t, "Failed to load instance capabilities", message)
}

func TestInstanceCapabilitiesClientErrorMessageFallsBack(t *testing.T) {
	require.Equal(t, "Failed to load instance capabilities", instanceCapabilitiesClientErrorMessage(errors.New("boom")))
}
