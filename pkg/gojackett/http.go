// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/autobrr/go-qbittorrent/errors"
	"github.com/avast/retry-go"

	"github.com/autobrr/qui/pkg/redact"
)

func (c *Client) getRawCtx(ctx context.Context, reqUrl string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqUrl, nil)
	if err != nil {
		return nil, errors.Wrap(err, "could not build request")
	}

	if c.cfg.BasicUser != "" && c.cfg.BasicPass != "" {
		req.SetBasicAuth(c.cfg.BasicUser, c.cfg.BasicPass)
	}

	resp, err := c.retryDo(ctx, req)
	if err != nil {
		err = redact.URLError(err)
		return nil, errors.Wrap(err, "error making get request: %v", redact.URLString(reqUrl))
	}

	return resp, nil
}

func (c *Client) getCtx(ctx context.Context, endpoint string, opts map[string]string) (*http.Response, error) {
	return c.getRawCtx(ctx, c.buildUrl(endpoint, opts))
}

func (c *Client) buildUrl(endpoint string, params map[string]string) string {
	var joinedUrl string

	if c.cfg.DirectMode {
		if endpoint != "" && endpoint != "/" {
			joinedUrl, _ = url.JoinPath(c.cfg.Host, endpoint)
		} else {
			joinedUrl = c.cfg.Host
		}
	} else {
		apiBase := "/api/v2.0/indexers/"
		joinedUrl, _ = url.JoinPath(c.cfg.Host, apiBase, endpoint)
	}

	queryParams := url.Values{}
	for key, value := range params {
		queryParams.Add(key, value)
	}

	parsedUrl, _ := url.Parse(joinedUrl)
	parsedUrl.RawQuery = queryParams.Encode()

	return parsedUrl.String()
}

// drainAndClose drains and closes the response body to ensure HTTP connection reuse
func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	// Drain the body to allow connection reuse
	_, _ = io.Copy(io.Discard, body)
	body.Close()
}

func copyBody(src io.ReadCloser) ([]byte, error) {
	b, err := io.ReadAll(src)
	if err != nil {
		return nil, err
	}
	src.Close()
	return b, nil
}

func resetBody(request *http.Request, originalBody []byte) {
	request.Body = io.NopCloser(bytes.NewBuffer(originalBody))
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewBuffer(originalBody)), nil
	}
}

func (c *Client) retryDo(ctx context.Context, req *http.Request) (*http.Response, error) {
	var (
		originalBody []byte
		err          error
	)

	if req != nil && req.Body != nil {
		originalBody, err = copyBody(req.Body)
		resetBody(req, originalBody)
	}

	if err != nil {
		return nil, err
	}

	var resp *http.Response

	err = retry.Do(func() error {
		resp, err = c.http.Do(req)

		if err == nil {
			if resp.StatusCode < 500 {
				return err
			} else if resp.StatusCode >= 500 {
				return retry.Unrecoverable(errors.New("unrecoverable status: %v", resp.StatusCode))
			}
		}

		retry.Delay(time.Second * 3)

		// Redact sensitive params from URL errors to prevent secret leakage in logs
		return redact.URLError(err)
	},
		retry.OnRetry(func(n uint, err error) {
			c.log.Printf("%q: attempt %d - %v\n", err, n, redact.URLString(req.URL.String()))
		}),
		retry.Attempts(5),
		retry.MaxJitter(time.Second*1),
	)

	if err != nil {
		return nil, errors.Wrap(err, "error making request")
	}

	return resp, nil
}
