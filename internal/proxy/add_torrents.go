// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"net/http"
	"strings"
)

type addNoter interface {
	NoteProxiedAdd(instanceID int)
}

func (h *Handler) handleAddTorrents(w http.ResponseWriter, r *http.Request) {
	answer := &capturingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
	h.proxy.ServeHTTP(answer, r)
	if acceptedAdd(answer.statusCode, string(answer.body)) {
		h.addNoter.NoteProxiedAdd(GetInstanceIDFromContext(r.Context()))
	}
}

func acceptedAdd(status int, body string) bool {
	return status/100 == 2 && strings.TrimSpace(body) != "Fails."
}
