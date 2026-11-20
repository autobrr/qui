// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package fakebrrbin is a fake of the brrbin report contract (#4247) for
// tests and live runs. It records each request and can refuse a report the
// way brrbin does.
package fakebrrbin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// Request is one request that the fake received.
type Request struct {
	Method string      `json:"method"`
	Path   string      `json:"path"`
	Header http.Header `json:"header"`
	// Report is the report JSON part of a POST /api/reports.
	Report json.RawMessage `json:"report,omitempty"`
	// Image is the image part of a POST /api/reports.
	Image []byte `json:"image,omitempty"`
}

// Fake answers POST /api/reports with 201 and a report id, also for a
// repeated submissionId. GET /fake/requests lists the recorded requests and
// POST /fake/refuse sets a refusal, for a live run.
type Fake struct {
	mu           sync.Mutex
	requests     []Request
	refusal      *refusal
	hold         chan struct{}
	held         chan struct{}
	bySubmission map[string]string
}

type refusal struct {
	Status int    `json:"status"`
	Reason string `json:"reason"`
}

func New() *Fake {
	return &Fake{bySubmission: map[string]string{}}
}

// Refuse makes each later report get status and {"error": reason}. A status
// of 0 makes the fake take reports again.
func (f *Fake) Refuse(status int, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refusal = nil
	if status != 0 {
		f.refusal = &refusal{Status: status, Reason: reason}
	}
}

// Hold makes each later report wait, after the fake recorded it, until
// release runs or its request ends. received gets a value for each held
// report.
func (f *Fake) Hold() (received <-chan struct{}, release func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	hold, held := make(chan struct{}), make(chan struct{}, 16)
	f.hold, f.held = hold, held
	return held, sync.OnceFunc(func() { close(hold) })
}

// Requests returns the recorded requests to the brrbin API, oldest first.
func (f *Fake) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.requests...)
}

func (f *Fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/fake/requests":
		writeJSON(w, http.StatusOK, f.Requests())
	case r.Method == http.MethodPost && r.URL.Path == "/fake/refuse":
		var body refusal
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.Refuse(body.Status, body.Reason)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && r.URL.Path == "/api/reports":
		f.report(w, r)
	default:
		f.record(Request{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone()})
		http.NotFound(w, r)
	}
}

func (f *Fake) record(req Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
}

func (f *Fake) report(w http.ResponseWriter, r *http.Request) {
	recorded := Request{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone()}
	reader, err := r.MultipartReader()
	if err != nil {
		f.record(recorded)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_report"})
		return
	}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			f.record(recorded)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_report"})
			return
		}
		data, _ := io.ReadAll(part)
		switch part.FormName() {
		case "report":
			recorded.Report = data
		case "image":
			recorded.Image = data
		}
	}
	f.record(recorded)

	f.mu.Lock()
	hold, held, refused := f.hold, f.held, f.refusal
	f.mu.Unlock()
	if hold != nil {
		select {
		case held <- struct{}{}:
		default:
		}
		select {
		case <-hold:
		case <-r.Context().Done():
			return
		}
	}
	if refused != nil {
		writeJSON(w, refused.Status, map[string]string{"error": refused.Reason})
		return
	}

	var report struct {
		SubmissionID string `json:"submissionId"`
	}
	if json.Unmarshal(recorded.Report, &report) != nil || report.SubmissionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_report"})
		return
	}
	if recorded.Image != nil && r.Header.Get("Authorization") == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_report"})
		return
	}
	f.mu.Lock()
	id, ok := f.bySubmission[report.SubmissionID]
	if !ok {
		id = fmt.Sprintf("r%d", len(f.bySubmission)+1)
		f.bySubmission[report.SubmissionID] = id
	}
	f.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body) //nolint:errchkjson // the status is sent; a failed write means the client left
}
