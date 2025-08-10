// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: MIT

package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog/log"
)

// RespondJSON sends a JSON response
func RespondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if data != nil {
		if err := json.NewEncoder(w).Encode(data); err != nil {
			log.Error().Err(err).Msg("Failed to encode JSON response")
		}
	}
}

// RespondError sends an error response
func RespondError(w http.ResponseWriter, status int, message string) {
	RespondJSON(w, status, map[string]string{
		"error": message,
	})
}

// ParseIDFromPath extracts an ID from the URL path
// This is a helper for chi router URL parameters
func ParseIDFromPath(r *http.Request, param string) (int, error) {
	// This will be implemented when we set up the router
	// For now, return a placeholder
	return 0, nil
}
