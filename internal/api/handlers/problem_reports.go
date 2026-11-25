// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/problemreport"
)

const ProblemReportsOff = "Problem reports are off in the qui config."

const maxReportPart = 16 << 20

type ProblemReportHandler struct {
	service *problemreport.Service
}

func NewProblemReportHandler(service *problemreport.Service) *ProblemReportHandler {
	return &ProblemReportHandler{service: service}
}

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

func (h *ProblemReportHandler) Send(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxReportPart+problemreport.MaxImageSize+1<<20)
	reader, err := r.MultipartReader()
	if err != nil {
		RespondError(w, http.StatusBadRequest, "the body must be multipart/form-data")
		return
	}

	var report *problemreport.Report
	var image *problemreport.Image
	shown := problemreport.Sender{Anonymous: true}
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
			limited := &io.LimitedReader{R: part, N: maxReportPart + 1}
			decoded, err := problemreport.DecodeReport(limited)
			if limited.N == 0 {
				RespondError(w, http.StatusRequestEntityTooLarge, problemreport.ReasonTooLarge)
				return
			}
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
		case "sender":
			if err := json.UnmarshalRead(part, &shown); err != nil {
				RespondError(w, http.StatusBadRequest, "the sender is not valid JSON: "+err.Error())
				return
			}
		}
	}
	if report == nil {
		RespondError(w, http.StatusBadRequest, "the report part is required")
		return
	}

	sent, err := h.service.Send(r.Context(), *report, image, shown)
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

func (h *ProblemReportHandler) SentReports(w http.ResponseWriter, r *http.Request) {
	reports, err := h.service.SentReports(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to list Sent reports")
		RespondError(w, http.StatusInternalServerError, "list the sent reports: "+err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, reports)
}

func (h *ProblemReportHandler) Delete(w http.ResponseWriter, r *http.Request) {
	err := h.service.Delete(r.Context(), chi.URLParam(r, "id"))
	if refusal, ok := errors.AsType[*problemreport.RefusalError](err); ok {
		RespondError(w, refusal.Status, refusal.Reason)
		return
	}
	switch {
	case errors.Is(err, problemreport.ErrNotFound):
		RespondError(w, http.StatusNotFound, err.Error())
	case err != nil:
		log.Error().Err(err).Msg("Failed to delete a Problem report")
		RespondError(w, http.StatusInternalServerError, "delete the report: "+err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *ProblemReportHandler) Sender(w http.ResponseWriter, r *http.Request) {
	sender, err := h.service.Sender(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to read the Problem report Sender")
		RespondError(w, http.StatusInternalServerError, "read the sender: "+err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, sender)
}

func (h *ProblemReportHandler) StartSignIn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
	}
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, 1<<10), &req); err != nil {
		RespondError(w, http.StatusBadRequest, "the body must be JSON with a provider")
		return
	}
	url, err := h.service.StartSignIn(r.Context(), req.Provider)
	if refusal, ok := errors.AsType[*problemreport.RefusalError](err); ok {
		RespondError(w, refusal.Status, refusal.Reason)
		return
	}
	switch {
	case errors.Is(err, problemreport.ErrInvalidRequest):
		RespondError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		log.Error().Err(err).Msg("Failed to start a Problem report sign-in")
		RespondError(w, http.StatusInternalServerError, "start the sign-in: "+err.Error())
	default:
		RespondJSON(w, http.StatusOK, map[string]string{"url": url})
	}
}

func (h *ProblemReportHandler) SignInState(w http.ResponseWriter, _ *http.Request) {
	RespondJSON(w, http.StatusOK, h.service.SignInState())
}

func (h *ProblemReportHandler) SignOut(w http.ResponseWriter, r *http.Request) {
	if err := h.service.SignOut(r.Context()); err != nil {
		log.Error().Err(err).Msg("Failed to sign out of Problem reports")
		RespondError(w, http.StatusInternalServerError, "sign out: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
