// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package gazellemusic

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

const DownloadScheme = "gazelle"

func BuildDownloadURL(host string, torrentID int64) string {
	return fmt.Sprintf("%s://%s/%d", DownloadScheme, strings.TrimSpace(host), torrentID)
}

func IsDownloadURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, DownloadScheme) && u.Host != ""
}

func ParseDownloadURL(raw string) (host string, torrentID int64, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", 0, false
	}
	if !strings.EqualFold(u.Scheme, DownloadScheme) {
		return "", 0, false
	}
	h := strings.ToLower(strings.TrimSpace(u.Host))
	if h == "" {
		return "", 0, false
	}
	idStr := strings.Trim(strings.TrimSpace(u.Path), "/")
	if idStr == "" {
		return "", 0, false
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		return "", 0, false
	}
	return h, id, true
}
