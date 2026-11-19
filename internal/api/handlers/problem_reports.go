// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/problemreport"
)

// ProblemReportsOff is the answer of every report route while the config
// turns Problem reports off.
const ProblemReportsOff = "Problem reports are off in the qui config."

// maxReportPart is the largest report part that qui reads. qui cuts the
// report to problemreport.MaxReportSize before it sends it.
const maxReportPart = 16 << 20

type ProblemReportHandler struct {
	service *problemreport.Service
}

func NewProblemReportHandler(service *problemreport.Service) *ProblemReportHandler {
	return &ProblemReportHandler{service: service}
}

// RequireProblemReports answers 403 on every report route while enabled
// returns false.
func RequireProblemReports(enabled func() bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled() {
				RespondError(w, http.StatusForbidden, ProblemReportsOff)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Facts handles GET /api/problem-reports/facts.
func (h *ProblemReportHandler) Facts(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	req := problemreport.FactsRequest{StartedFrom: query.Get("startedFrom"), Hash: strings.TrimSpace(query.Get("hash"))}
	if req.StartedFrom == problemreport.StartedFromSearch {
		var err error
		if req.InstanceID, err = strconv.Atoi(query.Get("instanceId")); err != nil {
			RespondError(w, http.StatusBadRequest, "instanceId must be a number")
			return
		}
		if req.Search.IndexerIDs, err = parseIDList(query.Get("indexerIds")); err != nil {
			RespondError(w, http.StatusBadRequest, "indexerIds must be a comma-separated list of numbers")
			return
		}
		if query.Has("gazelleSites") {
			req.Search.GazelleSites = splitList(query.Get("gazelleSites"))
		}
		req.Search.FindIndividualEpisodes = query.Get("findIndividualEpisodes") == "true"
	}

	facts, err := h.service.Facts(r.Context(), req)
	switch {
	case errors.Is(err, problemreport.ErrInvalidRequest):
		RespondError(w, http.StatusBadRequest, err.Error())
	case err != nil && mapCrossSeedErrorStatus(err) == http.StatusBadRequest:
		RespondError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		log.Error().Err(err).Str("startedFrom", req.StartedFrom).Msg("Failed to read the Problem report facts")
		RespondError(w, http.StatusInternalServerError, "read the facts: "+err.Error())
	default:
		RespondJSON(w, http.StatusOK, facts)
	}
}

// Send handles POST /api/problem-reports: a multipart body with a report
// part and an optional image part.
func (h *ProblemReportHandler) Send(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxReportPart+problemreport.MaxImageSize+1<<20)
	reader, err := r.MultipartReader()
	if err != nil {
		RespondError(w, http.StatusBadRequest, "the body must be multipart/form-data")
		return
	}

	var report *problemreport.Report
	var image *problemreport.Image
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			RespondError(w, http.StatusBadRequest, "read the body: "+err.Error())
			return
		}
		switch part.FormName() {
		case "report":
			decoded, err := problemreport.DecodeReport(io.LimitReader(part, maxReportPart))
			if err != nil {
				RespondError(w, http.StatusBadRequest, err.Error())
				return
			}
			report = &decoded
		case "image":
			data, err := io.ReadAll(io.LimitReader(part, problemreport.MaxImageSize+1))
			if err != nil {
				RespondError(w, http.StatusBadRequest, "read the image: "+err.Error())
				return
			}
			image = &problemreport.Image{Data: data, ContentType: part.Header.Get("Content-Type")}
		}
	}
	if report == nil {
		RespondError(w, http.StatusBadRequest, "the report part is required")
		return
	}

	sent, err := h.service.Send(r.Context(), *report, image)
	var refusal *problemreport.RefusalError
	switch {
	case errors.As(err, &refusal):
		RespondError(w, refusal.Status, refusal.Reason)
	case errors.Is(err, problemreport.ErrInvalidRequest):
		RespondError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		log.Error().Err(err).Msg("Failed to send a Problem report")
		RespondError(w, http.StatusInternalServerError, "send the report: "+err.Error())
	default:
		RespondJSON(w, http.StatusCreated, sent)
	}
}

func splitList(raw string) []string {
	values := []string{}
	for value := range strings.SplitSeq(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func parseIDList(raw string) ([]int, error) {
	var ids []int
	for _, value := range splitList(raw) {
		id, err := strconv.Atoi(value)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}
