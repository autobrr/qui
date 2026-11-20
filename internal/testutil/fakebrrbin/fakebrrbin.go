// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package fakebrrbin is a fake of the brrbin report contract (#4247) for
// tests and live runs. It records each request and can refuse a report the
// way brrbin does. Check its answers against internal/server/report.go in
// autobrr/brrbin.
package fakebrrbin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// Status is the Report status that brrbin answers for a report.
type Status struct {
	Status string `json:"status"`
	Issue  *Issue `json:"issue,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Issue is the GitHub issue or discussion that a report became.
type Issue struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	Closed bool   `json:"closed"`
}

type storedReport struct {
	installKey string
	status     Status
}

// Fake answers POST /api/reports with 201 and a report id, also for a
// repeated submissionId. GET /api/install/reports answers the Report status
// of each listed report of the install, and DELETE /api/reports/{id} deletes
// one. For a live run, GET /fake/requests lists the recorded requests,
// POST /fake/refuse sets a refusal, POST /fake/status sets a Report status and
// POST /fake/forget drops a report.
type Fake struct {
	mu           sync.Mutex
	requests     []Request
	refusal      *refusal
	unavailable  bool
	hold         chan struct{}
	held         chan struct{}
	bySubmission map[string]string
	reports      map[string]*storedReport
}

type refusal struct {
	Status int    `json:"status"`
	Reason string `json:"reason"`
}

func New() *Fake {
	return &Fake{bySubmission: map[string]string{}, reports: map[string]*storedReport{}}
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

// Unavailable makes each later request to the brrbin API answer 503 while on
// is true, as a brrbin that is down.
func (f *Fake) Unavailable(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unavailable = on
}

// SetStatus sets the Report status that the fake answers for the report id.
func (f *Fake) SetStatus(id string, status Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.reports[id]; ok {
		r.status = status
	}
}

// Forget drops the report id, as brrbin does at the end of its retention.
func (f *Fake) Forget(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.reports, id)
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
	if strings.HasPrefix(r.URL.Path, "/fake/") {
		f.control(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") && f.isUnavailable() {
		f.record(Request{Method: r.Method, Path: r.URL.RequestURI(), Header: r.Header.Clone()})
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "unavailable"})
		return
	}
	deleteID, isDelete := strings.CutPrefix(r.URL.Path, "/api/reports/")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/reports":
		f.report(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/install/reports":
		f.statuses(w, r)
	case r.Method == http.MethodDelete && isDelete:
		f.delete(w, r, deleteID)
	default:
		f.record(Request{Method: r.Method, Path: r.URL.RequestURI(), Header: r.Header.Clone()})
		http.NotFound(w, r)
	}
}

func (f *Fake) control(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/fake/requests":
		writeJSON(w, http.StatusOK, f.Requests())
		return
	case r.Method == http.MethodPost && r.URL.Path == "/fake/refuse":
		var body refusal
		if !decode(w, r, &body) {
			return
		}
		f.Refuse(body.Status, body.Reason)
	case r.Method == http.MethodPost && r.URL.Path == "/fake/status":
		var body struct {
			ID string `json:"id"`
			Status
		}
		if !decode(w, r, &body) {
			return
		}
		f.SetStatus(body.ID, body.Status)
	case r.Method == http.MethodPost && r.URL.Path == "/fake/forget":
		var body struct {
			ID string `json:"id"`
		}
		if !decode(w, r, &body) {
			return
		}
		f.Forget(body.ID)
	default:
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decode(w http.ResponseWriter, r *http.Request, body any) bool {
	if err := json.NewDecoder(r.Body).Decode(body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func (f *Fake) isUnavailable() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.unavailable
}

// statuses answers the Report status of each listed report that the install
// sent and the fake still has.
func (f *Fake) statuses(w http.ResponseWriter, r *http.Request) {
	f.record(Request{Method: r.Method, Path: r.URL.RequestURI(), Header: r.Header.Clone()})
	installKey := r.Header.Get("X-Install-Key")
	if installKey == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "install_key_required"})
		return
	}
	type answer struct {
		ID string `json:"id"`
		Status
	}
	answers := []answer{}
	f.mu.Lock()
	for id := range strings.SplitSeq(r.URL.Query().Get("ids"), ",") {
		if stored, ok := f.reports[id]; ok && stored.installKey == installKey {
			answers = append(answers, answer{ID: id, Status: stored.status})
		}
	}
	f.mu.Unlock()
	writeJSON(w, http.StatusOK, answers)
}

func (f *Fake) delete(w http.ResponseWriter, r *http.Request, id string) {
	f.record(Request{Method: r.Method, Path: r.URL.RequestURI(), Header: r.Header.Clone()})
	f.mu.Lock()
	stored, ok := f.reports[id]
	// brrbin answers a report of another install as one that it does not have.
	ok = ok && stored.installKey == r.Header.Get("X-Install-Key")
	if ok {
		delete(f.reports, id)
	}
	f.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
		f.reports[id] = &storedReport{installKey: r.Header.Get("X-Install-Key"), status: Status{Status: "received"}}
	}
	f.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body) //nolint:errchkjson // the status is sent; a failed write means the client left
}
