// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/rs/zerolog/log"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/api/middleware"
	"github.com/autobrr/qui/internal/qbittorrent"
)

func holdsRSSLink(text []byte) bool {
	if bytes.IndexByte(text, '<') >= 0 {
		return true
	}
	for rest := text; ; {
		colon := bytes.IndexByte(rest, ':')
		if colon < 0 {
			break
		}
		if colon >= 6 && bytes.EqualFold(rest[colon-6:colon], []byte("magnet")) {
			return true
		}
		rest = rest[colon+1:]
	}
	for at := 0; ; at += 2 {
		slashes := bytes.Index(text[at:], []byte("//"))
		if slashes < 0 {
			return false
		}
		at += slashes
		if at > 0 && text[at-1] == ':' {
			return true
		}
		if (at == 0 || strings.IndexByte(" \t\n\r", text[at-1]) >= 0) && at+2 < len(text) && isASCIIAlnum(text[at+2]) {
			return true
		}
	}
}

func isASCIIAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func rssStandIn(name []byte) string {
	sum := sha256.Sum256(name)
	return qbittorrent.TrackerOrigin(string(name)) + "#" + hex.EncodeToString(sum[:4])
}

func isRSSStandIn(name string) bool {
	origin, hash, found := strings.CutLast(name, "#")
	return found && len(hash) == 8 && strings.Trim(hash, "0123456789abcdef") == "" && qbittorrent.TrackerOrigin(origin) == origin
}

func hideRSSRuleFeeds(r *http.Request, rules qbt.RSSRules) {
	if middleware.CanReadTrackerPasskeys(r) {
		return
	}
	for _, rule := range rules {
		for i, feed := range rule.AffectedFeeds {
			rule.AffectedFeeds[i] = rssStandIn([]byte(feed))
		}
	}
}

func (h *RSSHandler) restoreRSSRuleFeeds(w http.ResponseWriter, r *http.Request, instanceID int, req *SetRuleRequest) bool {
	feeds := req.Rule.AffectedFeeds
	if middleware.CanReadTrackerPasskeys(r) || !slices.ContainsFunc(feeds, isRSSStandIn) {
		return true
	}
	known, err := h.rssFeedsByStandIn(r.Context(), instanceID, req.Name)
	if err != nil {
		if !respondIfInstanceDisabled(w, err, instanceID, "SetRSSRule") {
			log.Error().Err(err).Int("instanceID", instanceID).Str("ruleName", req.Name).Msg("failed to get RSS feeds for rule")
			RespondError(w, http.StatusInternalServerError, "Failed to set RSS rule")
		}
		return false
	}
	for i, feed := range feeds {
		if full, ok := known[feed]; ok {
			feeds[i] = full
		} else if isRSSStandIn(feed) {
			RespondError(w, http.StatusBadRequest, "The rule feed "+feed+" matches no RSS feed")
			return false
		}
	}
	return true
}

func (h *RSSHandler) rssFeedsByStandIn(ctx context.Context, instanceID int, ruleName string) (map[string]string, error) {
	items, err := h.syncManager.GetRSSItems(ctx, instanceID, false)
	if err != nil {
		return nil, err
	}
	feeds, err := items.ParseFeeds()
	if err != nil {
		return nil, err
	}
	rules, err := h.syncManager.GetRSSRules(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	urls := slices.Clone(rules[ruleName].AffectedFeeds)
	for _, feed := range feeds {
		urls = append(urls, feed.URL)
	}
	known := make(map[string]string, 2*len(urls))
	for _, url := range urls {
		known[url] = url
		known[rssStandIn([]byte(url))] = url
	}
	return known, nil
}

func hideRSSMatchingArticles(matches qbt.RSSMatchingArticles) qbt.RSSMatchingArticles {
	hidden := make(qbt.RSSMatchingArticles, len(matches))
	for feed, titles := range matches {
		for i, title := range titles {
			if holdsRSSLink([]byte(title)) {
				titles[i] = qbittorrent.TrackerOrigin(title)
			}
		}
		if holdsRSSLink([]byte(feed)) {
			feed = rssStandIn([]byte(feed))
		}
		hidden[feed] = titles
	}
	return hidden
}

func unquoteRSSText(value jsontext.Value) ([]byte, error) {
	text := []byte(value[1 : len(value)-1])
	if bytes.IndexByte(text, '\\') < 0 {
		return text, nil
	}
	return jsontext.AppendUnquote(nil, value)
}

type rssValueKind int

const (
	rssText rssValueKind = iota
	rssLink
	rssID
)

func rssKindOf(name []byte) rssValueKind {
	switch string(name) {
	case "url", "torrentURL", "link":
		return rssLink
	case "id":
		return rssID
	}
	return rssText
}

func writeRSSText(enc *jsontext.Encoder, value jsontext.Value, name bool, field rssValueKind) ([]byte, error) {
	text, err := unquoteRSSText(value)
	if err != nil {
		return nil, err
	}
	switch {
	case field == rssLink:
		err = enc.WriteToken(jsontext.String(qbittorrent.TrackerOrigin(string(text))))
	case !holdsRSSLink(text):
		err = enc.WriteValue(value)
	case name || field == rssID:
		err = enc.WriteToken(jsontext.String(rssStandIn(text)))
	default:
		err = enc.WriteToken(jsontext.String(qbittorrent.TrackerOrigin(string(text))))
	}
	return text, err
}

func hideRSSLinks(items []byte) ([]byte, error) {
	lenient := []jsontext.Options{jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true)}
	dec := jsontext.NewDecoder(bytes.NewReader(items), lenient...)
	var out bytes.Buffer
	out.Grow(len(items))
	enc := jsontext.NewEncoder(&out, lenient...)
	field := rssText
	for {
		if dec.PeekKind() == '"' {
			kind, length := dec.StackIndex(dec.StackDepth())
			name := kind == '{' && length%2 == 0
			value, err := dec.ReadValue()
			if err != nil {
				return nil, err
			}
			text, err := writeRSSText(enc, value, name, field)
			if err != nil {
				return nil, err
			}
			field = rssText
			if name {
				field = rssKindOf(text)
			}
			continue
		}
		tok, err := dec.ReadToken()
		if errors.Is(err, io.EOF) {
			return out.Bytes(), nil
		}
		if err != nil {
			return nil, err
		}
		field = rssText
		if err := enc.WriteToken(tok); err != nil {
			return nil, err
		}
	}
}
