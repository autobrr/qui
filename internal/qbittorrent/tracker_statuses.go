// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

// defaultUnregisteredStatuses lists tracker messages we map to the Unregistered health state.
var defaultUnregisteredStatuses = []string{
	"complete season uploaded",
	"dead",
	"dupe",
	"i'm sorry dave, i can't do that",
	"infohash not found",
	"internal available",
	"not exist",
	"not registered",
	"nuked",
	"pack is available",
	"packs are available",
	"problem with description",
	"problem with file",
	"problem with pack",
	"retitled",
	"season pack",
	"specifically banned",
	"torrent does not exist",
	"torrent existiert nicht",
	"torrent has been deleted",
	"torrent has been nuked",
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
}

// trackerDownStatuses lists tracker messages indicating an outage.
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

// TrackerMessageMatchesUnregistered reports whether the tracker message indicates an unregistered torrent.
func TrackerMessageMatchesUnregistered(message string) bool {
	return trackerMessageMatches(message, defaultUnregisteredStatuses)
}

// TrackerMessageMatchesDown reports whether the tracker message indicates tracker outage.
func TrackerMessageMatchesDown(message string) bool {
	return trackerMessageMatches(message, trackerDownStatuses)
}
