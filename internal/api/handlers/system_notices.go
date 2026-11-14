// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/autobrr/qui/internal/services/systemnotice"
)

type SystemNoticeHandler struct {
	notices *systemnotice.Module
}

func NewSystemNoticeHandler(notices *systemnotice.Module) *SystemNoticeHandler {
	return &SystemNoticeHandler{notices: notices}
}

// List handles GET /api/system-notices.
func (h *SystemNoticeHandler) List(w http.ResponseWriter, _ *http.Request) {
	RespondJSON(w, http.StatusOK, h.notices.List())
}

// Dismiss closes a notice that the user can dismiss.
// DELETE /api/system-notices/{kind}
func (h *SystemNoticeHandler) Dismiss(w http.ResponseWriter, r *http.Request) {
	if err := h.notices.Dismiss(systemnotice.Kind(chi.URLParam(r, "kind"))); err != nil {
		RespondError(w, http.StatusConflict, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
