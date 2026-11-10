// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
)

type HealthHandler struct {
}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

func (h *HealthHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errchkjson // map[string]string cannot fail to encode; only a client write error can occur after the 200 header, and the probe cannot recover from it
}

func (h *HealthHandler) HandleReady(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"}) //nolint:errchkjson // map[string]string cannot fail to encode; only a client write error can occur after the 200 header, and the probe cannot recover from it
}

func (h *HealthHandler) HandleLiveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "alive"}) //nolint:errchkjson // map[string]string cannot fail to encode; only a client write error can occur after the 200 header, and the probe cannot recover from it
}
