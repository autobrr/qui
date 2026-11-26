// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/share"
	"github.com/autobrr/qui/internal/services/sharetracker"
)

type SharesHandler struct {
	service *share.Service
}

func NewSharesHandler(service *share.Service) *SharesHandler {
	return &SharesHandler{service: service}
}

func (h *SharesHandler) Routes(r chi.Router) {
	r.Post("/", h.Start)
	r.Get("/", h.List)
	r.Get("/settings", h.Settings)
	r.Put("/settings", h.SaveSettings)
	r.Post("/{shareID}/stop", h.Stop)
	r.Post("/{shareID}/unlock", h.Unlock)
	r.Get("/{shareID}/torrent", h.TorrentFile)
	r.Delete("/{shareID}", h.Remove)
}

type startSharesRequest struct {
	InstanceID          int      `json:"instanceId"`
	Hashes              []string `json:"hashes"`
	Days                int      `json:"days"`
	EndAtFirstDownload  *bool    `json:"endAtFirstDownload"`
	FirstDownloaderOnly *bool    `json:"firstDownloaderOnly"`
}

type refusedShare struct {
	Hash   string `json:"hash"`
	Reason string `json:"reason"`
}

type startSharesResponse struct {
	Error   string         `json:"error,omitempty"`
	Shares  []shareView    `json:"shares"`
	Refused []refusedShare `json:"refused"`
}

type shareView struct {
	ID                  int64        `json:"id"`
	InstanceID          int          `json:"instanceId"`
	Hash                string       `json:"hash"`
	Name                string       `json:"name"`
	Size                int64        `json:"size"`
	Tracker             string       `json:"tracker"`
	EndAtFirstDownload  bool         `json:"endAtFirstDownload"`
	FirstDownloaderOnly bool         `json:"firstDownloaderOnly"`
	State               string       `json:"state"`
	EndReason           string       `json:"endReason"`
	EndReasonCode       string       `json:"endReasonCode"`
	CreatedAt           time.Time    `json:"createdAt"`
	ExpiresAt           time.Time    `json:"expiresAt"`
	EndedAt             *time.Time   `json:"endedAt"`
	Peers               []share.Peer `json:"peers"`
	Progress            float64      `json:"progress"`
	Downloaded          int64        `json:"downloaded"`
	Uploaded            int64        `json:"uploaded"`
}

func viewOf(sh *share.Share) shareView {
	v := shareView{
		ID:                  sh.ID,
		InstanceID:          sh.InstanceID,
		Hash:                sh.Hash,
		Name:                sh.Name,
		Size:                sh.Size,
		Tracker:             string(sh.Tracker),
		EndAtFirstDownload:  sh.EndAtFirstDownload,
		FirstDownloaderOnly: sh.FirstDownloaderOnly,
		State:               string(sh.State),
		EndReason:           sh.EndReason,
		EndReasonCode:       sh.EndReasonCode(),
		CreatedAt:           sh.CreatedAt,
		ExpiresAt:           sh.ExpiresAt,
		Peers:               sh.Peers,
		Downloaded:          sh.Downloaded,
		Uploaded:            sh.Uploaded,
	}
	if v.Peers == nil {
		v.Peers = []share.Peer{}
	}
	if !sh.EndedAt.IsZero() {
		v.EndedAt = &sh.EndedAt
	}
	if sh.Size > 0 {
		v.Progress = min(float64(sh.Downloaded)/float64(sh.Size), 1)
	}
	return v
}

func (h *SharesHandler) Start(w http.ResponseWriter, r *http.Request) {
	var req startSharesRequest
	if err := json.UnmarshalRead(r.Body, &req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.InstanceID <= 0 || len(req.Hashes) == 0 || len(req.Hashes) > 100 {
		RespondError(w, http.StatusBadRequest, "instanceId and 1 to 100 hashes are required")
		return
	}

	res := startSharesResponse{Shares: []shareView{}, Refused: []refusedShare{}}
	failed := 0
	var lastErr error
	for _, hash := range req.Hashes {
		sh, err := h.service.Start(r.Context(), share.Request{
			InstanceID:          req.InstanceID,
			Hash:                hash,
			Days:                req.Days,
			EndAtFirstDownload:  req.EndAtFirstDownload,
			FirstDownloaderOnly: req.FirstDownloaderOnly,
		})
		switch {
		case share.IsRefusal(err):
			res.Refused = append(res.Refused, refusedShare{Hash: hash, Reason: err.Error()})
		case err != nil:
			log.Debug().Err(err).Int("instanceID", req.InstanceID).Str("hash", hash).Msg("share: failed to start the Share")
			res.Refused = append(res.Refused, refusedShare{Hash: hash, Reason: "qui could not start the Share. The log has the details."})
			failed++
			lastErr = err
		default:
			res.Shares = append(res.Shares, viewOf(sh))
		}
	}
	if failed > 0 {
		log.Error().Err(lastErr).Int("instanceID", req.InstanceID).Int("started", len(res.Shares)).Int("failed", failed).
			Msg("share: failed to start Shares")
	}
	switch {
	case len(res.Shares) > 0:
		RespondJSON(w, http.StatusCreated, res)
	case failed > 0:
		res.Error = "Failed to start the Share"
		RespondJSON(w, http.StatusInternalServerError, res)
	default:
		res.Error = res.Refused[0].Reason
		RespondJSON(w, http.StatusConflict, res)
	}
}

func (h *SharesHandler) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.service.List(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("share: failed to list the Shares")
		RespondError(w, http.StatusInternalServerError, "Failed to list the Shares")
		return
	}
	views := make([]shareView, len(list))
	for i := range list {
		views[i] = viewOf(&list[i])
	}
	RespondJSON(w, http.StatusOK, views)
}

func (h *SharesHandler) Stop(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, "stop", h.service.Stop)
}

func (h *SharesHandler) Unlock(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, "unlock", h.service.Unlock)
}

func (h *SharesHandler) Remove(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, "remove", h.service.Remove)
}

func (h *SharesHandler) act(w http.ResponseWriter, r *http.Request, name string, action func(context.Context, int64) error) {
	id, ok := shareID(w, r)
	if !ok {
		return
	}
	err := action(r.Context(), id)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, share.ErrNotFound):
		RespondError(w, http.StatusNotFound, "Share not found")
	case errors.Is(err, share.ErrNotEnded):
		RespondError(w, http.StatusConflict, "Only an ended Share can be removed.")
	case share.IsRefusal(err):
		RespondError(w, http.StatusConflict, err.Error())
	default:
		log.Error().Err(err).Int64("share", id).Msgf("share: failed to %s the Share", name)
		RespondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to %s the Share", name))
	}
}

func (h *SharesHandler) TorrentFile(w http.ResponseWriter, r *http.Request) {
	id, ok := shareID(w, r)
	if !ok {
		return
	}
	data, filename, err := h.service.TorrentFile(r.Context(), id)
	switch {
	case errors.Is(err, share.ErrNotFound):
		RespondError(w, http.StatusNotFound, "Share not found")
		return
	case err != nil:
		log.Error().Err(err).Int64("share", id).Msg("share: failed to read the .torrent of the Share")
		RespondError(w, http.StatusInternalServerError, "Failed to read the .torrent of the Share")
		return
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": filename})
	if disposition == "" {
		disposition = fmt.Sprintf("attachment; filename=%q", filename)
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Content-Type", "application/x-bittorrent")
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(data); err != nil { //nolint:gosec // G705: a .torrent body served as an attachment with nosniff, not markup
		log.Error().Err(err).Int64("share", id).Msg("share: failed to write the .torrent of the Share")
	}
}

func shareID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "shareID"), 10, 64)
	if err != nil || id <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid Share ID")
		return 0, false
	}
	return id, true
}

type shareSettings struct {
	Port           int                   `json:"port"`
	PublicAddress  string                `json:"publicAddress"`
	TrustedProxies []string              `json:"trustedProxies"`
	TrackerURL     string                `json:"trackerUrl"`
	MaxActive      int                   `json:"maxActive"`
	PeersPerShare  int                   `json:"peersPerShare"`
	Defaults       shareSettingsDefaults `json:"defaults"`
	TrackerInUse   string                `json:"trackerInUse"`
}

type shareSettingsDefaults struct {
	Days                int  `json:"days"`
	EndAtFirstDownload  bool `json:"endAtFirstDownload"`
	FirstDownloaderOnly bool `json:"firstDownloaderOnly"`
}

func settingsView(s share.Settings) shareSettings {
	proxies := s.Tracker.TrustedProxies
	if proxies == nil {
		proxies = []string{}
	}
	return shareSettings{
		Port:           s.Tracker.Port,
		PublicAddress:  s.Tracker.PublicAddress,
		TrustedProxies: proxies,
		TrackerURL:     s.Options.TrackerURL,
		MaxActive:      s.Options.MaxActive,
		PeersPerShare:  s.Options.PeersPerShare,
		Defaults: shareSettingsDefaults{
			Days:                s.Options.Defaults.Days,
			EndAtFirstDownload:  s.Options.Defaults.EndAtFirstDownload,
			FirstDownloaderOnly: s.Options.Defaults.FirstDownloaderOnly,
		},
		TrackerInUse: string(s.TrackerInUse()),
	}
}

func (h *SharesHandler) Settings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.service.Settings(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("share: failed to read the Share settings")
		RespondError(w, http.StatusInternalServerError, "Failed to read the Share settings")
		return
	}
	RespondJSON(w, http.StatusOK, settingsView(settings))
}

func (h *SharesHandler) SaveSettings(w http.ResponseWriter, r *http.Request) {
	var body shareSettings
	if err := json.UnmarshalRead(r.Body, &body); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	settings := share.Settings{
		Tracker: sharetracker.Settings{
			Port:           body.Port,
			PublicAddress:  strings.TrimSpace(body.PublicAddress),
			TrustedProxies: body.TrustedProxies,
		},
		Options: share.Options{
			MaxActive:     body.MaxActive,
			PeersPerShare: body.PeersPerShare,
			TrackerURL:    strings.TrimSpace(body.TrackerURL),
			Defaults: share.Defaults{
				Days:                body.Defaults.Days,
				EndAtFirstDownload:  body.Defaults.EndAtFirstDownload,
				FirstDownloaderOnly: body.Defaults.FirstDownloaderOnly,
			},
		},
	}
	err := h.service.SaveSettings(r.Context(), settings)
	switch {
	case errors.Is(err, share.ErrInvalidSettings), errors.Is(err, sharetracker.ErrInvalidSettings):
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, sharetracker.ErrPortBusy):
		RespondError(w, http.StatusConflict, fmt.Sprintf("Another program uses port %d. Pick another port.", body.Port))
		return
	case share.IsRefusal(err):
		RespondError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		log.Error().Err(err).Msg("share: failed to save the Share settings")
		RespondError(w, http.StatusInternalServerError, "Failed to save the Share settings")
		return
	}
	h.Settings(w, r)
}
