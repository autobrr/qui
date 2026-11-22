// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package problemreport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

const (
	ReasonAnonymousFull  = "anonymous_full"
	ReasonSignInRequired = "sign_in_required"
	ReasonAccountLimit   = "account_limit"
	ReasonReporterFull   = "reporter_full"
	ReasonTooLarge       = "too_large"
	ReasonTokenRejected  = "token_rejected"
	ReasonUnavailable    = "unavailable"
)

const (
	MaxReportSize = 4 << 20
	MaxImageSize  = 10 << 20

	requestTimeout      = 30 * time.Second
	imageRequestTimeout = 2 * time.Minute
	titleLength         = 80
)

// RefusalError is a brrbin answer that qui passes on to the browser: a Send
// that brrbin refused or could not take, for which qui stores no Sent reports
// row, or a Delete that brrbin could not do. Status is the status that qui
// answers with.
type RefusalError struct {
	Status int
	Reason string
}

func (r *RefusalError) Error() string {
	return "brrbin refused the request: " + r.Reason
}

// Report is the report that the browser sends. The parts other than the
// text and the logs pass through as the browser showed them, and qui cleans
// every string in them.
type Report struct {
	SubmissionID   string     `json:"submissionId"`
	Text           string     `json:"text"`
	StartedFrom    string     `json:"startedFrom"`
	Qui            any        `json:"qui"`
	Instances      any        `json:"instances"`
	Notices        any        `json:"notices"`
	Browser        any        `json:"browser"`
	FailedRequests any        `json:"failedRequests"`
	Logs           ReportLogs `json:"logs"`
	Search         any        `json:"search,omitempty"`
	MatchPolicy    any        `json:"matchPolicy,omitempty"`
	SearchScope    any        `json:"searchScope,omitempty"`
}

type ReportLogs struct {
	Scope string   `json:"scope"`
	Lines []string `json:"lines"`
}

type Image struct {
	Data        []byte
	ContentType string
}

type SentReport struct {
	ID           string    `json:"id"`
	SubmissionID string    `json:"submissionId"`
	Title        string    `json:"title"`
	SentAt       time.Time `json:"sentAt"`
	Status       string    `json:"status"`
	Text         string    `json:"text"`
}

func DecodeReport(r io.Reader) (Report, error) {
	decoder := json.NewDecoder(r)
	decoder.UseNumber()
	var report Report
	if err := decoder.Decode(&report); err != nil {
		return Report{}, fmt.Errorf("%w: the report is not valid JSON: %w", ErrInvalidRequest, err)
	}
	return report, nil
}

var submissionIDRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

var imageTypes = []string{"image/png", "image/jpeg", "image/webp"}

func validate(report Report, image *Image) error {
	if !submissionIDRegex.MatchString(report.SubmissionID) {
		return fmt.Errorf("%w: submissionId must be a UUID", ErrInvalidRequest)
	}
	if strings.TrimSpace(report.Text) == "" {
		return fmt.Errorf("%w: the text is required", ErrInvalidRequest)
	}
	switch report.StartedFrom {
	case StartedFromMenu, StartedFromNotice, StartedFromSearch:
	default:
		return fmt.Errorf("%w: startedFrom must be menu, notice or search", ErrInvalidRequest)
	}
	switch report.Logs.Scope {
	case LogScopeWarnings, LogScopeAll, LogScopeNone:
	default:
		return fmt.Errorf("%w: logs.scope must be warnings, all or none", ErrInvalidRequest)
	}
	if image != nil {
		if len(image.Data) > MaxImageSize {
			return &RefusalError{Status: http.StatusRequestEntityTooLarge, Reason: ReasonTooLarge}
		}
		if !slices.Contains(imageTypes, http.DetectContentType(image.Data)) {
			return fmt.Errorf("%w: the image must be PNG, JPEG or WebP", ErrInvalidRequest)
		}
	}
	return nil
}

// Send cleans the report and sends it to brrbin. It answers a *RefusalError when
// brrbin refuses it or cannot take it, and an ErrInvalidRequest error when
// qui refuses it before any request to brrbin.
func (s *Service) Send(ctx context.Context, report Report, image *Image, shown Sender) (*SentReport, error) {
	if err := validate(report, image); err != nil {
		return nil, err
	}
	var signIn *models.ProblemReportSignIn
	if !shown.Anonymous {
		stored, err := s.store.SignIn(ctx)
		if err != nil {
			return nil, err
		}
		if stored == nil || stored.Provider != shown.Provider || stored.Name != shown.Name {
			return nil, &RefusalError{Status: http.StatusConflict, Reason: ReasonTokenRejected}
		}
		signIn = stored
	}
	if image != nil && signIn == nil {
		return nil, fmt.Errorf("%w: an anonymous report cannot carry an image", ErrInvalidRequest)
	}

	c, err := s.cleaner(ctx)
	if err != nil {
		return nil, err
	}
	report = cleanReport(c, report)
	body, err := encodeWithin(report, MaxReportSize)
	if err != nil {
		return nil, err
	}

	installKey, err := s.store.InstallKey(ctx)
	if err != nil {
		return nil, err
	}
	// From here the report can reach brrbin, so a closed tab must not stop
	// qui from storing its Sent reports row. post has its own timeout.
	ctx = context.WithoutCancel(ctx)
	id, err := s.post(ctx, body, image, installKey, signIn)
	if err != nil {
		var refusal *RefusalError
		if errors.As(err, &refusal) && refusal.Reason == ReasonTokenRejected {
			if clearErr := s.store.ClearSignIn(ctx); clearErr != nil {
				return nil, clearErr
			}
		}
		log.Warn().Err(err).Str("startedFrom", report.StartedFrom).Msg("Problem report not sent")
		return nil, err
	}

	sentAt := s.now().UTC()
	sent := &SentReport{
		ID:           id,
		SubmissionID: report.SubmissionID,
		Title:        title(report.Text),
		SentAt:       sentAt,
		Status:       models.ProblemReportReceived,
		Text:         report.Text,
	}
	if err := s.store.AddSentReport(ctx, models.SentProblemReport{
		BrrbinID:        sent.ID,
		SubmissionID:    sent.SubmissionID,
		Title:           sent.Title,
		SentAt:          sentAt,
		Status:          sent.Status,
		StatusChangedAt: sentAt,
	}); err != nil {
		return nil, err
	}
	log.Info().Str("reportID", id).Str("startedFrom", report.StartedFrom).Msg("Problem report sent")
	return sent, nil
}

func cleanReport(c cleaner, report Report) Report {
	report.Text = c.clean(report.Text)
	report.Qui = c.cleanValue(report.Qui)
	report.Instances = c.cleanValue(report.Instances)
	report.Notices = c.cleanValue(report.Notices)
	report.Browser = c.cleanValue(report.Browser)
	report.FailedRequests = c.cleanValue(report.FailedRequests)
	switch report.Logs.Scope {
	case LogScopeNone:
		report.Logs.Lines = []string{}
	case LogScopeWarnings:
		if len(report.Logs.Lines) > warningLines {
			report.Logs.Lines = report.Logs.Lines[len(report.Logs.Lines)-warningLines:]
		}
	}
	report.Logs.Lines = c.cleanAll(report.Logs.Lines)
	if report.StartedFrom == StartedFromSearch {
		report.Search = c.cleanValue(report.Search)
		report.MatchPolicy = c.cleanValue(report.MatchPolicy)
		report.SearchScope = c.cleanValue(report.SearchScope)
	} else {
		report.Search, report.MatchPolicy, report.SearchScope = nil, nil, nil
	}
	return report
}

func encodeWithin(report Report, limit int) ([]byte, error) {
	for {
		body, err := json.Marshal(report)
		if err != nil {
			return nil, fmt.Errorf("encode the report: %w", err)
		}
		over := len(body) - limit
		if over <= 0 {
			return body, nil
		}
		if len(report.Logs.Lines) == 0 {
			return nil, &RefusalError{Status: http.StatusRequestEntityTooLarge, Reason: ReasonTooLarge}
		}
		cut := 0
		for freed := 0; freed < over && cut < len(report.Logs.Lines); cut++ {
			line, err := json.Marshal(report.Logs.Lines[cut])
			if err != nil {
				return nil, fmt.Errorf("encode a log line: %w", err)
			}
			freed += len(line) + 1
		}
		report.Logs.Lines = report.Logs.Lines[cut:]
	}
}

func title(text string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	first = strings.TrimSpace(first)
	if utf8.RuneCountInString(first) <= titleLength {
		return first
	}
	return string([]rune(first)[:titleLength])
}

func (s *Service) post(ctx context.Context, report []byte, image *Image, installKey string, signIn *models.ProblemReportSignIn) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="report"`},
		"Content-Type":        {"application/json"},
	})
	if err != nil {
		return "", err
	}
	if _, err := part.Write(report); err != nil {
		return "", err
	}
	timeout := requestTimeout
	if image != nil {
		timeout = imageRequestTimeout
		part, err := writer.CreatePart(textproto.MIMEHeader{
			"Content-Disposition": {`form-data; name="image"; filename="image"`},
			"Content-Type":        {image.ContentType},
		})
		if err != nil {
			return "", err
		}
		if _, err := part.Write(image.Data); err != nil {
			return "", err
		}
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(s.baseURL, "/")+"/api/reports", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Install-Key", installKey)
	if signIn != nil {
		req.Header.Set("Authorization", "Bearer "+signIn.Token)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		log.Debug().Err(err).Msg("brrbin did not answer a Problem report")
		return "", &RefusalError{Status: http.StatusBadGateway, Reason: ReasonUnavailable}
	}
	defer resp.Body.Close()

	var answer struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&answer)
	if resp.StatusCode == http.StatusCreated && decodeErr == nil && answer.ID != "" {
		return answer.ID, nil
	}
	return "", refusal(resp.StatusCode, answer.Error)
}

// refusal maps a brrbin answer that is not a success to the answer of qui.
// qui never answers 401, because the web UI sends the user to the login page
// on every 401.
func refusal(status int, reason string) *RefusalError {
	switch {
	case status == http.StatusTooManyRequests && reason == ReasonAnonymousFull,
		status == http.StatusTooManyRequests && reason == ReasonAccountLimit:
		return &RefusalError{Status: http.StatusTooManyRequests, Reason: reason}
	case status == http.StatusForbidden && reason == ReasonSignInRequired:
		return &RefusalError{Status: http.StatusForbidden, Reason: reason}
	case status == http.StatusConflict && reason == ReasonReporterFull:
		return &RefusalError{Status: http.StatusConflict, Reason: reason}
	case status == http.StatusRequestEntityTooLarge:
		return &RefusalError{Status: http.StatusRequestEntityTooLarge, Reason: ReasonTooLarge}
	case status == http.StatusUnauthorized:
		return &RefusalError{Status: http.StatusConflict, Reason: ReasonTokenRejected}
	default:
		return &RefusalError{Status: http.StatusBadGateway, Reason: ReasonUnavailable}
	}
}
