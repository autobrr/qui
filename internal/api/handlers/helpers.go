// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"errors"
	"net/http"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/api/middleware"
	"github.com/autobrr/qui/internal/api/secretshapes"
	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/secrets"
	"github.com/autobrr/qui/pkg/httphelpers"
)

type ErrorResponse struct {
	Error string `json:"error"`
}

type WarningResponse struct {
	Warning string `json:"warning,omitempty"`
}

func RespondJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	writeJSON(w, middleware.AudienceOf(r), status, data)
}

func RespondError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, secrets.Audience{}, status, ErrorResponse{
		Error: message,
	})
}

const maxPooledJSONBuffer = 1 << 20

var jsonBuffers = sync.Pool{New: func() any { return new(bytes.Buffer) }}

func writeJSON(w http.ResponseWriter, audience secrets.Audience, status int, data any) {
	if status == http.StatusNoContent || status == http.StatusNotModified || data == nil {
		w.WriteHeader(status)
		return
	}

	buf := jsonBuffers.Get().(*bytes.Buffer)
	defer releaseJSONBuffer(buf)
	if err := secretshapes.Encoder.Write(buf, audience, data); err != nil {
		log.Error().Err(err).Msg("Failed to encode JSON response")
		writeJSON(w, secrets.Audience{}, http.StatusInternalServerError, ErrorResponse{Error: "Failed to encode response"})
		return
	}
	buf.WriteByte('\n')

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		if httphelpers.IsClientDisconnect(err) {
			log.Debug().Err(err).Msg("Client disconnected before the JSON response was written")
			return
		}
		log.Error().Err(err).Msg("Failed to write JSON response")
	}
}

func releaseJSONBuffer(buf *bytes.Buffer) {
	if buf.Cap() > maxPooledJSONBuffer {
		return
	}
	buf.Reset()
	jsonBuffers.Put(buf)
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
