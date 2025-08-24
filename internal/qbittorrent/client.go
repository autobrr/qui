// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

// Client wraps the qBittorrent client with additional functionality
type Client struct {
	*qbt.Client
	instanceID      int
	lastHealthCheck time.Time
	isHealthy       bool
	mu              sync.RWMutex
}

// NewClient creates a new qBittorrent client wrapper
func NewClient(instanceID int, instanceHost, username, password string, basicUsername, basicPassword *string) (*Client, error) {
	// Create the base client
	cfg := qbt.Config{
		Host:     instanceHost,
		Username: username,
		Password: password,
		Timeout:  30, // timeout in seconds
	}

	// Set Basic Auth credentials if provided
	if basicUsername != nil && *basicUsername != "" {
		cfg.BasicUser = *basicUsername
		if basicPassword != nil {
			cfg.BasicPass = *basicPassword
		}
	}

	qbtClient := qbt.NewClient(cfg)

	// Test connection - use 30 seconds to match the client timeout configuration
	// This is especially important for large instances with 10k+ torrents
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := qbtClient.LoginCtx(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to qBittorrent instance: %w", err)
	}

	client := &Client{
		Client:          qbtClient,
		instanceID:      instanceID,
		lastHealthCheck: time.Now(),
		isHealthy:       true,
	}

	return client, nil
}

// GetInstanceID returns the instance ID associated with this client
func (c *Client) GetInstanceID() int {
	return c.instanceID
}

// GetLastHealthCheck returns the time of the last health check
func (c *Client) GetLastHealthCheck() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastHealthCheck
}

// IsHealthy returns whether the client connection is healthy
func (c *Client) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isHealthy
}

// HealthCheck performs a health check on the qBittorrent connection
func (c *Client) HealthCheck(ctx context.Context) error {
	// Use a lightweight API call instead of full login
	// GetWebAPIVersion is perfect - it's fast and doesn't load torrent data
	_, err := c.Client.GetWebAPIVersionCtx(ctx)
	if err != nil {
		// If the version check fails, it might be an auth issue
		// Try to re-login once
		if loginErr := c.Client.LoginCtx(ctx); loginErr != nil {
			c.mu.Lock()
			c.isHealthy = false
			c.lastHealthCheck = time.Now()
			c.mu.Unlock()
			return fmt.Errorf("health check failed: login error: %w", loginErr)
		}
		// Retry the version check after login
		if _, err = c.Client.GetWebAPIVersionCtx(ctx); err != nil {
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

