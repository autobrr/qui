// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"net/http"

	"github.com/autobrr/qui/internal/services/livesamples"
)

type liveSampleReader interface {
	Samples() livesamples.Window
}

type LiveSamplesHandler struct {
	recorder liveSampleReader
}

func NewLiveSamplesHandler(recorder *livesamples.Recorder) *LiveSamplesHandler {
	return &LiveSamplesHandler{recorder: recorder}
}

// List returns the Live samples of each instance.
// GET /api/live-samples
func (h *LiveSamplesHandler) List(w http.ResponseWriter, _ *http.Request) {
	RespondJSON(w, http.StatusOK, h.recorder.Samples())
}
