package qbittorrent

import (
	"context"
	"fmt"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

// ClientManager manages qBittorrent client connections
type ClientManager struct {
	clients       map[int]*qbt.Client
	instanceStore *models.InstanceStore
	mu            sync.RWMutex
}

// NewClientManager creates a new client manager
func NewClientManager(instanceStore *models.InstanceStore) *ClientManager {
	return &ClientManager{
		clients:       make(map[int]*qbt.Client),
		instanceStore: instanceStore,
	}
}

// GetClient returns a qBittorrent client for the given instance ID
func (cm *ClientManager) GetClient(ctx context.Context, instanceID int) (*qbt.Client, error) {
	cm.mu.RLock()
	client, exists := cm.clients[instanceID]
	cm.mu.RUnlock()

	if exists {
		// Test if client is still healthy
		if err := cm.testClient(ctx, client); err == nil {
			return client, nil
		}
		// Client is unhealthy, remove it and create a new one
		cm.removeClient(instanceID)
	}

	// Create new client
	return cm.createClient(ctx, instanceID)
}

// createClient creates a new client connection
func (cm *ClientManager) createClient(ctx context.Context, instanceID int) (*qbt.Client, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Double-check after acquiring write lock
	if client, exists := cm.clients[instanceID]; exists {
		if err := cm.testClient(ctx, client); err == nil {
			return client, nil
		}
		// Remove unhealthy client
		delete(cm.clients, instanceID)
	}

	// Get instance details
	instance, err := cm.instanceStore.Get(instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}

	// Decrypt password
	password, err := cm.instanceStore.GetDecryptedPassword(instance)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt password: %w", err)
	}

	// Decrypt basic auth password if present
	var basicPassword string
	if instance.BasicPasswordEncrypted != nil {
		basicPasswordPtr, err := cm.instanceStore.GetDecryptedBasicPassword(instance)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt basic auth password: %w", err)
		}
		if basicPasswordPtr != nil {
			basicPassword = *basicPasswordPtr
		}
	}

	// Construct the host URL
	hostURL := constructHostURL(instance.Host, instance.Port)

	// Create client config
	cfg := qbt.Config{
		Host:     hostURL,
		Username: instance.Username,
		Password: password,
		Timeout:  30, // 30 seconds timeout
	}

	// Set Basic Auth credentials if provided
	if instance.BasicUsername != nil && *instance.BasicUsername != "" {
		cfg.BasicUser = *instance.BasicUsername
		cfg.BasicPass = basicPassword
	}

	client := qbt.NewClient(cfg)

	// Test connection
	if err := client.LoginCtx(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to qBittorrent instance: %w", err)
	}

	// Store in map
	cm.clients[instanceID] = client

	// Update last connected timestamp
	go func() {
		if err := cm.instanceStore.UpdateLastConnected(instanceID); err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to update last connected timestamp")
		}
	}()

	log.Info().Int("instanceID", instanceID).Str("name", instance.Name).Msg("Created new qBittorrent client")
	return client, nil
}

// testClient tests if a client connection is healthy
func (cm *ClientManager) testClient(ctx context.Context, client *qbt.Client) error {
	// Use a quick timeout for health checks
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Use GetWebAPIVersion as a lightweight health check
	_, err := client.GetWebAPIVersionCtx(ctx)
	return err
}

// removeClient removes a client from the manager
func (cm *ClientManager) removeClient(instanceID int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.clients, instanceID)
	log.Debug().Int("instanceID", instanceID).Msg("Removed client from manager")
}

// Close closes all clients and releases resources
func (cm *ClientManager) Close() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for id := range cm.clients {
		delete(cm.clients, id)
	}

	log.Info().Msg("Client manager closed")
	return nil
}

// constructHostURL constructs the full host URL
func constructHostURL(host string, port int) string {
	// Remove trailing slash if present
	if len(host) > 0 && host[len(host)-1] == '/' {
		host = host[:len(host)-1]
	}

	// If host already includes protocol and potentially path, use it as-is or append port appropriately
	if host[:7] == "http://" || (len(host) > 8 && host[:8] == "https://") {
		protocolEnd := 7
		if host[:8] == "https://" {
			protocolEnd = 8
		}

		// Check if there's already a path component
		pathIdx := -1
		for i := protocolEnd; i < len(host); i++ {
			if host[i] == '/' {
				pathIdx = i
				break
			}
		}

		if pathIdx != -1 {
			// Has a path, use as-is (reverse proxy scenario)
			return host
		}

		// Check if port is already included
		colonIdx := -1
		for i := protocolEnd; i < len(host); i++ {
			if host[i] == ':' {
				colonIdx = i
				break
			}
		}

		if colonIdx != -1 || port == 80 || port == 443 {
			// Port already included or standard port
			return host
		}

		// Append port
		return fmt.Sprintf("%s:%d", host, port)
	}

	// No protocol specified, assume HTTP and add port
	return fmt.Sprintf("http://%s:%d", host, port)
}
