// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/problemreport"
	"github.com/autobrr/qui/internal/testutil"
	"github.com/autobrr/qui/internal/testutil/fakebrrbin"
)

func (rig *reportRig) call(method, path, body string) (int, string) {
	rig.t.Helper()
	req, err := http.NewRequestWithContext(rig.t.Context(), method, rig.srv.URL+path, strings.NewReader(body))
	require.NoError(rig.t, err)
	status, raw := rig.do(req)
	return status, string(raw)
}

func (rig *reportRig) sender() problemreport.Sender {
	rig.t.Helper()
	status, body := rig.call(http.MethodGet, "/api/problem-reports/sender", "")
	require.Equal(rig.t, http.StatusOK, status, body)
	var sender problemreport.Sender
	require.NoError(rig.t, json.Unmarshal([]byte(body), &sender), body)
	return sender
}

func (rig *reportRig) startSignIn(provider string) string {
	rig.t.Helper()
	status, body := rig.call(http.MethodPost, "/api/problem-reports/sign-in", `{"provider":"`+provider+`"}`)
	require.Equal(rig.t, http.StatusOK, status, body)
	var started struct {
		URL string `json:"url"`
	}
	require.NoError(rig.t, json.Unmarshal([]byte(body), &started), body)
	rig.t.Cleanup(rig.stopSignIn)
	return started.URL
}

func (rig *reportRig) stopSignIn() {
	req, err := http.NewRequestWithContext(context.WithoutCancel(rig.t.Context()), http.MethodDelete, rig.srv.URL+"/api/problem-reports/sign-in", nil)
	require.NoError(rig.t, err)
	rig.do(req)
}

func (rig *reportRig) signInState() problemreport.SignInState {
	rig.t.Helper()
	status, body := rig.call(http.MethodGet, "/api/problem-reports/sign-in", "")
	require.Equal(rig.t, http.StatusOK, status, body)
	var state problemreport.SignInState
	require.NoError(rig.t, json.Unmarshal([]byte(body), &state), body)
	return state
}

func (rig *reportRig) awaitSignIn(want problemreport.SignInState) {
	rig.t.Helper()
	var got problemreport.SignInState
	require.Eventually(rig.t, func() bool {
		got = rig.signInState()
		return got == want
	}, 10*time.Second, time.Millisecond, "the sign-in state is %+v", got)
}

func (rig *reportRig) signIn(provider, name string) string {
	rig.t.Helper()
	rig.startSignIn(provider)
	token := rig.fake.EndSignIn(fakebrrbin.SignInOutcome{State: fakebrrbin.SignInDone, Name: name})
	require.NotEmpty(rig.t, token)
	rig.awaitSignIn(problemreport.SignInState{State: problemreport.SignInDone})
	return token
}

func signInRig(t *testing.T) *reportRig {
	t.Helper()
	rig := newReportRig(t, reportRigOptions{})
	rig.fake.SignInInterval = 0.001
	return rig
}

func TestProblemReportSignIn(t *testing.T) {
	rig := signInRig(t)
	assert.Equal(t, problemreport.Sender{Anonymous: true}, rig.sender())
	assert.Empty(t, rig.fake.Requests(), "reading the Sender asks brrbin nothing")

	url := rig.startSignIn("github")
	start := rig.fake.Requests()[0]
	assert.Equal(t, "/api/report-sign-in", start.Path)
	assert.JSONEq(t, `{"provider":"github"}`, string(start.Body))
	installKey := start.Header.Get("X-Install-Key")
	assert.Len(t, installKey, 43, "the first sign-in makes the Install key")
	assert.Contains(t, url, "/report-sign-in/")

	pollToken := <-rig.fake.SignInPolls()
	assert.Equal(t, problemreport.SignInState{State: problemreport.SignInPending}, rig.signInState())
	assert.Equal(t, problemreport.Sender{Anonymous: true}, rig.sender())
	poll := rig.fake.Requests()[1]
	assert.Equal(t, "/api/report-sign-in/poll", poll.Path)
	assert.JSONEq(t, `{"pollToken":"`+pollToken+`"}`, string(poll.Body))
	assert.Equal(t, installKey, poll.Header.Get("X-Install-Key"))

	token := rig.fake.EndSignIn(fakebrrbin.SignInOutcome{State: fakebrrbin.SignInDone, Name: "someone"})
	rig.awaitSignIn(problemreport.SignInState{State: problemreport.SignInDone})
	assert.Equal(t, problemreport.Sender{Provider: "github", Name: "someone"}, rig.sender())
	_, senderBody := rig.call(http.MethodGet, "/api/problem-reports/sender", "")
	assert.NotContains(t, senderBody, token, "the token never reaches the browser")

	var storedToken, storedKey string
	require.NoError(t, rig.db.QueryRowContext(t.Context(), `SELECT sign_in_token_encrypted, install_key_encrypted FROM problem_report_install WHERE id = 1`).Scan(&storedToken, &storedKey))
	assert.NotContains(t, storedToken, token, "the token is stored encrypted")
	assert.NotContains(t, storedKey, installKey, "the Install key is stored encrypted")

	_, facts := rig.facts("startedFrom=menu")
	shown := new(rig.sender())
	polls := len(rig.fake.Requests())
	status, answer := rig.sendAs(shown, menuReport("signed in", facts, "none", nil), nil)
	require.Equal(t, http.StatusCreated, status, answer)
	requests := rig.brrbinRequests(polls)
	require.Len(t, requests, 1, "qui stops polling after done")
	assert.Equal(t, "Bearer "+token, requests[0].Header.Get("Authorization"))
	assert.Equal(t, installKey, requests[0].Header.Get("X-Install-Key"))
}

func TestProblemReportSignInRefusedAndExpired(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome fakebrrbin.SignInOutcome
		want    problemreport.SignInState
	}{
		{"refused", fakebrrbin.SignInOutcome{State: fakebrrbin.SignInRefused, Reason: "discord_not_member"}, problemreport.SignInState{State: problemreport.SignInRefused, Reason: "discord_not_member"}},
		{"expired", fakebrrbin.SignInOutcome{State: fakebrrbin.SignInExpired}, problemreport.SignInState{State: problemreport.SignInExpired}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := signInRig(t)
			rig.startSignIn("discord")
			rig.fake.EndSignIn(tc.outcome)
			rig.awaitSignIn(tc.want)
			assert.Equal(t, problemreport.Sender{Anonymous: true}, rig.sender())

			polls := len(rig.fake.Requests())
			rig.sendReport(reportSubmission, "anonymous")
			requests := rig.brrbinRequests(polls)
			require.Len(t, requests, 1, "qui stops polling after the sign-in ended")
			assert.Empty(t, requests[0].Header.Get("Authorization"))
		})
	}
}

func TestProblemReportSecondSignInReplacesTheFirst(t *testing.T) {
	rig := signInRig(t)
	rig.startSignIn("github")
	first := <-rig.fake.SignInPolls()
	rig.startSignIn("discord")

	for seen := 0; seen < 5; {
		if pollToken := <-rig.fake.SignInPolls(); pollToken != first {
			seen++
		}
	}
	assert.Equal(t, problemreport.SignInState{State: problemreport.SignInPending}, rig.signInState(), "brrbin expired the first sign-in, and qui shows the second")

	rig.fake.EndSignIn(fakebrrbin.SignInOutcome{State: fakebrrbin.SignInDone, Name: "someone"})
	rig.awaitSignIn(problemreport.SignInState{State: problemreport.SignInDone})
	assert.Equal(t, problemreport.Sender{Provider: "discord", Name: "someone"}, rig.sender())
}

func TestProblemReportSignInWhenBrrbinCannotStartOne(t *testing.T) {
	rig := signInRig(t)

	status, body := rig.call(http.MethodPost, "/api/problem-reports/sign-in", `{"provider":"gitlab"}`)
	assert.Equal(t, http.StatusBadRequest, status, body)
	assert.Empty(t, rig.fake.Requests(), "an unknown provider never reaches brrbin")

	rig.fake.Unavailable(true)
	status, body = rig.call(http.MethodPost, "/api/problem-reports/sign-in", `{"provider":"github"}`)
	assert.Equal(t, http.StatusBadGateway, status)
	assert.JSONEq(t, `{"error":"unavailable"}`, body)
	assert.Equal(t, problemreport.SignInState{State: problemreport.SignInExpired}, rig.signInState(), "qui holds no sign-in")
}

func TestProblemReportSignInWhenBrrbinAnswersAStartThatQuiCannotUse(t *testing.T) {
	for name, answer := range map[string]map[string]any{
		"a script url":     {"url": "javascript:alert(1)", "pollToken": "poll1", "interval": 1},
		"no poll token":    {"url": "https://bin.getqui.com/report-sign-in/poll1", "interval": 1},
		"an interval of 0": {"url": "https://bin.getqui.com/report-sign-in/poll1", "pollToken": "poll1", "interval": 0},
	} {
		t.Run(name, func(t *testing.T) {
			rig := signInRig(t)
			rig.fake.BrokenSignInStart = answer

			status, body := rig.call(http.MethodPost, "/api/problem-reports/sign-in", `{"provider":"github"}`)

			assert.Equal(t, http.StatusBadGateway, status)
			assert.JSONEq(t, `{"error":"unavailable"}`, body)
			assert.Equal(t, problemreport.SignInState{State: problemreport.SignInExpired}, rig.signInState(), "qui holds no sign-in, so it polls nothing")
		})
	}
}

func TestProblemReportSignOut(t *testing.T) {
	rig := signInRig(t)
	token := rig.signIn("github", "someone")
	installKey := rig.fake.Requests()[0].Header.Get("X-Install-Key")

	before := len(rig.fake.Requests())
	status, body := rig.call(http.MethodDelete, "/api/problem-reports/sign-in", "")
	require.Equal(t, http.StatusNoContent, status, body)
	requests := rig.brrbinRequests(before)
	require.Len(t, requests, 1)
	assert.Equal(t, http.MethodDelete, requests[0].Method)
	assert.Equal(t, "/api/report-sign-in", requests[0].Path)
	assert.Equal(t, "Bearer "+token, requests[0].Header.Get("Authorization"))
	assert.Equal(t, installKey, requests[0].Header.Get("X-Install-Key"))
	assert.Equal(t, problemreport.Sender{Anonymous: true}, rig.sender())

	before = len(rig.fake.Requests())
	status, _ = rig.call(http.MethodDelete, "/api/problem-reports/sign-in", "")
	assert.Equal(t, http.StatusNoContent, status)
	assert.Empty(t, rig.brrbinRequests(before), "an anonymous Sender has no sign-in to end on brrbin")

	rig.sendReport(reportSubmission, "anonymous again")
	assert.Empty(t, rig.brrbinRequests(before)[0].Header.Get("Authorization"))
}

func TestProblemReportSignOutWhenBrrbinDoesNotAnswer(t *testing.T) {
	rig := signInRig(t)
	rig.signIn("github", "someone")

	rig.fake.Unavailable(true)
	status, body := rig.call(http.MethodDelete, "/api/problem-reports/sign-in", "")
	require.Equal(t, http.StatusNoContent, status, body)
	assert.Equal(t, problemreport.Sender{Anonymous: true}, rig.sender())
}

func TestProblemReportSignOutDropsAPendingSignIn(t *testing.T) {
	rig := signInRig(t)
	rig.startSignIn("github")
	<-rig.fake.SignInPolls()

	status, body := rig.call(http.MethodDelete, "/api/problem-reports/sign-in", "")
	require.Equal(t, http.StatusNoContent, status, body)
	assert.Equal(t, problemreport.SignInState{State: problemreport.SignInExpired}, rig.signInState())
	assert.Equal(t, problemreport.Sender{Anonymous: true}, rig.sender())
}

func (rig *reportRig) startDuringSignOut() int {
	rig.t.Helper()
	received, release := rig.fake.HoldSignInStarts()
	defer release()

	req, err := http.NewRequestWithContext(rig.t.Context(), http.MethodPost, rig.srv.URL+"/api/problem-reports/sign-in", strings.NewReader(`{"provider":"github"}`))
	require.NoError(rig.t, err)
	req.AddCookie(rig.cookie)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	started := make(chan int, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			started <- 0
			return
		}
		resp.Body.Close()
		started <- resp.StatusCode
	}()
	<-received

	status, body := rig.call(http.MethodDelete, "/api/problem-reports/sign-in", "")
	require.Equal(rig.t, http.StatusNoContent, status, body)
	release()
	return <-started
}

// regression: #4360
func TestProblemReportSignOutDropsASignInThatIsStillStarting(t *testing.T) {
	rig := signInRig(t)
	assert.Equal(t, http.StatusBadGateway, rig.startDuringSignOut(), "the sign-out ended the start, so the browser closes the popup")
	assert.Equal(t, problemreport.SignInState{State: problemreport.SignInExpired}, rig.signInState(), "qui polls no sign-in after the sign-out")
}

// regression: #4464
func TestProblemReportSignOutDropsAStartThatBrrbinAnswersAfterTheSignOut(t *testing.T) {
	answerAfterCancel := testutil.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return http.DefaultTransport.RoundTrip(r.WithContext(context.WithoutCancel(r.Context())))
	})
	rig := newReportRig(t, reportRigOptions{client: &http.Client{Transport: answerAfterCancel}})
	rig.fake.SignInInterval = 0.001
	assert.Equal(t, http.StatusBadGateway, rig.startDuringSignOut(), "the sign-out dropped the start, so the browser closes the popup")
	assert.Equal(t, problemreport.SignInState{State: problemreport.SignInExpired}, rig.signInState(), "qui polls no sign-in after the sign-out")
}
