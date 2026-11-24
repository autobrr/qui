// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package problemreport

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

const (
	ProviderGitHub  = "github"
	ProviderDiscord = "discord"
)

const (
	SignInPending = "pending"
	SignInDone    = "done"
	SignInRefused = "refused"
	SignInExpired = "expired"
)

const signInLifetime = 15 * time.Minute

type Sender struct {
	Anonymous bool   `json:"anonymous"`
	Provider  string `json:"provider,omitempty"`
	Name      string `json:"name,omitempty"`
}

type SignInState struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

type signInAttempt struct {
	cancel context.CancelFunc
	state  SignInState
}

type signInStart struct {
	URL       string  `json:"url"`
	PollToken string  `json:"pollToken"`
	Interval  float64 `json:"interval"`
}

type signInPoll struct {
	State    string `json:"state"`
	Reason   string `json:"reason"`
	Token    string `json:"token"`
	Provider string `json:"provider"`
	Name     string `json:"name"`
}

func (s *Service) Sender(ctx context.Context) (Sender, error) {
	signIn, err := s.store.SignIn(ctx)
	if err != nil {
		return Sender{}, err
	}
	if signIn == nil {
		return Sender{Anonymous: true}, nil
	}
	return Sender{Provider: signIn.Provider, Name: signIn.Name}, nil
}

func (s *Service) StartSignIn(ctx context.Context, provider string) (string, error) {
	if provider != ProviderGitHub && provider != ProviderDiscord {
		return "", fmt.Errorf("%w: provider must be github or discord", ErrInvalidRequest)
	}
	installKey, err := s.store.InstallKey(ctx)
	if err != nil {
		return "", err
	}
	attemptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), signInLifetime)
	attempt := &signInAttempt{cancel: cancel, state: SignInState{State: SignInPending}}
	s.signInMu.Lock()
	if s.signIn != nil {
		s.signIn.cancel()
	}
	s.signIn = attempt
	s.signInMu.Unlock()

	var start signInStart
	status, err := s.signInCall(attemptCtx, installKey, "/api/report-sign-in", map[string]string{"provider": provider}, &start)
	interval := time.Duration(start.Interval * float64(time.Second))
	started := err == nil && status == http.StatusOK && isWebURL(start.URL) && start.PollToken != "" && interval > 0
	if !started {
		log.Warn().Err(err).Int("status", status).Str("provider", provider).Msg("brrbin did not start a Problem report sign-in")
	}
	s.signInMu.Lock()
	current := s.signIn == attempt
	if current && !started {
		s.signIn = nil
	}
	s.signInMu.Unlock()
	if !started || !current {
		cancel()
		return "", &RefusalError{Status: http.StatusBadGateway, Reason: ReasonUnavailable}
	}
	go s.pollSignIn(attemptCtx, attempt, installKey, start.PollToken, interval)
	return start.URL, nil
}

func (s *Service) SignInState() SignInState {
	s.signInMu.Lock()
	defer s.signInMu.Unlock()
	if s.signIn == nil {
		return SignInState{State: SignInExpired}
	}
	return s.signIn.state
}

func (s *Service) SignOut(ctx context.Context) error {
	s.signInMu.Lock()
	if s.signIn != nil {
		s.signIn.cancel()
		s.signIn = nil
	}
	s.signInMu.Unlock()

	signIn, err := s.store.SignIn(ctx)
	if err != nil || signIn == nil {
		return err
	}
	installKey, err := s.store.InstallKey(ctx)
	if err != nil {
		return err
	}
	ctx = context.WithoutCancel(ctx)
	status, _, err := s.callBrrbin(ctx, installKey, http.MethodDelete, "/api/report-sign-in", signIn.Token, nil)
	if err != nil || status < 200 || status > 299 {
		log.Warn().Err(err).Int("status", status).Msg("brrbin did not end the Problem report sign-in, qui forgets it anyway")
	}
	return s.store.ClearSignIn(ctx)
}

func (s *Service) pollSignIn(ctx context.Context, attempt *signInAttempt, installKey, pollToken string, interval time.Duration) {
	defer attempt.cancel()
	end, polls, failed := s.awaitSignInEnd(ctx, installKey, pollToken, interval)
	state := SignInState{State: end.State}
	if end.State == SignInRefused {
		state.Reason = end.Reason
	}

	s.signInMu.Lock()
	current := s.signIn == attempt
	if current && end.State == SignInDone {
		if err := s.store.SaveSignIn(ctx, models.ProblemReportSignIn{Token: end.Token, Provider: end.Provider, Name: end.Name}); err != nil {
			log.Error().Err(err).Msg("Failed to store the Problem report sign-in")
			state = SignInState{State: SignInExpired}
		}
	}
	if current {
		attempt.state = state
	}
	s.signInMu.Unlock()
	log.Debug().Str("state", state.State).Bool("replaced", !current).Int("polls", polls).Int("failedPolls", failed).Msg("Problem report sign-in ended")
}

func (s *Service) awaitSignInEnd(ctx context.Context, installKey, pollToken string, interval time.Duration) (end signInPoll, polls, failed int) {
	for {
		select {
		case <-ctx.Done():
			return signInPoll{State: SignInExpired}, polls, failed
		case <-time.After(interval):
		}
		polls++
		var poll signInPoll
		status, err := s.signInCall(ctx, installKey, "/api/report-sign-in/poll", map[string]string{"pollToken": pollToken}, &poll)
		switch {
		case err != nil || status != http.StatusOK:
			failed++
			log.Trace().Err(err).Int("status", status).Msg("Problem report sign-in poll failed")
		case poll.State == SignInDone, poll.State == SignInRefused, poll.State == SignInExpired:
			return poll, polls, failed
		}
	}
}

func (s *Service) signInCall(ctx context.Context, installKey, path string, body map[string]string, answer any) (int, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	status, raw, err := s.callBrrbin(ctx, installKey, http.MethodPost, path, "", encoded)
	if err != nil || status != http.StatusOK {
		return status, err
	}
	if err := json.Unmarshal(raw, answer); err != nil {
		return status, fmt.Errorf("read the brrbin answer: %w", err)
	}
	return status, nil
}

func isWebURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != ""
}
