// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"errors"
	"io"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/qbittorrent"
)

func holdsRSSLink(text []byte) bool {
	if bytes.IndexByte(text, '<') >= 0 {
		return true
	}
	for rest := text; ; {
		colon := bytes.IndexByte(rest, ':')
		if colon < 0 {
			return false
		}
		if bytes.HasPrefix(rest[colon+1:], []byte("//")) || (colon >= 6 && bytes.EqualFold(rest[colon-6:colon], []byte("magnet"))) {
			return true
		}
		rest = rest[colon+1:]
	}
}

func rssNameStandIn(name []byte) string {
	sum := sha256.Sum256(name)
	return qbittorrent.TrackerOrigin(string(name)) + "#" + hex.EncodeToString(sum[:4])
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
			feed = rssNameStandIn([]byte(feed))
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

func writeRSSText(enc *jsontext.Encoder, value jsontext.Value, name bool) ([]byte, error) {
	text, err := unquoteRSSText(value)
	if err != nil {
		return nil, err
	}
	switch {
	case !holdsRSSLink(text):
		err = enc.WriteValue(value)
	case name:
		err = enc.WriteToken(jsontext.String(rssNameStandIn(text)))
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
	feedURL, articlesNext, articlesDepth := false, false, 0
	for {
		kind, length := dec.StackIndex(dec.StackDepth())
		atName := kind == '{' && length%2 == 0
		if (atName || articlesDepth > 0) && dec.PeekKind() == '"' {
			value, err := dec.ReadValue()
			if err != nil {
				return nil, err
			}
			text, err := writeRSSText(enc, value, atName)
			if err != nil {
				return nil, err
			}
			if atName {
				feedURL, articlesNext = string(text) == "url", string(text) == "articles"
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
		depth := dec.StackDepth()
		if depth < articlesDepth {
			articlesDepth = 0
		}
		switch {
		case articlesNext && tok.Kind() == '[':
			articlesDepth = depth
		case feedURL && tok.Kind() == '"':
			tok = jsontext.String(qbittorrent.TrackerOrigin(tok.String()))
		}
		feedURL, articlesNext = false, false
		if err := enc.WriteToken(tok); err != nil {
			return nil, err
		}
	}
}
