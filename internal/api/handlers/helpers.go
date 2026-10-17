// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rs/zerolog/log"

	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/httphelpers"
)

// ErrorResponse represents an API error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// WarningResponse represents a success response with optional warnings
type WarningResponse struct {
	Warning string `json:"warning,omitempty"`
}

// RespondJSON sends a JSON response.
// For 204 No Content and 304 Not Modified, no body or Content-Type is sent per HTTP spec.
func RespondJSON(w http.ResponseWriter, status int, data any) {
	// 204 and 304 must not have a body per RFC 7230/9110
	if status == http.StatusNoContent || status == http.StatusNotModified {
		w.WriteHeader(status)
		return
	}

	if data != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(data); err != nil {
			// The encoder writes straight to the socket, so a peer that went away
			// mid-response shows up here even though the JSON itself was valid.
			if !isEncodeFailure(err) && httphelpers.IsClientDisconnect(err) {
				log.Debug().Err(err).Msg("Client disconnected before the JSON response was written")
				return
			}
			log.Error().Err(err).Msg("Failed to encode JSON response")
		}
		return
	}

	w.WriteHeader(status)
}

// isEncodeFailure reports whether err came from serializing the payload rather
// than from writing it to the client.
func isEncodeFailure(err error) bool {
	var marshalerErr *json.MarshalerError
	var unsupportedType *json.UnsupportedTypeError
	var unsupportedValue *json.UnsupportedValueError
	return errors.As(err, &marshalerErr) || errors.As(err, &unsupportedType) || errors.As(err, &unsupportedValue)
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
