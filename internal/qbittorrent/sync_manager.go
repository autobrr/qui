package qbittorrent

import (
	"context"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
)

// SyncState represents the synchronization state for an instance
type SyncState struct {
	LastSync       time.Time
	LastError      error
	HasInitialSync bool
	SyncManager    *qbt.SyncManager
	mu             sync.RWMutex
}

// SyncManager manages torrent synchronization for qBittorrent instances
type SyncManager struct {
	clientManager *ClientManager
	states        map[int]*SyncState
	mu            sync.RWMutex
}

// GetClientManager returns the underlying client manager
func (sm *SyncManager) GetClientManager() *ClientManager {
	return sm.clientManager
}

// NewSyncManager creates a new sync manager
func NewSyncManager(clientManager *ClientManager) *SyncManager {
	return &SyncManager{
		clientManager: clientManager,
		states:        make(map[int]*SyncState),
	}
}

// getOrCreateState gets or creates sync state for an instance
func (sm *SyncManager) getOrCreateState(instanceID int) *SyncState {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	state, exists := sm.states[instanceID]
	if !exists {
		state = &SyncState{}
		sm.states[instanceID] = state
	}
	return state
}

// needsSync determines if synchronization is needed
func (sm *SyncManager) needsSync(state *SyncState) bool {
	state.mu.RLock()
	defer state.mu.RUnlock()

	// Need sync if:
	// 1. Never had initial sync
	// 2. Last operation was an error
	// 3. Been too long since last sync (5 seconds)
	return !state.HasInitialSync ||
		state.LastError != nil ||
		time.Since(state.LastSync) > 5*time.Second
}

// GetMainData gets the main data for an instance, using cached data if recent enough
func (sm *SyncManager) GetMainData(ctx context.Context, instanceID int) (*qbt.MainData, error) {
	state := sm.getOrCreateState(instanceID)

	// Check if sync is needed
	if !sm.needsSync(state) {
		state.mu.RLock()
		defer state.mu.RUnlock()
		if state.SyncManager != nil {
			return state.SyncManager.GetData(), nil
		}
	}

	// Get client
	client, err := sm.clientManager.GetClient(ctx, instanceID)
	if err != nil {
		state.mu.Lock()
		state.LastError = err
		state.mu.Unlock()
		return nil, err
	}

	// Perform sync
	state.mu.Lock()
	defer state.mu.Unlock()

	// Initialize SyncManager if nil
	if state.SyncManager == nil {
		state.SyncManager = qbt.NewSyncManager(client)
		err = state.SyncManager.Start(ctx)
		if err != nil {
			state.LastError = err
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to start sync manager")
			return nil, err
		}
	} else {
		// Just sync if already initialized
		err = state.SyncManager.Sync(ctx)
		if err != nil {
			state.LastError = err
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to sync data")
			return nil, err
		}
	}

	// Update state
	state.LastSync = time.Now()
	state.LastError = nil
	state.HasInitialSync = true

	return state.SyncManager.GetData(), nil
}

// GetTorrents gets cached torrents or syncs if needed
func (sm *SyncManager) GetTorrents(ctx context.Context, instanceID int) ([]qbt.Torrent, error) {
	mainData, err := sm.GetMainData(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	// Extract torrents from sync data
	var torrents []qbt.Torrent
	if mainData != nil && mainData.Torrents != nil {
		for _, torrent := range mainData.Torrents {
			torrents = append(torrents, torrent)
		}
	}

	return torrents, nil
}

// InvalidateCache invalidates the cache for an instance (call after modifications)
func (sm *SyncManager) InvalidateCache(instanceID int) {
	state := sm.getOrCreateState(instanceID)
	state.mu.Lock()
	defer state.mu.Unlock()

	// Force next call to sync by clearing the last sync time
	state.LastSync = time.Time{}
}

// GetStats gets torrent statistics from sync data
func (sm *SyncManager) GetStats(ctx context.Context, instanceID int) (*qbt.ServerState, error) {
	mainData, err := sm.GetMainData(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	if mainData != nil {
		return &mainData.ServerState, nil
	}

	return nil, nil
}
