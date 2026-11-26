// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/api/middleware"
	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/internal/services/orphanscan"
	"github.com/autobrr/qui/internal/services/peeraddress"
	"github.com/autobrr/qui/internal/services/reannounce"
	"github.com/autobrr/qui/internal/sshpool"
)

type InstancesHandler struct {
	instanceStore   *models.InstanceStore
	reannounceStore *models.InstanceReannounceStore
	reannounceCache *reannounce.SettingsCache
	clientPool      *internalqbittorrent.ClientPool
	syncManager     *internalqbittorrent.SyncManager
	reannounceSvc   *reannounce.Service
	sshDialer       *sshpool.Dialer
	sshPool         *sshpool.Pool
	orphanScan      orphanScanGuard
	routes          *crossseedroute.Service
	peers           *peeraddress.Resolver
	filesystem      *fsops.Pool
	connectionTests sync.WaitGroup
}

type orphanScanGuard interface {
	ChangeSSH(ctx context.Context, instanceID int, change func() error) error
}

func (h *InstancesHandler) SetOrphanScan(s *orphanscan.Service) {
	if s != nil {
		h.orphanScan = s
	}
}

func (h *InstancesHandler) SetCrossSeedRoutes(routes *crossseedroute.Service) {
	h.routes = routes
}

func (h *InstancesHandler) refuseRouteBreak(w http.ResponseWriter, r *http.Request, before, after *models.Instance) bool {
	if h.routes == nil {
		return false
	}
	err := h.routes.CheckInstanceChange(r.Context(), before, after)
	if err == nil {
		return false
	}
	if ruleErr, ok := errors.AsType[*crossseedroute.RuleError](err); ok {
		RespondError(w, http.StatusBadRequest, ruleErr.Message)
		return true
	}
	log.Error().Err(err).Int("instanceID", after.ID).Msg("Failed to check cross-seed routes")
	RespondError(w, http.StatusInternalServerError, "Failed to check cross-seed routes")
	return true
}

const sshChangeDuringDeletionMessage = "An orphan scan is deleting files on this instance. Wait for it to finish before changing its SSH settings."

func (h *InstancesHandler) changeSSH(ctx context.Context, instanceID int, change func() error) error {
	if h.orphanScan == nil {
		return change()
	}
	return h.orphanScan.ChangeSSH(ctx, instanceID, change)
}

func NewInstancesHandler(instanceStore *models.InstanceStore, reannounceStore *models.InstanceReannounceStore, reannounceCache *reannounce.SettingsCache, clientPool *internalqbittorrent.ClientPool, syncManager *internalqbittorrent.SyncManager, svc *reannounce.Service, sshDialer *sshpool.Dialer, sshPool *sshpool.Pool) *InstancesHandler {
	h := &InstancesHandler{
		instanceStore:   instanceStore,
		reannounceStore: reannounceStore,
		reannounceCache: reannounceCache,
		clientPool:      clientPool,
		syncManager:     syncManager,
		reannounceSvc:   svc,
		sshDialer:       sshDialer,
		sshPool:         sshPool,
	}
	if syncManager != nil {
		h.peers = peeraddress.New(instanceStore, syncManager)
	}
	return h
}

func (h *InstancesHandler) GetInstanceCapabilities(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	client, err := h.clientPool.GetClientOffline(ctx, instanceID)
	if err != nil {
		client, err = h.clientPool.GetClientWithTimeout(ctx, instanceID, 15*time.Second)
		if err != nil {
			if respondIfInstanceDisabled(w, err, instanceID, "instances:getCapabilities") {
				return
			}
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client for capabilities")
			RespondError(w, http.StatusServiceUnavailable, instanceCapabilitiesClientErrorMessage(err))
			return
		}
	}

	if err := client.EnsureCapabilities(ctx); err != nil {
		log.Error().
			Err(err).
			Int("instanceID", instanceID).
			Msg("Unable to refresh qBittorrent capabilities during request")
	}

	capabilities := NewInstanceCapabilitiesResponse(client)
	RespondJSON(w, http.StatusOK, capabilities)
}

func instanceCapabilitiesClientErrorMessage(err error) string {
	if message, ok := internalqbittorrent.InstanceHealthBlockerMessage(err); ok {
		return message
	}
	return "Failed to load instance capabilities"
}

func (h *InstancesHandler) GetReannounceActivity(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}
	if h.reannounceSvc == nil {
		RespondJSON(w, http.StatusOK, []reannounce.ActivityEvent{})
		return
	}
	limitParam := strings.TrimSpace(r.URL.Query().Get("limit"))
	var limit int
	if limitParam != "" {
		if parsed, err := strconv.Atoi(limitParam); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	events := h.reannounceSvc.GetActivity(instanceID, limit)
	if events == nil {
		events = []reannounce.ActivityEvent{}
	}
	normalized := make([]reannounce.ActivityEvent, len(events))
	for i, event := range events {
		event.Hash = strings.ToLower(event.Hash)
		normalized[i] = event
	}
	RespondJSON(w, http.StatusOK, normalized)
}

func (h *InstancesHandler) GetReannounceCandidates(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}
	if h.reannounceSvc == nil {
		RespondJSON(w, http.StatusOK, []reannounce.MonitoredTorrent{})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	candidates := h.reannounceSvc.GetMonitoredTorrents(ctx, instanceID)
	if candidates == nil {
		candidates = []reannounce.MonitoredTorrent{}
	}

	normalized := make([]reannounce.MonitoredTorrent, len(candidates))
	for i, candidate := range candidates {
		candidate.Hash = strings.ToLower(candidate.Hash)
		normalized[i] = candidate
	}

	RespondJSON(w, http.StatusOK, normalized)
}

type transferInfoResponse struct {
	qbt.TransferInfo
	AlltimeDl *int64 `json:"alltime_dl,omitempty"`
	AlltimeUl *int64 `json:"alltime_ul,omitempty"`
}

func newTransferInfoResponse(state *qbt.ServerState) transferInfoResponse {
	return transferInfoResponse{
		ConnectionStatus: qbt.ConnectionStatus(state.ConnectionStatus),
		DHTNodes:         state.DhtNodes,
		DlInfoData:       state.DlInfoData,
		DlInfoSpeed:      state.DlInfoSpeed,
		DlRateLimit:      state.DlRateLimit,
		UpInfoData:       state.UpInfoData,
		UpInfoSpeed:      state.UpInfoSpeed,
		UpRateLimit:      state.UpRateLimit,
		AlltimeDl:        &state.AlltimeDl,
		AlltimeUl:        &state.AlltimeUl,
	}
}

func (h *InstancesHandler) GetTransferInfo(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	client, err := h.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "instances:getTransferInfo") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client for transfer info")
		RespondError(w, http.StatusServiceUnavailable, "Failed to load transfer info")
		return
	}

	if syncManager := client.SyncOwner(); syncManager != nil {
		if err := syncManager.Sync(ctx); err != nil {
			log.Debug().Err(err).Int("instanceID", instanceID).Msg("Failed to sync before reading server state; serving cached state")
		}

		if state := client.GetCachedServerState(); state != nil {
			RespondJSON(w, http.StatusOK, newTransferInfoResponse(state))
			return
		}
	}

	info, err := client.GetTransferInfoCtx(ctx)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get transfer info")
		RespondError(w, http.StatusInternalServerError, "Failed to get transfer info")
		return
	}

	RespondJSON(w, http.StatusOK, transferInfoResponse{TransferInfo: *info})
}

type InstanceFreeSpaceResponse struct {
	Path  string `json:"path"`
	Bytes *int64 `json:"bytes"`
}

func (h *InstancesHandler) GetFreeSpaceAtPath(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		RespondError(w, http.StatusBadRequest, "Path is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	freeSpace, err := h.syncManager.GetFreeSpaceAtPath(ctx, instanceID, path)
	switch {
	case errors.Is(err, internalqbittorrent.ErrFreeSpaceAtPathUnsupported):
		RespondError(w, http.StatusNotImplemented, "This qBittorrent instance does not support free space at a path")
		return
	case err != nil:
		if respondIfInstanceDisabled(w, err, instanceID, "instances:getFreeSpaceAtPath") {
			return
		}
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("Failed to get free space at path")
		RespondJSON(w, http.StatusOK, InstanceFreeSpaceResponse{Path: path})
		return
	}

	RespondJSON(w, http.StatusOK, InstanceFreeSpaceResponse{Path: path, Bytes: &freeSpace})
}

func (h *InstancesHandler) buildInstanceResponsesParallel(ctx context.Context, instances []*models.Instance) []InstanceResponse {
	if len(instances) == 0 {
		return []InstanceResponse{}
	}

	type result struct {
		index    int
		response InstanceResponse
	}
	resultCh := make(chan result, len(instances))

	for i, instance := range instances {
		go func(index int, inst *models.Instance) {
			response := h.buildInstanceResponse(ctx, inst)
			resultCh <- result{index: index, response: response}
		}(i, instance)
	}

	responses := make([]InstanceResponse, len(instances))
	for i := range instances {
		select {
		case res := <-resultCh:
			responses[res.index] = res.response
		case <-ctx.Done():
			responses[i] = InstanceResponse{
				ID:                       instances[i].ID,
				Name:                     instances[i].Name,
				Host:                     instances[i].Host,
				Username:                 instances[i].Username,
				HasAPIKey:                instances[i].APIKeyEncrypted != "",
				BasicUsername:            instances[i].BasicUsername,
				TLSSkipVerify:            instances[i].TLSSkipVerify,
				HasLocalFilesystemAccess: instances[i].HasLocalFilesystemAccess,
				UseHardlinks:             instances[i].UseHardlinks,
				HardlinkBaseDir:          instances[i].HardlinkBaseDir,
				HardlinkDirPreset:        instances[i].HardlinkDirPreset,
				UseReflinks:              instances[i].UseReflinks,
				Connected:                false,
				HasDecryptionError:       false,
				SortOrder:                instances[i].SortOrder,
				IsActive:                 instances[i].IsActive,
				SSHHost:                  instances[i].SSHHost,
				SSHPort:                  instances[i].SSHPort,
				SSHUsername:              instances[i].SSHUsername,
				SSHHostKeyPinned:         instances[i].SSHHostKeyEncrypted != "",
				SSHTier:                  sshTierField(instances[i]),
				SSHHardlink:              instances[i].SSHHardlink,
				FilesystemMode:           string(models.FilesystemAccessMode(instances[i])),
				Capabilities:             models.FilesystemCapabilitiesOf(instances[i]),
				ReannounceSettings:       payloadFromModel(models.DefaultInstanceReannounceSettings(instances[i].ID)),
				ConnectionStatus: func(active bool) string {
					if !active {
						return "disabled"
					}
					return ""
				}(instances[i].IsActive),
			}
		}
	}

	return responses
}

func (h *InstancesHandler) buildInstanceResponse(ctx context.Context, instance *models.Instance) InstanceResponse {
	client, _ := h.clientPool.GetClientOffline(ctx, instance.ID)
	healthy := client != nil && client.IsHealthy() && instance.IsActive

	var connectionStatus string
	if !instance.IsActive {
		connectionStatus = "disabled"
	} else if client != nil && h.syncManager != nil {
		if status := internalqbittorrent.NormalizeConnectionStatus(h.syncManager.ReadCachedConnectionStatus(ctx, instance.ID)); status != "" {
			connectionStatus = status
		}
	}

	decryptionErrorInstances := h.clientPool.GetInstancesWithDecryptionErrors()
	hasDecryptionError := slices.Contains(decryptionErrorInstances, instance.ID)

	response := InstanceResponse{
		ID:                        instance.ID,
		Name:                      instance.Name,
		Host:                      instance.Host,
		Username:                  instance.Username,
		HasAPIKey:                 instance.APIKeyEncrypted != "",
		BasicUsername:             instance.BasicUsername,
		TLSSkipVerify:             instance.TLSSkipVerify,
		HasLocalFilesystemAccess:  instance.HasLocalFilesystemAccess,
		UseHardlinks:              instance.UseHardlinks,
		HardlinkBaseDir:           instance.HardlinkBaseDir,
		HardlinkDirPreset:         instance.HardlinkDirPreset,
		UseReflinks:               instance.UseReflinks,
		FallbackToRegularMode:     instance.FallbackToRegularMode,
		Connected:                 healthy,
		HasDecryptionError:        hasDecryptionError,
		ConnectionStatus:          connectionStatus,
		SortOrder:                 instance.SortOrder,
		IsActive:                  instance.IsActive,
		SSHHost:                   instance.SSHHost,
		SSHPort:                   instance.SSHPort,
		SSHUsername:               instance.SSHUsername,
		SSHHostKeyPinned:          instance.SSHHostKeyEncrypted != "",
		SSHTier:                   sshTierField(instance),
		SSHHardlink:               instance.SSHHardlink,
		FilesystemMode:            string(models.FilesystemAccessMode(instance)),
		Capabilities:              models.FilesystemCapabilitiesOf(instance),
		PublicPeerAddress:         instance.PublicPeerAddress,
		ResolvedPublicPeerAddress: h.resolvedPublicPeerAddress(ctx, instance, healthy),

		ReannounceSettings: h.getReannounceSettingsPayload(ctx, instance.ID)}

	if instance.IsActive && !healthy {
		errorStore := h.clientPool.GetErrorStore()
		recentErrors, err := errorStore.GetRecentErrors(ctx, instance.ID, 5)
		if err != nil {
			log.Error().Err(err).Int("instanceID", instance.ID).Msg("Failed to get recent errors")
		} else {
			response.RecentErrors = recentErrors
		}
	}

	return response
}

func (h *InstancesHandler) buildQuickInstanceResponse(instance *models.Instance) InstanceResponse {
	connectionStatus := ""
	if !instance.IsActive {
		connectionStatus = "disabled"
	}
	return InstanceResponse{
		ID:                       instance.ID,
		Name:                     instance.Name,
		Host:                     instance.Host,
		Username:                 instance.Username,
		HasAPIKey:                instance.APIKeyEncrypted != "",
		BasicUsername:            instance.BasicUsername,
		TLSSkipVerify:            instance.TLSSkipVerify,
		HasLocalFilesystemAccess: instance.HasLocalFilesystemAccess,
		UseHardlinks:             instance.UseHardlinks,
		HardlinkBaseDir:          instance.HardlinkBaseDir,
		HardlinkDirPreset:        instance.HardlinkDirPreset,
		UseReflinks:              instance.UseReflinks,
		FallbackToRegularMode:    instance.FallbackToRegularMode,
		Connected:                false,
		HasDecryptionError:       false,
		SortOrder:                instance.SortOrder,
		IsActive:                 instance.IsActive,
		ConnectionStatus:         connectionStatus,
		SSHHost:                  instance.SSHHost,
		SSHPort:                  instance.SSHPort,
		SSHUsername:              instance.SSHUsername,
		SSHHostKeyPinned:         instance.SSHHostKeyEncrypted != "",
		SSHTier:                  sshTierField(instance),
		SSHHardlink:              instance.SSHHardlink,
		FilesystemMode:           string(models.FilesystemAccessMode(instance)),
		Capabilities:             models.FilesystemCapabilitiesOf(instance),
		PublicPeerAddress:        instance.PublicPeerAddress,
	}
}

const resolvePeerAddressTimeout = 3 * time.Second

func (h *InstancesHandler) resolvedPublicPeerAddress(ctx context.Context, instance *models.Instance, connected bool) string {
	if instance.PublicPeerAddress != "" {
		return instance.PublicPeerAddress
	}
	if h.peers == nil || !connected {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, resolvePeerAddressTimeout)
	defer cancel()
	address, err := h.peers.LastPublicPeerAddress(ctx, instance.ID)
	if err != nil && !errors.Is(err, peeraddress.ErrNoAddress) {
		log.Debug().Err(err).Int("instanceID", instance.ID).Msg("Failed to work out the public peer address")
	}
	return address
}

func validPublicPeerAddress(address string) bool {
	if address == "" {
		return true
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}

func (h *InstancesHandler) testConnectionAsync(instanceID int) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log.Debug().Int("instanceID", instanceID).Msg("Testing connection asynchronously")

	client, err := h.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("Async connection test failed")
		return
	}

	if err := client.HealthCheck(ctx); err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("Async health check failed")
		return
	}

	log.Debug().Int("instanceID", instanceID).Msg("Async connection test succeeded")
}

func (h *InstancesHandler) loadReannounceSettings(ctx context.Context, instanceID int) *models.InstanceReannounceSettings {
	if h.reannounceStore == nil {
		return models.DefaultInstanceReannounceSettings(instanceID)
	}
	settings, err := h.reannounceStore.Get(ctx, instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to load reannounce settings")
		return models.DefaultInstanceReannounceSettings(instanceID)
	}
	return settings
}

func (h *InstancesHandler) getReannounceSettingsPayload(ctx context.Context, instanceID int) InstanceReannounceSettingsPayload {
	if h.reannounceCache != nil {
		if cached := h.reannounceCache.Get(instanceID); cached != nil {
			return payloadFromModel(cached)
		}
	}
	return payloadFromModel(h.loadReannounceSettings(ctx, instanceID))
}

func (h *InstancesHandler) persistReannounceSettings(ctx context.Context, instanceID int, payload *InstanceReannounceSettingsPayload) (*models.InstanceReannounceSettings, error) {
	desired := payload.toModel(instanceID, nil)
	if h.reannounceStore == nil {
		if h.reannounceCache != nil {
			h.reannounceCache.Replace(desired)
		}
		return desired, nil
	}
	saved, err := h.reannounceStore.Upsert(ctx, desired)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to persist reannounce settings")
		return nil, err
	}
	if h.reannounceCache != nil {
		h.reannounceCache.Replace(saved)
	}
	return saved, nil
}

type CreateInstanceRequest struct {
	Name                     string                             `json:"name"`
	Host                     string                             `json:"host"`
	Username                 string                             `json:"username"`
	Password                 string                             `json:"password"`
	APIKey                   string                             `json:"apiKey,omitempty"`
	BasicUsername            *string                            `json:"basicUsername,omitempty"`
	BasicPassword            *string                            `json:"basicPassword,omitempty"`
	TLSSkipVerify            bool                               `json:"tlsSkipVerify,omitempty"`
	HasLocalFilesystemAccess *bool                              `json:"hasLocalFilesystemAccess,omitempty"`
	ReannounceSettings       *InstanceReannounceSettingsPayload `json:"reannounceSettings,omitempty"`
}

type UpdateInstanceRequest struct {
	Name                     string                             `json:"name"`
	Host                     string                             `json:"host"`
	Username                 string                             `json:"username"`
	Password                 string                             `json:"password,omitempty"`
	APIKey                   *string                            `json:"apiKey,omitempty"`
	BasicUsername            *string                            `json:"basicUsername,omitempty"`
	BasicPassword            *string                            `json:"basicPassword,omitempty"`
	TLSSkipVerify            *bool                              `json:"tlsSkipVerify,omitempty"`
	HasLocalFilesystemAccess *bool                              `json:"hasLocalFilesystemAccess,omitempty"`
	UseHardlinks             *bool                              `json:"useHardlinks,omitempty"`
	HardlinkBaseDir          *string                            `json:"hardlinkBaseDir,omitempty"`
	HardlinkDirPreset        *string                            `json:"hardlinkDirPreset,omitempty"`
	UseReflinks              *bool                              `json:"useReflinks,omitempty"`
	FallbackToRegularMode    *bool                              `json:"fallbackToRegularMode,omitempty"`
	ReannounceSettings       *InstanceReannounceSettingsPayload `json:"reannounceSettings,omitempty"`
	PublicPeerAddress        *string                            `json:"publicPeerAddress,omitempty"`
}

type UpdateInstanceStatusRequest struct {
	IsActive bool `json:"isActive"`
}

type InstanceResponse struct {
	ID                        int                               `json:"id"`
	Name                      string                            `json:"name"`
	Host                      string                            `json:"host"`
	Username                  string                            `json:"username"`
	HasAPIKey                 bool                              `json:"hasApiKey"`
	BasicUsername             *string                           `json:"basicUsername,omitempty"`
	TLSSkipVerify             bool                              `json:"tlsSkipVerify"`
	HasLocalFilesystemAccess  bool                              `json:"hasLocalFilesystemAccess"`
	UseHardlinks              bool                              `json:"useHardlinks"`
	HardlinkBaseDir           string                            `json:"hardlinkBaseDir"`
	HardlinkDirPreset         string                            `json:"hardlinkDirPreset"`
	UseReflinks               bool                              `json:"useReflinks"`
	FallbackToRegularMode     bool                              `json:"fallbackToRegularMode"`
	Connected                 bool                              `json:"connected"`
	HasDecryptionError        bool                              `json:"hasDecryptionError"`
	RecentErrors              []models.InstanceError            `json:"recentErrors,omitempty"`
	ConnectionStatus          string                            `json:"connectionStatus,omitempty"`
	SortOrder                 int                               `json:"sortOrder"`
	IsActive                  bool                              `json:"isActive"`
	SSHHost                   string                            `json:"sshHost,omitempty"`
	SSHPort                   int                               `json:"sshPort,omitempty"`
	SSHUsername               string                            `json:"sshUsername,omitempty"`
	SSHHostKeyPinned          bool                              `json:"sshHostKeyPinned"`
	SSHTier                   string                            `json:"sshTier,omitempty"`
	SSHHardlink               *bool                             `json:"sshHardlink,omitempty"`
	FilesystemMode            string                            `json:"filesystemMode"`
	Capabilities              models.FilesystemCapabilities     `json:"capabilities"`
	ReannounceSettings        InstanceReannounceSettingsPayload `json:"reannounceSettings"`
	PublicPeerAddress         string                            `json:"publicPeerAddress"`
	ResolvedPublicPeerAddress string                            `json:"resolvedPublicPeerAddress"`
}

func sshTierField(instance *models.Instance) string {
	if instance.SSHKeyEncrypted == "" {
		return ""
	}
	return string(instance.SSHTier)
}

type InstanceReannounceSettingsPayload struct {
	Enabled                   bool     `json:"enabled"`
	InitialWaitSeconds        int      `json:"initialWaitSeconds"`
	ReannounceIntervalSeconds int      `json:"reannounceIntervalSeconds"`
	MaxAgeSeconds             int      `json:"maxAgeSeconds"`
	MaxRetries                int      `json:"maxRetries"`
	Aggressive                bool     `json:"aggressive"`
	MonitorAll                bool     `json:"monitorAll"`
	ExcludeCategories         bool     `json:"excludeCategories"`
	Categories                []string `json:"categories"`
	ExcludeTags               bool     `json:"excludeTags"`
	Tags                      []string `json:"tags"`
	ExcludeTrackers           bool     `json:"excludeTrackers"`
	Trackers                  []string `json:"trackers"`
}

type TestConnectionResponse struct {
	Connected bool   `json:"connected"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (p *InstanceReannounceSettingsPayload) toModel(instanceID int, base *models.InstanceReannounceSettings) *models.InstanceReannounceSettings {
	var target *models.InstanceReannounceSettings
	if base != nil {
		clone := *base
		target = &clone
	} else {
		target = models.DefaultInstanceReannounceSettings(instanceID)
	}
	if p == nil {
		target.InstanceID = instanceID
		return target
	}
	target.InstanceID = instanceID
	target.Enabled = p.Enabled
	target.InitialWaitSeconds = p.InitialWaitSeconds
	target.ReannounceIntervalSeconds = p.ReannounceIntervalSeconds
	target.MaxAgeSeconds = p.MaxAgeSeconds
	target.MaxRetries = p.MaxRetries
	target.Aggressive = p.Aggressive
	target.MonitorAll = p.MonitorAll
	target.ExcludeCategories = p.ExcludeCategories
	target.Categories = append([]string{}, p.Categories...)
	target.ExcludeTags = p.ExcludeTags
	target.Tags = append([]string{}, p.Tags...)
	target.ExcludeTrackers = p.ExcludeTrackers
	target.Trackers = append([]string{}, p.Trackers...)
	return target
}

func payloadFromModel(settings *models.InstanceReannounceSettings) InstanceReannounceSettingsPayload {
	if settings == nil {
		settings = models.DefaultInstanceReannounceSettings(0)
	}
	return InstanceReannounceSettingsPayload{
		Enabled:                   settings.Enabled,
		InitialWaitSeconds:        settings.InitialWaitSeconds,
		ReannounceIntervalSeconds: settings.ReannounceIntervalSeconds,
		MaxAgeSeconds:             settings.MaxAgeSeconds,
		MaxRetries:                settings.MaxRetries,
		Aggressive:                settings.Aggressive,
		MonitorAll:                settings.MonitorAll,
		ExcludeCategories:         settings.ExcludeCategories,
		Categories:                append([]string{}, settings.Categories...),
		ExcludeTags:               settings.ExcludeTags,
		Tags:                      append([]string{}, settings.Tags...),
		ExcludeTrackers:           settings.ExcludeTrackers,
		Trackers:                  append([]string{}, settings.Trackers...),
	}
}

type DeleteInstanceResponse struct {
	Message string `json:"message"`
}

func (h *InstancesHandler) ListInstances(w http.ResponseWriter, r *http.Request) {
	instances, err := h.instanceStore.List(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to list instances")
		RespondError(w, http.StatusInternalServerError, "Failed to list instances")
		return
	}
	instances = slices.DeleteFunc(instances, func(inst *models.Instance) bool { return !middleware.ReachesInstance(r, inst.ID) })

	response := h.buildInstanceResponsesParallel(r.Context(), instances)

	RespondJSON(w, http.StatusOK, response)
}

func (h *InstancesHandler) UpdateInstanceOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InstanceIDs []int `json:"instanceIds"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(req.InstanceIDs) == 0 {
		RespondError(w, http.StatusBadRequest, "instanceIds must not be empty")
		return
	}

	instances, err := h.instanceStore.List(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to list instances for reorder")
		RespondError(w, http.StatusInternalServerError, "Failed to list instances")
		return
	}

	if len(req.InstanceIDs) != len(instances) {
		RespondError(w, http.StatusBadRequest, "instanceIds must include all instances")
		return
	}

	validIDs := make(map[int]struct{}, len(instances))
	for _, inst := range instances {
		validIDs[inst.ID] = struct{}{}
	}

	seen := make(map[int]struct{}, len(req.InstanceIDs))
	for _, id := range req.InstanceIDs {
		if _, ok := validIDs[id]; !ok {
			RespondError(w, http.StatusBadRequest, "instanceIds contains an unknown instance")
			return
		}
		if _, ok := seen[id]; ok {
			RespondError(w, http.StatusBadRequest, "instanceIds must not contain duplicates")
			return
		}
		seen[id] = struct{}{}
	}

	if err := h.instanceStore.UpdateOrder(r.Context(), req.InstanceIDs); err != nil {
		log.Error().Err(err).Msg("Failed to update instance order")
		RespondError(w, http.StatusInternalServerError, "Failed to update instance order")
		return
	}

	updatedInstances, err := h.instanceStore.List(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to list instances after reorder")
		RespondError(w, http.StatusInternalServerError, "Failed to list instances")
		return
	}

	response := h.buildInstanceResponsesParallel(r.Context(), updatedInstances)
	RespondJSON(w, http.StatusOK, response)
}

func (h *InstancesHandler) CreateInstance(w http.ResponseWriter, r *http.Request) {
	var req CreateInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" || req.Host == "" {
		RespondError(w, http.StatusBadRequest, "Name and host are required")
		return
	}

	instance, err := h.instanceStore.Create(r.Context(), req.Name, req.Host, req.Username, req.Password, req.BasicUsername, req.BasicPassword, req.TLSSkipVerify, req.HasLocalFilesystemAccess, req.APIKey)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create instance")
		RespondError(w, http.StatusInternalServerError, "Failed to create instance")
		return
	}

	settings, err := h.persistReannounceSettings(r.Context(), instance.ID, req.ReannounceSettings)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to save reannounce settings")
		return
	}

	response := h.buildQuickInstanceResponse(instance)
	response.ReannounceSettings = payloadFromModel(settings)

	h.connectionTests.Go(func() { h.testConnectionAsync(instance.ID) })

	RespondJSON(w, http.StatusCreated, response)
}

func (h *InstancesHandler) UpdateInstance(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req UpdateInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" || req.Host == "" {
		RespondError(w, http.StatusBadRequest, "Name and host are required")
		return
	}

	if req.PublicPeerAddress != nil && !validPublicPeerAddress(*req.PublicPeerAddress) {
		RespondError(w, http.StatusBadRequest, "The public peer address must be a host and a port, such as 203.0.113.7:51413")
		return
	}

	existingInstance, err := h.instanceStore.Get(r.Context(), instanceID)
	if err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			RespondError(w, http.StatusNotFound, "Instance not found")
			return
		}
		log.Error().Err(err).Msg("Failed to fetch existing instance")
		RespondError(w, http.StatusInternalServerError, "Failed to fetch instance")
		return
	}

	req.restoreRedactedSecrets(existingInstance)

	effective := req.applyLinkSettings(existingInstance)

	if effective.UseHardlinks && effective.UseReflinks {
		RespondError(w, http.StatusBadRequest, "Cannot enable both hardlink and reflink modes - they are mutually exclusive")
		return
	}

	if h.refuseRouteBreak(w, r, existingInstance, &effective) {
		return
	}

	if msg := linkModeError(&effective); msg != "" {
		RespondError(w, http.StatusBadRequest, msg)
		return
	}

	updateParams := &models.InstanceUpdateParams{
		TLSSkipVerify:            req.TLSSkipVerify,
		HasLocalFilesystemAccess: req.HasLocalFilesystemAccess,
		UseHardlinks:             req.UseHardlinks,
		HardlinkBaseDir:          req.HardlinkBaseDir,
		HardlinkDirPreset:        req.HardlinkDirPreset,
		UseReflinks:              req.UseReflinks,
		FallbackToRegularMode:    req.FallbackToRegularMode,
		PublicPeerAddress:        req.PublicPeerAddress,
	}
	instance, err := h.instanceStore.Update(r.Context(), instanceID, req.Name, req.Host, req.Username, req.Password, req.BasicUsername, req.BasicPassword, updateParams, req.APIKey)
	if err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			RespondError(w, http.StatusNotFound, "Instance not found")
			return
		}
		log.Error().Err(err).Msg("Failed to update instance")
		RespondError(w, http.StatusInternalServerError, "Failed to update instance")
		return
	}

	h.clientPool.RemoveClient(instanceID)
	h.clientPool.ResetFailureTracking(instanceID) //nolint:contextcheck // the error clear has its own timeout and must outlive a cancelled request
	if models.FilesystemAccessMode(existingInstance) != models.FilesystemAccessMode(instance) {
		h.sshPool.Invalidate(instanceID)
	}
	if h.routes != nil {
		if err := h.routes.DropRemovedBaseDirs(r.Context(), instanceID); err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to drop the cross-seed route rows of removed base folders")
		}
	}

	var settings *models.InstanceReannounceSettings
	if req.ReannounceSettings != nil {
		var err error
		settings, err = h.persistReannounceSettings(r.Context(), instanceID, req.ReannounceSettings)
		if err != nil {
			RespondError(w, http.StatusInternalServerError, "Failed to save reannounce settings")
			return
		}
	} else {
		settings = h.loadReannounceSettings(r.Context(), instanceID)
	}

	response := h.buildQuickInstanceResponse(instance)
	response.ReannounceSettings = payloadFromModel(settings)

	h.connectionTests.Go(func() { h.testConnectionAsync(instance.ID) })

	RespondJSON(w, http.StatusOK, response)
}

func (req *UpdateInstanceRequest) restoreRedactedSecrets(existing *models.Instance) {
	if req.Password != "" && domain.IsRedactedString(req.Password) {
		req.Password = existing.PasswordEncrypted
	}

	if req.BasicPassword != nil && *req.BasicPassword != "" && domain.IsRedactedString(*req.BasicPassword) {
		req.BasicPassword = existing.BasicPasswordEncrypted
	}

	if req.APIKey != nil && domain.IsRedactedString(*req.APIKey) {
		req.APIKey = nil
	}
}

func (req *UpdateInstanceRequest) applyLinkSettings(existing *models.Instance) models.Instance {
	effective := *existing
	if req.HasLocalFilesystemAccess != nil {
		effective.HasLocalFilesystemAccess = *req.HasLocalFilesystemAccess
	}
	if req.UseHardlinks != nil {
		effective.UseHardlinks = *req.UseHardlinks
	}
	if req.UseReflinks != nil {
		effective.UseReflinks = *req.UseReflinks
	}
	if req.HardlinkBaseDir != nil {
		effective.HardlinkBaseDir = *req.HardlinkBaseDir
	}
	return effective
}

func linkModeError(effective *models.Instance) string {
	canWrite := models.FilesystemCapabilitiesOf(effective).Write
	canReflink := canWrite && models.FilesystemAccessMode(effective) == models.FilesystemModeLocal

	if effective.UseHardlinks {
		if !canWrite {
			return "Cannot enable hardlink mode without filesystem write access. Enable Local Filesystem Access, or set up SSH access, in instance settings first."
		}
		if effective.HardlinkBaseDir == "" {
			return "Cannot enable hardlink mode without a base directory"
		}
	}

	if effective.UseReflinks {
		if !canReflink {
			return "Cannot enable reflink mode without local filesystem access"
		}
		if effective.HardlinkBaseDir == "" {
			return "Cannot enable reflink mode without a base directory"
		}
	}
	return ""
}

func (h *InstancesHandler) DeleteInstance(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	if err := h.instanceStore.Delete(r.Context(), instanceID); err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			RespondError(w, http.StatusNotFound, "Instance not found")
			return
		}
		log.Error().Err(err).Msg("Failed to delete instance")
		RespondError(w, http.StatusInternalServerError, "Failed to delete instance")
		return
	}

	h.clientPool.RemoveClient(instanceID)
	h.sshPool.Remove(instanceID)

	response := DeleteInstanceResponse{
		Message: "Instance deleted successfully",
	}
	RespondJSON(w, http.StatusOK, response)
}

func (h *InstancesHandler) UpdateInstanceStatus(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req UpdateInstanceStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	instance, err := h.instanceStore.SetActiveState(r.Context(), instanceID, req.IsActive)
	if err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			RespondError(w, http.StatusNotFound, "Instance not found")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to update instance status")
		RespondError(w, http.StatusInternalServerError, "Failed to update instance status")
		return
	}

	if !req.IsActive {
		h.clientPool.RemoveClient(instanceID)
	} else {
		h.clientPool.ResetFailureTracking(instanceID)
		h.connectionTests.Go(func() { h.testConnectionAsync(instanceID) })
	}

	response := h.buildQuickInstanceResponse(instance)
	response.ReannounceSettings = h.getReannounceSettingsPayload(r.Context(), instanceID)
	RespondJSON(w, http.StatusOK, response)
}

func (h *InstancesHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	client, err := h.clientPool.GetClient(r.Context(), instanceID)
	if err != nil {
		if errors.Is(err, internalqbittorrent.ErrInstanceDisabled) {
			response := TestConnectionResponse{
				Connected: false,
				Message:   "Instance is disabled",
				Error:     err.Error(),
			}
			RespondJSON(w, http.StatusOK, response)
			return
		}
		response := TestConnectionResponse{
			Connected: false,
			Error:     err.Error(),
		}
		RespondJSON(w, http.StatusOK, response)
		return
	}

	if err := client.HealthCheck(r.Context()); err != nil {
		response := TestConnectionResponse{
			Connected: false,
			Error:     err.Error(),
		}
		RespondJSON(w, http.StatusOK, response)
		return
	}

	response := TestConnectionResponse{
		Connected: true,
		Message:   "Connection successful",
	}
	RespondJSON(w, http.StatusOK, response)
}
