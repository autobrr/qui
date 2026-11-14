// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json/v2"
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
// GET /api/instances/{instanceID}/transfer-history?from=&to=&bucket=&groupBy=&group=&groupOf=&limit=&timeZone=
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
		GroupOf:    transferhistory.Grouping(params.Get("groupOf")),
	}
	if params.Has("group") {
		query.Group = new(params.Get("group"))
	}
	if raw := params.Get("limit"); raw != "" {
		if query.Limit, err = strconv.Atoi(raw); err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid limit: use a whole number")
			return
		}
	}
	if name := params.Get("timeZone"); name != "" {
		if query.Location, err = time.LoadLocation(name); err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid timeZone: use an IANA time zone name")
			return
		}
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

// Status returns the instances that have history, also the removed ones, the
// size of the history, and the Statistics settings.
// GET /api/transfer-history/status
func (h *TransferHistoryHandler) Status(w http.ResponseWriter, r *http.Request) {
	status, err := h.module.Status(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("transfer history: failed to read the status")
		RespondError(w, http.StatusInternalServerError, "Failed to read transfer history status")
		return
	}
	if status.Instances == nil {
		status.Instances = []transferhistory.InstanceStatus{}
	}
	RespondJSON(w, http.StatusOK, status)
}

// UpdateSettings stores the recording switch and the Detail retention.
// PUT /api/transfer-history/settings
func (h *TransferHistoryHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Recording           *bool `json:"recording"`
		DetailRetentionDays *int  `json:"detailRetentionDays"`
	}
	if err := json.UnmarshalRead(r.Body, &body); err != nil || body.Recording == nil || body.DetailRetentionDays == nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body: give recording and detailRetentionDays")
		return
	}
	settings := transferhistory.Settings{Recording: *body.Recording, DetailRetentionDays: *body.DetailRetentionDays}
	switch err := h.module.SetSettings(r.Context(), settings); {
	case errors.Is(err, transferhistory.ErrInvalidSettings):
		RespondError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		log.Error().Err(err).Msg("transfer history: failed to save the settings")
		RespondError(w, http.StatusInternalServerError, "Failed to save transfer history settings")
	default:
		RespondJSON(w, http.StatusOK, settings)
	}
}

// DeleteHistory deletes the transfer history of one instance, also a removed
// one, or of each instance: everything, or the UTC days before the day of before.
// DELETE /api/transfer-history?instanceId=&before=
func (h *TransferHistoryHandler) DeleteHistory(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	d := transferhistory.HistoryDelete{AllInstances: params.Get("instanceId") == "all"}
	if !d.AllInstances {
		raw := params.Get("instanceId")
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			RespondError(w, http.StatusBadRequest, "Invalid instanceId: give an instance ID or all")
			return
		}
		d.InstanceID = id
	}
	if raw := params.Get("before"); raw != "" {
		before, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid before: use RFC 3339")
			return
		}
		if before.After(time.Now()) {
			RespondError(w, http.StatusBadRequest, "Invalid before: the time is in the future")
			return
		}
		// The module reads a zero Before as a delete of everything.
		if before.IsZero() {
			RespondError(w, http.StatusBadRequest, "Invalid before: give a time after 0001-01-01")
			return
		}
		d.Before = before.UTC()
	}
	err := h.module.DeleteHistory(r.Context(), d)
	switch {
	case err != nil && r.Context().Err() != nil:
		// The worker does not stop the delete when the client leaves.
		log.Warn().Int("instanceID", d.InstanceID).Bool("allInstances", d.AllInstances).Time("before", d.Before).
			Msg("transfer history: the client left before the delete of history ended; the delete goes on")
		return
	case err != nil:
		// The module logs the failure.
		RespondError(w, http.StatusInternalServerError, "Failed to delete transfer history")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
