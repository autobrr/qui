// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
)

// ErrorResponse represents an API error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// RespondJSON sends a JSON response
func RespondJSON(w http.ResponseWriter, status int, data any) {
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
	RespondJSON(w, status, ErrorResponse{
		Error: message,
	})
}

func respondIfInstanceDisabled(w http.ResponseWriter, err error, instanceID int, context string) bool {
	if errors.Is(err, internalqbittorrent.ErrInstanceDisabled) {
		log.Trace().
			Int("instanceID", instanceID).
			Str("context", context).
			Msg("Ignoring request for disabled instance")
		RespondError(w, http.StatusConflict, "Instance is disabled")
		return true
	}

	return false
}

// ParseInstanceID extracts and validates the instanceID from URL parameters.
// Returns the instance ID and true on success, or 0 and false if invalid (error already sent).
func ParseInstanceID(w http.ResponseWriter, r *http.Request) (int, bool) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return 0, false
	}
	return instanceID, true
}

// DecodeJSON decodes the request body into the provided struct.
// Returns false if decoding fails (error already sent to client).
func DecodeJSON[T any](w http.ResponseWriter, r *http.Request, dest *T) bool {
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return false
	}
	return true
}

// ParseTorrentHash extracts and validates the torrent hash from URL parameters.
// Returns the hash and true on success, or empty string and false if missing (error already sent).
func ParseTorrentHash(w http.ResponseWriter, r *http.Request) (string, bool) {
	hash := chi.URLParam(r, "hash")
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "Torrent hash is required")
		return "", false
	}
	return hash, true
}

// ParseIndexerID extracts and validates the indexer ID from URL parameters.
// Returns the ID and true on success, or 0 and false if invalid (error already sent).
func ParseIndexerID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(chi.URLParam(r, "indexerID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid indexer ID")
		return 0, false
	}
	return id, true
}

// ParseRuleID extracts and validates the rule ID from URL parameters.
// Returns the ID and true on success, or 0 and false if invalid (error already sent).
func ParseRuleID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(chi.URLParam(r, "ruleID"))
	if err != nil || id <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid rule ID")
		return 0, false
	}
	return id, true
}

// ParseIntParam extracts and validates a generic integer URL parameter.
// Returns the value and true on success, or 0 and false if invalid (error already sent).
func ParseIntParam(w http.ResponseWriter, r *http.Request, paramName string) (int, bool) {
	value, err := strconv.Atoi(chi.URLParam(r, paramName))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid "+paramName)
		return 0, false
	}
	return value, true
}
