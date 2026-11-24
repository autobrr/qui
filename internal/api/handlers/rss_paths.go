// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"cmp"
	"encoding/json/v2"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/api/middleware"
)

type rssNoMatchError string

func (e rssNoMatchError) Error() string {
	return string(e)
}

type rssNames struct {
	path, parent, dest, articleID, feedURL *string
}

func (h *RSSHandler) restoreRSSNames(w http.ResponseWriter, r *http.Request, instanceID int, names rssNames) bool {
	if middleware.CanReadTrackerPasskeys(r) {
		return true
	}
	ctx := r.Context()
	withArticles := names.articleID != nil && isRSSStandIn(*names.articleID)
	err := names.restore(sync.OnceValues(func() (qbt.RSSItems, error) {
		return h.syncManager.GetRSSItems(ctx, instanceID, withArticles)
	}), withArticles)
	return respondRSSRestore(w, err, instanceID)
}

func respondRSSRestore(w http.ResponseWriter, err error, instanceID int) bool {
	if err == nil {
		return true
	}
	if noMatch, ok := errors.AsType[rssNoMatchError](err); ok {
		RespondError(w, http.StatusBadRequest, noMatch.Error())
		return false
	}
	if !respondIfInstanceDisabled(w, err, instanceID, "GetRSSItems") {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("failed to get RSS items for a request")
		RespondError(w, http.StatusInternalServerError, "Failed to get RSS items")
	}
	return false
}

func (n rssNames) restore(load func() (qbt.RSSItems, error), withArticles bool) error {
	var source string
	feed, err := restoreRSSPath(load, n.path, false, withArticles)
	if err != nil {
		return err
	}
	if n.path != nil {
		source = *n.path
	}
	if _, err := restoreRSSPath(load, n.parent, true, false); err != nil {
		return err
	}
	if err := restoreRSSDest(load, source, n.dest); err != nil {
		return err
	}
	if err := restoreRSSFeedURL(load, n.feedURL); err != nil {
		return err
	}
	if withArticles {
		return restoreRSSArticleID(feed, n.articleID)
	}
	return nil
}

func restoreRSSFeedURL(load func() (qbt.RSSItems, error), feedURL *string) error {
	if feedURL == nil || !isRSSStandIn(*feedURL) {
		return nil
	}
	items, err := load()
	if err != nil {
		return err
	}
	urls, err := rssFeedURLs(items, nil)
	if err != nil {
		return err
	}
	feeds := []string{*feedURL}
	err = restoreRSSFeedURLs(feeds, urls, "The RSS feed URL ")
	*feedURL = feeds[0]
	return err
}

func restoreRSSPath(load func() (qbt.RSSItems, error), path *string, keepLast, force bool) ([]byte, error) {
	if path == nil {
		return nil, nil
	}
	parts := strings.Split(*path, `\`)
	resolve := parts
	if keepLast {
		resolve = parts[:len(parts)-1]
	}
	if !force && !slices.ContainsFunc(resolve, isRSSStandIn) {
		return nil, nil
	}
	level, err := load()
	if err != nil {
		return nil, err
	}
	var item []byte
	for i, part := range resolve {
		if i > 0 {
			level = rssChildren(item)
		}
		name, err := findRSSItem(level, part)
		if err != nil {
			return nil, err
		}
		parts[i], item = name, level[name]
	}
	*path = strings.Join(parts, `\`)
	return item, nil
}

func rssChildren(item []byte) qbt.RSSItems {
	var children qbt.RSSItems
	if json.Unmarshal(item, &children) != nil {
		return nil
	}
	return children
}

func findRSSItem(level qbt.RSSItems, part string) (string, error) {
	if !isRSSStandIn(part) {
		return part, nil
	}
	var names []string
	for name := range level {
		if name == part || rssStandIn([]byte(name)) == part {
			names = append(names, name)
		}
	}
	switch len(names) {
	case 0:
		return "", rssNoMatchError("The RSS path part " + part + " matches no RSS feed or folder")
	case 1:
		return names[0], nil
	}
	return "", rssNoMatchError("The RSS path part " + part + " matches more than one RSS feed or folder")
}

func restoreRSSDest(load func() (qbt.RSSItems, error), source string, dest *string) error {
	if dest == nil {
		return nil
	}
	if _, err := restoreRSSPath(load, dest, true, false); err != nil {
		return err
	}
	parent, last, found := strings.CutLast(*dest, `\`)
	if !found {
		parent, last = "", *dest
	}
	if !isRSSStandIn(last) {
		return nil
	}
	_, name, _ := strings.CutLast(`\`+source, `\`)
	switch {
	case last == name:
		return nil
	case rssStandIn([]byte(name)) != last:
		return rssNoMatchError("The RSS destination name " + last + " is not the name of the moved item")
	case found:
		*dest = parent + `\` + name
	default:
		*dest = name
	}
	return nil
}

func restoreRSSArticleID(feed []byte, id *string) error {
	var parsed struct {
		Articles []struct {
			ID string `json:"id"`
		} `json:"articles"`
	}
	if feed != nil && json.Unmarshal(feed, &parsed) != nil {
		parsed.Articles = nil
	}
	var ids []string
	for _, article := range parsed.Articles {
		if (article.ID == *id || rssStandIn([]byte(article.ID)) == *id) && !slices.Contains(ids, article.ID) {
			ids = append(ids, article.ID)
		}
	}
	switch len(ids) {
	case 0:
		return rssNoMatchError("The RSS article ID " + *id + " matches no article of the feed")
	case 1:
		*id = ids[0]
		return nil
	}
	return rssNoMatchError("The RSS article ID " + *id + " matches more than one article of the feed")
}

func logRSSPaths(event *zerolog.Event, err error, fields ...string) *zerolog.Event {
	var names []string
	for pair := range slices.Chunk(fields, 2) {
		for name := range strings.SplitSeq(pair[1], `\`) {
			if holdsRSSLink([]byte(name)) {
				names = append(names, name)
			}
		}
	}
	slices.SortFunc(names, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	hide := func(text string) string {
		for _, name := range names {
			text = strings.ReplaceAll(text, name, rssStandIn([]byte(name)))
		}
		return text
	}
	event = event.Str(zerolog.ErrorFieldName, hide(err.Error()))
	for pair := range slices.Chunk(fields, 2) {
		event = event.Str(pair[0], hide(pair[1]))
	}
	return event
}
