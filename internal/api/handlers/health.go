// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"net/http"
)

type HealthHandler struct {
}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

func (h *HealthHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	respondHealth(w, r, "ok")
}

func (h *HealthHandler) HandleReady(w http.ResponseWriter, r *http.Request) {
	respondHealth(w, r, "ready")
}

func (h *HealthHandler) HandleLiveness(w http.ResponseWriter, r *http.Request) {
	respondHealth(w, r, "alive")
}

func respondHealth(w http.ResponseWriter, r *http.Request, status string) {
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}
	RespondJSON(w, r, http.StatusOK, map[string]string{"status": status})
}
