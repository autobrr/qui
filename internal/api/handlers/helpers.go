// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

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

// DecodeJSON decodes the request body into the provided struct.
// Returns false if decoding fails (error already sent to client).
func DecodeJSON[T any](w http.ResponseWriter, r *http.Request, dest *T) bool {
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return false
	}
	return true
}

// DecodeJSONOptional decodes the request body into the provided struct.
// Returns true if decoding succeeds or body is empty (io.EOF).
// Returns false only on actual decode errors (error already sent to client).
func DecodeJSONOptional[T any](w http.ResponseWriter, r *http.Request, dest *T) bool {
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil && err != io.EOF {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return false
	}
	return true
}

// ParseIntParam extracts and validates a generic integer URL parameter.
// Returns the value and true on success, or 0 and false if invalid (error already sent).
// The displayName is used in error messages (e.g., "instance ID" for user-friendly output).
func ParseIntParam(w http.ResponseWriter, r *http.Request, paramName, displayName string) (int, bool) {
	str, ok := ParseStringParam(w, r, paramName, displayName)
	if !ok {
		return 0, false
	}
	value, err := strconv.Atoi(str)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid "+displayName)
		return 0, false
	}
	return value, true
}

// ParsePositiveIntParam extracts and validates a positive integer URL parameter (> 0).
// Returns the value and true on success, or 0 and false if invalid (error already sent).
// The displayName is used in error messages (e.g., "rule ID" for user-friendly output).
func ParsePositiveIntParam(w http.ResponseWriter, r *http.Request, paramName, displayName string) (int, bool) {
	value, ok := ParseIntParam(w, r, paramName, displayName)
	if !ok {
		return 0, false
	}
	if value <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid "+displayName)
		return 0, false
	}
	return value, true
}

// ParseIntParam64 extracts and validates a generic int64 URL parameter.
// Returns the value and true on success, or 0 and false if invalid (error already sent).
// The displayName is used in error messages (e.g., "run ID" for user-friendly output).
func ParseIntParam64(w http.ResponseWriter, r *http.Request, paramName, displayName string) (int64, bool) {
	str, ok := ParseStringParam(w, r, paramName, displayName)
	if !ok {
		return 0, false
	}
	value, err := strconv.ParseInt(str, 10, 64)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid "+displayName)
		return 0, false
	}
	return value, true
}

// ParseStringParam extracts and validates a generic string URL parameter.
// The value is trimmed of whitespace before validation.
// Returns the trimmed value and true on success, or empty string and false if missing (error already sent).
// The displayName is used in error messages (e.g., "torrent hash" for user-friendly output).
func ParseStringParam(w http.ResponseWriter, r *http.Request, paramName, displayName string) (string, bool) {
	value := strings.TrimSpace(chi.URLParam(r, paramName))
	if value == "" {
		RespondError(w, http.StatusBadRequest, displayName+" is required")
		return "", false
	}
	return value, true
}

// ParseInstanceID extracts and validates the instanceID from URL parameters.
// Returns the instance ID and true on success, or 0 and false if invalid (error already sent).
func ParseInstanceID(w http.ResponseWriter, r *http.Request) (int, bool) {
	return ParseIntParam(w, r, "instanceID", "instance ID")
}

// ParseTorrentHash extracts and validates the torrent hash from URL parameters.
// Returns the hash and true on success, or empty string and false if missing (error already sent).
func ParseTorrentHash(w http.ResponseWriter, r *http.Request) (string, bool) {
	return ParseStringParam(w, r, "hash", "Torrent hash")
}

// ParseIndexerID extracts and validates the indexer ID from URL parameters.
// Returns the ID and true on success, or 0 and false if invalid (error already sent).
func ParseIndexerID(w http.ResponseWriter, r *http.Request) (int, bool) {
	return ParseIntParam(w, r, "indexerID", "indexer ID")
}

// ParseRuleID extracts and validates the rule ID from URL parameters.
// Returns the ID and true on success, or 0 and false if invalid (error already sent).
func ParseRuleID(w http.ResponseWriter, r *http.Request) (int, bool) {
	return ParsePositiveIntParam(w, r, "ruleID", "rule ID")
}

// PaginationParams holds parsed pagination parameters.
type PaginationParams struct {
	Limit  int
	Offset int
}

// ParsePagination extracts and validates pagination parameters from query string.
// Uses provided defaults and enforces maxLimit. Invalid values are silently ignored.
func ParsePagination(r *http.Request, defaultLimit, maxLimit int) PaginationParams {
	p := PaginationParams{Limit: defaultLimit, Offset: 0}

	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			if parsed > maxLimit {
				parsed = maxLimit
			}
			p.Limit = parsed
		}
	}

	if v := r.URL.Query().Get("offset"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			p.Offset = parsed
		}
	}

	return p
}

// RespondNotFoundIfNoRows checks if err is sql.ErrNoRows and responds with 404 and the given message.
// Returns true if the error was handled, false otherwise.
func RespondNotFoundIfNoRows(w http.ResponseWriter, err error, notFoundMessage string) bool {
	if errors.Is(err, sql.ErrNoRows) {
		RespondError(w, http.StatusNotFound, notFoundMessage)
		return true
	}
	return false
}

// RespondDBError handles database errors with common patterns:
// - sql.ErrNoRows -> 404 with notFoundMessage
// - other errors -> 500 with fallbackMessage
// Always returns true (error was handled).
func RespondDBError(w http.ResponseWriter, err error, notFoundMessage, fallbackMessage string) {
	if errors.Is(err, sql.ErrNoRows) {
		RespondError(w, http.StatusNotFound, notFoundMessage)
		return
	}
	RespondError(w, http.StatusInternalServerError, fallbackMessage)
}
