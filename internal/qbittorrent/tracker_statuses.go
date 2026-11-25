// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"regexp"
	"slices"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

var urlPattern = regexp.MustCompile(`(?i)https?://\S+`)

var defaultUnregisteredStatuses = []string{
	"complete season uploaded",
	"dead",
	"dupe",
	"grab internal",
	"i'm sorry dave, i can't do that",
	"infohash not found",
	"internal available",
	"not exist",
	"not registered",
	"nuked",
	"other:",
	"pack is available",
	"packs are available",
	"problem with description",
	"problem with file",
	"problem with pack",
	"repack available",
	"retitled",
	"season pack",
	"specifically banned",
	"torrent does not exist",
	"torrent existiert nicht",
	"torrent has been deleted",
	"torrent has been nuked",
	"torrent has been rejected",
	"torrent introuvable",
	"torrent is not authorized for use on this tracker",
	"torrent is not found",
	"torrent nicht gefunden",
	"tracker nicht registriert",
	"torrent not found",
	"trump",
	"unknown",
	"unregistered",
	"não registrado",
	"upgraded",
	"uploaded",
	"nem található",
}

var trackerDownStatuses = []string{
	"continue",
	"multiple choices",
	"not modified",
	"bad request",
	"unauthorized",
	"forbidden",
	"internal server error",
	"not implemented",
	"bad gateway",
	"service unavailable",
	"moved permanently",
	"moved temporarily",
	"(unknown http error)",
	"down",
	"maintenance",
	"tracker is down",
	"tracker unavailable",
	"truncated",
	"unreachable",
	"not working",
	"not responding",
	"timeout",
	"refused",
	"no connection",
	"cannot connect",
	"connection failed",
	"ssl error",
	"no data",
	"timed out",
	"temporarily disabled",
	"unresolvable",
	"host not found",
	"offline",
	"your request could not be processed, please try again later",
	"unable to process your request",
	"<none>",
}

func trackerHealthOf(torrent *qbt.Torrent, trackers []qbt.TorrentTracker, now time.Time) TrackerHealth {
	if torrent == nil || len(trackers) == 0 {
		return ""
	}

	graceOver := torrent.AddedOn <= 0 || now.Sub(time.Unix(torrent.AddedOn, 0)) >= time.Hour
	var working, updating, unregistered, down, failed, pending bool
	for _, tracker := range trackers {
		switch tracker.Status {
		case qbt.TrackerStatusDisabled:
		case qbt.TrackerStatusOK:
			working = true
		case qbt.TrackerStatusUpdating:
			updating = true
			unregistered = unregistered || TrackerMessageMatchesUnregistered(tracker.Message)
		case qbt.TrackerStatusNotWorking, qbt.TrackerStatusTrackerError:
			failed = true
			unregistered = unregistered || TrackerMessageMatchesUnregistered(tracker.Message)
			down = down || ((!unregistered || !graceOver) && TrackerMessageMatchesDown(tracker.Message))
		case qbt.TrackerStatusUnreachable:
			failed = true
			down = true
		default:
			pending = true
		}
	}

	switch {
	case working:
		return ""
	case unregistered && graceOver:
		return TrackerHealthUnregistered
	case down && !updating:
		return TrackerHealthDown
	case failed && !updating && !pending && !unregistered:
		return TrackerHealthError
	}
	return ""
}

var trackerAuthErrorStatuses = []string{
	"passkey",
	"torrent pass",
	"user does not exist",
}

func TrackerMessageMatchesUnregistered(message string) bool {
	text := strings.ToLower(strings.ReplaceAll(message, "(unknown HTTP error)", ""))
	if !slices.ContainsFunc(defaultUnregisteredStatuses, func(pattern string) bool { return strings.Contains(text, pattern) }) {
		return false
	}
	authText := text
	if strings.Contains(authText, "://") {
		authText = urlPattern.ReplaceAllString(authText, "")
	}
	return !slices.ContainsFunc(trackerAuthErrorStatuses, func(pattern string) bool { return strings.Contains(authText, pattern) })
}

func TrackerMessageMatchesDown(message string) bool {
	if strings.Contains(message, "://") {
		message = urlPattern.ReplaceAllString(message, "")
	}
	return trackerMessageMatches(message, trackerDownStatuses)
}
