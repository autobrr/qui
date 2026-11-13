// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/transferhistory"
)

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
		Bucket:     transferhistory.Bucket(params.Get("bucket")),
		GroupBy:    transferhistory.Grouping(params.Get("groupBy")),
	}
	var ok bool
	if query.From, query.To, ok = parseHistoryPeriod(w, r); !ok {
		return
	}

	rows, err := h.module.Transfer(r.Context(), query)
	if respondHistoryError(w, err, instanceID) {
		return
	}
	if rows == nil {
		rows = []transferhistory.TransferRow{}
	}
	RespondJSON(w, http.StatusOK, transferHistoryResponse{Rows: rows})
}

// parseHistoryPeriod reads the period of a history query. Without from, the
// period has no start. Without to, the end is now. It writes the error
// response and returns false for a time that is not RFC 3339.
func parseHistoryPeriod(w http.ResponseWriter, r *http.Request) (from, to time.Time, ok bool) {
	to = time.Now().UTC()
	for name, dst := range map[string]*time.Time{"from": &from, "to": &to} {
		if raw := r.URL.Query().Get(name); raw != "" {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				RespondError(w, http.StatusBadRequest, "Invalid "+name+": use RFC 3339")
				return from, to, false
			}
			*dst = parsed.UTC()
		}
	}
	return from, to, true
}

// respondHistoryError writes the error response of a history query and
// reports whether err is set.
func respondHistoryError(w http.ResponseWriter, err error, instanceID int) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, transferhistory.ErrInvalidQuery):
		RespondError(w, http.StatusBadRequest, err.Error())
	default:
		log.Error().Err(err).Int("instanceID", instanceID).Msg("transfer history: failed to answer a query")
		RespondError(w, http.StatusInternalServerError, "Failed to read transfer history")
	}
	return true
}

type transferHistoryLibraryResponse struct {
	Rows []transferhistory.LibraryRow `json:"rows"`
}

// Library returns the adds, completions, and deletes of one instance for each
// UTC day of a period.
// GET /api/instances/{instanceID}/transfer-history/library?from=&to=
func (h *TransferHistoryHandler) Library(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}
	query := transferhistory.LibraryQuery{InstanceID: instanceID}
	var ok bool
	if query.From, query.To, ok = parseHistoryPeriod(w, r); !ok {
		return
	}

	rows, err := h.module.Library(r.Context(), query)
	if respondHistoryError(w, err, instanceID) {
		return
	}
	RespondJSON(w, http.StatusOK, transferHistoryLibraryResponse{Rows: rows})
}

const defaultDeletesPage = 50

// Deletes returns a page of the deleted torrents of one instance in a
// period, with the newest first.
// GET /api/instances/{instanceID}/transfer-history/deletes?from=&to=&cause=&limit=&offset=
func (h *TransferHistoryHandler) Deletes(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}
	params := r.URL.Query()
	query := transferhistory.DeletesQuery{
		InstanceID: instanceID,
		Cause:      transferhistory.CauseFilter(params.Get("cause")),
		Limit:      defaultDeletesPage,
	}
	var ok bool
	if query.From, query.To, ok = parseHistoryPeriod(w, r); !ok {
		return
	}
	for name, dst := range map[string]*int{"limit": &query.Limit, "offset": &query.Offset} {
		if raw := params.Get(name); raw != "" {
			if *dst, err = strconv.Atoi(raw); err != nil {
				RespondError(w, http.StatusBadRequest, "Invalid "+name+": use a whole number")
				return
			}
		}
	}

	answer, err := h.module.Deletes(r.Context(), query)
	if respondHistoryError(w, err, instanceID) {
		return
	}
	RespondJSON(w, http.StatusOK, answer)
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
