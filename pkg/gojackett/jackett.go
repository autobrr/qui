// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"time"

	"golang.org/x/net/publicsuffix"
)

var (
	DefaultTimeout = 60 * time.Second
)

type Client struct {
	cfg Config

	http    *http.Client
	timeout time.Duration

	log *log.Logger
}

type Config struct {
	Host   string
	APIKey string

	// TLS skip cert validation
	TLSSkipVerify bool

	// HTTP Basic auth username
	BasicUser string

	// HTTP Basic auth password
	BasicPass string

	Timeout int
	Log     *log.Logger

	// DirectMode enables direct tracker API access instead of proxying through Jackett
	// When true, Host should be the direct tracker URL (e.g., https://www.morethantv.me/api/torznab)
	DirectMode bool
}

func NewClient(cfg Config) *Client {
	c := &Client{
		cfg:     cfg,
		log:     log.New(io.Discard, "", log.LstdFlags),
		timeout: DefaultTimeout,
	}

	// override logger if we pass one
	if cfg.Log != nil {
		c.log = cfg.Log
	}

	if cfg.Timeout > 0 {
		c.timeout = time.Duration(cfg.Timeout) * time.Second
	}

	//store cookies in jar
	jarOptions := &cookiejar.Options{PublicSuffixList: publicsuffix.List}
	jar, err := cookiejar.New(jarOptions)
	if err != nil {
		c.log.Println("new client cookie error")
	}

	c.http = &http.Client{
		Timeout: c.timeout,
		Jar:     jar,
	}

	return c
}
