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

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (h *Handler) interceptForFolderCleanup(w http.ResponseWriter, r *http.Request, opFor func(form qbtForm) (qbittorrent.FolderCleanupOp, bool)) {
	body, err := bufferRequestBody(r)
	if err != nil {
		log.Warn().Err(err).Int("instanceId", GetInstanceIDFromContext(r.Context())).Str("path", r.URL.Path).Msg("Failed to read request body")
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}
	h.forwardWithFolderCleanup(w, r, body, opFor)
}

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
	if rec.status == http.StatusOK && folders.Queue() {
		h.syncManager.HintMainDataRefresh(instanceID, "proxy_folder_cleanup")
	}
}

type qbtForm url.Values

func (f qbtForm) get(key string) string {
	if v := f[key]; len(v) > 0 {
		return v[len(v)-1]
	}
	return ""
}

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
