// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
)

func setupTrackerRuleHandler(t *testing.T) (*TrackerRuleHandler, *models.TrackerRuleStore, *models.InstanceStore, func()) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)

	store := models.NewTrackerRuleStore(db)

	encryptionKey := make([]byte, 32)
	for i := range encryptionKey {
		encryptionKey[i] = byte(i)
	}
	instanceStore, err := models.NewInstanceStore(db, encryptionKey)
	require.NoError(t, err)

	handler := NewTrackerRuleHandler(store, nil)

	cleanup := func() {
		require.NoError(t, db.Close())
	}

	return handler, store, instanceStore, cleanup
}

func TestNewTrackerRuleHandler(t *testing.T) {
	t.Parallel()

	handler, _, _, cleanup := setupTrackerRuleHandler(t)
	defer cleanup()

	require.NotNil(t, handler)
	assert.NotNil(t, handler.store)
}

func TestTrackerRuleHandler_List(t *testing.T) {
	t.Parallel()

	t.Run("returns empty list", func(t *testing.T) {
		t.Parallel()

		handler, _, instanceStore, cleanup := setupTrackerRuleHandler(t)
		defer cleanup()

		ctx := t.Context()

		// Create instance first
		instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false)
		require.NoError(t, err)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", strconv.Itoa(instance.ID))

		req := httptest.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("/api/instances/%d/tracker-rules", instance.ID), nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		resp := httptest.NewRecorder()

		handler.List(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)
		// Empty Go slices marshal to null, not []
		assert.Contains(t, []string{"[]\n", "null\n"}, resp.Body.String())
	})

	t.Run("returns rules for instance", func(t *testing.T) {
		t.Parallel()

		handler, store, instanceStore, cleanup := setupTrackerRuleHandler(t)
		defer cleanup()

		ctx := t.Context()

		// Create instance first
		instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false)
		require.NoError(t, err)

		// Create a rule
		rule := &models.TrackerRule{
			InstanceID:     instance.ID,
			Name:           "Test Rule",
			TrackerPattern: "tracker.example.com",
			Enabled:        true,
		}
		_, err = store.Create(ctx, rule)
		require.NoError(t, err)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", strconv.Itoa(instance.ID))

		req := httptest.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("/api/instances/%d/tracker-rules", instance.ID), nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		resp := httptest.NewRecorder()

		handler.List(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)
		assert.Contains(t, resp.Body.String(), "Test Rule")
		assert.Contains(t, resp.Body.String(), "tracker.example.com")
	})

	t.Run("invalid instance ID", func(t *testing.T) {
		t.Parallel()

		handler, _, _, cleanup := setupTrackerRuleHandler(t)
		defer cleanup()

		ctx := t.Context()
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", "invalid")

		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/instances/invalid/tracker-rules", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		resp := httptest.NewRecorder()

		handler.List(resp, req)

		assert.Equal(t, http.StatusBadRequest, resp.Code)
	})
}

func TestTrackerRuleHandler_Create(t *testing.T) {
	t.Parallel()

	t.Run("successful creation with pattern", func(t *testing.T) {
		t.Parallel()

		handler, _, instanceStore, cleanup := setupTrackerRuleHandler(t)
		defer cleanup()

		ctx := t.Context()

		// Create instance first
		instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false)
		require.NoError(t, err)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", strconv.Itoa(instance.ID))

		body := strings.NewReader(`{
			"name": "New Rule",
			"trackerPattern": "*.example.com"
		}`)
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("/api/instances/%d/tracker-rules", instance.ID), body)
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		resp := httptest.NewRecorder()

		handler.Create(resp, req)

		assert.Equal(t, http.StatusCreated, resp.Code)
		assert.Contains(t, resp.Body.String(), "New Rule")
	})

	t.Run("successful creation with domains", func(t *testing.T) {
		t.Parallel()

		handler, _, instanceStore, cleanup := setupTrackerRuleHandler(t)
		defer cleanup()

		ctx := t.Context()

		// Create instance first
		instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false)
		require.NoError(t, err)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", strconv.Itoa(instance.ID))

		body := strings.NewReader(`{
			"name": "Domain Rule",
			"trackerDomains": ["tracker1.example.com", "tracker2.example.com"]
		}`)
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("/api/instances/%d/tracker-rules", instance.ID), body)
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		resp := httptest.NewRecorder()

		handler.Create(resp, req)

		assert.Equal(t, http.StatusCreated, resp.Code)
		assert.Contains(t, resp.Body.String(), "Domain Rule")
	})

	t.Run("missing name", func(t *testing.T) {
		t.Parallel()

		handler, _, instanceStore, cleanup := setupTrackerRuleHandler(t)
		defer cleanup()

		ctx := t.Context()

		// Create instance first
		instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false)
		require.NoError(t, err)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", strconv.Itoa(instance.ID))

		body := strings.NewReader(`{
			"trackerPattern": "*.example.com"
		}`)
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("/api/instances/%d/tracker-rules", instance.ID), body)
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		resp := httptest.NewRecorder()

		handler.Create(resp, req)

		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assert.Contains(t, resp.Body.String(), "Name is required")
	})

	t.Run("missing tracker pattern and domains", func(t *testing.T) {
		t.Parallel()

		handler, _, instanceStore, cleanup := setupTrackerRuleHandler(t)
		defer cleanup()

		ctx := t.Context()

		// Create instance first
		instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false)
		require.NoError(t, err)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", strconv.Itoa(instance.ID))

		body := strings.NewReader(`{
			"name": "Empty Rule"
		}`)
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("/api/instances/%d/tracker-rules", instance.ID), body)
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		resp := httptest.NewRecorder()

		handler.Create(resp, req)

		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assert.Contains(t, resp.Body.String(), "Select at least one tracker")
	})

	t.Run("with optional fields", func(t *testing.T) {
		t.Parallel()

		handler, _, instanceStore, cleanup := setupTrackerRuleHandler(t)
		defer cleanup()

		ctx := t.Context()

		// Create instance first
		instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false)
		require.NoError(t, err)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", strconv.Itoa(instance.ID))

		body := strings.NewReader(`{
			"name": "Full Rule",
			"trackerPattern": "tracker.example.com",
			"category": "movies",
			"tag": "private",
			"uploadLimitKiB": 1024,
			"downloadLimitKiB": 2048,
			"ratioLimit": 2.5,
			"seedingTimeLimitMinutes": 1440,
			"enabled": true
		}`)
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("/api/instances/%d/tracker-rules", instance.ID), body)
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		resp := httptest.NewRecorder()

		handler.Create(resp, req)

		assert.Equal(t, http.StatusCreated, resp.Code)
		assert.Contains(t, resp.Body.String(), "Full Rule")
		assert.Contains(t, resp.Body.String(), "movies")
		assert.Contains(t, resp.Body.String(), "private")
	})
}

func TestTrackerRuleHandler_Update(t *testing.T) {
	t.Parallel()

	t.Run("successful update", func(t *testing.T) {
		t.Parallel()

		handler, store, instanceStore, cleanup := setupTrackerRuleHandler(t)
		defer cleanup()

		ctx := t.Context()

		// Create instance first
		instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false)
		require.NoError(t, err)

		// Create a rule first
		rule := &models.TrackerRule{
			InstanceID:     instance.ID,
			Name:           "Original Name",
			TrackerPattern: "original.example.com",
			Enabled:        true,
		}
		created, err := store.Create(ctx, rule)
		require.NoError(t, err)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", strconv.Itoa(instance.ID))
		rctx.URLParams.Add("ruleID", strconv.Itoa(created.ID))

		body := strings.NewReader(`{
			"name": "Updated Name",
			"trackerPattern": "updated.example.com"
		}`)
		req := httptest.NewRequestWithContext(ctx, http.MethodPut, fmt.Sprintf("/api/instances/%d/tracker-rules/%d", instance.ID, created.ID), body)
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		resp := httptest.NewRecorder()

		handler.Update(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)
		assert.Contains(t, resp.Body.String(), "Updated Name")
		assert.Contains(t, resp.Body.String(), "updated.example.com")
	})

	t.Run("rule not found", func(t *testing.T) {
		t.Parallel()

		handler, _, instanceStore, cleanup := setupTrackerRuleHandler(t)
		defer cleanup()

		ctx := t.Context()

		// Create instance first
		instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false)
		require.NoError(t, err)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", strconv.Itoa(instance.ID))
		rctx.URLParams.Add("ruleID", "999")

		body := strings.NewReader(`{
			"name": "Updated Name",
			"trackerPattern": "updated.example.com"
		}`)
		req := httptest.NewRequestWithContext(ctx, http.MethodPut, fmt.Sprintf("/api/instances/%d/tracker-rules/999", instance.ID), body)
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		resp := httptest.NewRecorder()

		handler.Update(resp, req)

		assert.Equal(t, http.StatusNotFound, resp.Code)
	})

	t.Run("missing name", func(t *testing.T) {
		t.Parallel()

		handler, store, instanceStore, cleanup := setupTrackerRuleHandler(t)
		defer cleanup()

		ctx := t.Context()

		// Create instance first
		instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false)
		require.NoError(t, err)

		// Create a rule first
		rule := &models.TrackerRule{
			InstanceID:     instance.ID,
			Name:           "Original Name",
			TrackerPattern: "original.example.com",
			Enabled:        true,
		}
		created, err := store.Create(ctx, rule)
		require.NoError(t, err)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", strconv.Itoa(instance.ID))
		rctx.URLParams.Add("ruleID", strconv.Itoa(created.ID))

		body := strings.NewReader(`{
			"trackerPattern": "updated.example.com"
		}`)
		req := httptest.NewRequestWithContext(ctx, http.MethodPut, fmt.Sprintf("/api/instances/%d/tracker-rules/%d", instance.ID, created.ID), body)
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		resp := httptest.NewRecorder()

		handler.Update(resp, req)

		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assert.Contains(t, resp.Body.String(), "Name is required")
	})
}

func TestTrackerRuleHandler_Delete(t *testing.T) {
	t.Parallel()

	t.Run("successful deletion", func(t *testing.T) {
		t.Parallel()

		handler, store, instanceStore, cleanup := setupTrackerRuleHandler(t)
		defer cleanup()

		ctx := t.Context()

		// Create instance first
		instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false)
		require.NoError(t, err)

		// Create a rule first
		rule := &models.TrackerRule{
			InstanceID:     instance.ID,
			Name:           "To Delete",
			TrackerPattern: "delete.example.com",
			Enabled:        true,
		}
		created, err := store.Create(ctx, rule)
		require.NoError(t, err)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", strconv.Itoa(instance.ID))
		rctx.URLParams.Add("ruleID", strconv.Itoa(created.ID))

		req := httptest.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("/api/instances/%d/tracker-rules/%d", instance.ID, created.ID), nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		resp := httptest.NewRecorder()

		handler.Delete(resp, req)

		assert.Equal(t, http.StatusNoContent, resp.Code)
	})

	t.Run("rule not found", func(t *testing.T) {
		t.Parallel()

		handler, _, instanceStore, cleanup := setupTrackerRuleHandler(t)
		defer cleanup()

		ctx := t.Context()

		// Create instance first
		instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false)
		require.NoError(t, err)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", strconv.Itoa(instance.ID))
		rctx.URLParams.Add("ruleID", "999")

		req := httptest.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("/api/instances/%d/tracker-rules/999", instance.ID), nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		resp := httptest.NewRecorder()

		handler.Delete(resp, req)

		assert.Equal(t, http.StatusNotFound, resp.Code)
	})
}

func TestTrackerRuleHandler_Reorder(t *testing.T) {
	t.Parallel()

	t.Run("successful reorder", func(t *testing.T) {
		t.Parallel()

		handler, store, instanceStore, cleanup := setupTrackerRuleHandler(t)
		defer cleanup()

		ctx := t.Context()

		// Create instance first
		instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false)
		require.NoError(t, err)

		// Create multiple rules
		rule1 := &models.TrackerRule{
			InstanceID:     instance.ID,
			Name:           "Rule 1",
			TrackerPattern: "tracker1.example.com",
			Enabled:        true,
		}
		created1, err := store.Create(ctx, rule1)
		require.NoError(t, err)

		rule2 := &models.TrackerRule{
			InstanceID:     instance.ID,
			Name:           "Rule 2",
			TrackerPattern: "tracker2.example.com",
			Enabled:        true,
		}
		created2, err := store.Create(ctx, rule2)
		require.NoError(t, err)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", strconv.Itoa(instance.ID))

		// Reorder: rule2 first, then rule1
		body := strings.NewReader(fmt.Sprintf(`{"orderedIds": [%d, %d]}`, created2.ID, created1.ID))
		req := httptest.NewRequestWithContext(ctx, http.MethodPut, fmt.Sprintf("/api/instances/%d/tracker-rules/reorder", instance.ID), body)
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		resp := httptest.NewRecorder()

		handler.Reorder(resp, req)

		assert.Equal(t, http.StatusNoContent, resp.Code)
	})

	t.Run("empty ordered ids", func(t *testing.T) {
		t.Parallel()

		handler, _, instanceStore, cleanup := setupTrackerRuleHandler(t)
		defer cleanup()

		ctx := t.Context()

		// Create instance first
		instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false)
		require.NoError(t, err)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", strconv.Itoa(instance.ID))

		body := strings.NewReader(`{"orderedIds": []}`)
		req := httptest.NewRequestWithContext(ctx, http.MethodPut, fmt.Sprintf("/api/instances/%d/tracker-rules/reorder", instance.ID), body)
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		resp := httptest.NewRecorder()

		handler.Reorder(resp, req)

		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assert.Contains(t, resp.Body.String(), "Invalid request payload")
	})
}

func TestTrackerRuleHandler_ApplyNow(t *testing.T) {
	t.Parallel()

	t.Run("successful apply without service", func(t *testing.T) {
		t.Parallel()

		handler, _, instanceStore, cleanup := setupTrackerRuleHandler(t)
		defer cleanup()

		ctx := t.Context()

		// Create instance first
		instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false)
		require.NoError(t, err)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", strconv.Itoa(instance.ID))

		req := httptest.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("/api/instances/%d/tracker-rules/apply", instance.ID), nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		resp := httptest.NewRecorder()

		handler.ApplyNow(resp, req)

		assert.Equal(t, http.StatusAccepted, resp.Code)
		assert.Contains(t, resp.Body.String(), "applied")
	})
}

func TestCleanStringPtr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input *string
		want  *string
	}{
		{
			name:  "nil input",
			input: nil,
			want:  nil,
		},
		{
			name:  "empty string",
			input: stringPtr(""),
			want:  nil,
		},
		{
			name:  "whitespace only",
			input: stringPtr("   "),
			want:  nil,
		},
		{
			name:  "valid string",
			input: stringPtr("value"),
			want:  stringPtr("value"),
		},
		{
			name:  "string with whitespace",
			input: stringPtr("  value  "),
			want:  stringPtr("value"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := cleanStringPtr(tt.input)
			if tt.want == nil {
				assert.Nil(t, got)
			} else {
				require.NotNil(t, got)
				assert.Equal(t, *tt.want, *got)
			}
		})
	}
}

func TestNormalizeTrackerDomains(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  []string
		expect []string
	}{
		{
			name:   "empty slice",
			input:  []string{},
			expect: nil,
		},
		{
			name:   "nil slice",
			input:  nil,
			expect: nil,
		},
		{
			name:   "single domain",
			input:  []string{"tracker.example.com"},
			expect: []string{"tracker.example.com"},
		},
		{
			name:   "multiple domains",
			input:  []string{"tracker1.com", "tracker2.com"},
			expect: []string{"tracker1.com", "tracker2.com"},
		},
		{
			name:   "removes duplicates",
			input:  []string{"tracker.com", "tracker.com"},
			expect: []string{"tracker.com"},
		},
		{
			name:   "trims whitespace",
			input:  []string{"  tracker.com  "},
			expect: []string{"tracker.com"},
		},
		{
			name:   "removes empty strings",
			input:  []string{"tracker.com", "", "  "},
			expect: []string{"tracker.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeTrackerDomains(tt.input)
			assert.Equal(t, tt.expect, got)
		})
	}
}
