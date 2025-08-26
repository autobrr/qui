// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"io"
	stdlog "log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
)

type Client struct {
	*qbt.Client
	instanceID      int
	webAPIVersion   string
	supportsSetTags bool
	lastHealthCheck time.Time
	isHealthy       bool
	mu              sync.RWMutex
}

// filteredWriter wraps stderr to filter out HTTP "unsolicited response" errors.
//
// qBittorrent occasionally sends extra HTTP responses after the main request completes,
// which causes Go's HTTP client to log "Unsolicited response received on idle HTTP channel"
// errors to stderr. While these don't affect functionality, they create noise in the logs.
//
// Since the go-qbittorrent library doesn't expose HTTP client configuration, we filter
// these specific messages at the standard library log level to keep logs clean.
type filteredWriter struct {
	writer io.Writer
}

func (fw *filteredWriter) Write(p []byte) (n int, err error) {
	s := string(p)
	// Filter out the specific HTTP error we want to suppress - this is cosmetic
	// and doesn't affect the actual speed limit functionality which works correctly
	if strings.Contains(s, "Unsolicited response received on idle HTTP channel") {
		return len(p), nil // Pretend we wrote it successfully but drop the message
	}
	return fw.writer.Write(p)
}

func init() {
	// Set up filtered stderr to suppress HTTP "unsolicited response" errors from qBittorrent.
	// These occur due to qBittorrent's HTTP response behavior but don't impact functionality.
	filteredStderr := &filteredWriter{writer: os.Stderr}
	stdlog.SetOutput(filteredStderr)
}

func NewClient(instanceID int, instanceHost, username, password string, basicUsername, basicPassword *string) (*Client, error) {
	cfg := qbt.Config{
		Host:     instanceHost,
		Username: username,
		Password: password,
		Timeout:  30,
	}

	if basicUsername != nil && *basicUsername != "" {
		cfg.BasicUser = *basicUsername
		if basicPassword != nil {
			cfg.BasicPass = *basicPassword
		}
	}

	qbtClient := qbt.NewClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := qbtClient.LoginCtx(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to qBittorrent instance: %w", err)
	}

	webAPIVersion, err := qbtClient.GetWebAPIVersionCtx(ctx)
	if err != nil {
		webAPIVersion = ""
	}

	supportsSetTags := false
	if webAPIVersion != "" {
		if v, err := semver.NewVersion(webAPIVersion); err == nil {
			minVersion := semver.MustParse("2.11.4")
			supportsSetTags = !v.LessThan(minVersion)
		}
	}

	client := &Client{
		Client:          qbtClient,
		instanceID:      instanceID,
		webAPIVersion:   webAPIVersion,
		supportsSetTags: supportsSetTags,
		lastHealthCheck: time.Now(),
		isHealthy:       true,
	}

	log.Debug().
		Int("instanceID", instanceID).
		Str("host", instanceHost).
		Str("webAPIVersion", webAPIVersion).
		Bool("supportsSetTags", supportsSetTags).
		Msg("qBittorrent client created successfully")

	return client, nil
}

func (c *Client) GetInstanceID() int {
	return c.instanceID
}

func (c *Client) GetLastHealthCheck() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastHealthCheck
}

func (c *Client) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isHealthy
}

func (c *Client) HealthCheck(ctx context.Context) error {
	_, err := c.GetWebAPIVersionCtx(ctx)
	if err != nil {
		if loginErr := c.LoginCtx(ctx); loginErr != nil {
			c.mu.Lock()
			c.isHealthy = false
			c.lastHealthCheck = time.Now()
			c.mu.Unlock()
			return fmt.Errorf("health check failed: login error: %w", loginErr)
		}
		if _, err = c.GetWebAPIVersionCtx(ctx); err != nil {
			c.mu.Lock()
			c.isHealthy = false
			c.lastHealthCheck = time.Now()
			c.mu.Unlock()
			return fmt.Errorf("health check failed: api error: %w", err)
		}
	}

	c.mu.Lock()
	c.isHealthy = true
	c.lastHealthCheck = time.Now()
	c.mu.Unlock()
	return nil
}

func (c *Client) SupportsSetTags() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.supportsSetTags
}

func (c *Client) GetWebAPIVersion() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.webAPIVersion
}
