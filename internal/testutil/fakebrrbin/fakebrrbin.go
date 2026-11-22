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
	Body  []byte `json:"body,omitempty"`
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

const (
	SignInPending = "pending"
	SignInDone    = "done"
	SignInRefused = "refused"
	SignInExpired = "expired"
)

type SignInOutcome struct {
	State  string `json:"state"`
	Name   string `json:"name,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type signIn struct {
	installKey string
	provider   string
	outcome    SignInOutcome
	token      string
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

	SignInInterval    float64
	BrokenSignInStart map[string]any
	startHold         chan struct{}
	startHeld         chan struct{}
	signIns           map[string]*signIn
	pendingByKey      map[string]string
	newestSignIn      string
	minted            int
	pageOutcome       SignInOutcome
	tokens            map[string]string
	polls             chan string
}

type refusal struct {
	Status int    `json:"status"`
	Reason string `json:"reason"`
}

func New() *Fake {
	return &Fake{
		bySubmission:   map[string]string{},
		reports:        map[string]*storedReport{},
		SignInInterval: 1,
		signIns:        map[string]*signIn{},
		pendingByKey:   map[string]string{},
		pageOutcome:    SignInOutcome{State: SignInDone, Name: "fake-user"},
		tokens:         map[string]string{},
		polls:          make(chan string, 1024),
	}
}

func (f *Fake) EndSignIn(outcome SignInOutcome) (token string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.endSignIn(f.newestSignIn, outcome)
}

func (f *Fake) endSignIn(pollToken string, outcome SignInOutcome) string {
	pending, ok := f.signIns[pollToken]
	if !ok || pending.outcome.State != SignInPending {
		return ""
	}
	pending.outcome = outcome
	if outcome.State == SignInDone {
		f.minted++
		pending.token = fmt.Sprintf("brt_fake%d", f.minted)
		f.tokens[pending.token] = pending.installKey
	}
	return pending.token
}

func (f *Fake) SignInPolls() <-chan string {
	return f.polls
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

func (f *Fake) HoldSignInStarts() (received <-chan struct{}, release func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	hold, held := make(chan struct{}), make(chan struct{}, 16)
	f.startHold, f.startHeld = hold, held
	return held, sync.OnceFunc(func() { close(hold) })
}

func awaitRelease(r *http.Request, hold, held chan struct{}) bool {
	if hold == nil {
		return true
	}
	select {
	case held <- struct{}{}:
	default:
	}
	select {
	case <-hold:
		return true
	case <-r.Context().Done():
		return false
	}
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
	case r.Method == http.MethodPost && r.URL.Path == "/api/report-sign-in":
		f.startSignIn(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/report-sign-in/poll":
		f.pollSignIn(w, r)
	case r.Method == http.MethodDelete && r.URL.Path == "/api/report-sign-in":
		f.signOut(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/report-sign-in/"):
		f.signInPage(w, r)
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
	case r.Method == http.MethodPost && r.URL.Path == "/fake/sign-in":
		var body SignInOutcome
		if !decode(w, r, &body) {
			return
		}
		f.mu.Lock()
		f.pageOutcome = body
		f.mu.Unlock()
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

func (f *Fake) startSignIn(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.record(Request{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone(), Body: body})
	f.mu.Lock()
	hold, held := f.startHold, f.startHeld
	f.mu.Unlock()
	if !awaitRelease(r, hold, held) {
		return
	}
	installKey := r.Header.Get("X-Install-Key")
	if installKey == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "install_key_required"})
		return
	}
	if f.BrokenSignInStart != nil {
		writeJSON(w, http.StatusOK, f.BrokenSignInStart)
		return
	}
	var start struct {
		Provider string `json:"provider"`
	}
	if json.Unmarshal(body, &start) != nil || (start.Provider != "github" && start.Provider != "discord") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_provider"})
		return
	}
	f.mu.Lock()
	f.endSignIn(f.pendingByKey[installKey], SignInOutcome{State: SignInExpired})
	f.minted++
	pollToken := fmt.Sprintf("poll%d", f.minted)
	f.signIns[pollToken] = &signIn{installKey: installKey, provider: start.Provider, outcome: SignInOutcome{State: SignInPending}}
	f.pendingByKey[installKey] = pollToken
	f.newestSignIn = pollToken
	interval := f.SignInInterval
	f.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"url":       "http://" + r.Host + "/report-sign-in/" + pollToken,
		"pollToken": pollToken,
		"interval":  interval,
	})
}

func (f *Fake) pollSignIn(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.record(Request{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone(), Body: body})
	var poll struct {
		PollToken string `json:"pollToken"`
	}
	_ = json.Unmarshal(body, &poll)
	answer := map[string]string{"state": SignInExpired}
	f.mu.Lock()
	if pending, ok := f.signIns[poll.PollToken]; ok && pending.installKey == r.Header.Get("X-Install-Key") {
		answer["state"] = pending.outcome.State
		switch pending.outcome.State {
		case SignInDone:
			answer["token"], answer["provider"], answer["name"] = pending.token, pending.provider, pending.outcome.Name
			delete(f.signIns, poll.PollToken)
		case SignInRefused:
			answer["reason"] = pending.outcome.Reason
		}
	}
	f.mu.Unlock()
	select {
	case f.polls <- poll.PollToken:
	default:
	}
	writeJSON(w, http.StatusOK, answer)
}

func (f *Fake) signOut(w http.ResponseWriter, r *http.Request) {
	f.record(Request{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone()})
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	f.mu.Lock()
	ok := f.tokenWorks(r)
	if ok {
		delete(f.tokens, token)
	}
	f.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "token_rejected"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (f *Fake) tokenWorks(r *http.Request) bool {
	token, isBearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	installKey, known := f.tokens[token]
	return isBearer && known && installKey == r.Header.Get("X-Install-Key")
}

func (f *Fake) signInPage(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.endSignIn(strings.TrimPrefix(r.URL.Path, "/report-sign-in/"), f.pageOutcome)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, "<!doctype html><title>fake brrbin sign-in</title><p>The fake brrbin ended this sign-in.</p><script>window.close()</script>")
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
	if !awaitRelease(r, hold, held) {
		return
	}
	if refused != nil {
		writeJSON(w, refused.Status, map[string]string{"error": refused.Reason})
		return
	}
	if r.Header.Get("Authorization") != "" {
		f.mu.Lock()
		ok := f.tokenWorks(r)
		f.mu.Unlock()
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "token_rejected"})
			return
		}
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
