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

type ErrorResponse struct {
	Error string `json:"error"`
}

type WarningResponse struct {
	Warning string `json:"warning,omitempty"`
}

func RespondJSON(w http.ResponseWriter, status int, data any) {
	if status == http.StatusNoContent || status == http.StatusNotModified {
		w.WriteHeader(status)
		return
	}

	if data != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(data); err != nil {
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

func isEncodeFailure(err error) bool {
	_, marshaler := errors.AsType[*json.MarshalerError](err)
	_, unsupportedType := errors.AsType[*json.UnsupportedTypeError](err)
	_, unsupportedValue := errors.AsType[*json.UnsupportedValueError](err)
	return marshaler || unsupportedType || unsupportedValue
}

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
