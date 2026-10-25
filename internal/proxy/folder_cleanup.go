// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/qbittorrent"
)

// statusRecorder notes the status the proxied qBittorrent answered with. It
// starts at 0, not 200, so a response that never wrote a status cannot read
// as accepted.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// interceptForFolderCleanup forwards a write that can move torrents, with a
// snapshot taken first.
func (h *Handler) interceptForFolderCleanup(w http.ResponseWriter, r *http.Request, opFor func(form qbtForm) (qbittorrent.FolderCleanupOp, bool)) {
	body, err := bufferRequestBody(r)
	if err != nil {
		log.Warn().Err(err).Int("instanceId", GetInstanceIDFromContext(r.Context())).Str("path", r.URL.Path).Msg("Failed to read request body")
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}
	h.forwardWithFolderCleanup(w, r, body, opFor)
}

// forwardWithFolderCleanup snapshots the torrents opFor names, sends body to
// qBittorrent, and queues the cleanup only when qBittorrent accepted it. A
// body that does not parse is forwarded unchanged with no snapshot, so
// intercepting cannot break a request qBittorrent would take.
func (h *Handler) forwardWithFolderCleanup(w http.ResponseWriter, r *http.Request, body []byte, opFor func(form qbtForm) (qbittorrent.FolderCleanupOp, bool)) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	var folders qbittorrent.FolderCleanupBatch
	if form, ok := parseQBTForm(r, body); ok {
		if op, ok := opFor(form); ok {
			folders = h.syncManager.PrepareProxiedFolderCleanup(ctx, instanceID, op)
		}
	}
	rec := &statusRecorder{ResponseWriter: w}
	r.Body = io.NopCloser(bytes.NewReader(body))
	h.proxy.ServeHTTP(rec, r)
	// The cleanup judges folders against the sync cache, so refresh it as qui
	// does after its own writes. A write that queued nothing, such as a delete
	// that keeps the files, needs no refresh.
	if rec.status == http.StatusOK && folders.Queue() {
		h.syncManager.HintMainDataRefresh(instanceID, "proxy_folder_cleanup")
	}
}

// qbtForm is a request's parameters as qBittorrent 5 reads them for a POST
// (WebApplication::processRequest, RequestParser::parsePostMessage): the body
// only, never the query string, with the last value of a repeated key.
type qbtForm url.Values

func (f qbtForm) get(key string) string {
	if v := f[key]; len(v) > 0 {
		return v[len(v)-1]
	}
	return ""
}

// parseQBTForm reads body as qBittorrent does when the lowercased Content-Type
// starts with the urlencoded type (RequestParser::parsePostMessage), whatever
// parameters follow. It returns ok false for any other POST body and for
// another method. qui does not parse a multipart body, which qBittorrent also
// reads, so it takes no snapshot rather than guess.
func parseQBTForm(r *http.Request, body []byte) (qbtForm, bool) {
	if r.Method != http.MethodPost ||
		!strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/x-www-form-urlencoded") {
		return nil, false
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, false
	}
	return qbtForm(values), true
}

// splitHashes reads the WebAPI's "|"-separated list; "all" stays ["all"].
func splitHashes(list string) []string {
	return strings.Split(list, "|")
}

func (h *Handler) handleSetCategory(w http.ResponseWriter, r *http.Request) {
	h.interceptForFolderCleanup(w, r, func(form qbtForm) (qbittorrent.FolderCleanupOp, bool) {
		return qbittorrent.FolderCleanupOp{Kind: qbittorrent.FolderCleanupSetCategory, Hashes: splitHashes(form.get("hashes")), Category: form.get("category")}, true
	})
}

func (h *Handler) handleSetAutoManagement(w http.ResponseWriter, r *http.Request) {
	h.interceptForFolderCleanup(w, r, func(form qbtForm) (qbittorrent.FolderCleanupOp, bool) {
		return qbittorrent.FolderCleanupOp{Kind: qbittorrent.FolderCleanupEnableATM, Hashes: splitHashes(form.get("hashes"))},
			strings.EqualFold(form.get("enable"), "true")
	})
}

func (h *Handler) handleEditCategory(w http.ResponseWriter, r *http.Request) {
	h.interceptForFolderCleanup(w, r, func(form qbtForm) (qbittorrent.FolderCleanupOp, bool) {
		return qbittorrent.FolderCleanupOp{Kind: qbittorrent.FolderCleanupEditCategory, EditedCategory: form.get("category"), Path: form.get("savePath")}, true
	})
}

// setSavePath and setDownloadPath name their torrents "id", not "hashes".
func (h *Handler) handleSetSavePath(w http.ResponseWriter, r *http.Request) {
	h.interceptForFolderCleanup(w, r, func(form qbtForm) (qbittorrent.FolderCleanupOp, bool) {
		return qbittorrent.FolderCleanupOp{Kind: qbittorrent.FolderCleanupSetSavePath, Hashes: splitHashes(form.get("id")), Path: form.get("path")}, true
	})
}

func (h *Handler) handleSetDownloadPath(w http.ResponseWriter, r *http.Request) {
	h.interceptForFolderCleanup(w, r, func(form qbtForm) (qbittorrent.FolderCleanupOp, bool) {
		return qbittorrent.FolderCleanupOp{Kind: qbittorrent.FolderCleanupSetDownloadPath, Hashes: splitHashes(form.get("id")), Path: form.get("path")}, true
	})
}
