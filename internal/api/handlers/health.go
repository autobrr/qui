// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type HealthHandler struct {
}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

func (h *HealthHandler) Routes(r chi.Router) {
	r.Get("/readiness", h.HandleReady)
	r.Get("/liveness", h.HandleLiveness)
}

func (h *HealthHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Perform actual health checks
	//health := h.checkOverallHealth()

	//if health.Status == "ok" {
	//	w.WriteHeader(http.StatusOK)
	//} else {
	//	w.WriteHeader(http.StatusServiceUnavailable)
	//}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *HealthHandler) HandleReady(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Check if service is ready to serve traffic
	if h.isReady() {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "not ready"})
	}
}

func (h *HealthHandler) HandleLiveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Check if service is alive (should restart if this fails)
	if h.isAlive() {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "alive"})
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "dead"})
	}
}

// Helper methods for actual health checking
//func (h *HealthHandler) checkOverallHealth() HealthResponse {
//	checks := make(map[string]CheckResult)
//	overallStatus := "ok"
//
//	// Example: Database check
//	if h.db != nil {
//		if err := h.db.Ping(); err != nil {
//			checks["database"] = CheckResult{Status: "fail", Error: err.Error()}
//			overallStatus = "fail"
//		} else {
//			checks["database"] = CheckResult{Status: "ok"}
//		}
//	}
//
//	// Example: External service check
//	if h.externalService != nil {
//		if err := h.checkExternalService(); err != nil {
//			checks["external_service"] = CheckResult{Status: "fail", Error: err.Error()}
//			overallStatus = "fail"
//		} else {
//			checks["external_service"] = CheckResult{Status: "ok"}
//		}
//	}
//
//	return HealthResponse{
//		Status:    overallStatus,
//		Checks:    checks,
//		Timestamp: time.Now().UTC(),
//	}
//}

func (h *HealthHandler) isReady() bool {
	// Check if all dependencies are available and service can handle requests
	//if h.db != nil && h.db.Ping() != nil {
	//	return false
	//}
	// Add other readiness checks
	return true
}

func (h *HealthHandler) isAlive() bool {
	// Basic liveness check - service is running and responsive
	// This should rarely return false unless there's a deadlock or similar
	return true
}

type HealthResponse struct {
	Status    string                 `json:"status"`
	Checks    map[string]CheckResult `json:"checks,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

type CheckResult struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}
