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
	"unicode/utf8"

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
		if (at == 0 || !isASCIIAlnum(text[at-1])) && at+2 < len(text) && startsRSSHost(text[at+2]) {
			return true
		}
	}
}

func startsRSSHost(c byte) bool {
	return isASCIIAlnum(c) || c == '[' || c >= utf8.RuneSelf
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
	urls, err := h.rssRuleFeedURLs(r.Context(), instanceID, req.Name)
	if err == nil {
		err = restoreRSSFeedURLs(feeds, urls, "The rule feed ")
	}
	return respondRSSRestore(w, err, instanceID)
}

func (h *RSSHandler) rssRuleFeedURLs(ctx context.Context, instanceID int, ruleName string) ([]string, error) {
	items, err := h.syncManager.GetRSSItems(ctx, instanceID, false)
	if err != nil {
		return nil, err
	}
	rules, err := h.syncManager.GetRSSRules(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return rssFeedURLs(items, rules[ruleName].AffectedFeeds)
}

func rssFeedURLs(items qbt.RSSItems, extra []string) ([]string, error) {
	feeds, err := items.ParseFeeds()
	if err != nil {
		return nil, err
	}
	urls := slices.Clone(extra)
	for _, feed := range feeds {
		urls = append(urls, feed.URL)
	}
	return urls, nil
}

func restoreRSSFeedURLs(feeds, urls []string, label string) error {
	byShortForm := make(map[string][]string, len(urls))
	seen := make(map[string]bool, len(urls))
	for _, url := range urls {
		if seen[url] {
			continue
		}
		seen[url] = true
		short := rssStandIn([]byte(url))
		byShortForm[url] = append(byShortForm[url], url)
		byShortForm[short] = append(byShortForm[short], url)
	}
	for i, feed := range feeds {
		if !isRSSStandIn(feed) {
			continue
		}
		switch full := byShortForm[feed]; len(full) {
		case 0:
			return rssNoMatchError(label + feed + " matches no RSS feed")
		case 1:
			feeds[i] = full[0]
		default:
			return rssNoMatchError(label + feed + " matches more than one RSS feed")
		}
	}
	return nil
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
	rssFeedURL
)

func rssKindOf(name []byte, inList bool) rssValueKind {
	switch string(name) {
	case "url":
		if inList {
			return rssLink
		}
		return rssFeedURL
	case "torrentURL", "link":
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
	case field == rssFeedURL:
		err = enc.WriteToken(jsontext.String(rssStandIn(text)))
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
				parent, _ := dec.StackIndex(dec.StackDepth() - 1)
				field = rssKindOf(text, parent == '[')
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
