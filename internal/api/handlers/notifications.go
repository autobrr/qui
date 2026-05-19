// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/notifications"
)

type NotificationsHandler struct {
	store   *models.NotificationTargetStore
	service *notifications.Service
}

func NewNotificationsHandler(store *models.NotificationTargetStore, service *notifications.Service) *NotificationsHandler {
	return &NotificationsHandler{
		store:   store,
		service: service,
	}
}

type notificationTargetRequest struct {
	Name       string    `json:"name"`
	URL        string    `json:"url"`
	Enabled    *bool     `json:"enabled"`
	EventTypes *[]string `json:"eventTypes"`
}

type notificationTestRequest struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

const maxNotificationBodySize = 1 << 20

// ListEvents handles GET /api/notifications/events
func (h *NotificationsHandler) ListEvents(w http.ResponseWriter, _ *http.Request) {
	RespondJSON(w, http.StatusOK, notifications.EventDefinitions())
}

// ListTargets handles GET /api/notifications/targets
func (h *NotificationsHandler) ListTargets(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		RespondError(w, http.StatusInternalServerError, "notification store unavailable")
		return
	}

	targets, err := h.store.List(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("notifications: failed to list targets")
		RespondError(w, http.StatusInternalServerError, "failed to list notification targets")
		return
	}

	RespondJSON(w, http.StatusOK, targets)
}

// CreateTarget handles POST /api/notifications/targets
func (h *NotificationsHandler) CreateTarget(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		RespondError(w, http.StatusInternalServerError, "notification store unavailable")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxNotificationBodySize)
	dec := json.NewDecoder(r.Body)

	var req notificationTargetRequest
	if err := dec.Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		RespondError(w, http.StatusBadRequest, "name is required")
		return
	}

	url := strings.TrimSpace(req.URL)
	if url == "" {
		RespondError(w, http.StatusBadRequest, "url is required")
		return
	}

	if err := validateNotificationURL(r.Context(), url); err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	var eventTypeInput []string
	if req.EventTypes != nil {
		eventTypeInput = *req.EventTypes
	}
	eventTypes, err := notifications.NormalizeEventTypes(eventTypeInput)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(eventTypes) == 0 {
		eventTypes = notifications.AllEventTypeStrings()
	}

	created, err := h.store.Create(r.Context(), &models.NotificationTargetCreate{
		Name:       name,
		URL:        url,
		Enabled:    enabled,
		EventTypes: eventTypes,
	})
	if err != nil {
		log.Error().Err(err).Msg("notifications: failed to create target")
		RespondError(w, http.StatusInternalServerError, "failed to create notification target")
		return
	}

	RespondJSON(w, http.StatusCreated, created)
}

// UpdateTarget handles PUT /api/notifications/targets/{id}
func (h *NotificationsHandler) UpdateTarget(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		RespondError(w, http.StatusInternalServerError, "notification store unavailable")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "invalid target id")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxNotificationBodySize)
	dec := json.NewDecoder(r.Body)

	var req notificationTargetRequest
	if err := dec.Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		RespondError(w, http.StatusBadRequest, "name is required")
		return
	}

	url := strings.TrimSpace(req.URL)
	if url == "" {
		RespondError(w, http.StatusBadRequest, "url is required")
		return
	}

	if err := validateNotificationURL(r.Context(), url); err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	existing, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, models.ErrNotificationTargetNotFound) {
			RespondError(w, http.StatusNotFound, "notification target not found")
			return
		}
		log.Error().Err(err).Msg("notifications: failed to load target")
		RespondError(w, http.StatusInternalServerError, "failed to load notification target")
		return
	}

	enabled := existing.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	eventTypes := existing.EventTypes
	if req.EventTypes != nil {
		eventTypes, err = notifications.NormalizeEventTypes(*req.EventTypes)
		if err != nil {
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		if len(eventTypes) == 0 {
			eventTypes = notifications.AllEventTypeStrings()
		}
	}

	updated, err := h.store.Update(r.Context(), id, &models.NotificationTargetUpdate{
		Name:       name,
		URL:        url,
		Enabled:    enabled,
		EventTypes: eventTypes,
	})
	if err != nil {
		if errors.Is(err, models.ErrNotificationTargetNotFound) {
			RespondError(w, http.StatusNotFound, "notification target not found")
			return
		}
		log.Error().Err(err).Msg("notifications: failed to update target")
		RespondError(w, http.StatusInternalServerError, "failed to update notification target")
		return
	}

	RespondJSON(w, http.StatusOK, updated)
}

// DeleteTarget handles DELETE /api/notifications/targets/{id}
func (h *NotificationsHandler) DeleteTarget(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		RespondError(w, http.StatusInternalServerError, "notification store unavailable")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "invalid target id")
		return
	}

	if err := h.store.Delete(r.Context(), id); err != nil {
		if errors.Is(err, models.ErrNotificationTargetNotFound) {
			RespondError(w, http.StatusNotFound, "notification target not found")
			return
		}
		log.Error().Err(err).Msg("notifications: failed to delete target")
		RespondError(w, http.StatusInternalServerError, "failed to delete notification target")
		return
	}

	RespondJSON(w, http.StatusNoContent, nil)
}

func validateNotificationURL(ctx context.Context, url string) error {
	if err := notifications.ValidateURL(url); err != nil {
		return fmt.Errorf("invalid notification url: %w", err)
	}
	if err := notifications.ValidateNotifiarrAPIKey(ctx, url); err != nil {
		return err
	}
	return nil
}

// TestTarget handles POST /api/notifications/targets/{id}/test
func (h *NotificationsHandler) TestTarget(w http.ResponseWriter, r *http.Request) {
	if h.store == nil || h.service == nil {
		RespondError(w, http.StatusInternalServerError, "notification service unavailable")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "invalid target id")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxNotificationBodySize)
	dec := json.NewDecoder(r.Body)

	var req notificationTestRequest
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Test notification"
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		message = "This is a test notification from qui."
	}

	target, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, models.ErrNotificationTargetNotFound) {
			RespondError(w, http.StatusNotFound, "notification target not found")
			return
		}
		log.Error().Err(err).Msg("notifications: failed to load target")
		RespondError(w, http.StatusInternalServerError, "failed to load notification target")
		return
	}

	if err := h.service.SendTest(r.Context(), target, title, message); err != nil {
		log.Error().Err(err).Str("target", target.Name).Msg("notifications: test send failed")
		RespondError(w, http.StatusBadGateway, "failed to send test notification")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}
