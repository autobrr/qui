// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpdateAutomationSettings_RejectsSubMiBPooledRecheckLimit(t *testing.T) {
	t.Parallel()

	handler := &CrossSeedHandler{}
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPut,
		"/api/cross-seed/settings",
		strings.NewReader(`{"maxMissingBytesAfterRecheck":1048575}`),
	)
	rec := httptest.NewRecorder()

	handler.UpdateAutomationSettings(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"error":"maxMissingBytesAfterRecheck must be one MiB or greater"}`, rec.Body.String())
}

func TestPatchAutomationSettings_RejectsSubMiBPooledRecheckLimit(t *testing.T) {
	t.Parallel()

	handler := &CrossSeedHandler{}
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPatch,
		"/api/cross-seed/settings",
		strings.NewReader(`{"maxMissingBytesAfterRecheck":1048575}`),
	)
	rec := httptest.NewRecorder()

	handler.PatchAutomationSettings(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"error":"maxMissingBytesAfterRecheck must be one MiB or greater"}`, rec.Body.String())
}
