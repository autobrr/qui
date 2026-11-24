// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/api/middleware"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/redact"
)

// RSSHandler handles RSS API endpoints
type RSSHandler struct {
	syncManager *qbittorrent.SyncManager
}

// NewRSSHandler creates a new RSS handler
func NewRSSHandler(syncManager *qbittorrent.SyncManager) *RSSHandler {
	return &RSSHandler{
		syncManager: syncManager,
	}
}

// Routes registers RSS routes on the given router
func (h *RSSHandler) Routes(r chi.Router) {
	rss := middleware.Permissions(models.AreaRSS)

	// Feed and folder management
	r.With(rss.Read).Get("/items", h.GetItems)
	r.With(rss.Write).Post("/folders", h.AddFolder)
	r.With(rss.Write).Post("/feeds", h.AddFeed)
	r.With(rss.Write).Put("/feeds/url", h.SetFeedURL)
	r.With(rss.Write).Post("/items/move", h.MoveItem)
	r.With(rss.Write).Delete("/items", h.RemoveItem)
	r.With(rss.Run).Post("/items/refresh", h.RefreshItem)

	// Article management
	r.With(rss.Write).Post("/articles/read", h.MarkAsRead)

	// Auto-download rules
	r.With(rss.Read).Get("/rules", h.GetRules)
	r.With(rss.Write).Post("/rules", h.SetRule)
	r.With(rss.Write).Put("/rules/{ruleName}/rename", h.RenameRule)
	r.With(rss.Write).Delete("/rules/{ruleName}", h.RemoveRule)
	r.With(rss.Read).Get("/rules/{ruleName}/preview", h.GetMatchingArticles)
	r.With(rss.Run).Post("/rules/reprocess", h.ReprocessRules)
}

// Request/Response types

type AddFolderRequest struct {
	Path string `json:"path"`
}

type AddFeedRequest struct {
	URL  string `json:"url"`
	Path string `json:"path"`
}

type SetFeedURLRequest struct {
	Path string `json:"path"`
	URL  string `json:"url"`
}

type MoveItemRequest struct {
	ItemPath string `json:"itemPath"`
	DestPath string `json:"destPath"`
}

type RemoveItemRequest struct {
	Path string `json:"path"`
}

type RefreshItemRequest struct {
	ItemPath string `json:"itemPath"`
}

type MarkAsReadRequest struct {
	ItemPath  string `json:"itemPath"`
	ArticleID string `json:"articleId,omitempty"`
}

type SetRuleRequest struct {
	Name string                  `json:"name"`
	Rule qbt.RSSAutoDownloadRule `json:"rule"`
}

type RenameRuleRequest struct {
	NewName string `json:"newName"`
}

// Handlers

// GetItems retrieves all RSS feeds and folders
func (h *RSSHandler) GetItems(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	withData := true
	if withDataParam := r.URL.Query().Get("withData"); withDataParam != "" {
		withData = withDataParam == "true"
	}

	items, err := h.syncManager.GetRSSItems(r.Context(), instanceID, withData)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "GetRSSItems") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("failed to get RSS items")
		RespondError(w, http.StatusInternalServerError, "Failed to get RSS items")
		return
	}

	if middleware.CanReadTrackerPasskeys(r) {
		RespondJSON(w, http.StatusOK, items)
		return
	}
	full, err := json.Marshal(items)
	if err == nil {
		full, err = hideRSSLinks(full)
	}
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("failed to hide RSS links")
		RespondError(w, http.StatusInternalServerError, "Failed to get RSS items")
		return
	}
	RespondJSON(w, http.StatusOK, json.RawMessage(full))
}

// AddFolder creates a new RSS folder
func (h *RSSHandler) AddFolder(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	var req AddFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if req.Path == "" {
		RespondError(w, http.StatusBadRequest, "Path is required")
		return
	}
	if !h.restoreRSSNames(w, r, instanceID, rssNames{parent: &req.Path}) {
		return
	}

	if err := h.syncManager.AddRSSFolder(r.Context(), instanceID, req.Path); err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "AddRSSFolder") {
			return
		}
		logRSSPaths(log.Error(), err, "path", req.Path).Int("instanceID", instanceID).Msg("failed to add RSS folder")
		RespondError(w, http.StatusInternalServerError, "Failed to add RSS folder")
		return
	}

	RespondJSON(w, http.StatusCreated, nil)
}

// AddFeed adds a new RSS feed
func (h *RSSHandler) AddFeed(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	var req AddFeedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if req.URL == "" {
		RespondError(w, http.StatusBadRequest, "URL is required")
		return
	}

	parsedURL, err := url.Parse(req.URL)
	if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		RespondError(w, http.StatusBadRequest, "Invalid URL: must be a valid http or https URL")
		return
	}
	if !h.restoreRSSNames(w, r, instanceID, rssNames{path: &req.Path}) {
		return
	}

	if req.Path != "" {
		h.addFeedToFolder(w, r, instanceID, req.URL, req.Path)
		return
	}

	// No folder specified - add directly to root
	if err := h.syncManager.AddRSSFeed(r.Context(), instanceID, req.URL, ""); err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "AddRSSFeed") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("url", redact.URLString(req.URL)).Msg("failed to add RSS feed")
		RespondError(w, http.StatusInternalServerError, "Failed to add RSS feed")
		return
	}

	RespondJSON(w, http.StatusCreated, nil)
}

// qBittorrent's path param is the full item path, not the parent folder, so
// the feed goes to the root first and then moves into targetFolder.
func (h *RSSHandler) addFeedToFolder(w http.ResponseWriter, r *http.Request, instanceID int, feedURL, targetFolder string) {
	ctx := r.Context()

	existingFeeds, err := h.syncManager.GetRSSItems(ctx, instanceID, false)
	hasBaseline := err == nil
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("failed to get existing RSS items before add")
	}

	existingNames := make(map[string]bool)
	if hasBaseline {
		for name := range existingFeeds {
			existingNames[name] = true
		}
	}

	if err := h.syncManager.AddRSSFeed(ctx, instanceID, feedURL, ""); err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "AddRSSFeed") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("url", redact.URLString(feedURL)).Msg("failed to add RSS feed")
		RespondError(w, http.StatusInternalServerError, "Failed to add RSS feed")
		return
	}

	newFeeds, err := h.syncManager.GetRSSItems(ctx, instanceID, false)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("failed to get RSS items after add")
		RespondJSON(w, http.StatusCreated, WarningResponse{
			Warning: "Feed added to root - could not move to folder",
		})
		return
	}

	if !hasBaseline {
		log.Warn().Int("instanceID", instanceID).Msg("RSS feed added to root; could not determine baseline to safely move into folder")
		RespondJSON(w, http.StatusCreated, WarningResponse{
			Warning: "Feed added to root - could not move to folder",
		})
		return
	}

	newFeedName := findNewRSSFeedName(newFeeds, existingNames, instanceID, feedURL)
	if newFeedName == "" {
		log.Warn().Int("instanceID", instanceID).Str("url", redact.URLString(feedURL)).Msg("could not identify RSS feed to move to folder")
		RespondJSON(w, http.StatusCreated, WarningResponse{
			Warning: "Feed added to root - could not identify feed to move",
		})
		return
	}

	// destPath must be the full new path: "FolderName\FeedName"
	destPath := targetFolder + "\\" + newFeedName
	if err := h.syncManager.MoveRSSItem(ctx, instanceID, newFeedName, destPath); err != nil {
		logRSSPaths(log.Warn(), err, "feedName", newFeedName, "destPath", destPath).
			Int("instanceID", instanceID).
			Msg("failed to move RSS feed to folder (feed was added to root)")
		RespondJSON(w, http.StatusCreated, WarningResponse{
			Warning: "Feed added to root - could not move to folder",
		})
		return
	}

	RespondJSON(w, http.StatusCreated, nil)
}

// findNewRSSFeedName returns the one feed that is not in existingNames and whose
// URL matches feedURL, or "" when no feed or several feeds match.
func findNewRSSFeedName(newFeeds qbt.RSSItems, existingNames map[string]bool, instanceID int, feedURL string) string {
	type rssFeedMeta struct {
		URL string `json:"url"`
	}

	var candidates []string
	for name, rawItem := range newFeeds {
		if existingNames[name] {
			continue
		}

		var meta rssFeedMeta
		if err := json.Unmarshal(rawItem, &meta); err != nil {
			log.Warn().Err(err).Str("itemName", name).Msg("failed to unmarshal RSS item while finding feed to move")
			continue
		}
		if meta.URL == "" {
			continue // folder
		}
		if !matchesRSSFeedURL(meta.URL, feedURL) {
			continue
		}
		candidates = append(candidates, name)
	}

	switch {
	case len(candidates) == 1:
		return candidates[0]
	case len(candidates) > 1:
		log.Warn().
			Int("instanceID", instanceID).
			Str("url", redact.URLString(feedURL)).
			Strs("candidates", candidates).
			Msg("multiple RSS feeds matched URL; cannot safely move")
	}
	return ""
}

// SetFeedURL changes the URL of an existing feed
func (h *RSSHandler) SetFeedURL(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	var req SetFeedURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if req.Path == "" || req.URL == "" {
		RespondError(w, http.StatusBadRequest, "Path and URL are required")
		return
	}

	parsedURL, err := url.Parse(req.URL)
	if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		RespondError(w, http.StatusBadRequest, "Invalid URL: must be a valid http or https URL")
		return
	}
	if !h.restoreRSSNames(w, r, instanceID, rssNames{path: &req.Path, feedURL: &req.URL}) {
		return
	}

	if err := h.syncManager.SetRSSFeedURL(r.Context(), instanceID, req.Path, req.URL); err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "SetRSSFeedURL") {
			return
		}
		logRSSPaths(log.Error(), err, "path", req.Path).Int("instanceID", instanceID).Msg("failed to set RSS feed URL")
		RespondError(w, http.StatusInternalServerError, "Failed to set RSS feed URL")
		return
	}

	RespondJSON(w, http.StatusOK, nil)
}

// MoveItem moves a feed or folder to a new location
func (h *RSSHandler) MoveItem(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	var req MoveItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if req.ItemPath == "" {
		RespondError(w, http.StatusBadRequest, "ItemPath is required")
		return
	}
	if !h.restoreRSSNames(w, r, instanceID, rssNames{path: &req.ItemPath, dest: &req.DestPath}) {
		return
	}

	if err := h.syncManager.MoveRSSItem(r.Context(), instanceID, req.ItemPath, req.DestPath); err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "MoveRSSItem") {
			return
		}
		logRSSPaths(log.Error(), err, "itemPath", req.ItemPath, "destPath", req.DestPath).Int("instanceID", instanceID).Msg("failed to move RSS item")
		RespondError(w, http.StatusInternalServerError, "Failed to move RSS item")
		return
	}

	RespondJSON(w, http.StatusOK, nil)
}

// RemoveItem removes a feed or folder
func (h *RSSHandler) RemoveItem(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	var req RemoveItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if req.Path == "" {
		RespondError(w, http.StatusBadRequest, "Path is required")
		return
	}
	if !h.restoreRSSNames(w, r, instanceID, rssNames{path: &req.Path}) {
		return
	}

	if err := h.syncManager.RemoveRSSItem(r.Context(), instanceID, req.Path); err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "RemoveRSSItem") {
			return
		}
		logRSSPaths(log.Error(), err, "path", req.Path).Int("instanceID", instanceID).Msg("failed to remove RSS item")
		RespondError(w, http.StatusInternalServerError, "Failed to remove RSS item")
		return
	}

	RespondJSON(w, http.StatusOK, nil)
}

// RefreshItem triggers a manual refresh of a feed or folder.
// An empty ItemPath refreshes all feeds.
func (h *RSSHandler) RefreshItem(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	var req RefreshItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	// Trim whitespace - empty string is valid (refreshes all feeds)
	itemPath := strings.TrimSpace(req.ItemPath)
	if !h.restoreRSSNames(w, r, instanceID, rssNames{path: &itemPath}) {
		return
	}

	if err := h.syncManager.RefreshRSSItem(r.Context(), instanceID, itemPath); err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "RefreshRSSItem") {
			return
		}
		logRSSPaths(log.Error(), err, "itemPath", itemPath).Int("instanceID", instanceID).Msg("failed to refresh RSS item")
		RespondError(w, http.StatusInternalServerError, "Failed to refresh RSS item")
		return
	}

	RespondJSON(w, http.StatusOK, nil)
}

// MarkAsRead marks articles as read
func (h *RSSHandler) MarkAsRead(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	var req MarkAsReadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if req.ItemPath == "" {
		RespondError(w, http.StatusBadRequest, "ItemPath is required")
		return
	}
	if !h.restoreRSSNames(w, r, instanceID, rssNames{path: &req.ItemPath, articleID: &req.ArticleID}) {
		return
	}

	if err := h.syncManager.MarkRSSItemAsRead(r.Context(), instanceID, req.ItemPath, req.ArticleID); err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "MarkRSSItemAsRead") {
			return
		}
		logRSSPaths(log.Error(), err, "itemPath", req.ItemPath).Int("instanceID", instanceID).Msg("failed to mark RSS item as read")
		RespondError(w, http.StatusInternalServerError, "Failed to mark RSS item as read")
		return
	}

	RespondJSON(w, http.StatusOK, nil)
}

// GetRules retrieves all RSS auto-download rules
func (h *RSSHandler) GetRules(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	rules, err := h.syncManager.GetRSSRules(r.Context(), instanceID)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "GetRSSRules") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("failed to get RSS rules")
		RespondError(w, http.StatusInternalServerError, "Failed to get RSS rules")
		return
	}

	hideRSSRuleFeeds(r, rules)
	RespondJSON(w, http.StatusOK, rules)
}

// SetRule creates or updates an auto-download rule
func (h *RSSHandler) SetRule(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	var req SetRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if req.Name == "" {
		RespondError(w, http.StatusBadRequest, "Name is required")
		return
	}
	if !h.restoreRSSRuleFeeds(w, r, instanceID, &req) {
		return
	}

	if err := h.syncManager.SetRSSRule(r.Context(), instanceID, req.Name, req.Rule); err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "SetRSSRule") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("ruleName", req.Name).Msg("failed to set RSS rule")
		RespondError(w, http.StatusInternalServerError, "Failed to set RSS rule")
		return
	}

	RespondJSON(w, http.StatusCreated, nil)
}

// RenameRule renames an existing rule
func (h *RSSHandler) RenameRule(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	ruleName := chi.URLParam(r, "ruleName")
	if ruleName == "" {
		RespondError(w, http.StatusBadRequest, "Rule name is required")
		return
	}

	var req RenameRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if req.NewName == "" {
		RespondError(w, http.StatusBadRequest, "New name is required")
		return
	}

	if err := h.syncManager.RenameRSSRule(r.Context(), instanceID, ruleName, req.NewName); err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "RenameRSSRule") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("ruleName", ruleName).Msg("failed to rename RSS rule")
		RespondError(w, http.StatusInternalServerError, "Failed to rename RSS rule")
		return
	}

	RespondJSON(w, http.StatusOK, nil)
}

// RemoveRule deletes an auto-download rule
func (h *RSSHandler) RemoveRule(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	ruleName := chi.URLParam(r, "ruleName")
	if ruleName == "" {
		RespondError(w, http.StatusBadRequest, "Rule name is required")
		return
	}

	if err := h.syncManager.RemoveRSSRule(r.Context(), instanceID, ruleName); err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "RemoveRSSRule") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("ruleName", ruleName).Msg("failed to remove RSS rule")
		RespondError(w, http.StatusInternalServerError, "Failed to remove RSS rule")
		return
	}

	RespondJSON(w, http.StatusOK, nil)
}

// GetMatchingArticles gets articles matching a rule for preview
func (h *RSSHandler) GetMatchingArticles(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	ruleName := chi.URLParam(r, "ruleName")
	if ruleName == "" {
		RespondError(w, http.StatusBadRequest, "Rule name is required")
		return
	}

	articles, err := h.syncManager.GetRSSMatchingArticles(r.Context(), instanceID, ruleName)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "GetRSSMatchingArticles") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("ruleName", ruleName).Msg("failed to get RSS matching articles")
		RespondError(w, http.StatusInternalServerError, "Failed to get RSS matching articles")
		return
	}

	if !middleware.CanReadTrackerPasskeys(r) {
		articles = hideRSSMatchingArticles(articles)
	}
	RespondJSON(w, http.StatusOK, articles)
}

// ReprocessRules triggers qBittorrent to reprocess all unread articles against rules.
// It does this by toggling auto-downloading off then on.
func (h *RSSHandler) ReprocessRules(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	if err := h.syncManager.ReprocessRSSRules(r.Context(), instanceID); err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "ReprocessRSSRules") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("failed to reprocess RSS rules")
		RespondError(w, http.StatusInternalServerError, "Failed to reprocess RSS rules")
		return
	}

	RespondJSON(w, http.StatusOK, nil)
}
