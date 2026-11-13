// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/transferhistory"
)

// TransferHistoryHandler answers the Transfer history routes.
type TransferHistoryHandler struct {
	module *transferhistory.Module
}

func NewTransferHistoryHandler(module *transferhistory.Module) *TransferHistoryHandler {
	return &TransferHistoryHandler{module: module}
}

type transferHistoryResponse struct {
	Rows []transferhistory.TransferRow `json:"rows"`
}

// Transfer returns the transfer of one instance in a period.
// GET /api/instances/{instanceID}/transfer-history?from=&to=&bucket=&groupBy=
func (h *TransferHistoryHandler) Transfer(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}
	params := r.URL.Query()
	query := transferhistory.TransferQuery{
		InstanceID: instanceID,
		To:         time.Now().UTC(),
		Bucket:     transferhistory.Bucket(params.Get("bucket")),
		GroupBy:    transferhistory.Grouping(params.Get("groupBy")),
	}
	for name, dst := range map[string]*time.Time{"from": &query.From, "to": &query.To} {
		if raw := params.Get(name); raw != "" {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				RespondError(w, http.StatusBadRequest, "Invalid "+name+": use RFC 3339")
				return
			}
			*dst = parsed.UTC()
		}
	}

	rows, err := h.module.Transfer(r.Context(), query)
	if errors.Is(err, transferhistory.ErrInvalidQuery) {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("transfer history: failed to answer a transfer query")
		RespondError(w, http.StatusInternalServerError, "Failed to read transfer history")
		return
	}
	if rows == nil {
		rows = []transferhistory.TransferRow{}
	}
	RespondJSON(w, http.StatusOK, transferHistoryResponse{Rows: rows})
}

type transferHistoryStatusResponse struct {
	Instances []transferhistory.InstanceStatus `json:"instances"`
}

// Status returns the instances that have history, also the removed ones.
// GET /api/transfer-history/status
func (h *TransferHistoryHandler) Status(w http.ResponseWriter, r *http.Request) {
	instances, err := h.module.Status(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("transfer history: failed to read the status")
		RespondError(w, http.StatusInternalServerError, "Failed to read transfer history status")
		return
	}
	if instances == nil {
		instances = []transferhistory.InstanceStatus{}
	}
	RespondJSON(w, http.StatusOK, transferHistoryStatusResponse{Instances: instances})
}
