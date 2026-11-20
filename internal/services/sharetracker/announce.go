// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sharetracker

import (
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"

	"github.com/autobrr/go-torrent/bencode"
)

// defaultNumwant is the number of peers that a client gets when it sends no
// numwant (BEP 3).
const defaultNumwant = 50

// announceRequest is a parsed announce (BEP 3). The tracker ignores the ip
// parameter: a client could name any address with it.
type announceRequest struct {
	infoHash   [20]byte
	port       uint16
	uploaded   int64
	downloaded int64
	left       int64
	event      Event
	numwant    int
}

var (
	errInfoHash = errors.New("invalid info_hash")
	errPort     = errors.New("invalid port")
	errNumwant  = errors.New("invalid numwant")
	errCounts   = errors.New("invalid uploaded, downloaded, or left")
	errCompact  = errors.New("only compact peer lists")
)

func parseAnnounce(rawQuery string) (announceRequest, error) {
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		return announceRequest{}, err
	}
	var req announceRequest
	hash := q.Get("info_hash")
	if len(hash) != len(req.infoHash) {
		return announceRequest{}, errInfoHash
	}
	copy(req.infoHash[:], hash)
	port, err := strconv.ParseUint(q.Get("port"), 10, 16)
	if err != nil || port == 0 {
		return announceRequest{}, errPort
	}
	req.port = uint16(port)
	for _, field := range []struct {
		name string
		dst  *int64
	}{{"uploaded", &req.uploaded}, {"downloaded", &req.downloaded}, {"left", &req.left}} {
		n, err := strconv.ParseInt(q.Get(field.name), 10, 64)
		if err != nil || n < 0 {
			return announceRequest{}, errCounts
		}
		*field.dst = n
	}
	req.numwant = defaultNumwant
	if q.Has("numwant") {
		n, err := strconv.Atoi(q.Get("numwant"))
		if err != nil || n < 0 {
			return announceRequest{}, errNumwant
		}
		req.numwant = n
	}
	if q.Get("compact") == "0" {
		return announceRequest{}, errCompact
	}
	if e := Event(q.Get("event")); slices.Contains([]Event{EventStarted, EventCompleted, EventStopped}, e) {
		req.event = e
	}
	return req, nil
}

type successResponse struct {
	Interval int    `bencode:"interval"`
	Peers    []byte `bencode:"peers"`
	Peers6   []byte `bencode:"peers6,omitempty"`
}

type failureResponse struct {
	Reason string `bencode:"failure reason"`
}

// reply sends peers as the compact lists of BEP 23 (IPv4) and BEP 7 (IPv6).
func reply(w http.ResponseWriter, peers []netip.AddrPort) {
	resp := successResponse{Interval: int(Interval.Seconds()), Peers: []byte{}}
	for _, p := range peers {
		port := []byte{byte(p.Port() >> 8), byte(p.Port())}
		if a := p.Addr().Unmap(); a.Is4() {
			ip := a.As4()
			resp.Peers = append(append(resp.Peers, ip[:]...), port...)
		} else {
			ip := a.As16()
			resp.Peers6 = append(append(resp.Peers6, ip[:]...), port...)
		}
	}
	write(w, resp)
}

func fail(w http.ResponseWriter, reason string) {
	write(w, failureResponse{Reason: reason})
}

func write(w http.ResponseWriter, v any) {
	body, err := bencode.Marshal(v)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write(body)
}
