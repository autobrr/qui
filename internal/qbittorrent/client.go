package qbittorrent

import (
	"context"
	"fmt"
	"strings"
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

// firstPathSeparator finds the first path separator after the protocol
func firstPathSeparator(s string) int {
	return strings.IndexByte(s, '/')
}

// NewClient creates a new qBittorrent client wrapper
func NewClient(instanceID int, host string, port int, username, password string, basicUsername, basicPassword *string) (*Client, error) {
	// Construct the host URL
	// If the host already includes a port or path (like a reverse proxy URL), use it as-is
	// Otherwise, append the port
	var hostURL string
	
	// Remove trailing slash if present
	if len(host) > 0 && host[len(host)-1] == '/' {
		host = host[:len(host)-1]
	}
	
	// Check if the host already contains a path (reverse proxy scenario)
	// In this case, we don't append the port as it's already handled by the proxy
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		// Parse to see if there's already a path component after the domain
		protocolEnd := 0
		if strings.HasPrefix(host, "https://") {
			protocolEnd = 8
		} else {
			protocolEnd = 7
		}
		
		pathIdx := strings.IndexByte(host[protocolEnd:], '/')
		if pathIdx != -1 {
			// Has a path, use as-is (reverse proxy scenario)
			hostURL = host
		} else if port == 443 || port == 80 {
			// Standard ports, don't append
			hostURL = host
		} else {
			// Non-standard port, append it
			hostURL = fmt.Sprintf("%s:%d", host, port)
		}
	} else {
		// Fallback to original behavior
		hostURL = fmt.Sprintf("%s:%d", host, port)
	}
	
	// Create the base client
	cfg := qbt.Config{
		Host:     hostURL,
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

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

// IsHealthy returns whether the client connection is healthy
func (c *Client) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isHealthy
}

// SetHealthy updates the health status of the client
func (c *Client) SetHealthy(healthy bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.isHealthy = healthy
	c.lastHealthCheck = time.Now()
}

// HealthCheck performs a health check on the qBittorrent connection
func (c *Client) HealthCheck(ctx context.Context) error {
	// Try to login again as a health check
	if err := c.Client.LoginCtx(ctx); err != nil {
		c.SetHealthy(false)
		return fmt.Errorf("health check failed: %w", err)
	}

	c.SetHealthy(true)
	return nil
}

// GetLastHealthCheck returns the time of the last health check
func (c *Client) GetLastHealthCheck() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastHealthCheck
}
