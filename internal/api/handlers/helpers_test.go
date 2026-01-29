// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRespondJSON_NoContent(t *testing.T) {
	rr := httptest.NewRecorder()

	// Pass a payload that would normally be encoded
	RespondJSON(rr, http.StatusNoContent, map[string]string{"ignored": "value"})

	assert.Equal(t, http.StatusNoContent, rr.Code)
	assert.Empty(t, rr.Body.String(), "body must be empty for 204")
	assert.Empty(t, rr.Header().Get("Content-Type"), "Content-Type must not be set for 204")
}

func TestRespondJSON_NotModified(t *testing.T) {
	rr := httptest.NewRecorder()

	RespondJSON(rr, http.StatusNotModified, map[string]string{"ignored": "value"})

	assert.Equal(t, http.StatusNotModified, rr.Code)
	assert.Empty(t, rr.Body.String(), "body must be empty for 304")
	assert.Empty(t, rr.Header().Get("Content-Type"), "Content-Type must not be set for 304")
}

func TestRespondJSON_OK(t *testing.T) {
	rr := httptest.NewRecorder()

	RespondJSON(rr, http.StatusOK, map[string]string{"key": "value"})

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"key":"value"}`, rr.Body.String())
}

func TestRespondJSON_NilData(t *testing.T) {
	rr := httptest.NewRecorder()

	RespondJSON(rr, http.StatusOK, nil)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, rr.Header().Get("Content-Type"), "Content-Type should not be set for nil data")
	assert.Empty(t, rr.Body.String())
}
