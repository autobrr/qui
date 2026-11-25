// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/transfer"
)

type RuleReader interface {
	Get(ctx context.Context, instanceID, id int) (*models.Automation, error)
}

type TransfersHandler struct {
	service *transfer.Service
	store   *transfer.Store
	rules   RuleReader
}

func NewTransfersHandler(service *transfer.Service, store *transfer.Store, rules RuleReader) *TransfersHandler {
	return &TransfersHandler{service: service, store: store, rules: rules}
}

func (h *TransfersHandler) Routes(r chi.Router) {
	r.Post("/", h.Start)
	r.Get("/", h.List)
	r.Post("/summary", h.Summary)
	r.Post("/bulk", h.Bulk)
	r.Get("/{transferID}", h.Get)
	r.Post("/{transferID}/retry", h.Retry)
	r.Post("/{transferID}/cancel", h.Cancel)
	r.Delete("/{transferID}", h.Remove)
}

type startTransferRequest struct {
	SourceInstanceID int            `json:"sourceInstanceId"`
	Hash             string         `json:"hash"`
	TargetInstanceID int            `json:"targetInstanceId"`
	SavePath         string         `json:"savePath"`
	Category         string         `json:"category"`
	Tags             []string       `json:"tags"`
	After            transfer.After `json:"after"`
	LeaveStopped     bool           `json:"leaveStopped"`
	Way              string         `json:"way"`
}

type startTransferResponse struct {
	ID int64 `json:"id"`
}

func (h *TransfersHandler) Start(w http.ResponseWriter, r *http.Request) {
	req, ok := readTransferRequest(w, r)
	if !ok {
		return
	}
	id, err := h.service.Start(r.Context(), req)
	_, refused := errors.AsType[*transfer.RefusalError](err)
	switch {
	case refused, errors.Is(err, transfer.ErrClaimed):
		RespondError(w, http.StatusConflict, err.Error())
	case err != nil:
		log.Error().Err(err).Msg("transfer: failed to start the Transfer")
		RespondError(w, http.StatusInternalServerError, "Failed to start the Transfer")
	default:
		RespondJSON(w, http.StatusAccepted, startTransferResponse{ID: id})
	}
}

func readTransferRequest(w http.ResponseWriter, r *http.Request) (transfer.Request, bool) {
	var req startTransferRequest
	if err := json.UnmarshalRead(r.Body, &req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return transfer.Request{}, false
	}
	if strings.TrimSpace(req.Hash) == "" || req.SourceInstanceID <= 0 || req.TargetInstanceID <= 0 {
		RespondError(w, http.StatusBadRequest, "sourceInstanceId, hash, and targetInstanceId are required")
		return transfer.Request{}, false
	}
	way, ok := transfer.ForcedWays[req.Way]
	if !ok {
		RespondError(w, http.StatusBadRequest, "way must be auto, copy, or bittorrent")
		return transfer.Request{}, false
	}
	return transfer.Request{
		SourceInstanceID: req.SourceInstanceID,
		Hash:             req.Hash,
		TargetInstanceID: req.TargetInstanceID,
		Placement:        transfer.Placement{SavePath: req.SavePath, Category: req.Category, Tags: req.Tags},
		Options:          transfer.Options{After: req.After, LeaveStopped: req.LeaveStopped, ForcedWay: way},
		StartedBy:        transfer.StartedByUser,
	}, true
}

type transferProgress struct {
	Done  int64 `json:"done"`
	Total int64 `json:"total"`
	Speed int64 `json:"speed"`
}

type transferRow struct {
	ID               int64              `json:"id"`
	Kind             transfer.Kind      `json:"kind"`
	Way              transfer.Way       `json:"way"`
	State            transfer.State     `json:"state"`
	StartedBy        transfer.StartedBy `json:"startedBy"`
	RuleID           *int               `json:"ruleId"`
	RuleName         string             `json:"ruleName"`
	SourceInstanceID int                `json:"sourceInstanceId"`
	TargetInstanceID int                `json:"targetInstanceId"`
	Hash             string             `json:"hash"`
	Name             string             `json:"name"`
	Torrents         int                `json:"torrents"`
	Size             int64              `json:"size"`
	After            transfer.After     `json:"after"`
	LeaveStopped     bool               `json:"leaveStopped"`
	RetryAt          *time.Time         `json:"retryAt"`
	FailureClass     string             `json:"failureClass"`
	FailureReason    string             `json:"failureReason"`
	CreatedAt        time.Time          `json:"createdAt"`
	FinishedAt       *time.Time         `json:"finishedAt"`
	Hints            []transfer.Hint    `json:"hints"`
	Progress         *transferProgress  `json:"progress"`
}

type transferCounts struct {
	Active   int `json:"active"`
	Waiting  int `json:"waiting"`
	Finished int `json:"finished"`
	Failed   int `json:"failed"`
}

type transferListResponse struct {
	Rows   []transferRow  `json:"rows"`
	Counts transferCounts `json:"counts"`
}

type transferMember struct {
	Hash             string   `json:"hash"`
	Name             string   `json:"name"`
	TargetInstanceID int      `json:"targetInstanceId"`
	SavePath         string   `json:"savePath"`
	Category         string   `json:"category"`
	Tags             []string `json:"tags"`
}

type transferDetail struct {
	transferRow
	Steps   []transfer.Step    `json:"steps"`
	Log     []transfer.LogLine `json:"log"`
	Members []transferMember   `json:"members"`
}

const (
	transferPageSize = 50
	transferPageMax  = 500
)

type ruleNames struct {
	rules RuleReader
	names map[[2]int]string
}

func (n *ruleNames) name(ctx context.Context, instanceID int, id *int) string {
	if id == nil || n.rules == nil {
		return ""
	}
	key := [2]int{instanceID, *id}
	if name, ok := n.names[key]; ok {
		return name
	}
	var name string
	if rule, err := n.rules.Get(ctx, instanceID, *id); err == nil && rule != nil {
		name = rule.Name
	}
	n.names[key] = name
	return name
}

func (h *TransfersHandler) row(ctx context.Context, row transfer.Row, names *ruleNames) transferRow {
	out := transferRow{
		ID: row.ID, Kind: row.Kind, Way: row.Way, State: row.State, StartedBy: row.StartedBy,
		RuleID: row.RuleID, RuleName: names.name(ctx, row.SourceInstanceID, row.RuleID),
		SourceInstanceID: row.SourceInstanceID, TargetInstanceID: row.TargetInstanceID,
		Hash: row.Hash, Name: row.Name, Torrents: row.Torrents, Size: row.Size,
		After: row.Options.After, LeaveStopped: row.Options.LeaveStopped,
		FailureClass: string(row.FailureClass), FailureReason: row.FailureReason,
		CreatedAt: row.CreatedAt, Hints: row.Hints,
	}
	if out.Hints == nil {
		out.Hints = []transfer.Hint{}
	}
	if !row.RetryAt.IsZero() {
		out.RetryAt = &row.RetryAt
	}
	if !row.FinishedAt.IsZero() {
		out.FinishedAt = &row.FinishedAt
	}
	if p, ok := h.service.Progress(row.ID); ok {
		out.Progress = &transferProgress{Done: p.Done, Total: p.Total, Speed: p.Speed}
	}
	return out
}

func (h *TransfersHandler) List(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter, ok := transfer.Filters[query.Get("filter")]
	if !ok {
		RespondError(w, http.StatusBadRequest, "filter must be all, active, waiting, finished, or failed")
		return
	}
	limit, err := queryNumber(query.Get("limit"), transferPageSize)
	if err != nil || limit < 1 || limit > transferPageMax {
		RespondError(w, http.StatusBadRequest, fmt.Sprintf("limit must be a number from 1 to %d", transferPageMax))
		return
	}
	offset, err := queryNumber(query.Get("offset"), 0)
	if err != nil || offset < 0 {
		RespondError(w, http.StatusBadRequest, "offset must be 0 or a larger number")
		return
	}
	list, err := h.store.List(r.Context(), filter, limit, offset)
	if err != nil {
		log.Error().Err(err).Msg("transfer: failed to list the Transfers")
		RespondError(w, http.StatusInternalServerError, "Failed to list the Transfers")
		return
	}
	names := &ruleNames{rules: h.rules, names: map[[2]int]string{}}
	res := transferListResponse{Rows: make([]transferRow, 0, len(list.Rows)), Counts: transferCounts(list.Counts)}
	for _, row := range list.Rows {
		res.Rows = append(res.Rows, h.row(r.Context(), row, names))
	}
	RespondJSON(w, http.StatusOK, res)
}

func queryNumber(value string, fallback int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}

func transferID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "transferID"), 10, 64)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid Transfer id")
		return 0, false
	}
	return id, true
}

func (h *TransfersHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := transferID(w, r)
	if !ok {
		return
	}
	record, err := h.store.Get(r.Context(), id)
	if err != nil {
		h.respondActionError(w, err, "read")
		return
	}
	row := transfer.Row{
		ID: record.ID, Kind: record.Kind, Way: record.Way, State: record.State, StartedBy: record.StartedBy,
		RuleID: record.RuleID, SourceInstanceID: record.SourceInstanceID, TargetInstanceID: record.TargetInstanceID,
		Hash: record.Members[0].Hash, Name: record.Members[0].Name, Torrents: len(record.Members), Size: record.Size, Options: record.Options,
		RetryAt: record.RetryAt, FailureClass: record.FailureClass, FailureReason: record.FailureReason,
		CreatedAt: record.CreatedAt, FinishedAt: record.FinishedAt,
	}
	res := transferDetail{Steps: record.Steps, Log: record.Log, Members: make([]transferMember, len(record.Members))}
	if res.Steps == nil {
		res.Steps = []transfer.Step{}
	}
	if res.Log == nil {
		res.Log = []transfer.LogLine{}
	}
	for _, line := range record.Log {
		if line.Hint != nil {
			row.Hints = append(row.Hints, *line.Hint)
		}
	}
	res.transferRow = h.row(r.Context(), row, &ruleNames{rules: h.rules, names: map[[2]int]string{}})
	for i, m := range record.Members {
		tags := m.Placement.Tags
		if tags == nil {
			tags = []string{}
		}
		res.Members[i] = transferMember{Hash: m.Hash, Name: m.Name, TargetInstanceID: m.TargetInstanceID,
			SavePath: m.Placement.SavePath, Category: m.Placement.Category, Tags: tags}
	}
	RespondJSON(w, http.StatusOK, res)
}

type transferSummaryResponse struct {
	Way          transfer.Way     `json:"way"`
	Bytes        int64            `json:"bytes"`
	Members      []transferMember `json:"members"`
	Free         int64            `json:"free"`
	FreeKnown    bool             `json:"freeKnown"`
	NoFileAccess bool             `json:"noFileAccess"`
	StayOnSource int              `json:"stayOnSource"`
	Hints        []transfer.Hint  `json:"hints"`
}

func (h *TransfersHandler) Summary(w http.ResponseWriter, r *http.Request) {
	req, ok := readTransferRequest(w, r)
	if !ok {
		return
	}
	summary, err := h.service.Summary(r.Context(), req)
	_, refused := errors.AsType[*transfer.RefusalError](err)
	switch {
	case refused, errors.Is(err, transfer.ErrClaimed):
		RespondError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		log.Error().Err(err).Msg("transfer: failed to plan the Transfer")
		RespondError(w, http.StatusInternalServerError, "Failed to plan the Transfer")
		return
	}
	res := transferSummaryResponse{Way: summary.Way, Bytes: summary.Bytes, Members: make([]transferMember, len(summary.Members)),
		Free: summary.Free, FreeKnown: summary.FreeKnown, NoFileAccess: summary.NoFileAccess,
		StayOnSource: summary.Stay, Hints: summary.Hints}
	if res.Hints == nil {
		res.Hints = []transfer.Hint{}
	}
	for i, m := range summary.Members {
		tags := m.Placement.Tags
		if tags == nil {
			tags = []string{}
		}
		res.Members[i] = transferMember{Hash: m.Hash, Name: m.Name, TargetInstanceID: m.TargetInstanceID,
			SavePath: m.Placement.SavePath, Category: m.Placement.Category, Tags: tags}
	}
	RespondJSON(w, http.StatusOK, res)
}

func (h *TransfersHandler) respondActionError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, transfer.ErrNotFound):
		RespondError(w, http.StatusNotFound, "Transfer not found")
	case actionDoesNotFit(err):
		RespondError(w, http.StatusConflict, err.Error())
	default:
		log.Error().Err(err).Str("action", action).Msg("transfer: failed to act on the Transfer")
		RespondError(w, http.StatusInternalServerError, "Failed to "+action+" the Transfer")
	}
}

func actionDoesNotFit(err error) bool {
	_, refused := errors.AsType[*transfer.RefusalError](err)
	return refused || errors.Is(err, transfer.ErrClaimed) || errors.Is(err, transfer.ErrNotRetryable) ||
		errors.Is(err, transfer.ErrFinished) || errors.Is(err, transfer.ErrCancelTooLate) || errors.Is(err, transfer.ErrNotFinished)
}

func (h *TransfersHandler) Retry(w http.ResponseWriter, r *http.Request) {
	id, ok := transferID(w, r)
	if !ok {
		return
	}
	newID, err := h.service.Retry(r.Context(), id)
	if err != nil {
		h.respondActionError(w, err, "retry")
		return
	}
	RespondJSON(w, http.StatusAccepted, startTransferResponse{ID: newID})
}

func (h *TransfersHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id, ok := transferID(w, r)
	if !ok {
		return
	}
	if err := h.service.Cancel(r.Context(), id); err != nil {
		h.respondActionError(w, err, "cancel")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *TransfersHandler) Remove(w http.ResponseWriter, r *http.Request) {
	id, ok := transferID(w, r)
	if !ok {
		return
	}
	if err := h.service.Remove(r.Context(), id); err != nil {
		h.respondActionError(w, err, "remove")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type bulkTransfersRequest struct {
	Action string  `json:"action"`
	IDs    []int64 `json:"ids"`
	Filter *string `json:"filter"`
}

type bulkTransfersResponse struct {
	Done    int      `json:"done"`
	Skipped int      `json:"skipped"`
	Failed  int      `json:"failed"`
	Reasons []string `json:"reasons"`
}

func (h *TransfersHandler) Bulk(w http.ResponseWriter, r *http.Request) {
	var req bulkTransfersRequest
	if err := json.UnmarshalRead(r.Body, &req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	act, ok := map[string]func(context.Context, int64) error{
		"retry": func(ctx context.Context, id int64) error {
			_, err := h.service.Retry(ctx, id)
			return err
		},
		"cancel": h.service.Cancel,
		"remove": h.service.Remove,
	}[req.Action]
	if !ok {
		RespondError(w, http.StatusBadRequest, "action must be retry, cancel, or remove")
		return
	}
	ids := req.IDs
	if req.Filter != nil && req.Action == "retry" {
		RespondError(w, http.StatusBadRequest, "retry takes ids, not a filter")
		return
	}
	if req.Filter != nil {
		filter, ok := transfer.Filters[*req.Filter]
		if !ok {
			RespondError(w, http.StatusBadRequest, "filter must be all, active, waiting, finished, or failed")
			return
		}
		var err error
		if ids, err = h.store.IDs(r.Context(), filter); err != nil {
			log.Error().Err(err).Msg("transfer: failed to list the Transfers of a bulk action")
			RespondError(w, http.StatusInternalServerError, "Failed to list the Transfers")
			return
		}
	}

	res := bulkTransfersResponse{Reasons: []string{}}
	errored := 0
	var lastErr error
	for _, id := range ids {
		err := act(r.Context(), id)
		refusal, refused := errors.AsType[*transfer.RefusalError](err)
		switch {
		case err == nil:
			res.Done++
		case refused:
			res.Failed++
			if !slices.Contains(res.Reasons, refusal.Reason) {
				res.Reasons = append(res.Reasons, refusal.Reason)
			}
		case errors.Is(err, transfer.ErrNotFound), actionDoesNotFit(err):
			res.Skipped++
		default:
			res.Failed++
			errored++
			lastErr = err
		}
	}
	if errored > 0 {
		log.Error().Err(lastErr).Int("rows", errored).Str("action", req.Action).Msg("transfer: a bulk action failed on some rows")
	}
	RespondJSON(w, http.StatusOK, res)
}
